package amazon

import (
	"encoding/base64"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/cloud/amazon/client/mocks"
	"github.com/kelda/kelda/cloud/cfg"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/util"
)

const testNamespace = "namespace"

func TestList(t *testing.T) {
	t.Parallel()

	mc := new(mocks.Client)
	instances := []*ec2.Instance{
		// A booted spot instance.
		{
			InstanceId:            aws.String("inst1"),
			SpotInstanceRequestId: aws.String("spot1"),
			PublicIpAddress:       aws.String("publicIP"),
			PrivateIpAddress:      aws.String("privateIP"),
			InstanceType:          aws.String("size"),
			State: &ec2.InstanceState{
				Name: aws.String(ec2.InstanceStateNameRunning),
			},
		},
		// A booted spot instance.
		{
			InstanceId:            aws.String("inst2"),
			SpotInstanceRequestId: aws.String("spot2"),
			InstanceType:          aws.String("size2"),
			State: &ec2.InstanceState{
				Name: aws.String(ec2.InstanceStateNameRunning),
			},
		},
		// A reserved instance.
		{
			InstanceId:   aws.String("inst3"),
			InstanceType: aws.String("size2"),
			State: &ec2.InstanceState{
				Name: aws.String(ec2.InstanceStateNameRunning),
			},
			BlockDeviceMappings: []*ec2.InstanceBlockDeviceMapping{
				{
					Ebs: &ec2.EbsInstanceBlockDevice{
						VolumeId: aws.String("volume-id"),
					},
				},
			},
		},
	}
	mc.On("DescribeInstances", mock.Anything).Return(
		&ec2.DescribeInstancesOutput{
			Reservations: []*ec2.Reservation{
				{
					Instances: instances,
				},
			},
		}, nil,
	)
	mc.On("DescribeVolumes", mock.Anything).Return([]*ec2.Volume{{
		Size: aws.Int64(32)}}, nil)
	mc.On("DescribeSpotInstanceRequests", mock.Anything, mock.Anything).Return(
		[]*ec2.SpotInstanceRequest{
			// A spot request and a corresponding instance.
			{
				SpotInstanceRequestId: aws.String("spot1"),
				State: aws.String(
					ec2.SpotInstanceStateActive),
				InstanceId: aws.String("inst1"),
			}, {
				SpotInstanceRequestId: aws.String("spot2"),
				State: aws.String(
					ec2.SpotInstanceStateActive),
				InstanceId: aws.String("inst2"),
			},
			// A spot request that hasn't been booted yet.
			{
				SpotInstanceRequestId: aws.String("spot3"),
				State: aws.String(ec2.SpotInstanceStateOpen)}}, nil)

	mc.On("DescribeAddresses").Return([]*ec2.Address{{
		InstanceId: aws.String("inst2"),
		PublicIp:   aws.String("xx.xxx.xxx.xxx"),
	}, {
		InstanceId: aws.String("inst3"),
		PublicIp:   aws.String("8.8.8.8")}}, nil)

	amazonProvider := newAmazon(testNamespace, DefaultRegion)
	amazonProvider.Client = mc

	machines, err := amazonProvider.List()

	assert.Nil(t, err)
	assert.Equal(t, []db.Machine{
		{
			Provider:    "Amazon",
			Region:      "us-west-1",
			CloudID:     "inst3",
			Size:        "size2",
			DiskSize:    32,
			FloatingIP:  "8.8.8.8",
			Preemptible: false,
		},
		{
			Provider:    "Amazon",
			Region:      "us-west-1",
			CloudID:     "spot1",
			PublicIP:    "publicIP",
			PrivateIP:   "privateIP",
			Size:        "size",
			Preemptible: true,
		},
		{
			Provider:    "Amazon",
			Region:      "us-west-1",
			CloudID:     "spot2",
			Size:        "size2",
			FloatingIP:  "xx.xxx.xxx.xxx",
			Preemptible: true,
		},
		{
			Provider:    "Amazon",
			Region:      "us-west-1",
			CloudID:     "spot3",
			Preemptible: true,
		},
	}, machines)
}

