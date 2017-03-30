package stitch

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"github.com/quilt/quilt/util"
)

func TestMachine(t *testing.T) {
	t.Parallel()

	checkMachines(t, `deployment.deploy([new Machine({
		role: "Worker",
		provider: "Amazon",
		region: "us-west-2",
		size: "m4.large",
		cpu: new Range(2, 4),
		ram: new Range(4, 8),
		diskSize: 32,
		sshKeys: ["key1", "key2"]
	})])`,
		[]Machine{
			{
				ID:          "0dba3e899652cc7280485c75ad82e7ac7fd6b26e",
				Role:        "Worker",
				Provider:    "Amazon",
				Region:      "us-west-2",
				Size:        "m4.large",
				CPU:         Range{2, 4},
				RAM:         Range{4, 8},
				DiskSize:    32,
				SSHKeys:     []string{"key1", "key2"},
				Preemptible: false,
			}})

	// Check that changing the SSH keys doesn't change the hash.
	checkMachines(t, `deployment.deploy([new Machine({
		role: "Worker",
		provider: "Amazon",
		region: "us-west-2",
		size: "m4.large",
		cpu: new Range(2, 4),
		ram: new Range(4, 8),
		diskSize: 32,
		sshKeys: ["key3"]
	})])`,
		[]Machine{
			{
				ID:          "0dba3e899652cc7280485c75ad82e7ac7fd6b26e",
				Role:        "Worker",
				Provider:    "Amazon",
				Region:      "us-west-2",
				Size:        "m4.large",
				CPU:         Range{2, 4},
				RAM:         Range{4, 8},
				DiskSize:    32,
				SSHKeys:     []string{"key3"},
				Preemptible: false,
			}})

	checkMachines(t, `var baseMachine = new Machine({provider: "Amazon"});
		deployment.deploy(baseMachine.asMaster().replicate(2));`,
		[]Machine{
			{
				ID:          "4a364f325f7143db589a507cd3defb41a385d1bd",
				Role:        "Master",
				Provider:    "Amazon",
				SSHKeys:     []string{},
				Preemptible: false,
			},
			{
				ID:          "6595fa48111eec0c9cef2367f370b20f96d4a38c",
				Role:        "Master",
				Provider:    "Amazon",
				SSHKeys:     []string{},
				Preemptible: false,
			},
		},
	)

	checkMachines(t, `var baseMachine = new Machine({provider: "Amazon"});
		var machines = baseMachine.asMaster().replicate(2);
		machines[0].sshKeys.push("key");
		deployment.deploy(machines);`,
		[]Machine{
			{
				ID:          "4a364f325f7143db589a507cd3defb41a385d1bd",
				Role:        "Master",
				Provider:    "Amazon",
				SSHKeys:     []string{"key"},
				Preemptible: false,
			},
			{
				ID:          "6595fa48111eec0c9cef2367f370b20f96d4a38c",
				Role:        "Master",
				Provider:    "Amazon",
				SSHKeys:     []string{},
				Preemptible: false,
			},
		},
	)

	checkMachines(t, `var baseMachine = new Machine({
	  provider: "Amazon",
	  floatingIp: "xxx.xxx.xxx.xxx"
	});
	deployment.deploy(baseMachine.asMaster());`,
		[]Machine{
			{
				ID:          "632f9edd209f2fcda1e9d407832f169abf66a1a2",
				Role:        "Master",
				Provider:    "Amazon",
				FloatingIP:  "xxx.xxx.xxx.xxx",
				SSHKeys:     []string{},
				Preemptible: false,
			},
		})

	checkMachines(t, `var baseMachine = new Machine({
	  provider: "Amazon",
	  preemptible: true
	});
	deployment.deploy(baseMachine.asMaster());`,
		[]Machine{
			{
				ID:          "8a0d2198229c09b8b5ec1bdba7105a9e08f8ef0b",
				Role:        "Master",
				Provider:    "Amazon",
				SSHKeys:     []string{},
				Preemptible: true,
			},
		})
}

