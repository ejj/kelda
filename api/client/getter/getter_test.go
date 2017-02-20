package getter

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/quilt/quilt/api"
	"github.com/quilt/quilt/api/client"
	"github.com/quilt/quilt/api/client/mocks"
	"github.com/quilt/quilt/db"
)

func TestGetLeaderClient(t *testing.T) {
	t.Parallel()

	passedClient := &mocks.Client{}
	mockGetter := mockAddrClientGetter{
		func(host string) (client.Client, error) {
			switch host {
			// One machine doesn't know the LeaderIP
			case api.RemoteAddress("8.8.8.8"):
				return &mocks.Client{
					EtcdReturn: []db.Etcd{
						{
							LeaderIP: "",
						},
					},
				}, nil
			// The other machine knows the LeaderIP
			case api.RemoteAddress("9.9.9.9"):
				return &mocks.Client{
					EtcdReturn: []db.Etcd{
						{
							LeaderIP: "leader-priv",
						},
					},
				}, nil
			case api.RemoteAddress("leader"):
				return passedClient, nil
			default:
				t.Fatalf("Unexpected call to getClient with host %s",
					host)
			}
			panic("unreached")
		},
	}

	localClient := &mocks.Client{
		MachineReturn: []db.Machine{
			{
				PublicIP: "8.8.8.8",
			},
			{
				PublicIP: "9.9.9.9",
			},
			{
				PublicIP:  "leader",
				PrivateIP: "leader-priv",
			},
		},
	}

	res, err := clientGetterImpl{mockGetter}.LeaderClient(localClient)
	assert.Nil(t, err)
	assert.Equal(t, passedClient, res)
}

func TestNoLeader(t *testing.T) {
	t.Parallel()

	mockGetter := mockAddrClientGetter{
		func(host string) (client.Client, error) {
			// No client knows the leader IP.
			return &mocks.Client{
				EtcdReturn: []db.Etcd{
					{
						LeaderIP: "",
					},
				},
			}, nil
		},
	}

	localClient := &mocks.Client{
		MachineReturn: []db.Machine{
			{
				PublicIP: "8.8.8.8",
			},
			{
				PublicIP: "9.9.9.9",
			},
		},
	}

	_, err := clientGetterImpl{mockGetter}.LeaderClient(localClient)
	assert.EqualError(t, err, "no leader found")
}

func TestGetContainerClient(t *testing.T) {
	t.Parallel()

	targetContainer := "1"
	workerHost := "worker"
	leaderHost := "leader"
	passedClient := &mocks.Client{}
	mockGetter := mockAddrClientGetter{
		func(host string) (client.Client, error) {
			switch host {
			case api.RemoteAddress(leaderHost):
				return &mocks.Client{
					ContainerReturn: []db.Container{
						{
							StitchID: targetContainer,
							Minion:   workerHost,
						},
						{
							StitchID: "5",
							Minion:   "bad",
						},
					},
					EtcdReturn: []db.Etcd{
						{
							LeaderIP: leaderHost,
						},
					},
				}, nil
			case api.RemoteAddress(workerHost):
				return passedClient, nil
			default:
				t.Fatalf("Unexpected call to getClient with host %s",
					host)
			}
			panic("unreached")
		},
	}

	localClient := &mocks.Client{
		MachineReturn: []db.Machine{
			{
				PublicIP:  leaderHost,
				PrivateIP: leaderHost,
			},
			{
				PrivateIP: workerHost,
				PublicIP:  workerHost,
			},
		},
	}

	res, err := clientGetterImpl{mockGetter}.ContainerClient(
		localClient, targetContainer)
	assert.Nil(t, err)
	assert.Equal(t, passedClient, res)
}

type mockAddrClientGetter struct {
	getter func(host string) (client.Client, error)
}

func (mcg mockAddrClientGetter) Client(host string) (client.Client, error) {
	return mcg.getter(host)
}
