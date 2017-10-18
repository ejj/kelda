package amazon

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/cloud/amazon/client"
	"github.com/kelda/kelda/cloud/cfg"
	"github.com/kelda/kelda/cloud/wait"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/defaults"
	"github.com/aws/aws-sdk-go/service/ec2"

	log "github.com/sirupsen/logrus"
)

// The Provider wraps a client to Amazon EC2.
type Provider struct {
	client.Client

	namespace string
	region    string
}

type awsMachine struct {
	instanceID string
	spotID     string

	machine db.Machine
}

const (
	// DefaultRegion is the preferred location for machines that don't have a
	// user specified region preference.
	DefaultRegion = "us-west-1"

	spotPrice = "0.5"
)

// Regions is the list of supported AWS regions.
var Regions = []string{"ap-southeast-2", "us-west-1", "us-west-2"}

// Ubuntu 16.04, 64-bit hvm:ebs-ssd
var amis = map[string]string{
	"ap-southeast-2": "ami-943d3bf7",
	"us-west-1":      "ami-79df8219",
	"us-west-2":      "ami-d206bdb2",
}

var sleep = time.Sleep

var timeout = 5 * time.Minute

// New creates a new Amazon EC2 cluster.
func New(namespace, region string) (*Provider, error) {
	prvdr := newAmazon(namespace, region)
	if _, err := prvdr.List(); err != nil {
		// Attempt to add information about the AWS access key to the error
		// message.
		awsConfig := defaults.Config().WithCredentialsChainVerboseErrors(true)
		handlers := defaults.Handlers()
		awsCreds := defaults.CredChain(awsConfig, handlers)
		credValue, credErr := awsCreds.Get()
		if credErr == nil {
			return nil, fmt.Errorf(
				"AWS failed to connect (using access key ID: %s): %s",
				credValue.AccessKeyID, err.Error())
		}
		// AWS probably failed to connect because no access credentials
		// were found. AWS's error message is not very helpful, so try to
		// point the user in the right direction.
		return nil, fmt.Errorf("AWS failed to find access "+
			"credentials. At least one method for finding access "+
			"credentials must succeed, but they all failed: %s)",
			credErr.Error())
	}
	return prvdr, nil
}

// Creates a new provider, and connects its client to AWS
func newAmazon(namespace, region string) *Provider {
	prvdr := &Provider{
		namespace: strings.ToLower(namespace),
		region:    region,
		Client:    client.New(region),
	}

	return prvdr
}

type bootReq struct {
	groupID     string
	cfg         string
	size        string
	diskSize    int
	preemptible bool
}