func TestNewACLs(t *testing.T) {
	t.Parallel()

	mc := new(mocks.Client)
	mc.On("DescribeSecurityGroup", mock.Anything).Return(
		[]*ec2.SecurityGroup{{
			IpPermissions: []*ec2.IpPermission{
				{
					IpRanges: []*ec2.IpRange{
						{CidrIp: aws.String(
							"deleteMe")},
					},
					IpProtocol: aws.String("-1"),
				},
				{
					IpRanges: []*ec2.IpRange{
						{CidrIp: aws.String(
							"foo")},
					},
					FromPort:   aws.Int64(1),
					ToPort:     aws.Int64(65535),
					IpProtocol: aws.String("tcp"),
				},
				{
					IpRanges: []*ec2.IpRange{
						{CidrIp: aws.String(
							"foo")},
					},
					FromPort:   aws.Int64(1),
					ToPort:     aws.Int64(65535),
					IpProtocol: aws.String("udp"),
				},
			},
			GroupId: aws.String("")}}, nil)

	mc.On("RevokeSecurityGroup", mock.Anything, mock.Anything).Return(nil)
	mc.On("AuthorizeSecurityGroup", mock.Anything, mock.Anything,
		mock.Anything).Return(nil)
	mc.On("DescribeInstances", mock.Anything).Return(
		&ec2.DescribeInstancesOutput{}, nil,
	)

	cluster := newAmazon(testNamespace, DefaultRegion)
	cluster.Client = mc

	err := cluster.SetACLs([]acl.ACL{
		{
			CidrIP:  "foo",
			MinPort: 1,
			MaxPort: 65535,
		},
		{
			CidrIP:  "bar",
			MinPort: 80,
			MaxPort: 80,
		},
	})

	assert.Nil(t, err)

	mc.AssertCalled(t, "RevokeSecurityGroup", testNamespace, []*ec2.IpPermission{{
		IpRanges:   []*ec2.IpRange{{CidrIp: aws.String("deleteMe")}},
		IpProtocol: aws.String("-1")}})

	mc.AssertCalled(t, "AuthorizeSecurityGroup", testNamespace, testNamespace,
		mock.Anything)

	// Manually extract and compare the ingress rules for allowing traffic based
	// on IP ranges so that we can sort them because HashJoin returns results
	// in a non-deterministic order.
	var perms []*ec2.IpPermission
	var foundCall bool
	for _, call := range mc.Calls {
		if call.Method == "AuthorizeSecurityGroup" {
			arg := call.Arguments.Get(2).([]*ec2.IpPermission)
			if len(arg) != 0 {
				perms = arg
				foundCall = true
			}
		}
	}
	if !foundCall {
		t.Errorf("Expected call to AuthorizeSecurityGroup to set IP ACLs")
	}

	sort.Sort(ipPermSlice(perms))
	exp := []*ec2.IpPermission{
		{
			IpRanges: []*ec2.IpRange{
				{
					CidrIp: aws.String("bar"),
				},
			},
			FromPort:   aws.Int64(-1),
			ToPort:     aws.Int64(-1),
			IpProtocol: aws.String("icmp"),
		},
		{
			IpRanges: []*ec2.IpRange{
				{CidrIp: aws.String(
					"foo")},
			},
			FromPort:   aws.Int64(-1),
			ToPort:     aws.Int64(-1),
			IpProtocol: aws.String("icmp"),
		},
		{
			IpRanges: []*ec2.IpRange{
				{
					CidrIp: aws.String("bar"),
				},
			},
			FromPort:   aws.Int64(80),
			ToPort:     aws.Int64(80),
			IpProtocol: aws.String("tcp"),
		},
		{
			IpRanges: []*ec2.IpRange{
				{
					CidrIp: aws.String("bar"),
				},
			},
			FromPort:   aws.Int64(80),
			ToPort:     aws.Int64(80),
			IpProtocol: aws.String("udp"),
		},
	}
	if !reflect.DeepEqual(perms, exp) {
		t.Errorf("Bad args to AuthorizeSecurityGroup: "+
			"Expected %v, got %v.", exp, perms)
	}
}