func TestContainer(t *testing.T) {
	t.Parallel()

	expContainers := map[string]Container{
		"3f0028780b8d9e35ae8c02e4e8b87e2ca55305db": {
			ID: "3f0028780b8d9e35ae8c02e4e8b87e2ca55305db",
			Image: Image{
				Name: "image",
			},
			Command:           []string{"arg1", "arg2"},
			Env:               map[string]string{"foo": "bar"},
			FilepathToContent: map[string]string{"qux": "quuz"},
		},
	}
	checkContainers(t, `deployment.deploy(new Service("foo", [
	new Container("image", ["arg1", "arg2"]).withEnv({"foo": "bar"}).
		withFiles({"qux": "quuz"})
	]));`, expContainers)

	expContainers = map[string]Container{
		"45b5015830c4b8fb738d15c7a2822ee108c20bd8": {
			ID: "45b5015830c4b8fb738d15c7a2822ee108c20bd8",
			Image: Image{
				Name: "image",
			},
			Command:           []string{"arg1", "arg2"},
			Env:               map[string]string{},
			FilepathToContent: map[string]string{},
		},
	}
	checkContainers(t, `deployment.deploy(new Service("foo", [
	new Container("image", ["arg1", "arg2"])
	]));`, expContainers)

	expContainers = map[string]Container{
		"475c40d6070969839ba0f88f7a9bd0cc7936aa30": {
			ID: "475c40d6070969839ba0f88f7a9bd0cc7936aa30",
			Image: Image{
				Name: "image",
			},
			Command:           []string{},
			Env:               map[string]string{},
			FilepathToContent: map[string]string{},
		},
	}
	checkContainers(t, `deployment.deploy(
		new Service("foo", [
		new Container("image")
		])
	);`, expContainers)

	expContainers = map[string]Container{
		"f54486d0f95b4cc478952a1a775a0699d8f5d959": {
			ID: "f54486d0f95b4cc478952a1a775a0699d8f5d959",
			Image: Image{
				Name: "image",
			},
			Command:           []string{},
			Env:               map[string]string{"foo": "bar"},
			FilepathToContent: map[string]string{},
		},
	}
	checkContainers(t, `var c = new Container("image");
	c.env["foo"] = "bar";
	deployment.deploy(new Service("foo", [c]));`, expContainers)

	expContainers = map[string]Container{
		"6563036090dd1a6d4a2fe2f56a31e61c2cdca8e2": {
			ID: "6563036090dd1a6d4a2fe2f56a31e61c2cdca8e2",
			Image: Image{
				Name: "image",
			},
			Command:           []string{"arg"},
			Env:               map[string]string{},
			FilepathToContent: map[string]string{},
		},
		"c6cea5bd411c6e5afac755c37517af89a2d03dbe": {
			ID: "c6cea5bd411c6e5afac755c37517af89a2d03dbe",
			Image: Image{
				Name: "image",
			},
			Command:           []string{"arg"},
			Env:               map[string]string{},
			FilepathToContent: map[string]string{},
		},
	}
	checkContainers(t, `deployment.deploy(
		new Service("foo", new Container("image", ["arg"]).replicate(2))
	);`, expContainers)

	expContainers = map[string]Container{
		"1dc23744f60b5c2fb7e3eafb3e8a7e3b085b9b9c": {
			ID: "1dc23744f60b5c2fb7e3eafb3e8a7e3b085b9b9c",
			Image: Image{
				Name:       "name",
				Dockerfile: "dockerfile",
			},
			Command:           []string{},
			Env:               map[string]string{},
			FilepathToContent: map[string]string{},
		},
	}
	checkContainers(t, `var c = new Container(new Image("name", "dockerfile"));
	deployment.deploy(new Service("foo", [c]));`, expContainers)

	// Test changing attributes of replicated container.
	expContainers = map[string]Container{
		"c3371b4dec2600f20cd8cc5b59bc116dedcbea92": {
			ID: "c3371b4dec2600f20cd8cc5b59bc116dedcbea92",
			Image: Image{
				Name: "image",
			},
			Command: []string{"arg", "changed"},
			Env: map[string]string{
				"foo": "bar",
			},
			FilepathToContent: map[string]string{},
		},
		"6563036090dd1a6d4a2fe2f56a31e61c2cdca8e2": {
			ID: "6563036090dd1a6d4a2fe2f56a31e61c2cdca8e2",
			Image: Image{
				Name: "image",
			},
			Command:           []string{"arg"},
			Env:               map[string]string{},
			FilepathToContent: map[string]string{},
		},
	}
	checkContainers(t, `var repl = new Container("image", ["arg"]).replicate(2);
	repl[0].env["foo"] = "bar";
	repl[0].command.push("changed");
	deployment.deploy(
		new Service("baz", repl)
	);`, expContainers)

	expContainers = map[string]Container{
		"475c40d6070969839ba0f88f7a9bd0cc7936aa30": {
			ID: "475c40d6070969839ba0f88f7a9bd0cc7936aa30",
			Image: Image{
				Name: "image",
			},
			Command:           []string{},
			Env:               map[string]string{},
			FilepathToContent: map[string]string{},
			Hostname:          "host",
		},
	}
	checkContainers(t, `var c = new Container("image");
	c.setHostname("host");
	deployment.deploy(new Service("foo", [c]));`, expContainers)

	checkJavascript(t, `(function() {
		var c = new Container("image");
		c.setHostname("host");
		return c.getHostname();
	})()`, "host.q")

	checkError(t, `var a = new Container("image");
	a.setHostname("host");
	var b = new Container("image");
	b.setHostname("host");
	deployment.deploy(new Service("foo", [a, b]))`,
		`hostname "host" used for multiple containers`)
}

