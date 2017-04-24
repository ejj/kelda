package amazon

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

type client interface {
	AuthorizeSecurityGroupIngress(*ec2.AuthorizeSecurityGroupIngressInput) (
		*ec2.AuthorizeSecurityGroupIngressOutput, error)

	CancelSpotInstanceRequests(*ec2.CancelSpotInstanceRequestsInput) (
		*ec2.CancelSpotInstanceRequestsOutput, error)

	CreateSecurityGroup(*ec2.CreateSecurityGroupInput) (
		*ec2.CreateSecurityGroupOutput, error)

	DescribeSecurityGroups(*ec2.DescribeSecurityGroupsInput) (
		*ec2.DescribeSecurityGroupsOutput, error)

	DescribeInstances(*ec2.DescribeInstancesInput) (
		*ec2.DescribeInstancesOutput, error)

	DescribeSpotInstanceRequests(*ec2.DescribeSpotInstanceRequestsInput) (
		*ec2.DescribeSpotInstanceRequestsOutput, error)

	DescribeVolumes(*ec2.DescribeVolumesInput) (
		*ec2.DescribeVolumesOutput, error)

	RevokeSecurityGroupIngress(*ec2.RevokeSecurityGroupIngressInput) (
		*ec2.RevokeSecurityGroupIngressOutput, error)

	RunInstances(*ec2.RunInstancesInput) (*ec2.Reservation, error)

	TerminateInstances(*ec2.TerminateInstancesInput) (
		*ec2.TerminateInstancesOutput, error)

	RequestSpotInstances(*ec2.RequestSpotInstancesInput) (
		*ec2.RequestSpotInstancesOutput, error)

	AssociateAddress(*ec2.AssociateAddressInput) (*ec2.AssociateAddressOutput, error)

	DescribeAddresses(*ec2.DescribeAddressesInput) (*ec2.DescribeAddressesOutput,
		error)

	DisassociateAddress(*ec2.DisassociateAddressInput) (
		*ec2.DisassociateAddressOutput, error)
}

func newClient(region string) client {
	session := session.New()
	session.Config.Region = aws.String(region)
	return ec2.New(session)
}
