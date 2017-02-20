package vagrant

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/quilt/quilt/util"
	log "github.com/Sirupsen/logrus"
	homedir "github.com/mitchellh/go-homedir"
)

var vagrantCmd = "vagrant"
var shCmd = "sh"

const vagrantFile = `CLOUD_CONFIG_PATH = File.join(File.dirname(__FILE__), "user-data")
SIZE_PATH = File.join(File.dirname(__FILE__), "size")
Vagrant.require_version ">= 1.6.0"

size = File.open(SIZE_PATH).read.strip.split(",")
Vagrant.configure(2) do |config|
  config.vm.box = "boxcutter/ubuntu1604"

  config.vm.network "private_network", type: "dhcp"

  ram=(size[0].to_f*1024).to_i
  cpus=size[1]
  config.vm.provider "virtualbox" do |v|
    v.memory = ram
    v.cpus = cpus
  end

  if File.exist?(CLOUD_CONFIG_PATH)
    config.vm.provision "shell", path: "#{CLOUD_CONFIG_PATH}"
  end
end
`

func initMachine(cloudConfig string, size string, id string) error {
	vdir, err := vagrantDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(vdir); os.IsNotExist(err) {
		os.Mkdir(vdir, os.ModeDir|os.ModePerm)
	}
	path := vdir + id
	os.Mkdir(path, os.ModeDir|os.ModePerm)

	_, stderr, err := shell(id, `vagrant --machine-readable init coreos-beta`)
	if err != nil {
		log.Errorf("Failed to initialize Vagrant environment: %s", stderr)
		destroy(id)
		return errors.New("unable to init machine")
	}

	err = util.WriteFile(path+"/user-data", []byte(cloudConfig), 0644)
	if err != nil {
		destroy(id)
		return err
	}

	err = util.WriteFile(path+"/vagrantFile", []byte(vagrantFile), 0644)
	if err != nil {
		destroy(id)
		return err
	}

	err = util.WriteFile(path+"/size", []byte(size), 0644)
	if err != nil {
		destroy(id)
		return err
	}

	return nil
}

func up(id string) error {
	_, stderr, err := shell(id, `vagrant --machine-readable up`)
	if err != nil {
		log.Errorf("Failed to start Vagrant machine: %s", string(stderr))
		return errors.New("unable to start machine")
	}
	return nil
}

func destroy(id string) error {
	_, stderr, err := shell(id,
		`vagrant --machine-readable destroy -f; cd ../; rm -rf %s`)
	if err != nil {
		log.Errorf("Failed to destroy Vagrant machine: %s", string(stderr))
		return errors.New("unable to destroy machine")
	}
	return nil
}

func publicIP(id string) (string, error) {
	ip, stderr, err := shell(id,
		`vagrant ssh -c "ip -f inet addr show enp0s8 | grep -Po 'inet \K[\d.]+'"`)
	if err != nil {
		log.Errorf("Failed to parse Vagrant machine IP: %s", string(stderr))
		return "", err
	}
	return strings.TrimSuffix(string(ip), "\n"), nil
}

func status(id string) (string, error) {
	output, stderr, err := shell(id, `vagrant --machine-readable status`)
	if err != nil {
		log.Errorf("Failed to retrieve Vagrant machine status: %s",
			string(stderr))
		return "", errors.New("unable to retrieve machine status")
	}
	lines := bytes.Split(output, []byte("\n"))
	for _, line := range lines {
		words := strings.Split(string(line[:]), ",")
		if len(words) >= 4 {
			if strings.Compare(words[2], "state") == 0 {
				return words[3], nil
			}
		}
	}
	return "", nil
}

func list() ([]string, error) {
	subdirs := []string{}
	vdir, err := vagrantDir()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(vdir); os.IsNotExist(err) {
		return subdirs, nil
	}

	files, err := ioutil.ReadDir(vdir)
	if err != nil {
		return subdirs, err
	}
	for _, file := range files {
		subdirs = append(subdirs, file.Name())
	}
	return subdirs, nil
}

func addBox(name string, provider string) error {
	/* Adding a box fails if it already exists, hence the check. */
	exists, err := containsBox(name)
	if err == nil && exists {
		return nil
	}
	err = exec.Command(vagrantCmd, []string{"--machine-readable", "box", "add",
		"--provider", provider, name}...).Run()
	if err != nil {
		return errors.New("unable to add box")
	}
	return nil
}

func containsBox(name string) (bool, error) {
	output, err := exec.Command(vagrantCmd, []string{"--machine-readable", "box",
		"list"}...).Output()
	if err != nil {
		return false, errors.New("unable to list machines")
	}
	lines := bytes.Split(output, []byte("\n"))
	for _, line := range lines {
		words := strings.Split(string(line[:]), ",")
		if words[len(words)-1] == name {
			return true, nil
		}
	}
	return false, nil
}

func shell(id string, commands string) ([]byte, []byte, error) {
	chdir := `(cd %s; `
	vdir, err := vagrantDir()
	if err != nil {
		return nil, nil, err
	}
	chdir = fmt.Sprintf(chdir, vdir+id)
	shellCommand := chdir + strings.Replace(commands, "%s", id, -1) + ")"

	var outbuf, errbuf bytes.Buffer
	cmd := exec.Command(shCmd, []string{"-c", shellCommand}...)
	cmd.Stdout = &outbuf
	cmd.Stderr = &errbuf
	err = cmd.Run()

	return outbuf.Bytes(), errbuf.Bytes(), err
}

func vagrantDir() (string, error) {
	dir, err := homedir.Dir()
	if err != nil {
		return "", err
	}
	vagrantDir := dir + "/.vagrant/"
	return vagrantDir, nil
}

func size(id string) string {
	size, _, err := shell(id, "cat size")
	if err != nil {
		return ""
	}
	return string(size)
}