func TestPlacement(t *testing.T) {
	t.Parallel()

	pre := `var target = new Service("target", []);
	var other = new Service("other", []);`
	post := `deployment.deploy(target);
	deployment.deploy(other);`
	checkPlacements(t, pre+`target.place(new LabelRule(true, other));`+post,
		[]Placement{
			{
				TargetLabel: "target",
				OtherLabel:  "other",
				Exclusive:   true,
			},
		})

	checkPlacements(t, pre+`target.place(new MachineRule(true,
	{size: "m4.large",
	region: "us-west-2",
	provider: "Amazon"}));`+post,
		[]Placement{
			{
				TargetLabel: "target",
				Exclusive:   true,
				Region:      "us-west-2",
				Provider:    "Amazon",
				Size:        "m4.large",
			},
		})

	checkPlacements(t, pre+`target.place(new MachineRule(true,
	{size: "m4.large",
	provider: "Amazon"}));`+post,
		[]Placement{
			{
				TargetLabel: "target",
				Exclusive:   true,
				Provider:    "Amazon",
				Size:        "m4.large",
			},
		})

	checkPlacements(t, pre+`target.place(new MachineRule(false,
	{floatingIp: "xxx.xxx.xxx.xxx"}));`+post,
		[]Placement{
			{
				TargetLabel: "target",
				Exclusive:   false,
				FloatingIP:  "xxx.xxx.xxx.xxx",
			},
		})
}

func TestLabel(t *testing.T) {
	t.Parallel()

	checkLabels(t, `deployment.deploy(
		new Service("web_tier", [new Container("nginx")])
	);`,
		map[string]Label{
			"web_tier": {
				Name: "web_tier",
				IDs: []string{
					"c47b5770b59a4459519ba2b3ae3cd7a1598fbd8d",
				},
				Annotations: []string{},
			},
		})

	checkLabels(t, `deployment.deploy(
		new Service("web_tier", [
		new Container("nginx"),
		new Container("nginx")
		])
	);`,
		map[string]Label{
			"web_tier": {
				Name: "web_tier",
				IDs: []string{
					"c47b5770b59a4459519ba2b3ae3cd7a1598fbd8d",
					"6044e40ba6e4d97be45ca290b993ef2f368c7bb1",
				},
				Annotations: []string{},
			},
		})

	// Conflicting label names.
	// We need to generate a couple of dummy containers so that the two
	// deployed containers have _refID's that are sorted differently lexicographically
	// and numerically.
	checkLabels(t, `for (var i = 0; i < 2; i++) new Container("image");
	deployment.deploy(new Service("foo", [new Container("image")]));
	for (var i = 0; i < 7; i++) new Container("image");
	deployment.deploy(new Service("foo", [new Container("image")]));`,
		map[string]Label{
			"foo": {
				Name: "foo",
				IDs: []string{
					"475c40d6070969839ba0f88f7a9bd0cc7936aa30",
				},
				Annotations: []string{},
			},
			"foo2": {
				Name: "foo2",
				IDs: []string{
					"3047630375a1621cb400811b795757a07de8e268",
				},
				Annotations: []string{},
			},
		})

	expHostname := "foo.q"
	checkJavascript(t, `(function() {
		var foo = new Service("foo", []);
		return foo.hostname();
	})()`, expHostname)

	expChildren := []string{"1.foo.q", "2.foo.q"}
	checkJavascript(t, `(function() {
		var foo = new Service("foo",
		[new Container("bar"), new Container("baz")]);
		return foo.children();
	})()`, expChildren)
}