// Boot creates instances in the `prvdr` configured according to the `bootSet`.
func (prvdr *Provider) Boot(bootSet []db.Machine) error {
	if len(bootSet) <= 0 {
		return nil
	}

	groupID, _, err := prvdr.getCreateSecurityGroup()
	if err != nil {
		return err
	}

	bootReqMap := make(map[bootReq]int64) // From boot request to an instance count.
	for _, m := range bootSet {
		br := bootReq{
			groupID:     groupID,
			cfg:         cfg.Ubuntu(m, ""),
			size:        m.Size,
			diskSize:    m.DiskSize,
			preemptible: m.Preemptible,
		}
		bootReqMap[br] = bootReqMap[br] + 1
	}

	for br, count := range bootReqMap {
		if br.preemptible {
			err = prvdr.bootSpot(br, count)
		} else {
			err = prvdr.bootReserved(br, count)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func (prvdr *Provider) bootReserved(br bootReq, count int64) error {
	cloudConfig64 := base64.StdEncoding.EncodeToString([]byte(br.cfg))
	resp, err := prvdr.RunInstances(&ec2.RunInstancesInput{
		ImageId:          aws.String(amis[prvdr.region]),
		InstanceType:     aws.String(br.size),
		UserData:         &cloudConfig64,
		SecurityGroupIds: []*string{aws.String(br.groupID)},
		BlockDeviceMappings: []*ec2.BlockDeviceMapping{
			blockDevice(br.diskSize)},
		MaxCount: &count,
		MinCount: &count,
	})
	if err != nil {
		return err
	}

	var ids []string
	for _, inst := range resp.Instances {
		ids = append(ids, *inst.InstanceId)
	}

	err = prvdr.wait(ids, true)
	if err != nil {
		if stopErr := prvdr.stopInstances(ids); stopErr != nil {
			log.WithError(stopErr).WithField("ids", ids).
				Error("Failed to cleanup failed boots")
		}
	}

	return err
}

func (prvdr *Provider) bootSpot(br bootReq, count int64) error {
	cloudConfig64 := base64.StdEncoding.EncodeToString([]byte(br.cfg))
	spots, err := prvdr.RequestSpotInstances(spotPrice, count,
		&ec2.RequestSpotLaunchSpecification{
			ImageId:          aws.String(amis[prvdr.region]),
			InstanceType:     aws.String(br.size),
			UserData:         &cloudConfig64,
			SecurityGroupIds: []*string{aws.String(br.groupID)},
			BlockDeviceMappings: []*ec2.BlockDeviceMapping{
				blockDevice(br.diskSize)}})
	if err != nil {
		return err
	}

	var ids []string
	for _, request := range spots {
		ids = append(ids, *request.SpotInstanceRequestId)
	}

	err = prvdr.wait(ids, true)
	if err != nil {
		if stopErr := prvdr.stopSpots(ids); stopErr != nil {
			log.WithError(stopErr).WithField("ids", ids).
				Error("Failed to cleanup failed boots")
		}
	}
	return err
}

// Stop shuts down `machines` in `prvdr`.
func (prvdr *Provider) Stop(machines []db.Machine) error {
	var spotIDs, instIDs []string
	for _, m := range machines {
		if m.Preemptible {
			spotIDs = append(spotIDs, m.CloudID)
		} else {
			instIDs = append(instIDs, m.CloudID)
		}
	}

	var spotErr, instErr error
	if len(spotIDs) != 0 {
		spotErr = prvdr.stopSpots(spotIDs)
	}

	if len(instIDs) > 0 {
		instErr = prvdr.stopInstances(instIDs)
	}

	switch {
	case spotErr == nil:
		return instErr
	case instErr == nil:
		return spotErr
	default:
		return fmt.Errorf("reserved: %v, and spot: %v", instErr, spotErr)
	}
}

func (prvdr *Provider) stopSpots(ids []string) error {
	spots, err := prvdr.DescribeSpotInstanceRequests(ids, nil)
	if err != nil {
		return err
	}

	var instIDs []string
	for _, spot := range spots {
		if spot.InstanceId != nil {
			instIDs = append(instIDs, *spot.InstanceId)
		}
	}

	var stopInstsErr, cancelSpotsErr error
	if len(instIDs) != 0 {
		stopInstsErr = prvdr.stopInstances(instIDs)
	}

	cancelSpotsErr = prvdr.CancelSpotInstanceRequests(ids)
	switch {
	case stopInstsErr == nil && cancelSpotsErr == nil:
		return prvdr.wait(ids, false)
	case stopInstsErr == nil:
		return cancelSpotsErr
	case cancelSpotsErr == nil:
		return stopInstsErr
	default:
		return fmt.Errorf("stop: %v, cancel: %v", stopInstsErr, cancelSpotsErr)
	}
}

func (prvdr *Provider) stopInstances(ids []string) error {
	err := prvdr.TerminateInstances(ids)
	if err != nil {
		return err
	}
	return prvdr.wait(ids, false)
}

var trackedSpotStates = aws.StringSlice(
	[]string{ec2.SpotInstanceStateActive, ec2.SpotInstanceStateOpen})

func (prvdr *Provider) listSpots() (machines []awsMachine, err error) {
	spots, err := prvdr.DescribeSpotInstanceRequests(nil, []*ec2.Filter{{
		Name:   aws.String("state"),
		Values: trackedSpotStates,
	}, {
		Name:   aws.String("launch.group-name"),
		Values: []*string{aws.String(prvdr.namespace)}}})
	if err != nil {
		return nil, err
	}

	for _, spot := range spots {
		machines = append(machines, awsMachine{
			spotID: resolveString(spot.SpotInstanceRequestId),
		})
	}
	return machines, nil
}

func (prvdr *Provider) parseDiskSize(inst ec2.Instance) (int, error) {
	if len(inst.BlockDeviceMappings) == 0 {
		return 0, nil
	}

	volumeID := *inst.BlockDeviceMappings[0].Ebs.VolumeId
	volumes, err := prvdr.DescribeVolumes(volumeID)
	if err != nil || len(volumes) == 0 {
		return 0, err
	}
	return int(*volumes[0].Size), nil
}

// `listInstances` fetches and parses all machines in the namespace into a list
// of `awsMachine`s
func (prvdr *Provider) listInstances() (instances []awsMachine, err error) {
	insts, err := prvdr.DescribeInstances([]*ec2.Filter{{
		Name:   aws.String("instance.group-name"),
		Values: []*string{aws.String(prvdr.namespace)},
	}, {
		Name:   aws.String("instance-state-name"),
		Values: []*string{aws.String(ec2.InstanceStateNameRunning)}}})
	if err != nil {
		return nil, err
	}

	addrs, err := prvdr.DescribeAddresses()
	if err != nil {
		return nil, err
	}
	ipMap := map[string]*ec2.Address{}
	for _, ip := range addrs {
		if ip.InstanceId != nil {
			ipMap[*ip.InstanceId] = ip
		}
	}

	for _, res := range insts.Reservations {
		for _, inst := range res.Instances {
			diskSize, err := prvdr.parseDiskSize(*inst)
			if err != nil {
				log.WithError(err).
					Warn("Error retrieving Amazon machine " +
						"disk information.")
			}

			var floatingIP string
			if ip := ipMap[*inst.InstanceId]; ip != nil {
				floatingIP = *ip.PublicIp
			}

			instances = append(instances, awsMachine{
				instanceID: resolveString(inst.InstanceId),
				spotID: resolveString(
					inst.SpotInstanceRequestId),
				machine: db.Machine{
					PublicIP:   resolveString(inst.PublicIpAddress),
					PrivateIP:  resolveString(inst.PrivateIpAddress),
					FloatingIP: floatingIP,
					Size:       resolveString(inst.InstanceType),
					DiskSize:   diskSize,
				},
			})
		}
	}
	return instances, nil
}

// List queries `prvdr` for the list of booted machines.
func (prvdr *Provider) List() (machines []db.Machine, err error) {
	allSpots, err := prvdr.listSpots()
	if err != nil {
		return nil, err
	}
	ourInsts, err := prvdr.listInstances()
	if err != nil {
		return nil, err
	}

	spotIDKey := func(intf interface{}) interface{} {
		return intf.(awsMachine).spotID
	}
	bootedSpots, nonbootedSpots, reservedInstances :=
		join.HashJoin(awsMachineSlice(allSpots), awsMachineSlice(ourInsts),
			spotIDKey, spotIDKey)

	var awsMachines []awsMachine
	for _, mIntf := range reservedInstances {
		awsMachines = append(awsMachines, mIntf.(awsMachine))
	}
	for _, pair := range bootedSpots {
		awsMachines = append(awsMachines, pair.R.(awsMachine))
	}
	for _, mIntf := range nonbootedSpots {
		awsMachines = append(awsMachines, mIntf.(awsMachine))
	}

	for _, awsm := range awsMachines {
		cm := awsm.machine
		cm.Provider = db.Amazon
		cm.Region = prvdr.region
		cm.Preemptible = awsm.spotID != ""
		cm.CloudID = awsm.spotID
		if !cm.Preemptible {
			cm.CloudID = awsm.instanceID
		}
		machines = append(machines, cm)
	}
	return machines, nil
}

// UpdateFloatingIPs updates Elastic IPs <> EC2 instance associations.
func (prvdr *Provider) UpdateFloatingIPs(machines []db.Machine) error {
	addrs, err := prvdr.DescribeAddresses()
	if err != nil {
		return err
	}

	// Map IP Address -> Elastic IP.
	addresses := map[string]string{}
	// Map EC2 Instance -> Elastic IP association.
	associations := map[string]string{}
	for _, addr := range addrs {
		if addr.AllocationId != nil {
			addresses[*addr.PublicIp] = *addr.AllocationId
		}

		if addr.InstanceId != nil && addr.AssociationId != nil {
			associations[*addr.InstanceId] = *addr.AssociationId
		}
	}

	for _, machine := range machines {
		id := machine.CloudID
		if machine.Preemptible {
			id, err = prvdr.getInstanceID(id)
			if err != nil {
				return err
			}
		}

		if machine.FloatingIP == "" {
			associationID, ok := associations[id]
			if !ok {
				continue
			}

			err := prvdr.DisassociateAddress(associationID)
			if err != nil {
				return err
			}
		} else {
			allocationID := addresses[machine.FloatingIP]
			err := prvdr.AssociateAddress(id, allocationID)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (prvdr Provider) getInstanceID(spotID string) (string, error) {
	spots, err := prvdr.DescribeSpotInstanceRequests([]string{spotID}, nil)
	if err != nil {
		return "", err
	}

	if len(spots) == 0 {
		return "", fmt.Errorf("no spot requests with ID %s", spotID)
	}

	return *spots[0].InstanceId, nil
}

/* Wait for the 'ids' to have booted or terminated depending on the value
 * of 'boot' */
func (prvdr *Provider) wait(ids []string, boot bool) error {
	return wait.Wait(func() bool {
		machines, err := prvdr.List()
		if err != nil {
			log.WithError(err).Warn("Failed to list machines in the cluster.")
			return false
		}

		exists := make(map[string]struct{})
		for _, inst := range machines {
			// When booting, if the machine isn't configured completely
			// when the List() call was made, the cluster will fail to join
			// and boot them twice. When halting, we don't consider this as
			// the opposite will happen and we'll try to halt multiple times.
			// To halt, we need the machines to be completely gone.
			if boot && inst.Size == "" {
				continue
			}

			exists[inst.CloudID] = struct{}{}
		}

		for _, id := range ids {
			if _, ok := exists[id]; ok != boot {
				return false
			}
		}

		return true
	})
}

// SetACLs adds and removes acls in `prvdr` so that it conforms to `acls`.
func (prvdr *Provider) SetACLs(acls []acl.ACL) error {
	groupID, ingress, err := prvdr.getCreateSecurityGroup()
	if err != nil {
		return err
	}

	rangesToAdd, foundGroup, rulesToRemove := syncACLs(acls, groupID, ingress)

	if len(rangesToAdd) != 0 {
		logACLs(true, rangesToAdd)
		err = prvdr.AuthorizeSecurityGroup(
			prvdr.namespace, "", rangesToAdd)
		if err != nil {
			return err
		}
	}

	if !foundGroup {
		log.WithField("Group", prvdr.namespace).Debug("Amazon: Add group")
		err = prvdr.AuthorizeSecurityGroup(
			prvdr.namespace, prvdr.namespace, nil)
		if err != nil {
			return err
		}
	}

	if len(rulesToRemove) != 0 {
		logACLs(false, rulesToRemove)
		err = prvdr.RevokeSecurityGroup(prvdr.namespace, rulesToRemove)
		if err != nil {
			return err
		}
	}

	return nil
}

func (prvdr *Provider) getCreateSecurityGroup() (
	string, []*ec2.IpPermission, error) {

	groups, err := prvdr.DescribeSecurityGroup(prvdr.namespace)
	if err != nil {
		return "", nil, err
	} else if len(groups) > 1 {
		err := errors.New("Multiple Security Groups with the same name: " +
			prvdr.namespace)
		return "", nil, err
	} else if len(groups) == 1 {
		return *groups[0].GroupId, groups[0].IpPermissions, nil
	}

	id, err := prvdr.CreateSecurityGroup(prvdr.namespace, "Kelda Group")
	return id, nil, err
}

// syncACLs returns the permissions that need to be removed and added in order
// for the cloud ACLs to match the policy.
// rangesToAdd is guaranteed to always have exactly one item in the IpRanges slice.
func syncACLs(desiredACLs []acl.ACL, desiredGroupID string,
	current []*ec2.IpPermission) (rangesToAdd []*ec2.IpPermission, foundGroup bool,
	toRemove []*ec2.IpPermission) {

	var currRangeRules []*ec2.IpPermission
	for _, perm := range current {
		for _, ipRange := range perm.IpRanges {
			currRangeRules = append(currRangeRules, &ec2.IpPermission{
				IpProtocol: perm.IpProtocol,
				FromPort:   perm.FromPort,
				ToPort:     perm.ToPort,
				IpRanges: []*ec2.IpRange{
					ipRange,
				},
			})
		}
		for _, pair := range perm.UserIdGroupPairs {
			if *pair.GroupId != desiredGroupID {
				toRemove = append(toRemove, &ec2.IpPermission{
					UserIdGroupPairs: []*ec2.UserIdGroupPair{
						pair,
					},
				})
			} else {
				foundGroup = true
			}
		}
	}

	var desiredRangeRules []*ec2.IpPermission
	for _, acl := range desiredACLs {
		desiredRangeRules = append(desiredRangeRules, &ec2.IpPermission{
			FromPort: aws.Int64(int64(acl.MinPort)),
			ToPort:   aws.Int64(int64(acl.MaxPort)),
			IpRanges: []*ec2.IpRange{
				{
					CidrIp: aws.String(acl.CidrIP),
				},
			},
			IpProtocol: aws.String("tcp"),
		}, &ec2.IpPermission{
			FromPort: aws.Int64(int64(acl.MinPort)),
			ToPort:   aws.Int64(int64(acl.MaxPort)),
			IpRanges: []*ec2.IpRange{
				{
					CidrIp: aws.String(acl.CidrIP),
				},
			},
			IpProtocol: aws.String("udp"),
		}, &ec2.IpPermission{
			FromPort: aws.Int64(-1),
			ToPort:   aws.Int64(-1),
			IpRanges: []*ec2.IpRange{
				{
					CidrIp: aws.String(acl.CidrIP),
				},
			},
			IpProtocol: aws.String("icmp"),
		})
	}

	_, toAdd, rangesToRemove := join.HashJoin(ipPermSlice(desiredRangeRules),
		ipPermSlice(currRangeRules), permToACLKey, permToACLKey)
	for _, intf := range toAdd {
		rangesToAdd = append(rangesToAdd, intf.(*ec2.IpPermission))
	}
	for _, intf := range rangesToRemove {
		toRemove = append(toRemove, intf.(*ec2.IpPermission))
	}

	return rangesToAdd, foundGroup, toRemove
}

func logACLs(add bool, perms []*ec2.IpPermission) {
	action := "Remove"
	if add {
		action = "Add"
	}

	for _, perm := range perms {
		if len(perm.IpRanges) != 0 {
			// Each rule has three variants (TCP, UDP, and ICMP), but
			// we only want to log once.
			protocol := *perm.IpProtocol
			if protocol != "tcp" {
				continue
			}

			cidrIP := *perm.IpRanges[0].CidrIp
			ports := fmt.Sprintf("%d", *perm.FromPort)
			if *perm.FromPort != *perm.ToPort {
				ports += fmt.Sprintf("-%d", *perm.ToPort)
			}
			log.WithField("ACL",
				fmt.Sprintf("%s:%s", cidrIP, ports)).
				Debugf("Amazon: %s ACL", action)
		} else {
			log.WithField("Group",
				*perm.UserIdGroupPairs[0].GroupName).
				Debugf("Amazon: %s group", action)
		}
	}
}

// blockDevice returns the block device we use for our AWS machines.
func blockDevice(diskSize int) *ec2.BlockDeviceMapping {
	return &ec2.BlockDeviceMapping{
		DeviceName: aws.String("/dev/sda1"),
		Ebs: &ec2.EbsBlockDevice{
			DeleteOnTermination: aws.Bool(true),
			VolumeSize:          aws.Int64(int64(diskSize)),
			VolumeType:          aws.String("gp2"),
		},
	}
}

func resolveString(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

type awsMachineSlice []awsMachine

func (ams awsMachineSlice) Get(ii int) interface{} {
	return ams[ii]
}

func (ams awsMachineSlice) Len() int {
	return len(ams)
}

type ipPermissionKey struct {
	protocol string
	ipRange  string
	minPort  int
	maxPort  int
}

func permToACLKey(permIntf interface{}) interface{} {
	perm := permIntf.(*ec2.IpPermission)

	key := ipPermissionKey{}

	if perm.FromPort != nil {
		key.minPort = int(*perm.FromPort)
	}

	if perm.ToPort != nil {
		key.maxPort = int(*perm.ToPort)
	}

	if perm.IpProtocol != nil {
		key.protocol = *perm.IpProtocol
	}

	if perm.IpRanges[0].CidrIp != nil {
		key.ipRange = *perm.IpRanges[0].CidrIp
	}

	return key
}

type ipPermSlice []*ec2.IpPermission

func (slc ipPermSlice) Get(ii int) interface{} {
	return slc[ii]
}

func (slc ipPermSlice) Len() int {
	return len(slc)
}

func (slc ipPermSlice) Less(i, j int) bool {
	return strings.Compare(slc[i].String(), slc[j].String()) < 0
}

func (slc ipPermSlice) Swap(i, j int) {
	slc[i], slc[j] = slc[j], slc[i]
}
