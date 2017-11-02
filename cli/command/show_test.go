package command

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	units "github.com/docker/go-units"
	"github.com/stretchr/testify/assert"

	"github.com/kelda/kelda/api/client/mocks"
	"github.com/kelda/kelda/db"
)

func TestShowFlags(t *testing.T) {
	t.Parallel()

	expHost := "IP"

	cmd := NewShowCommand()
	err := parseHelper(cmd, []string{"-H", expHost})

	assert.NoError(t, err)
	assert.Equal(t, expHost, cmd.host)

	cmd = NewShowCommand()
	err = parseHelper(cmd, []string{"-no-trunc"})

	assert.NoError(t, err)
	assert.True(t, cmd.noTruncate)
}

func TestShowErrors(t *testing.T) {
	t.Parallel()

	mockErr := errors.New("error")

	// Error querying containers
	mockClient := new(mocks.Client)
	mockClient.On("QueryConnections").Return(nil, nil)
	mockClient.On("QueryMachines").Return([]db.Machine{{Status: db.Connected}}, nil)
	mockClient.On("QueryContainers").Return(nil, mockErr)
	mockClient.On("QueryImages").Return(nil, nil)
	cmd := &Show{false, connectionHelper{client: mockClient}}
	assert.EqualError(t, cmd.run(), "unable to query containers: error")

	// Error querying connections from LeaderClient
	mockClient = new(mocks.Client)
	mockClient.On("QueryContainers").Return(nil, nil)
	mockClient.On("QueryMachines").Return([]db.Machine{{Status: db.Connected}}, nil)
	mockClient.On("QueryConnections").Return(nil, mockErr)
	mockClient.On("QueryImages").Return(nil, nil)
	cmd = &Show{false, connectionHelper{client: mockClient}}
	assert.EqualError(t, cmd.run(), "unable to query connections: error")
}

// Test that we don't query the cluster if it's not up.
func TestMachineOnly(t *testing.T) {
	t.Parallel()

	mockClient := new(mocks.Client)
	cmd := &Show{false, connectionHelper{client: mockClient}}

	// Test failing to query machines.
	mockClient.On("QueryMachines").Once().Return(nil, assert.AnError)
	cmd.run()
	mockClient.AssertNotCalled(t, "QueryContainers")

	// Test no machines in database.
	mockClient.On("QueryMachines").Once().Return(nil, nil)
	cmd.run()
	mockClient.AssertNotCalled(t, "QueryContainers")

	// Test no connected machines.
	mockClient.On("QueryMachines").Once().Return(
		[]db.Machine{{Status: db.Booting}}, nil)
	cmd.run()
	mockClient.AssertNotCalled(t, "QueryContainers")
}

func TestShowSuccess(t *testing.T) {
	t.Parallel()

	mockClient := new(mocks.Client)
	mockClient.On("QueryContainers").Return(nil, nil)
	mockClient.On("QueryMachines").Return(nil, nil)
	mockClient.On("QueryConnections").Return(nil, nil)
	mockClient.On("QueryImages").Return(nil, nil)
	cmd := &Show{false, connectionHelper{client: mockClient}}
	assert.Equal(t, 0, cmd.Run())
}

func TestMachineOutput(t *testing.T) {
	t.Parallel()

	machines := []db.Machine{
		{
			CloudID:  "1",
			Role:     db.Master,
			Provider: "Amazon",
			Region:   "us-west-1",
			Size:     "m4.large",
			PublicIP: "8.8.8.8",
			Status:   db.Connected,
		}, {
			CloudID:    "2",
			Role:       db.Worker,
			Provider:   "DigitalOcean",
			Region:     "sfo1",
			Size:       "2gb",
			PublicIP:   "9.9.9.9",
			FloatingIP: "10.10.10.10",
			Status:     db.Connected,
		},
	}

	var b bytes.Buffer
	writeMachines(&b, machines)
	result := string(b.Bytes())

	/* By replacing space with underscore, we make the spaces explicit and whitespace
	* errors easier to debug. */
	result = strings.Replace(result, " ", "_", -1)

	exp := `MACHINE____ROLE______PROVIDER________REGION_______SIZE` +
		`________PUBLIC_IP______STATUS
1__________Master____Amazon__________us-west-1____m4.large____8.8.8.8________connected
2__________Worker____DigitalOcean____sfo1_________2gb_________10.10.10.10____connected
`

	assert.Equal(t, exp, result)
}

func checkContainerOutput(t *testing.T, containers []db.Container,
	machines []db.Machine, connections []db.Connection, images []db.Image,
	truncate bool, exp string) {

	var b bytes.Buffer
	writeContainers(&b, containers, machines, connections, images, truncate)

	/* By replacing space with underscore, we make the spaces explicit and whitespace
	* errors easier to debug. */
	result := strings.Replace(b.String(), " ", "_", -1)
	assert.Equal(t, exp, result)
}

