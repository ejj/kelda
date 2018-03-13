//go:generate mockery -name=Client

package client

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/kelda/kelda/counter"
)

// A Client to an Amazon EC2 region.
type Client interface {
	DescribeInstances([]*ec2.Filter) (*ec2.DescribeInstancesOutput, error)
	RunInstances(*ec2.RunInstancesInput) (*ec2.Reservation, error)
	TerminateInstances(ids []string) error

	DescribeSpotInstanceRequests(ids []string, filters []*ec2.Filter) (
		[]*ec2.SpotInstanceRequest, error)
	RequestSpotInstances(spotPrice string, count int64,
		launchSpec *ec2.RequestSpotLaunchSpecification) (
		[]*ec2.SpotInstanceRequest, error)
	CancelSpotInstanceRequests(ids []string) error

	DescribeVpcs(tagKey string) ([]*ec2.Vpc, error)
	CreateVpc(cidrBlock string) (*ec2.Vpc, error)
	DeleteVpc(vpcID string) error

	DescribeInternetGateways(tagKey string) ([]*ec2.InternetGateway, error)
	CreateInternetGateway() (*ec2.InternetGateway, error)
	AttachInternetGateway(igID, vpcID string) error
	DetachInternetGateway(igID, vpcID string) error
	DeleteInternetGateway(igID string) error

	DescribeRouteTables(vpcID string) ([]*ec2.RouteTable, error)
	CreateRoute(rtID, cidrBlock, igID string) error

	DescribeSubnets() ([]*ec2.Subnet, error)
	CreateSubnet(vpcID, cidrBlock string) (*ec2.Subnet, error)
	SubnetMapPublicIPOnLaunch(subnetID string, val bool) error
	DeleteSubnet(subnetID string) error

	DescribeSecurityGroup(name string) ([]*ec2.SecurityGroup, error)
	CreateSecurityGroup(name, vpcID, description string) (string, error)
	DeleteSecurityGroup(id string) error
	AuthorizeSecurityGroup(groupID string, ranges []*ec2.IpPermission) error
	RevokeSecurityGroup(groupID string, ranges []*ec2.IpPermission) error

	DescribeAddresses() ([]*ec2.Address, error)
	AssociateAddress(id, allocationID string) error
	DisassociateAddress(associationID string) error

	DescribeVolumes() ([]*ec2.Volume, error)

	CreateTags(ids []*string, key, value string) error
}

type awsClient struct {
	client *ec2.EC2
}

var c = counter.New("Amazon")

func (ac awsClient) DescribeInstances(filters []*ec2.Filter) (
	*ec2.DescribeInstancesOutput, error) {
	c.Inc("List Instances")
	return ac.client.DescribeInstances(&ec2.DescribeInstancesInput{Filters: filters})
}

func (ac awsClient) RunInstances(in *ec2.RunInstancesInput) (*ec2.Reservation, error) {
	c.Inc("Run Instances")
	return ac.client.RunInstances(in)
}

func (ac awsClient) TerminateInstances(ids []string) error {
	c.Inc("Term Instances")
	_, err := ac.client.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: stringSlice(ids)})
	return err
}

func (ac awsClient) DescribeSpotInstanceRequests(ids []string, filters []*ec2.Filter) (
	[]*ec2.SpotInstanceRequest, error) {
	c.Inc("List Spots")
	resp, err := ac.client.DescribeSpotInstanceRequests(
		&ec2.DescribeSpotInstanceRequestsInput{
			SpotInstanceRequestIds: stringSlice(ids),
			Filters:                filters})
	return resp.SpotInstanceRequests, err
}

func (ac awsClient) RequestSpotInstances(spotPrice string, count int64,
	launchSpec *ec2.RequestSpotLaunchSpecification) (
	[]*ec2.SpotInstanceRequest, error) {
	c.Inc("Request Spots")

	resp, err := ac.client.RequestSpotInstances(&ec2.RequestSpotInstancesInput{
		SpotPrice:           &spotPrice,
		InstanceCount:       &count,
		LaunchSpecification: launchSpec})
	if err != nil {
		return nil, err
	}
	return resp.SpotInstanceRequests, err
}
func (ac awsClient) CancelSpotInstanceRequests(ids []string) error {
	c.Inc("Cancel Spots")
	_, err := ac.client.CancelSpotInstanceRequests(
		&ec2.CancelSpotInstanceRequestsInput{
			SpotInstanceRequestIds: stringSlice(ids)})
	return err
}