func TestBoot(t *testing.T) {
	t.Parallel()

	sleep = func(t time.Duration) {}
	instances := []*ec2.Instance{
		{
			InstanceId:            aws.String("inst1"),
			SpotInstanceRequestId: aws.String("spot1"),
			InstanceType:          aws.String("m4.large"),
			State: &ec2.InstanceState{
				Name: aws.String(ec2.InstanceStateNameRunning),
			},
		},
		{
			InstanceId:            aws.String("inst2"),
			SpotInstanceRequestId: aws.String("spot2"),
			InstanceType:          aws.String("m4.large"),
			State: &ec2.InstanceState{
				Name: aws.String(ec2.InstanceStateNameRunning),
			},
		},
		{
			InstanceId:   aws.String("reserved1"),
			InstanceType: aws.String("m4.large"),
			State: &ec2.InstanceState{
				Name: aws.String(ec2.InstanceStateNameRunning),
			},
		},
		{
			InstanceId:   aws.String("reserved2"),
			InstanceType: aws.String("m4.large"),
			State: &ec2.InstanceState{
				Name: aws.String(ec2.InstanceStateNameRunning),
			},
		},
	}
	mc := new(mocks.Client)
	mc.On("DescribeSecurityGroup", mock.Anything).Return([]*ec2.SecurityGroup{{
		GroupId: aws.String("groupId")}}, nil)

	mc.On("RequestSpotInstances", mock.Anything, mock.Anything,
		mock.Anything).Return([]*ec2.SpotInstanceRequest{{
		SpotInstanceRequestId: aws.String("spot1"),
	}, {
		SpotInstanceRequestId: aws.String("spot2"),
	}}, nil)
	mc.On("RunInstances", mock.Anything).Return(
		&ec2.Reservation{
			Instances: []*ec2.Instance{
				{
					InstanceId: aws.String("reserved1"),
				},
				{
					InstanceId: aws.String("reserved2"),
				},
			},
		}, nil,
	)
	mc.On("DescribeInstances", mock.Anything).Return(
		&ec2.DescribeInstancesOutput{
			Reservations: []*ec2.Reservation{
				{
					Instances: instances,
				},
			},
		}, nil,
	)
	mc.On("DescribeAddresses").Return(nil, nil)

	mc.On("DescribeSpotInstanceRequests", mock.Anything, mock.Anything).Return(
		[]*ec2.SpotInstanceRequest{{
			InstanceId:            aws.String("inst1"),
			SpotInstanceRequestId: aws.String("spot1"),
			State: aws.String(ec2.SpotInstanceStateActive),
		}, {
			InstanceId:            aws.String("inst2"),
			SpotInstanceRequestId: aws.String("spot2"),
			State: aws.String(ec2.SpotInstanceStateActive)}}, nil)

	amazonProvider := newAmazon(testNamespace, DefaultRegion)
	amazonProvider.Client = mc

	err := amazonProvider.Boot([]db.Machine{
		{
			Role:        db.Master,
			Size:        "m4.large",
			DiskSize:    32,
			Preemptible: true,
		},
		{
			Role:        db.Master,
			Size:        "m4.large",
			DiskSize:    32,
			Preemptible: true,
		},
		{
			Role:        db.Master,
			Size:        "m4.large",
			DiskSize:    32,
			Preemptible: false,
		},
		{
			Role:        db.Master,
			Size:        "m4.large",
			DiskSize:    32,
			Preemptible: false,
		},
	})
	assert.Nil(t, err)

	cfg := cfg.Ubuntu(db.Machine{Role: db.Master}, "")
	mc.AssertCalled(t, "RequestSpotInstances", spotPrice, int64(2),
		&ec2.RequestSpotLaunchSpecification{
			ImageId:      aws.String(amis[DefaultRegion]),
			InstanceType: aws.String("m4.large"),
			UserData: aws.String(base64.StdEncoding.EncodeToString(
				[]byte(cfg))),
			SecurityGroupIds: aws.StringSlice([]string{"groupId"}),
			BlockDeviceMappings: []*ec2.BlockDeviceMapping{
				blockDevice(32)}})
	mc.AssertCalled(t, "RunInstances", &ec2.RunInstancesInput{
		ImageId:      aws.String(amis[DefaultRegion]),
		InstanceType: aws.String("m4.large"),
		UserData: aws.String(base64.StdEncoding.EncodeToString(
			[]byte(cfg))),
		SecurityGroupIds: aws.StringSlice([]string{"groupId"}),
		BlockDeviceMappings: []*ec2.BlockDeviceMapping{
			blockDevice(32)},
		MaxCount: aws.Int64(2),
		MinCount: aws.Int64(2),
	})
	mc.AssertExpectations(t)
}