func TestContainerOutput(t *testing.T) {
	t.Parallel()

	containers := []db.Container{
		{ID: 1, BlueprintID: "3", Minion: "3.3.3.3", IP: "1.2.3.4",
			Image: "image1", Command: []string{"cmd", "1"},
			Hostname: "notpublic", Status: "running"},
		{ID: 2, BlueprintID: "1", Minion: "1.1.1.1", Image: "image2",
			Status: "scheduled", Hostname: "frompublic1"},
		{ID: 3, BlueprintID: "4", Minion: "1.1.1.1", Image: "image3",
			Command:  []string{"cmd"},
			Hostname: "frompublic2",
			Status:   "scheduled"},
		{ID: 4, BlueprintID: "7", Minion: "2.2.2.2", Image: "image1",
			Command:  []string{"cmd", "3", "4"},
			Hostname: "frompublic3"},
		{ID: 5, BlueprintID: "8", Image: "image1"},
	}
	machines := []db.Machine{
		{CloudID: "5", PublicIP: "7.7.7.7", PrivateIP: "1.1.1.1"},
		{CloudID: "6", PrivateIP: "2.2.2.2"},
		{CloudID: "7", PrivateIP: ""},
	}
	connections := []db.Connection{
		{ID: 1, From: []string{"public"}, To: []string{"frompublic1"},
			MinPort: 80, MaxPort: 80},
		{ID: 1, From: []string{"public", "notpublic"},
			To: []string{"frompublic2"}, MinPort: 80, MaxPort: 80},
		{ID: 1, From: []string{"public"}, To: []string{"frompublic3"},
			MinPort: 80, MaxPort: 80},
		{ID: 2, From: []string{"notpublic", "frompublic2"},
			To: []string{"frompublic1"}, MinPort: 100, MaxPort: 101},
	}

	expected := `CONTAINER____MACHINE____COMMAND___________HOSTNAME_______` +
		`STATUS_______CREATED____PUBLIC_IP
3_______________________image1_cmd_1______notpublic______running_________________
_________________________________________________________________________________
1____________5__________image2____________frompublic1____scheduled_______________` +
		`7.7.7.7:80
4____________5__________image3_cmd________frompublic2____scheduled_______________` +
		`7.7.7.7:80
_________________________________________________________________________________
7____________6__________image1_cmd_3_4____frompublic3____scheduled_______________
_________________________________________________________________________________
8____________7__________image1___________________________________________________
`
	checkContainerOutput(t, containers, machines, connections, nil, true, expected)

	// Testing writeContainers with created time values.
	mockTime := time.Now()
	humanDuration := units.HumanDuration(time.Since(mockTime))
	mockCreatedString := fmt.Sprintf("%s ago", humanDuration)
	mockCreatedString = strings.Replace(mockCreatedString, " ", "_", -1)

	containers = []db.Container{
		{ID: 1, BlueprintID: "3", Minion: "3.3.3.3", IP: "1.2.3.4",
			Image: "image1", Command: []string{"cmd", "1"},
			Status: "running", Created: mockTime.UTC()},
	}
	machines = []db.Machine{}
	connections = []db.Connection{}

	expected = `CONTAINER____MACHINE____COMMAND_________HOSTNAME____` +
		`STATUS_____CREATED___________________PUBLIC_IP
3_______________________image1_cmd_1________________running____` +
		mockCreatedString + `____
`
	checkContainerOutput(t, containers, machines, connections, nil, true, expected)

	// Testing writeContainers with longer durations.
	mockDuration := time.Hour
	mockTime = time.Now().Add(-mockDuration)
	humanDuration = units.HumanDuration(time.Since(mockTime))
	mockCreatedString = fmt.Sprintf("%s ago", humanDuration)
	mockCreatedString = strings.Replace(mockCreatedString, " ", "_", -1)

	containers = []db.Container{
		{ID: 1, BlueprintID: "3", Minion: "3.3.3.3", IP: "1.2.3.4",
			Image: "image1", Command: []string{"cmd", "1"},
			Status: "running", Created: mockTime.UTC()},
	}
	machines = []db.Machine{}
	connections = []db.Connection{}

	expected = `CONTAINER____MACHINE____COMMAND_________HOSTNAME____` +
		`STATUS_____CREATED______________PUBLIC_IP
3_______________________image1_cmd_1________________running____` +
		mockCreatedString + `____
`
	checkContainerOutput(t, containers, machines, connections, nil, true, expected)

	// Test that long outputs are truncated when `truncate` is true
	containers = []db.Container{
		{ID: 1, BlueprintID: "3", Minion: "3.3.3.3", IP: "1.2.3.4",
			Image: "image1", Command: []string{"cmd", "1", "&&", "cmd",
				"91283403472903847293014320984723908473248-23843984"},
			Status: "running", Created: mockTime.UTC()},
	}
	machines = []db.Machine{}
	connections = []db.Connection{}

	expected = `CONTAINER____MACHINE____COMMAND______________________________` +
		`HOSTNAME____STATUS_____CREATED______________PUBLIC_IP
3_______________________image1_cmd_1_&&_cmd_9128340347...________________running____` +
		mockCreatedString + `____
`
	checkContainerOutput(t, containers, machines, connections, nil, true, expected)

	// Test that long outputs are not truncated when `truncate` is false
	expected = `CONTAINER____MACHINE____COMMAND_________________________________` +
		`__________________________________HOSTNAME____STATUS_____CREATED` +
		`______________PUBLIC_IP
3_______________________image1_cmd_1_&&_cmd_912834034729038472930143209847239084` +
		`73248-23843984________________running____` + mockCreatedString + `____
`
	checkContainerOutput(t, containers, machines, connections, nil, false, expected)

	// Test writing container that has multiple connections to the public
	// internet.
	containers = []db.Container{{
		BlueprintID: "3",
		Minion:      "1.1.1.1",
		Image:       "image1",
		Hostname:    "frompub",
	}}
	machines = []db.Machine{
		{CloudID: "5", PublicIP: "7.7.7.7", PrivateIP: "1.1.1.1"},
	}
	connections = []db.Connection{
		{ID: 1, From: []string{"public"}, To: []string{"frompub"},
			MinPort: 80, MaxPort: 80},
		{ID: 2, From: []string{"public"}, To: []string{"frompub"},
			MinPort: 100, MaxPort: 101},
	}

	expected = `CONTAINER____MACHINE____COMMAND____HOSTNAME____STATUS_______` +
		`CREATED____PUBLIC_IP
3____________5__________image1_____frompub_____scheduled_______________` +
		`7.7.7.7:[80,100-101]
`
	checkContainerOutput(t, containers, machines, connections, nil, true, expected)
}