func TestConnect(t *testing.T) {
	t.Parallel()

	pre := `var foo = new Service("foo", []);
	var bar = new Service("bar", []);
	deployment.deploy([foo, bar]);`

	checkConnections(t, pre+`foo.connect(new Port(80), bar);`,
		[]Connection{
			{
				From:    "foo",
				To:      "bar",
				MinPort: 80,
				MaxPort: 80,
			},
		})

	checkConnections(t, pre+`foo.connect(new PortRange(80, 85), bar);`,
		[]Connection{
			{
				From:    "foo",
				To:      "bar",
				MinPort: 80,
				MaxPort: 85,
			},
		})

	checkConnections(t, pre+`foo.connect(80, publicInternet);`,
		[]Connection{
			{
				From:    "foo",
				To:      "public",
				MinPort: 80,
				MaxPort: 80,
			},
		})

	checkConnections(t, pre+`foo.connect(80, publicInternet);`,
		[]Connection{
			{
				From:    "foo",
				To:      "public",
				MinPort: 80,
				MaxPort: 80,
			},
		})

	checkConnections(t, pre+`publicInternet.connect(80, foo);`,
		[]Connection{
			{
				From:    "public",
				To:      "foo",
				MinPort: 80,
				MaxPort: 80,
			},
		})

	checkError(t, pre+`foo.connect(new PortRange(80, 81), publicInternet);`,
		"public internet cannot connect on port ranges")
	checkError(t, pre+`publicInternet.connect(new PortRange(80, 81), foo);`,
		"public internet cannot connect on port ranges")
}

func TestVet(t *testing.T) {
	pre := `var foo = new Service("foo", []);
	deployment.deploy([foo]);`

	// Connect to undeployed label.
	checkError(t, pre+`foo.connect(80, new Service("baz", []));`,
		"foo has a connection to undeployed service: baz")

	checkError(t, pre+`foo.place(new MachineRule(false, {
			provider: "Amazon"
		}));
	foo.place(new LabelRule(true, new Service("baz", [])));`,
		"foo has a placement in terms of an undeployed service: baz")

	checkError(t, `
		var foo = new Service("foo", new Container("image").replicate(2));
		foo.place(new MachineRule(false, {
			floatingIp: "123",
		}));
		foo.connectFromPublic(80);
		deployment.deploy([foo]);
	`, "foo has a floating IP and multiple containers. This is "+
		"not yet supported.")

	checkError(t, `
		deployment.deploy(new Service("foo",
			[new Container(new Image("img", "dk"))]
		));
		deployment.deploy(new Service("foo",
			[new Container(new Image("img", "dk"))]
		));
	`, "")

	checkError(t, `
		deployment.deploy(new Service("foo",
			[new Container(new Image("img", "dk"))]
		));
		deployment.deploy(new Service("foo",
			[new Container(new Image("img", "dk2"))]
		));
	`, "img has differing Dockerfiles")
}

func TestCustomDeploy(t *testing.T) {
	t.Parallel()

	checkLabels(t, `deployment.deploy(
		{
			deploy: function(deployment) {
				deployment.deploy([
				new Service("web_tier", [new Container("nginx")]),
				new Service("web_tier2", [new Container("nginx")])
			]);
			}
		}
	);`,
		map[string]Label{
			"web_tier": {
				Name: "web_tier",
				IDs: []string{
					"c47b5770b59a4459519ba2b3ae3cd7a1598fbd8d",
				},
				Annotations: []string{},
			},
			"web_tier2": {
				Name: "web_tier2",
				IDs: []string{
					"6044e40ba6e4d97be45ca290b993ef2f368c7bb1",
				},
				Annotations: []string{},
			},
		})

	checkError(t, `deployment.deploy({})`,
		`only objects that implement "deploy(deployment)" can be deployed`)
}