// This test attempts to boot a preemptible and non-preemptible instance,
// but simulates a boot error where the machines never show up in `List`.
// We should consider this a boot failure, and try to clean up by stopping
// the pending instances.
func TestBootUnsuccessful(t *testing.T) {
	util.After = func(t time.Time) bool { return true }

	mc := new(mocks.Client)
	mc.On("DescribeSecurityGroup", mock.Anything).Return([]*ec2.SecurityGroup{{
		GroupId: aws.String("groupId")}}, nil)

	mc.On("RequestSpotInstances", mock.Anything, mock.Anything,
		mock.Anything).Return([]*ec2.SpotInstanceRequest{{
		SpotInstanceRequestId: aws.String("spot1")}}, nil)

	mc.On("RunInstances", mock.Anything).Return(
		&ec2.Reservation{
			Instances: []*ec2.Instance{
				{
					InstanceId: aws.String("reserved1"),
				},
			},
		}, nil,
	)
	mc.On("DescribeInstances", mock.Anything).Return(
		&ec2.DescribeInstancesOutput{
			Reservations: []*ec2.Reservation{
				{
					Instances: nil,
				},
			},
		}, nil,
	)
	mc.On("DescribeAddresses").Return(nil, nil)

	mc.On("DescribeSpotInstanceRequests", mock.Anything,
		mock.Anything).Return(nil, nil)
	mc.On("TerminateInstances", []string{"reserved1"}).Return(nil)
	mc.On("CancelSpotInstanceRequests", []string{"spot1"}).Return(nil)

	amazonProvider := newAmazon(testNamespace, DefaultRegion)
	amazonProvider.Client = mc
	err := amazonProvider.Boot([]db.Machine{{Preemptible: false}})
	assert.Error(t, err)

	err = amazonProvider.Boot([]db.Machine{{Preemptible: true}})
	assert.Error(t, err)

	mc.AssertExpectations(t)
}

func TestStop(t *testing.T) {
	t.Parallel()

	sleep = func(t time.Duration) {}
	mc := new(mocks.Client)
	spotIDs := []string{"spot1", "spot2"}
	reservedIDs := []string{"reserved1"}
	// When we're getting information about what machines to stop.
	mc.On("DescribeSpotInstanceRequests", spotIDs, mock.Anything).Return(
		[]*ec2.SpotInstanceRequest{{
			SpotInstanceRequestId: aws.String(spotIDs[0]),
			InstanceId:            aws.String("inst1"),
			State:                 aws.String(ec2.SpotInstanceStateActive),
		}, {
			SpotInstanceRequestId: aws.String(spotIDs[1]),
			State: aws.String(ec2.SpotInstanceStateActive),
		}}, nil)
	// When we're listing machines to tell if they've stopped.
	mc.On("DescribeSpotInstanceRequests", mock.Anything,
		mock.Anything).Return(nil, nil)

	mc.On("TerminateInstances", mock.Anything).Return(nil)

	mc.On("CancelSpotInstanceRequests", mock.Anything).Return(nil)
	mc.On("DescribeInstances", mock.Anything).Return(
		&ec2.DescribeInstancesOutput{}, nil,
	)
	mc.On("DescribeAddresses").Return(nil, nil)

	amazonProvider := newAmazon(testNamespace, DefaultRegion)
	amazonProvider.Client = mc

	err := amazonProvider.Stop([]db.Machine{
		{
			CloudID:     spotIDs[0],
			Preemptible: true,
		},
		{
			CloudID:     spotIDs[1],
			Preemptible: true,
		},
		{
			CloudID:     reservedIDs[0],
			Preemptible: false,
		},
	})
	assert.Nil(t, err)

	mc.AssertCalled(t, "TerminateInstances", []string{"inst1"})

	mc.AssertCalled(t, "TerminateInstances", []string{reservedIDs[0]})

	mc.AssertCalled(t, "CancelSpotInstanceRequests", spotIDs)
}

