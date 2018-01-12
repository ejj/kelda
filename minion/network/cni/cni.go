package cni

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"runtime"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/kelda/kelda/minion/ipdef"
	"github.com/kelda/kelda/minion/nl"
	"github.com/vishvananda/netns"
)

type ipSchema struct {
	Metadata struct {
		Labels struct {
			KeldaIP string
		}
	}
}

const mtu int = 1400

func cmdDel(args *skel.CmdArgs) error {
	podName, err := grepPodName(args.Args)
	if err != nil {
		return err
	}

	outerName := ipdef.IFName(podName)
	peerBr, peerKelda := ipdef.PatchPorts(podName)
	err = exec.Command("ovs-vsctl",
		"--", "del-port", ipdef.KeldaBridge, outerName,
		"--", "del-port", ipdef.KeldaBridge, peerKelda,
		"--", "del-port", ipdef.OvnBridge, peerBr).Run()
	if err != nil {
		return fmt.Errorf("failed to teardown OVS ports: %s", err)
	}

	link, err := nl.N.LinkByName(outerName)
	if err != nil {
		return fmt.Errorf("failed to find outer veth: %s", err)
	}

	if err := nl.N.LinkDel(link); err != nil {
		return fmt.Errorf("failed to find delete veth %s: %s",
			link.Attrs().Name, err)
	}

	return nil
}

func cmdAdd(args *skel.CmdArgs) error {
	podName, err := grepPodName(args.Args)
	if err != nil {
		return err
	}

	ip, mac, err := getIPMac(podName)
	if err != nil {
		return err
	}

	outerName := ipdef.IFName(podName)
	tmpPodName := ipdef.IFName("tmp_" + outerName)
	if err := nl.N.AddVeth(outerName, tmpPodName, mtu); err != nil {
		return fmt.Errorf("failed to create veth %s: %s", outerName, err)
	}

	if err := setupPod(args, tmpPodName, ip, mac); err != nil {
		return err
	}

	if err := setupOuterLink(outerName); err != nil {
		return err
	}

	if err := setupOVS(outerName, podName, ip, mac); err != nil {
		return fmt.Errorf("failed to setup OVS: %s", err)
	}

	// TODO pre-populate OpenFlow tables.

	iface := current.Interface{Name: "eth0", Mac: mac.String(), Sandbox: args.Netns}
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
	return result.Print()
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

func setupPod(args *skel.CmdArgs, name string, ip net.IPNet,
	mac net.HardwareAddr) error {
	// This function jumps into the pod namespace and thus can't risk being
	// scheduled onto an OS thread that hasn't made the jump.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	link, err := nl.N.LinkByName(name)
	if err != nil {
		return fmt.Errorf("failed to find link %s: %s", name, err)
	}

	rootns, err := netns.Get()
	if err != nil {
		return fmt.Errorf("failed to get current namespace handle: %s", err)
	}

	nsh, err := netns.GetFromPath(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open network namespace handle: %s", err)
	} else {
		defer nsh.Close()
	}

	if err := nl.N.LinkSetNs(link, nsh); err != nil {
		return fmt.Errorf("failed to put link in pod namespace: %s", err)
	}

	if err := netns.Set(nsh); err != nil {
		return err
	} else {
		defer netns.Set(rootns)
	}

	if err := nl.N.LinkSetHardwareAddr(link, mac); err != nil {
		return fmt.Errorf("failed to set mac address: %s", err)
	}

	if err := nl.N.AddrAdd(link, ip); err != nil {
		return fmt.Errorf("failed ot set link IP %s: %s", link, err)
	}

	if err := nl.N.LinkSetName(link, args.IfName); err != nil {
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

func getIPMac(podName string) (net.IPNet, net.HardwareAddr, error) {
	out, err := exec.Command("kubectl", "get", "pod", podName, "-o=json").Output()
	if err != nil {
		return net.IPNet{}, nil, fmt.Errorf("kubectl get: %s", err)
	}

	schema := ipSchema{}
	if err := json.Unmarshal(out, &schema); err != nil {
		return net.IPNet{}, nil, fmt.Errorf("failed to parse IP from labels")
	}

	ipStr := schema.Metadata.Labels.KeldaIP
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

	return net.IPNet{ip, net.IPv4Mask(255, 255, 255, 255)}, mac, nil
}

func setupOVS(outerName, podName string, ip net.IPNet, mac net.HardwareAddr) error {
	peerBr, peerKelda := ipdef.PatchPorts(podName)
	return exec.Command("ovs-vsctl",
		"--", "add-port", ipdef.KeldaBridge, outerName,

		"--", "add-port", ipdef.KeldaBridge, peerKelda,

		"--", "set", "Interface", peerKelda, "type=patch",
		"options:peer="+peerBr,

		"--", "add-port", ipdef.OvnBridge, peerBr,

		"--", "set", "Interface", peerBr, "type=patch",
		"options:peer="+peerKelda,
		"external-ids:attached-mac="+mac.String(),
		"external-ids:iface-id="+ip.IP.String()).Run()
}

func Main() {
	skel.PluginMain(cmdAdd, cmdDel, version.PluginSupports(version.Current()))
}
