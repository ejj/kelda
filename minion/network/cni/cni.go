package cni

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"runtime"

	"github.com/kelda/kelda/minion/ipdef"
	"github.com/kelda/kelda/minion/network/openflow"
	"github.com/kelda/kelda/minion/nl"
	"github.com/kelda/kelda/minion/ovsdb"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/vishvananda/netlink"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	mtu            int    = 1400
	containerIDTag string = "cni-container-id"
)

// execRun is a variable so that it can be mocked out by the unit tests.
var execRun = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

var addFlows = openflow.AddFlows

var listPortsByTag = func(key, value string) ([]string, error) {
	client, err := ovsdb.Open()
	if err != nil {
		return nil, fmt.Errorf("get OVSDB client: %s", client)
	}
	return client.ListPortsByTag(key, value)
}

// The CNI spec specifies that cmdDel shouldn't error if items are missing.
// This is because cmdDel is used to cleanup failed cmdAdds. If a cmdAdd
// failed before it could create the ports or veth, it is expected that they
// won't exist when this function is called.
func cmdDel(args *skel.CmdArgs) error {
	ports, err := listPortsByTag(containerIDTag, args.ContainerID)
	if err != nil {
		return fmt.Errorf("list OVS ports: %s", err)
	}

	if len(ports) > 0 {
		var cmd []string
		for _, port := range ports {
			cmd = append(cmd, "--", "del-port", port)
		}
		output, err := execRun("ovs-vsctl", cmd...)
		if err != nil {
			return fmt.Errorf("failed to teardown OVS ports: %s (%s)",
				err, output)
		}
	}

	return deleteLinkIfExists(args.ContainerID)
}

func deleteLinkIfExists(alias string) error {
	link, err := nl.N.LinkByAlias(alias)
	if err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return nil
		}
		return fmt.Errorf("failed to find outer veth: %s", err)
	}

	if err := nl.N.LinkDel(link); err != nil {
		return fmt.Errorf("failed to delete veth %s: %s", link.Attrs().Name, err)
	}
	return nil
}

func cmdAdd(args *skel.CmdArgs) error {
	result, err := cmdAddResult(args)
	if err != nil {
		return err
	}
	return result.Print()
}

func cmdAddResult(args *skel.CmdArgs) (current.Result, error) {
	podName, err := grepPodName(args.Args)
	if err != nil {
		return current.Result{}, err
	}

	ip, mac, err := getIPMac(podName)
	if err != nil {
		return current.Result{}, err
	}

	outerName := ipdef.IFName(ip.IP.String())
	tmpPodName := ipdef.IFName("-" + outerName)
	if err := nl.N.AddVeth(outerName, args.ContainerID, tmpPodName, mtu); err != nil {
		return current.Result{},
			fmt.Errorf("failed to create veth %s: %s", outerName, err)
	}

	if err := setupPod(args.Netns, args.IfName, tmpPodName, ip, mac); err != nil {
		return current.Result{}, err
	}

	if err := setupOuterLink(outerName); err != nil {
		return current.Result{}, err
	}

	if err := setupOVS(outerName, ip.IP, mac, args.ContainerID); err != nil {
		return current.Result{}, fmt.Errorf("failed to setup OVS: %s", err)
	}

	iface := current.Interface{Name: args.IfName, Mac: mac.String(),
		Sandbox: args.Netns}
	ipconfig := current.IPConfig{
		Version:   "4",
		Interface: current.Int(0),
		Address:   ip,
		Gateway:   net.IPv4(10, 0, 0, 1),
	}

	result := current.Result{
		CNIVersion: "0.3.1",
		Interfaces: []*current.Interface{&iface},
		IPs:        []*current.IPConfig{&ipconfig},
	}
	return result, nil
}

func grepPodName(args string) (string, error) {
	nameRegex := regexp.MustCompile("K8S_POD_NAME=([^;]+);")
	sm := nameRegex.FindStringSubmatch(args)
	if len(sm) < 2 {
		return "", errors.New("failed to find pod name in arguments")
	}
	return sm[1], nil
}

func setupOuterLink(name string) error {
	link, err := nl.N.LinkByName(name)
	if err != nil {
		return fmt.Errorf("failed to find link %s: %s", name, err)
	}

	if err := nl.N.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to bring link up: %s", err)
	}

	return nil
}