func TestWaitBoot(t *testing.T) {
	t.Parallel()
	util.Sleep = func(time.Duration) {}
	i := 0
	util.After = func(t time.Time) bool {
		i++
		return i > 5
	}

	timeout = 10 * time.Second

	instances := []*ec2.Instance{
		{
			InstanceId:            aws.String("inst1"),
			SpotInstanceRequestId: aws.String("spot1"),
			InstanceType:          aws.String("m4.large"),
			State: &ec2.InstanceState{
				Name: aws.String(ec2.InstanceStateNameRunning),
			},
		},
		{
			InstanceId:            aws.String("inst2"),
			SpotInstanceRequestId: aws.String("spot2"),
			InstanceType:          aws.String("m4.large"),
			State: &ec2.InstanceState{
				Name: aws.String(ec2.InstanceStateNameRunning),
			},
		},
	}
	mc := new(mocks.Client)
	mc.On("DescribeAddresses").Return(nil, nil)
	mc.On("DescribeSecurityGroup", mock.Anything).Return([]*ec2.SecurityGroup{{
		GroupId: aws.String("groupId")}}, nil)

	mc.On("RequestSpotInstances", mock.Anything, mock.Anything,
		mock.Anything).Return([]*ec2.SpotInstanceRequest{{
		SpotInstanceRequestId: aws.String("spot1"),
	}, {
		SpotInstanceRequestId: aws.String("spot2"),
	}}, nil)
	describeInstances := mc.On("DescribeInstances", mock.Anything)
	describeInstances.Return(
		&ec2.DescribeInstancesOutput{}, nil,
	)
	mc.On("DescribeSpotInstanceRequests", mock.Anything, mock.Anything).Return(
		[]*ec2.SpotInstanceRequest{{
			InstanceId:            aws.String("inst1"),
			SpotInstanceRequestId: aws.String("spot1"),
			State: aws.String(ec2.SpotInstanceStateActive),
		}, {
			InstanceId:            aws.String("inst2"),
			SpotInstanceRequestId: aws.String("spot2"),
			State: aws.String(ec2.SpotInstanceStateActive)}}, nil)

	amazonProvider := newAmazon(testNamespace, DefaultRegion)
	amazonProvider.Client = mc

	exp := []string{"spot1", "spot2"}
	err := amazonProvider.wait(exp, true)
	assert.Error(t, err, "timed out")

	describeInstances.Return(
		&ec2.DescribeInstancesOutput{
			Reservations: []*ec2.Reservation{
				{
					Instances: instances,
				},
			},
		}, nil,
	)

	err = amazonProvider.wait(exp, true)
	assert.NoError(t, err)
}

func TestWaitStop(t *testing.T) {
	t.Parallel()

	util.Sleep = func(t time.Duration) {}
	i := 0
	util.After = func(t time.Time) bool {
		i++
		return i > 5
	}

	timeout = 10 * time.Second
	instances := []*ec2.Instance{
		{
			InstanceId:            aws.String("inst1"),
			SpotInstanceRequestId: aws.String("spot1"),
			State: &ec2.InstanceState{
				Name: aws.String(ec2.InstanceStateNameRunning),
			},
		},
		{
			InstanceId:            aws.String("inst2"),
			SpotInstanceRequestId: aws.String("spot2"),
			State: &ec2.InstanceState{
				Name: aws.String(ec2.InstanceStateNameRunning),
			},
		},
	}
	mc := new(mocks.Client)
	mc.On("DescribeSecurityGroup", mock.Anything).Return([]*ec2.SecurityGroup{{
		GroupId: aws.String("groupId")}}, nil)

	mc.On("RequestSpotInstances", mock.Anything, mock.Anything,
		mock.Anything).Return([]*ec2.SpotInstanceRequest{{
		SpotInstanceRequestId: aws.String("spot1"),
	}, {
		SpotInstanceRequestId: aws.String("spot2"),
	}}, nil)
	describeInstances := mc.On("DescribeInstances", mock.Anything)
	describeInstances.Return(
		&ec2.DescribeInstancesOutput{
			Reservations: []*ec2.Reservation{
				{
					Instances: instances,
				},
			},
		}, nil,
	)

	mc.On("DescribeAddresses").Return(nil, nil)

	describeRequests := mc.On("DescribeSpotInstanceRequests", mock.Anything,
		mock.Anything)
	describeRequests.Return([]*ec2.SpotInstanceRequest{{
		InstanceId:            aws.String("inst1"),
		SpotInstanceRequestId: aws.String("spot1"),
		State: aws.String(ec2.SpotInstanceStateActive),
	}, {
		InstanceId:            aws.String("inst2"),
		SpotInstanceRequestId: aws.String("spot2"),
		State: aws.String(ec2.SpotInstanceStateActive)}}, nil)

	amazonProvider := newAmazon(testNamespace, DefaultRegion)
	amazonProvider.Client = mc

	exp := []string{"spot1", "spot2"}
	err := amazonProvider.wait(exp, false)
	assert.Error(t, err, "timed out")

	describeInstances.Return(
		&ec2.DescribeInstancesOutput{
			Reservations: []*ec2.Reservation{
				{
					Instances: []*ec2.Instance{},
				},
			},
		}, nil,
	)
	describeRequests.Return([]*ec2.SpotInstanceRequest{}, nil)

	err = amazonProvider.wait(exp, false)
	assert.NoError(t, err)
}

