package kubernetes

import (
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/minion/kubernetes/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestUpdateStatus(t *testing.T) {
	t.Parallel()

	mockTime := time.Now()

	runningContainer := db.Container{
		// Set a blueprint ID so that the order is deterministic when sorting.
		BlueprintID: "1",
		Hostname:    "runningContainer",
	}
	runningContainerPod := corev1.Pod{
		Spec: corev1.PodSpec{
			Hostname: runningContainer.Hostname,
		},
	}

	rebuildingContainer := db.Container{
		BlueprintID: "2",
		Hostname:    "wasRunningNowRebuilding",
		Dockerfile:  "differentDockerfile",
		Status:      "running",
		Created:     time.Now(),
	}
	conn := db.New()
	conn.Txn(db.ContainerTable).Run(func(view db.Database) error {
		runningContainer.ID = view.InsertContainer().ID
		view.Commit(runningContainer)

		rebuildingContainer.ID = view.InsertContainer().ID
		view.Commit(rebuildingContainer)
		return nil
	})

	joinContainersToPods = func(dbcs []db.Container, _ []corev1.Pod) (
		pairs []join.Pair, noInfoContainers []interface{}) {
		for _, dbc := range dbcs {
			switch dbc.Hostname {
			case runningContainer.Hostname:
				pairs = append(pairs, join.Pair{
					L: runningContainer,
					R: runningContainerPod,
				})
			case rebuildingContainer.Hostname:
				noInfoContainers = append(noInfoContainers,
					rebuildingContainer)
			default:
				assert.FailNow(t, "unexpected container given to "+
					"joinContainersToPods: %v", dbc)
			}
		}
		return
	}

	statusForPod = func(pod corev1.Pod) (string, time.Time) {
		if pod.Spec.Hostname != runningContainer.Hostname {
			assert.FailNow(t, "unexpected call to statusForPod "+
				"for %s", pod.Spec.Hostname)
		}
		return "running", mockTime
	}

	statusForContainer = func(_ map[db.Image]db.Image, _ SecretClient,
		dbc db.Container) string {
		if dbc.Hostname != rebuildingContainer.Hostname {
			assert.FailNow(t, "unexpected call to statusForContainer "+
				"for %s", dbc.Hostname)
		}
		return "building"
	}

	mockPodsClient := &mocks.PodInterface{}
	mockPodsClient.On("List", mock.Anything).Return(&corev1.PodList{
		Items: nil,
	}, nil)
	updateContainerStatuses(conn, mockPodsClient, nil)

	runningContainer.Status = "running"
	runningContainer.Created = mockTime
	rebuildingContainer.Status = "building"
	rebuildingContainer.Created = time.Time{}

	actualDbcs := conn.SelectFromContainer(nil)
	sort.Sort(db.ContainerSlice(actualDbcs))
	assert.Equal(t, []db.Container{runningContainer, rebuildingContainer}, actualDbcs)
}

// Test that if the list fails, nothing changes.
func TestStatusFailedList(t *testing.T) {
	t.Parallel()
	conn := db.New()
	podsClient := &mocks.PodInterface{}

	containerToStatus := map[string]string{
		"host1": "foo",
		"host2": "bar",
	}
	conn.Txn(db.ContainerTable).Run(func(view db.Database) error {
		for host, status := range containerToStatus {
			dbc := view.InsertContainer()
			dbc.Hostname = host
			dbc.Status = status
			view.Commit(dbc)
		}
		return nil
	})

	podsClient.On("List", mock.Anything).Return(nil, assert.AnError).Once()
	updateContainerStatuses(conn, podsClient, nil)

	conn.Txn(db.ContainerTable).Run(func(view db.Database) error {
		dbcs := view.SelectFromContainer(nil)
		assert.Len(t, dbcs, len(containerToStatus))
		for _, dbc := range dbcs {
			expStatus, ok := containerToStatus[dbc.Hostname]
			assert.True(t, ok)
			assert.Equal(t, expStatus, dbc.Status)
		}
		return nil
	})
}

func TestStatusForPod(t *testing.T) {
	t.Parallel()

	mockCreatedTime := time.Now()
	tests := []struct {
		expStatus      string
		expCreatedTime time.Time
		pod            corev1.Pod
	}{{
		expStatus: "waiting: pulling image",
		pod: corev1.Pod{
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "pulling image",
						},
					}},
				},
			},
		},
	}, {
		expStatus:      "running",
		expCreatedTime: mockCreatedTime,
		pod: corev1.Pod{
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.Time{
								Time: mockCreatedTime,
							},
						},
					}},
				},
			},
		},
	}, {
		expStatus: "terminated: exited",
		pod: corev1.Pod{Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						Reason: "exited",
					},
				}},
			},
		},
		},
	}, {
		expStatus: "scheduled",
		pod: corev1.Pod{
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{Status: corev1.ConditionTrue,
						Type: corev1.PodScheduled},
				},
			},
		},
	}, {
		// "Running" should supersede "scheduled".
		expStatus:      "running",
		expCreatedTime: mockCreatedTime,
		pod: corev1.Pod{
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{Status: corev1.ConditionTrue,
						Type: corev1.PodScheduled},
				},
				ContainerStatuses: []corev1.ContainerStatus{
					{State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.Time{
								Time: mockCreatedTime,
							},
						},
					}},
				},
			},
		},
	}, {
		expStatus: "unrecognized container state",
		pod: corev1.Pod{
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{State: corev1.ContainerState{}},
				},
			},
		},
	}, {
		expStatus: "no status information",
		pod:       corev1.Pod{Status: corev1.PodStatus{}},
	}}

	for _, test := range tests {
		actualStatus, actualCreatedTime := statusForPodImpl(test.pod)
		assert.Equal(t, test.expStatus, actualStatus)
		assert.Equal(t, test.expCreatedTime, actualCreatedTime)
	}
}