func TestCreateDeploymentNoArgs(t *testing.T) {
	checkError(t, "createDeployment()", "")
}

func TestRunModule(t *testing.T) {
	checkJavascript(t, `(function() {
		module.exports = function() {}
	})()`, nil)
}

func TestGithubKeys(t *testing.T) {
	HTTPGet = func(url string) (*http.Response, error) {
		resp := http.Response{
			Body: ioutil.NopCloser(bytes.NewBufferString("githubkeys")),
		}
		return &resp, nil
	}

	checkJavascript(t, `(function() {
		return githubKeys("username");
	})()`, []string{"githubkeys"})
}

func TestQuery(t *testing.T) {
	t.Parallel()

	namespaceChecker := queryChecker(func(handle Stitch) interface{} {
		return handle.Namespace
	})
	maxPriceChecker := queryChecker(func(handle Stitch) interface{} {
		return handle.MaxPrice
	})
	adminACLChecker := queryChecker(func(handle Stitch) interface{} {
		return handle.AdminACL
	})

	namespaceChecker(t, `createDeployment({namespace: "myNamespace"});`,
		"myNamespace")
	namespaceChecker(t, ``, "default-namespace")
	maxPriceChecker(t, `createDeployment({maxPrice: 5});`, 5.0)
	maxPriceChecker(t, ``, 0.0)
	adminACLChecker(t, `createDeployment({adminACL: ["local"]});`, []string{"local"})
	adminACLChecker(t, ``, []string{})
}

func TestMarshal(t *testing.T) {
	t.Parallel()

	exp := Stitch{
		Machines: []Machine{
			{
				Role:     "Master",
				Provider: "Amazon",
			},
			{
				Role:     "Worker",
				Provider: "Amazon",
			},
		},
	}

	actual, err := FromJSON(exp.String())
	assert.Nil(t, err)
	assert.Equal(t, exp, actual)
}

func TestHash(t *testing.T) {
	t.Parallel()

	checkJavascript(t, `hash("foo");`, "0beec7b5ea3f0fdbc95d0dd47f3c5bc275da8a33")
	checkError(t, `hash();`, "RangeError: hash requires an argument")
}

func TestRead(t *testing.T) {
	t.Parallel()

	util.AppFs = afero.NewMemMapFs()
	checkError(t, `read("foo");`, "StitchError: open foo: file does not exist")

	util.WriteFile("foo", []byte("bar"), 0644)
	checkJavascript(t, `read("foo");`, "bar")

	checkError(t, `read();`, "RangeError: no path supplied")
}