func TestUpdateFloatingIPs(t *testing.T) {
	t.Parallel()

	mockClient := new(mocks.Client)
	amazonProvider := newAmazon(testNamespace, DefaultRegion)
	amazonProvider.Client = mockClient

	mockMachines := []db.Machine{
		// Kelda should assign "x.x.x.x" to sir-1.
		{
			CloudID:     "sir-1",
			FloatingIP:  "x.x.x.x",
			Preemptible: true,
		},
		// Kelda should disassociate all floating IPs from spot instance sir-2.
		{
			CloudID:     "sir-2",
			FloatingIP:  "",
			Preemptible: true,
		},
		// Kelda is asked to disassociate floating IPs from sir-3. sir-3 no longer
		// has IP associations, but Kelda should not error.
		{
			CloudID:     "sir-3",
			FloatingIP:  "",
			Preemptible: true,
		},
		// Kelda should assign "x.x.x.x" to reserved-1.
		{
			CloudID:     "reserved-1",
			FloatingIP:  "reservedAdd",
			Preemptible: false,
		},
		// Kelda should disassociate all floating IPs from reserved-2.
		{
			CloudID:     "reserved-2",
			FloatingIP:  "",
			Preemptible: false,
		},
		// Kelda is asked to disassociate floating IPs from reserved-3.
		// reserved-3 no longer has IP associations, but Kelda should not
		// error.
		{
			CloudID:     "reserved-3",
			FloatingIP:  "",
			Preemptible: false,
		},
	}

	mockClient.On("DescribeAddresses").Return([]*ec2.Address{{
		// Kelda should assign x.x.x.x to sir-1.
		AllocationId: aws.String("alloc-1"),
		PublicIp:     aws.String("x.x.x.x"),
	}, { // Kelda should disassociate y.y.y.y from sir-2.
		AllocationId:  aws.String("alloc-2"),
		PublicIp:      aws.String("y.y.y.y"),
		AssociationId: aws.String("assoc-2"),
		InstanceId:    aws.String("i-2"),
	}, {
		AllocationId: aws.String("alloc-reservedAdd"),
		PublicIp:     aws.String("reservedAdd"),
	}, {
		AllocationId:  aws.String("alloc-reservedRemove"),
		PublicIp:      aws.String("reservedRemove"),
		AssociationId: aws.String("assoc-reservedRemove"),
		InstanceId:    aws.String("reserved-2"),
	}, { // Kelda should ignore z.z.z.z.
		PublicIp:   aws.String("z.z.z.z"),
		InstanceId: aws.String("i-4")}}, nil)

	mockClient.On("DescribeSpotInstanceRequests", mock.Anything,
		mock.Anything).Return([]*ec2.SpotInstanceRequest{{
		SpotInstanceRequestId: aws.String("sir-1"),
		InstanceId:            aws.String("i-1"),
	}, {
		SpotInstanceRequestId: aws.String("sir-2"),
		InstanceId:            aws.String("i-2"),
	}, {
		SpotInstanceRequestId: aws.String("sir-3"),
		InstanceId:            aws.String("i-3")}}, nil)
	instancesOut := []*ec2.Instance{
		{
			InstanceId:            aws.String("i-1"),
			SpotInstanceRequestId: aws.String("sir-1"),
			State: &ec2.InstanceState{
				Name: aws.String(ec2.InstanceStateNameRunning),
			},
		},
		{
			InstanceId:            aws.String("i-2"),
			SpotInstanceRequestId: aws.String("sir-2"),
			State: &ec2.InstanceState{
				Name: aws.String(ec2.InstanceStateNameRunning),
			},
		},
		{
			InstanceId:            aws.String("i-3"),
			SpotInstanceRequestId: aws.String("sir-3"),
			State: &ec2.InstanceState{
				Name: aws.String(ec2.InstanceStateNameRunning),
			},
		},
	}
	describeInstancesOut := ec2.DescribeInstancesOutput{
		Reservations: []*ec2.Reservation{
			{
				Instances: instancesOut,
			},
		},
	}
	mockClient.On("DescribeInstances", mock.Anything).Return(
		&describeInstancesOut, nil)

	mockClient.On("AssociateAddress", "i-1", "alloc-1").Return(nil)
	mockClient.On("DisassociateAddress", "assoc-2").Return(nil)

	mockClient.On("AssociateAddress", "reserved-1", "alloc-reservedAdd").Return(nil)

	mockClient.On("DisassociateAddress", "assoc-reservedRemove").Return(nil)

	err := amazonProvider.UpdateFloatingIPs(mockMachines)
	assert.Nil(t, err)
}