func (ac awsClient) DescribeSubnets() ([]*ec2.Subnet, error) {
	c.Inc("Describe Subnets")
	resp, err := ac.client.DescribeSubnets(nil)
	if err != nil {
		return nil, err
	}
	return resp.Subnets, nil
}

func (ac awsClient) DescribeVpcs(tagKey string) ([]*ec2.Vpc, error) {
	c.Inc("Describe Vpcs")
	resp, err := ac.client.DescribeVpcs(&ec2.DescribeVpcsInput{
		Filters: []*ec2.Filter{{
			Name:   aws.String("tag-key"),
			Values: []*string{&tagKey},
		}}})
	if err != nil {
		return nil, err
	}
	return resp.Vpcs, nil
}

func (ac awsClient) CreateVpc(cidrBlock string) (*ec2.Vpc, error) {
	c.Inc("Create VPC")
	resp, err := ac.client.CreateVpc(&ec2.CreateVpcInput{
		CidrBlock: &cidrBlock,
	})
	if err != nil {
		return nil, err
	}

	return resp.Vpc, nil
}
func (ac awsClient) DeleteVpc(vpcID string) error {
	c.Inc("Delete VPC")
	_, err := ac.client.DeleteVpc(&ec2.DeleteVpcInput{
		VpcId: &vpcID,
	})
	return err
}

func (ac awsClient) DescribeInternetGateways(tagKey string) (
	[]*ec2.InternetGateway, error) {
	c.Inc("Describe Internet Gateways")
	resp, err := ac.client.DescribeInternetGateways(
		&ec2.DescribeInternetGatewaysInput{
			Filters: []*ec2.Filter{{
				Name:   aws.String("tag-key"),
				Values: []*string{&tagKey},
			}}})
	if err != nil {
		return nil, err
	}
	return resp.InternetGateways, nil
}

func (ac awsClient) CreateInternetGateway() (*ec2.InternetGateway, error) {
	c.Inc("Create Internet Gateway")
	resp, err := ac.client.CreateInternetGateway(&ec2.CreateInternetGatewayInput{})
	return resp.InternetGateway, err
}

func (ac awsClient) AttachInternetGateway(igID, vpcID string) error {
	c.Inc("Attach Internet Gateway")
	_, err := ac.client.AttachInternetGateway(&ec2.AttachInternetGatewayInput{
		InternetGatewayId: &igID,
		VpcId:             &vpcID,
	})
	return err
}

func (ac awsClient) DetachInternetGateway(igID, vpcID string) error {
	c.Inc("Detach Internet Gateway")
	_, err := ac.client.DetachInternetGateway(&ec2.DetachInternetGatewayInput{
		InternetGatewayId: &igID,
		VpcId:             &vpcID,
	})
	return err
}

func (ac awsClient) DescribeRouteTables(vpcID string) (
	[]*ec2.RouteTable, error) {
	c.Inc("Describe Route Tables")
	resp, err := ac.client.DescribeRouteTables(
		&ec2.DescribeRouteTablesInput{
			Filters: []*ec2.Filter{{
				Name:   aws.String("vpc-id"),
				Values: []*string{&vpcID},
			}}})
	if err != nil {
		return nil, err
	}
	return resp.RouteTables, nil
}

func (ac awsClient) CreateRoute(rtID, cidrBlock, igID string) error {
	c.Inc("Create Internet Gateway")
	_, err := ac.client.CreateRoute(&ec2.CreateRouteInput{
		GatewayId:            &igID,
		RouteTableId:         &rtID,
		DestinationCidrBlock: &cidrBlock,
	})
	return err
}

func (ac awsClient) DeleteInternetGateway(igID string) error {
	c.Inc("Delete Internet Gateway")
	_, err := ac.client.DeleteInternetGateway(&ec2.DeleteInternetGatewayInput{
		InternetGatewayId: &igID,
	})
	return err
}

func (ac awsClient) CreateSubnet(vpcID, cidrBlock string) (*ec2.Subnet, error) {
	c.Inc("Create Subnet")
	resp, err := ac.client.CreateSubnet(&ec2.CreateSubnetInput{
		CidrBlock: &cidrBlock,
		VpcId:     &vpcID,
	})
	return resp.Subnet, err
}

