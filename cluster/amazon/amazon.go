package amazon

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/quilt/quilt/cluster/acl"
	"github.com/quilt/quilt/cluster/cloudcfg"
	"github.com/quilt/quilt/cluster/machine"
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/join"
	"github.com/quilt/quilt/util"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"

	log "github.com/Sirupsen/logrus"
)

// The Cluster object represents a connection to Amazon EC2.
type Cluster struct {
	namespace string
	region    string
	client    client

	newClient func(string) client
}

type awsID struct {
	spotID string
	region string
}

// DefaultRegion is the preferred location for machines which haven't a user specified
// region preference.
const DefaultRegion = "us-west-1"

// Regions is the list of supported AWS regions.
var Regions = []string{"ap-southeast-2", "us-west-1", "us-west-2"}

const spotPrice = "0.5"

// Ubuntu 16.04, 64-bit hvm:ebs-ssd
var amis = map[string]string{
	"ap-southeast-2": "ami-943d3bf7",
	"us-west-1":      "ami-79df8219",
	"us-west-2":      "ami-d206bdb2",
}

var sleep = time.Sleep

var timeout = 5 * time.Minute

// New creates a new Amazon EC2 cluster.
func New(namespace, region string) (*Cluster, error) {
	clst := newAmazon(namespace, region)
	if _, err := clst.List(); err != nil {
		return nil, errors.New("AWS failed to connect")
	}
	return clst, nil
}

// creates a new client, and connects its client to AWS
func newAmazon(namespace, region string) *Cluster {
	clst := &Cluster{
		namespace: strings.ToLower(namespace),
		region:    region,
		newClient: newClient,
	}

	return clst
}

// Boot creates instances in the `clst` configured according to the `bootSet`.
func (clst *Cluster) Boot(bootSet []machine.Machine) error {
	clst.connectClient()

	if len(bootSet) <= 0 {
		return nil
	}

	type bootReq struct {
		cfg      string
		size     string
		diskSize int
	}

	bootReqMap := make(map[bootReq]int64) // From boot request to an instance count.
	for _, m := range bootSet {
		br := bootReq{
			cfg:      cloudcfg.Ubuntu(m.SSHKeys, "xenial"),
			size:     m.Size,
			diskSize: m.DiskSize,
		}
		bootReqMap[br] = bootReqMap[br] + 1
	}

	var awsIDs []awsID
	for br, count := range bootReqMap {
		groupID, _, err := clst.getCreateSecurityGroup()
		if err != nil {
			return err
		}

		cloudConfig64 := base64.StdEncoding.EncodeToString([]byte(br.cfg))
		resp, err := clst.client.RequestSpotInstances(
			&ec2.RequestSpotInstancesInput{
				SpotPrice: aws.String(spotPrice),
				LaunchSpecification: &ec2.RequestSpotLaunchSpecification{
					ImageId:          aws.String(amis[clst.region]),
					InstanceType:     aws.String(br.size),
					UserData:         &cloudConfig64,
					SecurityGroupIds: []*string{aws.String(groupID)},
					BlockDeviceMappings: []*ec2.BlockDeviceMapping{
						blockDevice(br.diskSize),
					},
				},
				InstanceCount: &count,
			})

		if err != nil {
			return err
		}

		for _, request := range resp.SpotInstanceRequests {
			awsIDs = append(awsIDs, awsID{
				spotID: *request.SpotInstanceRequestId,
				region: clst.region})
		}
	}

	if err := clst.tagSpotRequests(awsIDs); err != nil {
		return err
	}

	return clst.wait(awsIDs, true)
}

// Stop shuts down `machines` in `clst.
func (clst *Cluster) Stop(machines []machine.Machine) error {
	clst.connectClient()

	var ids []awsID
	for _, m := range machines {
		ids = append(ids, awsID{
			region: m.Region,
			spotID: m.ID,
		})
	}

	spotIDs := getSpotIDs(ids)
	spots, err := clst.client.DescribeSpotInstanceRequests(
		&ec2.DescribeSpotInstanceRequestsInput{
			SpotInstanceRequestIds: aws.StringSlice(spotIDs),
		})
	if err != nil {
		return err
	}

	instIds := []string{}
	for _, spot := range spots.SpotInstanceRequests {
		if spot.InstanceId != nil {
			instIds = append(instIds, *spot.InstanceId)
		}
	}

	if len(instIds) > 0 {
		_, err = clst.client.TerminateInstances(&ec2.TerminateInstancesInput{
			InstanceIds: aws.StringSlice(instIds),
		})
		if err != nil {
			return err
		}
	}

	_, err = clst.client.CancelSpotInstanceRequests(
		&ec2.CancelSpotInstanceRequestsInput{
			SpotInstanceRequestIds: aws.StringSlice(spotIDs),
		})
	if err != nil {
		return err
	}

	if err := clst.wait(ids, false); err != nil {
		return err
	}

	return nil
}