func TestReadDir(t *testing.T) {
	util.AppFs = afero.NewMemMapFs()
	checkError(t, `readDir("/foo");`, "StitchError: open /foo: file does not exist")

	util.AppFs.Mkdir("/foo", 0755)
	util.AppFs.Mkdir("/foo/bar", 0755)
	util.WriteFile("/foo/bar/baz", []byte("qux"), 0644)
	util.WriteFile("/foo/hello", []byte("world"), 0644)
	checkJavascript(t, `readDir("/foo");`, []map[string]interface{}{
		{"name": "bar", "isDir": true},
		{"name": "hello", "isDir": false},
	})
	checkJavascript(t, `readDir("/foo/bar");`, []map[string]interface{}{
		{"name": "baz", "isDir": false},
	})

	checkJavascript(t, `(function() {
		function walk(path, fn) {
			var files = readDir(path);
			for (var i = 0; i < files.length; i++) {
				var filePath = path + "/" + files[i].name;
				if (files[i].isDir) {
					walk(filePath, fn);
				} else {
					fn(filePath)
				}
			}
		}

		var files = {};
		walk("/foo", function(path) {
			files[path] = read(path);
		})

		return files;
	})();`, map[string]interface{}{
		"/foo/bar/baz": "qux",
		"/foo/hello":   "world",
	})

	checkContainers(t, `
	function walk(path, fn) {
		var files = readDir(path);
		for (var i = 0; i < files.length; i++) {
			var filePath = path + "/" + files[i].name;
			if (files[i].isDir) {
				walk(filePath, fn);
			} else {
				fn(filePath)
			}
		}
	}

	function chroot(root, files) {
		var chrooted = {};
		for (path in files) {
			chrooted[root + "/" + path] = files[path];
		}
		return chrooted;
	}

	var files = {};
	walk("/foo", function(path) {
		files[path] = read(path);
	})

	var c = new Container("quilt/bearmaps");
	c.filepathToContent = chroot("/src", files);
	deployment.deploy(new Service("ignoreme", [c]));`,
		map[string]Container{
			"720caf060f8fe7428d85b39d269a8810e905b3e5": {
				ID:      "720caf060f8fe7428d85b39d269a8810e905b3e5",
				Image:   Image{Name: "quilt/bearmaps"},
				Command: []string{},
				Env:     map[string]string{},
				FilepathToContent: map[string]string{
					// XXX: We need path.join, which is available in
					// node rather than just blindly concatenating
					// paths.
					"/src//foo/bar/baz": "qux",
					"/src//foo/hello":   "world",
				},
			},
		})
}

func checkJavascript(t *testing.T, code string, exp interface{}) {
	resultKey := "result"

	vm, err := newVM(ImportGetter{
		Path: ".",
	})
	if err != nil {
		t.Errorf(`Unexpected error: "%s".`, err.Error())
		return
	}

	exec := fmt.Sprintf(`exports.%s = %s;`, resultKey, code)
	moduleVal, err := runSpec(vm, "<test_code>", exec)
	if err != nil {
		t.Errorf(`Unexpected error: "%s".`, err.Error())
		return
	}

	actualVal, err := moduleVal.Object().Get(resultKey)
	if err != nil {
		t.Errorf(`Unexpected error retrieving result from VM: "%s".`,
			err.Error())
		return
	}

	actual, _ := actualVal.Export()
	if !reflect.DeepEqual(actual, exp) {
		t.Errorf("Bad javascript code: Expected %s, got %s.",
			spew.Sdump(exp), spew.Sdump(actual))
	}
}

func checkError(t *testing.T, code string, exp string) {
	_, err := FromJavascript(code, ImportGetter{
		Path: ".",
	})
	if err == nil {
		if exp != "" {
			t.Errorf(`Expected error "%s", but got nothing.`, exp)
		}
		return
	}
	if actual := err.Error(); actual != exp {
		t.Errorf(`Expected error "%s", but got "%s".`, exp, actual)
	}
}

func queryChecker(
	queryFunc func(Stitch) interface{}) func(*testing.T, string, interface{}) {

	return func(t *testing.T, code string, exp interface{}) {
		handle, err := FromJavascript(code, DefaultImportGetter)
		if err != nil {
			t.Errorf(`Unexpected error: "%s".`, err.Error())
			return
		}

		actual := queryFunc(handle)
		if !reflect.DeepEqual(actual, exp) {
			t.Errorf("Bad query: Expected %s, got %s.",
				spew.Sdump(exp), spew.Sdump(actual))
		}
	}
}

var checkMachines = queryChecker(func(s Stitch) interface{} {
	return s.Machines
})

var checkContainers = queryChecker(func(s Stitch) interface{} {
	// Convert the slice to a map because the ordering is non-deterministic.
	containersMap := make(map[string]Container)
	for _, c := range s.Containers {
		containersMap[c.ID] = c
	}
	return containersMap
})

var checkPlacements = queryChecker(func(s Stitch) interface{} {
	return s.Placements
})

var checkLabels = queryChecker(func(s Stitch) interface{} {
	// Convert the slice to a map because the ordering is non-deterministic.
	labelsMap := make(map[string]Label)
	for _, label := range s.Labels {
		labelsMap[label.Name] = label
	}
	return labelsMap
})

var checkConnections = queryChecker(func(s Stitch) interface{} {
	return s.Connections
})