func setupPod(ns, goalName, vethName string, ip net.IPNet, mac net.HardwareAddr) error {
	// This function jumps into the pod namespace and thus can't risk being
	// scheduled onto an OS thread that hasn't made the jump.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	link, err := nl.N.LinkByName(vethName)
	if err != nil {
		return fmt.Errorf("failed to find link %s: %s", vethName, err)
	}

	rootns, err := nl.N.GetNetns()
	if err != nil {
		return fmt.Errorf("failed to get current namespace handle: %s", err)
	}

	nsh, err := nl.N.GetNetnsFromPath(ns)
	if err != nil {
		return fmt.Errorf("failed to open network namespace handle: %s", err)
	}
	defer nl.N.CloseNsHandle(nsh)

	if err := nl.N.LinkSetNs(link, nsh); err != nil {
		return fmt.Errorf("failed to put link in pod namespace: %s", err)
	}

	if err := nl.N.SetNetns(nsh); err != nil {
		return fmt.Errorf("failed to enter pod network namespace: %s", err)
	}
	defer nl.N.SetNetns(rootns)

	if err := nl.N.LinkSetHardwareAddr(link, mac); err != nil {
		return fmt.Errorf("failed to set mac address: %s", err)
	}

	if err := nl.N.AddrAdd(link, ip); err != nil {
		return fmt.Errorf("failed to set IP %s: %s", ip.String(), err)
	}

	if err := nl.N.LinkSetName(link, goalName); err != nil {
		return fmt.Errorf("failed to set device name: %s", err)
	}

	if err := nl.N.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to bring link up: %s", err)
	}

	podIndex := link.Attrs().Index
	err = nl.N.RouteAdd(nl.Route{
		Scope:     nl.ScopeLink,
		LinkIndex: podIndex,
		Dst:       &ipdef.KeldaSubnet,
		Src:       ip.IP,
	})
	if err != nil {
		return fmt.Errorf("failed to add route: %s", err)
	}

	err = nl.N.RouteAdd(nl.Route{LinkIndex: podIndex, Gw: ipdef.GatewayIP})
	if err != nil {
		return fmt.Errorf("failed to add default route: %s", err)
	}

	return nil
}

var getPodAnnotations = func(podName string) (map[string]string, error) {
	kubeconfig, err := clientcmd.BuildConfigFromFlags("",
		"/var/lib/kubelet/kubeconfig")
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %s", err)
	}

	clientset, err := kubernetes.NewForConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("get kube client: %s", err)
	}

	podsClient := clientset.CoreV1().Pods(corev1.NamespaceDefault)
	pod, err := podsClient.Get(podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get pod: %s", err)
	}

	return pod.Annotations, nil
}

func getIPMac(podName string) (net.IPNet, net.HardwareAddr, error) {
	annotations, err := getPodAnnotations(podName)
	if err != nil {
		return net.IPNet{}, nil, err
	}

	ipStr, ok := annotations["keldaIP"]
	if !ok {
		return net.IPNet{}, nil, errors.New("pod has no Kelda IP")
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return net.IPNet{}, nil, fmt.Errorf("invalid IP: %s", ipStr)
	}

	macStr := ipdef.IPToMac(ip)
	mac, err := net.ParseMAC(macStr)
	if err != nil {
		err := fmt.Errorf("failed to parse mac address %s: %s", macStr, err)
		return net.IPNet{}, nil, err
	}

	return net.IPNet{IP: ip, Mask: net.IPv4Mask(255, 255, 255, 255)}, mac, nil
}

func setupOVS(outerName string, ip net.IP, mac net.HardwareAddr,
	containerID string) error {
	portExternalID := fmt.Sprintf("external-ids:%s=%s", containerIDTag, containerID)
	peerBr, peerKelda := ipdef.PatchPorts(ip.String())
	output, err := execRun("ovs-vsctl",
		"--", "add-port", ipdef.KeldaBridge, outerName, portExternalID,

		"--", "add-port", ipdef.KeldaBridge, peerKelda, portExternalID,

		"--", "set", "Interface", peerKelda, "type=patch",
		"options:peer="+peerBr,

		"--", "add-port", ipdef.OvnBridge, peerBr, portExternalID,

		"--", "set", "Interface", peerBr, "type=patch",
		"options:peer="+peerKelda,
		"external-ids:attached-mac="+mac.String(),
		"external-ids:iface-id="+ip.String())
	if err != nil {
		return fmt.Errorf("failed to configure OVSDB: %s (%s)", err, output)
	}

	err = addFlows([]openflow.Container{{
		Veth:  outerName,
		Patch: peerKelda,
		Mac:   mac.String(),
		IP:    ip.String(),
	}})
	if err != nil {
		return fmt.Errorf("failed to populate OpenFlow tables: %s", err)
	}

	return nil
}

// Run executes the CNI plugin code, exiting when finished.
func Run() {
	skel.PluginMain(cmdAdd, cmdDel, version.PluginSupports(version.Current()))
}