func (ac awsClient) SubnetMapPublicIPOnLaunch(subnetID string, val bool) error {
	c.Inc("Modify Subnet Attribute")
	_, err := ac.client.ModifySubnetAttribute(&ec2.ModifySubnetAttributeInput{
		SubnetId:            &subnetID,
		MapPublicIpOnLaunch: &ec2.AttributeBooleanValue{Value: &val},
	})
	return err
}

func (ac awsClient) DeleteSubnet(subnetID string) error {
	c.Inc("Delete Subnet")
	_, err := ac.client.DeleteSubnet(&ec2.DeleteSubnetInput{
		SubnetId: &subnetID,
	})
	return err
}

func (ac awsClient) DescribeSecurityGroup(name string) ([]*ec2.SecurityGroup, error) {
	c.Inc("List Security Groups")
	resp, err := ac.client.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{{
			Name:   aws.String("group-name"),
			Values: []*string{&name}}}})
	if err != nil {
		return nil, err
	}
	return resp.SecurityGroups, err
}

func (ac awsClient) CreateSecurityGroup(name, vpcID, desc string) (string, error) {
	c.Inc("Create Security Group")
	csgResp, err := ac.client.CreateSecurityGroup(&ec2.CreateSecurityGroupInput{
		GroupName:   &name,
		VpcId:       &vpcID,
		Description: &desc})
	if err != nil {
		return "", err
	}
	return *csgResp.GroupId, err
}

func (ac awsClient) DeleteSecurityGroup(id string) error {
	c.Inc("Delete Security Group")
	_, err := ac.client.DeleteSecurityGroup(
		&ec2.DeleteSecurityGroupInput{GroupId: &id})
	return err
}

func (ac awsClient) AuthorizeSecurityGroup(groupID string,
	ranges []*ec2.IpPermission) error {
	c.Inc("Authorize Security Group")

	_, err := ac.client.AuthorizeSecurityGroupIngress(
		&ec2.AuthorizeSecurityGroupIngressInput{
			GroupId:       &groupID,
			IpPermissions: ranges})
	return err
}

func (ac awsClient) RevokeSecurityGroup(groupID string,
	ranges []*ec2.IpPermission) error {
	c.Inc("Revoke Security Group")
	_, err := ac.client.RevokeSecurityGroupIngress(
		&ec2.RevokeSecurityGroupIngressInput{
			GroupId:       &groupID,
			IpPermissions: ranges})
	return err
}

func (ac awsClient) DescribeAddresses() ([]*ec2.Address, error) {
	c.Inc("List Addresses")
	resp, err := ac.client.DescribeAddresses(nil)
	if err != nil {
		return nil, err
	}
	return resp.Addresses, err
}

func (ac awsClient) AssociateAddress(id, allocationID string) error {
	c.Inc("Associate Address")
	_, err := ac.client.AssociateAddress(&ec2.AssociateAddressInput{
		InstanceId:   &id,
		AllocationId: &allocationID})
	return err
}

func (ac awsClient) DisassociateAddress(associationID string) error {
	c.Inc("Disassociate Address")
	_, err := ac.client.DisassociateAddress(&ec2.DisassociateAddressInput{
		AssociationId: &associationID})
	return err
}

func (ac awsClient) DescribeVolumes() ([]*ec2.Volume, error) {
	c.Inc("List Volumes")
	resp, err := ac.client.DescribeVolumes(nil)
	if err != nil {
		return nil, err
	}
	return resp.Volumes, err
}

func (ac awsClient) CreateTags(ids []*string, key, value string) error {
	c.Inc("Create Tags")
	_, err := ac.client.CreateTags(&ec2.CreateTagsInput{
		Resources: ids,
		Tags:      []*ec2.Tag{{Key: &key, Value: &value}},
	})
	return err
}

// New creates a new Client.
func New(region string) Client {
	c.Inc("New Client")
	session := session.New()
	session.Config.Region = &region
	return awsClient{ec2.New(session)}
}

// The amazon API makes a distinction between `nil` which means "this parameter was
// omitted" and `[]*string` which means "this parameter was provided with no elements".
// aws.StringSlice() clobbers that distinction, so we wrap with stringSlice.
func stringSlice(slice []string) []*string {
	if slice == nil {
		return nil
	}
	return aws.StringSlice(slice)
}