// List queries `clst` for the list of booted machines.
func (clst *Cluster) List() ([]machine.Machine, error) {
	clst.connectClient()

	machines := []machine.Machine{}
	spots, err := clst.client.DescribeSpotInstanceRequests(nil)
	if err != nil {
		return nil, err
	}

	insts, err := clst.client.DescribeInstances(&ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("instance.group-name"),
				Values: []*string{aws.String(clst.namespace)},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	instMap := make(map[string]*ec2.Instance)
	for _, res := range insts.Reservations {
		for _, inst := range res.Instances {
			instMap[*inst.InstanceId] = inst
		}
	}

	addrResp, err := clst.client.DescribeAddresses(nil)
	if err != nil {
		return nil, err
	}
	ipMap := map[string]*ec2.Address{}
	for _, ip := range addrResp.Addresses {
		if ip.InstanceId != nil {
			ipMap[*ip.InstanceId] = ip
		}
	}

	for _, spot := range spots.SpotInstanceRequests {
		if *spot.State != ec2.SpotInstanceStateActive &&
			*spot.State != ec2.SpotInstanceStateOpen {
			continue
		}

		var inst *ec2.Instance
		if spot.InstanceId != nil {
			inst = instMap[*spot.InstanceId]
		}

		// Due to a race condition in the AWS API, it's possible that
		// spot requests might lose their Tags. If handled naively,
		// those spot requests would technically be without a namespace,
		// meaning the instances they create would be live forever as
		// zombies.
		//
		// To mitigate this issue, we rely not only on the spot request
		// tags, but additionally on the instance security group. If a
		// spot request has a running instance in the appropriate
		// security group, it is by definition in our namespace.
		// Thus, we only check the tags for spot requests without
		// running instances.
		if inst == nil {
			var isOurs bool
			for _, tag := range spot.Tags {
				ns := clst.namespace
				if tag != nil && tag.Key != nil &&
					*tag.Key == ns {
					isOurs = true
					break
				}
			}

			if !isOurs {
				continue
			}
		}

		machine := machine.Machine{
			ID:       *spot.SpotInstanceRequestId,
			Region:   clst.region,
			Provider: db.Amazon,
		}

		if inst != nil {
			if *inst.State.Name != ec2.InstanceStateNamePending &&
				*inst.State.Name != ec2.InstanceStateNameRunning {
				continue
			}

			if inst.PublicIpAddress != nil {
				machine.PublicIP = *inst.PublicIpAddress
			}

			if inst.PrivateIpAddress != nil {
				machine.PrivateIP = *inst.PrivateIpAddress
			}

			if inst.InstanceType != nil {
				machine.Size = *inst.InstanceType
			}

			if len(inst.BlockDeviceMappings) != 0 {
				volumeID := inst.BlockDeviceMappings[0].
					Ebs.VolumeId
				filters := []*ec2.Filter{
					{
						Name: aws.String("volume-id"),
						Values: []*string{
							aws.String(*volumeID),
						},
					},
				}

				volumeInfo, err := clst.client.DescribeVolumes(
					&ec2.DescribeVolumesInput{
						Filters: filters,
					})
				if err != nil {
					return nil, err
				}
				if len(volumeInfo.Volumes) == 1 {
					machine.DiskSize = int(
						*volumeInfo.Volumes[0].Size)
				}
			}

			if ip := ipMap[*inst.InstanceId]; ip != nil {
				machine.FloatingIP = *ip.PublicIp
			}
		}

		machines = append(machines, machine)
	}

	return machines, nil
}

// UpdateFloatingIPs updates Elastic IPs <> EC2 instance associations.
func (clst *Cluster) UpdateFloatingIPs(machines []machine.Machine) error {
	clst.connectClient()
	addressDesc, err := clst.client.DescribeAddresses(nil)
	if err != nil {
		return err
	}

	// Map IP Address -> Elastic IP.
	addresses := map[string]*string{}
	// Map EC2 Instance -> Elastic IP association.
	associations := map[string]*string{}
	for _, addr := range addressDesc.Addresses {
		addresses[*addr.PublicIp] = addr.AllocationId
		if addr.InstanceId != nil {
			associations[*addr.InstanceId] = addr.AssociationId
		}
	}

	// Map spot request ID to EC2 instance ID.
	var spotIDs []string
	for _, machine := range machines {
		spotIDs = append(spotIDs, machine.ID)
	}
	instances, err := clst.getInstances(clst.region, spotIDs)
	if err != nil {
		return err
	}

	for _, machine := range machines {
		if machine.FloatingIP == "" {
			instanceID := *instances[machine.ID].InstanceId
			associationID := associations[instanceID]
			if associationID == nil {
				continue
			}

			input := ec2.DisassociateAddressInput{
				AssociationId: associationID,
			}
			_, err = clst.client.DisassociateAddress(&input)
			if err != nil {
				return err
			}
		} else {
			allocationID := addresses[machine.FloatingIP]
			input := ec2.AssociateAddressInput{
				InstanceId:   instances[machine.ID].InstanceId,
				AllocationId: allocationID,
			}
			if _, err := clst.client.AssociateAddress(&input); err != nil {
				return err
			}
		}
	}

	return nil
}

func (clst *Cluster) connectClient() {
	if clst.client == nil {
		clst.client = clst.newClient(clst.region)
	}
}

func (clst Cluster) getInstances(region string, spotIDs []string) (
	map[string]*ec2.Instance, error) {

	clst.connectClient()
	instances := map[string]*ec2.Instance{}

	spotQuery := ec2.DescribeSpotInstanceRequestsInput{
		SpotInstanceRequestIds: aws.StringSlice(spotIDs),
	}
	spotResp, err := clst.client.DescribeSpotInstanceRequests(&spotQuery)
	if err != nil {
		return nil, err
	}

	var instanceIDs []string
	for _, spot := range spotResp.SpotInstanceRequests {
		if spot.InstanceId == nil {
			instances[*spot.SpotInstanceRequestId] = nil
		} else {
			instanceIDs = append(instanceIDs, *spot.InstanceId)
		}
	}

	instQuery := ec2.DescribeInstancesInput{
		InstanceIds: aws.StringSlice(instanceIDs),
	}
	instResp, err := clst.client.DescribeInstances(&instQuery)
	if err != nil {
		return nil, err
	}

	for _, reservation := range instResp.Reservations {
		for _, instance := range reservation.Instances {
			instances[*instance.SpotInstanceRequestId] = instance
		}
	}

	return instances, nil
}

func (clst *Cluster) tagSpotRequests(awsIDs []awsID) error {
	var err error
	spotIDs := getSpotIDs(awsIDs)
	for i := 0; i < 30; i++ {
		_, err = clst.client.CreateTags(&ec2.CreateTagsInput{
			Tags: []*ec2.Tag{
				{
					Key:   aws.String(clst.namespace),
					Value: aws.String(""),
				},
			},
			Resources: aws.StringSlice(spotIDs),
		})
		if err == nil {
			return nil
		}
		time.Sleep(5 * time.Second)
	}

	log.Warn("Failed to tag spot requests: ", err)
	clst.client.CancelSpotInstanceRequests(
		&ec2.CancelSpotInstanceRequestsInput{
			SpotInstanceRequestIds: aws.StringSlice(spotIDs),
		})

	return err
}

/* Wait for the spot request 'ids' to have booted or terminated depending on the value
 * of 'boot' */
func (clst *Cluster) wait(awsIDs []awsID, boot bool) error {
	return util.WaitFor(func() bool {
		machines, err := clst.List()
		if err != nil {
			log.WithError(err).Warn("Failed to get machines.")
			return true
		}

		exists := make(map[awsID]struct{})
		for _, inst := range machines {
			// When booting, if the machine isn't configured completely
			// when the List() call was made, the cluster will fail to join
			// and boot them twice. When halting, we don't consider this as
			// the opposite will happen and we'll try to halt multiple times.
			// To halt, we need the machines to be completely gone.
			if boot && inst.Size == "" {
				continue
			}

			id := awsID{
				spotID: inst.ID,
				region: inst.Region,
			}
			exists[id] = struct{}{}
		}

		for _, id := range awsIDs {
			if _, ok := exists[id]; ok != boot {
				return false
			}
		}

		return true
	}, 10*time.Second, timeout)
}

func (clst *Cluster) isDoneWaiting(awsIDs []awsID, boot bool) (bool, error) {
	machines, err := clst.List()
	if err != nil {
		log.WithError(err).Warn("Failed to get machines.")
		return true, err
	}

	exists := make(map[awsID]struct{})
	for _, inst := range machines {
		// If the machine wasn't configured completely when the List()
		// call was made, the cluster will fail to join and boot them
		// twice.
		if inst.Size == "" {
			continue
		}

		id := awsID{
			spotID: inst.ID,
			region: inst.Region,
		}
		exists[id] = struct{}{}
	}

	for _, id := range awsIDs {
		if _, ok := exists[id]; ok != boot {
			return false, nil
		}
	}

	return true, nil
}

// SetACLs adds and removes acls in `clst` so that it conforms to `acls`.
func (clst *Cluster) SetACLs(acls []acl.ACL) error {
	clst.connectClient()
	groupID, ingress, err := clst.getCreateSecurityGroup()
	if err != nil {
		return err
	}

	rangesToAdd, foundGroup, rulesToRemove := syncACLs(acls, groupID, ingress)

	if len(rangesToAdd) != 0 {
		logACLs(true, rangesToAdd)
		_, err = clst.client.AuthorizeSecurityGroupIngress(
			&ec2.AuthorizeSecurityGroupIngressInput{
				GroupName:     aws.String(clst.namespace),
				IpPermissions: rangesToAdd,
			},
		)
		if err != nil {
			return err
		}
	}

	if !foundGroup {
		log.WithField("Group", clst.namespace).Debug("Amazon: Add group")
		_, err = clst.client.AuthorizeSecurityGroupIngress(
			&ec2.AuthorizeSecurityGroupIngressInput{
				GroupName: aws.String(
					clst.namespace),
				SourceSecurityGroupName: aws.String(
					clst.namespace),
			},
		)
		if err != nil {
			return err
		}
	}

	if len(rulesToRemove) != 0 {
		logACLs(false, rulesToRemove)
		_, err = clst.client.RevokeSecurityGroupIngress(
			&ec2.RevokeSecurityGroupIngressInput{
				GroupName:     aws.String(clst.namespace),
				IpPermissions: rulesToRemove,
			},
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func (clst *Cluster) getCreateSecurityGroup() (
	string, []*ec2.IpPermission, error) {

	resp, err := clst.client.DescribeSecurityGroups(
		&ec2.DescribeSecurityGroupsInput{
			Filters: []*ec2.Filter{
				{
					Name: aws.String("group-name"),
					Values: []*string{
						aws.String(clst.namespace),
					},
				},
			},
		})

	if err != nil {
		return "", nil, err
	}

	groups := resp.SecurityGroups
	if len(groups) > 1 {
		err := errors.New("Multiple Security Groups with the same name: " +
			clst.namespace)
		return "", nil, err
	}

	if len(groups) == 1 {
		return *groups[0].GroupId, groups[0].IpPermissions, nil
	}

	csgResp, err := clst.client.CreateSecurityGroup(
		&ec2.CreateSecurityGroupInput{
			Description: aws.String("Quilt Group"),
			GroupName:   aws.String(clst.namespace),
		})
	if err != nil {
		return "", nil, err
	}

	return *csgResp.GroupId, nil, nil
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

func getSpotIDs(ids []awsID) []string {
	var spotIDs []string
	for _, id := range ids {
		spotIDs = append(spotIDs, id.spotID)
	}

	return spotIDs
}

func groupByRegion(ids []awsID) map[string][]awsID {
	grouped := make(map[string][]awsID)
	for _, id := range ids {
		region := id.region
		if _, ok := grouped[region]; !ok {
			grouped[region] = []awsID{}
		}
		grouped[region] = append(grouped[region], id)
	}

	return grouped
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