func TestStatusForContainer(t *testing.T) {
	t.Parallel()

	buildingDockerfile := "buildingDockerfile"
	buildingImage := "buildingImage"
	builtDockerfile := "builtDockerfile"
	builtImage := "builtImage"

	secrets := map[string]string{
		"defined": "val",
	}
	images := []db.Image{
		{Name: buildingImage, Dockerfile: buildingDockerfile,
			Status: db.Building},
		{Name: builtImage, Dockerfile: builtDockerfile, Status: db.Built},
	}

	// Test building status.
	checkStatusForContainer(t, db.Container{
		Hostname:   "hostname",
		Dockerfile: buildingDockerfile,
		Image:      buildingImage,
	}, images, secrets, "building")

	// Building should supersede secrets status.
	checkStatusForContainer(t, db.Container{
		Hostname: "hostname",
		FilepathToContent: map[string]blueprint.ContainerValue{
			"foo": blueprint.NewSecret("defined"),
		},
		Dockerfile: buildingDockerfile,
		Image:      buildingImage,
	}, images, secrets, "building")

	// Test built status.
	checkStatusForContainer(t, db.Container{
		Hostname:   "hostname",
		Dockerfile: builtDockerfile,
		Image:      builtImage,
	}, images, secrets, "built")

	// Test waiting for secrets.
	checkStatusForContainer(t, db.Container{
		Hostname: "hostname",
		FilepathToContent: map[string]blueprint.ContainerValue{
			"foo": blueprint.NewSecret("undefined"),
			"bar": blueprint.NewSecret("undefined2"),
			"baz": blueprint.NewSecret("defined"),
		},
	}, nil, secrets, "Waiting for secrets: [undefined undefined2]")
}

func checkStatusForContainer(t *testing.T, dbc db.Container, images []db.Image,
	secrets map[string]string, expStatus string) {

	imageMap := map[db.Image]db.Image{}
	for _, img := range images {
		imageMap[db.Image{
			Name:       img.Name,
			Dockerfile: img.Dockerfile,
		}] = img
	}

	secretClient := &mocks.SecretClient{}
	testSecretUndefined := func(secretName string) bool {
		if secrets == nil {
			return true
		}
		_, exists := secrets[secretName]
		return !exists
	}
	secretClient.On("Get", mock.MatchedBy(testSecretUndefined)).
		Return("", errors.New("secret does not exist"))
	for name, val := range secrets {
		secretClient.On("Get", name).Return(val, nil)
	}

	actualStatus := statusForContainerImpl(imageMap, secretClient, dbc)
	assert.Equal(t, expStatus, actualStatus)
}

func TestJoinContainersToPods(t *testing.T) {
	t.Parallel()

	// A container that will be matched up with a pod.
	matchContainer := db.Container{
		Hostname: "hostname1",
		Image:    "custom-image",
		Command:  []string{"arg1", "arg2"},
		FilepathToContent: map[string]blueprint.ContainerValue{
			"key": blueprint.NewString("value"),
		},
		IP: "ignored",
	}

	// A container that won't be matched up with a pod.
	unmatchedContainerA := db.Container{
		Hostname: "hostname2",
		Image:    "no-matching-pod",
		Command:  []string{"args"},
		IP:       "ignored",
	}

	// Convert the to-be-matched container into a pod, and convert the
	// container that shouldn't be matched into a pod with a different command.
	unmatchedContainerB := unmatchedContainerA
	unmatchedContainerB.Command = []string{"different", "args"}
	pods, ok := dbcsToPods([]db.Container{matchContainer, unmatchedContainerB})
	assert.True(t, ok)

	// Also test a malformed pod without any containers.
	pods = append(pods, corev1.Pod{})

	pairs, noInfoContainers := joinContainersToPodsImpl([]db.Container{
		matchContainer, unmatchedContainerA,
	}, pods)
	assert.Equal(t, []join.Pair{{L: matchContainer, R: pods[0]}}, pairs)
	assert.Equal(t, []interface{}{unmatchedContainerA}, noInfoContainers)
}

func dbcsToPods(dbcs []db.Container) (pods []corev1.Pod, ok bool) {
	builder := newDeploymentBuilder(nil, nil, nil)
	for _, dbc := range dbcs {
		deployment, ok := builder.makeDeploymentForContainer(dbc)
		if !ok {
			return nil, false
		}

		pods = append(pods, corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: deployment.Spec.Template.Annotations,
			},
			Spec: deployment.Spec.Template.Spec,
		})
	}
	return pods, true
}
