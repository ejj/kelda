package client

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/stretchr/testify/assert"
)

func TestErrors(t *testing.T) {
	ac := New("junk")

	// Disable HTTP requesting for unit tests
	ac.(awsClient).client.Client.Handlers.Clear()
	ac.(awsClient).client.Client.Handlers.Send.PushBack(func(r *request.Request) {
		r.Error = errors.New("test")
	})

	_, err := ac.DescribeInstances(nil)
	assert.EqualError(t, err, "test")

	_, err = ac.RunInstances(nil)
	assert.EqualError(t, err, "test")

	err = ac.TerminateInstances([]string{"a"})
	assert.EqualError(t, err, "test")

	_, err = ac.DescribeSpotInstanceRequests(nil, nil)
	assert.EqualError(t, err, "test")

	_, err = ac.RequestSpotInstances("", 0, nil)
	assert.EqualError(t, err, "test")

	err = ac.CancelSpotInstanceRequests(nil)
	assert.EqualError(t, err, "test")

	_, err = ac.DescribeSecurityGroup("")
	assert.EqualError(t, err, "test")

	_, err = ac.CreateSecurityGroup("", "")
	assert.EqualError(t, err, "test")

	err = ac.DeleteSecurityGroup("")
	assert.EqualError(t, err, "test")

	err = ac.AuthorizeSecurityGroup("name", "src", nil)
	assert.EqualError(t, err, "test")

	err = ac.RevokeSecurityGroup("", nil)
	assert.EqualError(t, err, "test")

	_, err = ac.DescribeAddresses()
	assert.EqualError(t, err, "test")

	err = ac.AssociateAddress("", "")
	assert.EqualError(t, err, "test")

	err = ac.DisassociateAddress("")
	assert.EqualError(t, err, "test")

	_, err = ac.DescribeVolumes()
	assert.EqualError(t, err, "test")
}