func TestContainerOutputCustomImage(t *testing.T) {
	t.Parallel()

	// Building.
	containers := []db.Container{
		{BlueprintID: "3", Image: "custom-dockerfile"},
	}
	images := []db.Image{
		{Name: "custom-dockerfile", Status: db.Building},
	}

	exp := `CONTAINER____MACHINE____COMMAND_______________HOSTNAME____STATUS` +
		`______CREATED____PUBLIC_IP
3_______________________custom-dockerfile_________________building_______________
`
	checkContainerOutput(t, containers, nil, nil, images, true, exp)

	// Built, but not scheduled.
	images = []db.Image{
		{Name: "custom-dockerfile", Status: db.Built},
	}
	exp = `CONTAINER____MACHINE____COMMAND_______________HOSTNAME____STATUS` +
		`____CREATED____PUBLIC_IP
3_______________________custom-dockerfile_________________built________________
`
	checkContainerOutput(t, containers, nil, nil, images, true, exp)

	// We only have image data on a different image, so we can't update the status.
	images = []db.Image{
		{Name: "ignoreme", Status: db.Built},
	}
	exp = `CONTAINER____MACHINE____COMMAND_______________HOSTNAME____STATUS` +
		`____CREATED____PUBLIC_IP
3_______________________custom-dockerfile______________________________________
`
	checkContainerOutput(t, containers, nil, nil, images, true, exp)

	// Built and scheduled.
	images = []db.Image{
		{Name: "custom-dockerfile", Status: db.Built},
	}
	containers = []db.Container{
		{BlueprintID: "3", Image: "custom-dockerfile", Minion: "foo"},
	}
	exp = `CONTAINER____MACHINE____COMMAND_______________HOSTNAME____STATUS` +
		`_______CREATED____PUBLIC_IP
3_______________________custom-dockerfile_________________scheduled_______________
`
	checkContainerOutput(t, containers, nil, nil, images, true, exp)
}

func TestContainerStr(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", containerStr("", nil, false))
	assert.Equal(t, "", containerStr("", []string{"arg0"}, false))
	assert.Equal(t, "container arg0 arg1",
		containerStr("container", []string{"arg0", "arg1"}, false))
}

func TestPublicIPStr(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", publicIPStr(db.Machine{}, nil))
	assert.Equal(t, "", publicIPStr(db.Machine{}, []string{"80-88"}))
	assert.Equal(t, "", publicIPStr(db.Machine{PublicIP: "1.2.3.4"}, nil))
	assert.Equal(t, "1.2.3.4:80-88",
		publicIPStr(db.Machine{PublicIP: "1.2.3.4"}, []string{"80-88"}))
	assert.Equal(t, "1.2.3.4:[70,80-88]",
		publicIPStr(db.Machine{PublicIP: "1.2.3.4"}, []string{"70", "80-88"}))
	assert.Equal(t, "8.8.8.8:[70,80-88]",
		publicIPStr(db.Machine{PublicIP: "1.2.3.4", FloatingIP: "8.8.8.8"},
			[]string{"70", "80-88"}))
}
