package cloudcfg

var cfgTemplate = `#!/bin/bash

initialize_ovs() {
	cat <<- EOF > /etc/systemd/system/ovs.service
	[Unit]
	Description=OVS
	After=docker.service
	Requires=docker.service

	[Service]
	ExecStartPre=/sbin/modprobe gre
	ExecStartPre=/sbin/modprobe nf_nat_ipv6
	ExecStart=/usr/bin/docker run --rm --privileged {{.QuiltImage}} \
	bash -c "insmod /modules/openvswitch.ko \
	         && insmod /modules/vport-geneve.ko \
	         && insmod /modules/vport-stt.ko"

	[Install]
	WantedBy=multi-user.target
	EOF
}

initialize_docker() {
	mkdir -p /etc/systemd/system/docker.service.d

	cat <<- EOF > /etc/systemd/system/docker.service.d/override.conf
	[Unit]
	Description=docker

	[Service]
	# The below empty ExecStart deletes the official one installed by docker daemon.
	ExecStart=
	ExecStart=/usr/bin/docker daemon --bridge=none -H unix:///var/run/docker.sock

	[Install]
	WantedBy=multi-user.target
	EOF
}

initialize_minion() {
	cat <<- EOF > /etc/systemd/system/minion.service
	[Unit]
	Description=Quilt Minion
	After=docker.service
	Requires=docker.service

	[Service]
	TimeoutSec=1000
	ExecStartPre=-/usr/bin/docker kill minion
	ExecStartPre=-/usr/bin/docker rm minion
	ExecStartPre=/usr/bin/docker pull {{.QuiltImage}}
	ExecStart=/usr/bin/docker run --net=host --name=minion --privileged \
	-v /var/run/docker.sock:/var/run/docker.sock \
	-v /etc/ssl/certs/ca-certificates.crt:/etc/ssl/certs/ca-certificates.crt \
	-v /home/quilt/.ssh:/home/quilt/.ssh:rw \
	-v /proc:/hostproc:ro \
	-v /run/docker:/run/docker:rw {{.QuiltImage}} \
	quilt minion
	Restart=on-failure

	[Install]
	WantedBy=multi-user.target
	EOF
}

install_docker() {
	echo "deb https://apt.dockerproject.org/repo ubuntu-{{.UbuntuVersion}} main" > /etc/apt/sources.list.d/docker.list
	apt-get update
	apt-get install docker-engine=1.12.3-0~{{.UbuntuVersion}} -y --force-yes
	systemctl stop docker.service
}

setup_user() {
	user=$1
	ssh_keys=$2
	sudo groupadd $user
	sudo useradd $user -s /bin/bash -g $user
	sudo usermod -aG sudo $user

	user_dir=/home/$user

	# Create dirs and files with correct users and permissions
	install -d -o $user -m 744 $user_dir
	install -d -o $user -m 700 $user_dir/.ssh
	install -o $user -m 600 /dev/null $user_dir/.ssh/authorized_keys
	printf "$ssh_keys" >> $user_dir/.ssh/authorized_keys
	printf "$user ALL = (ALL) NOPASSWD: ALL\n" >> /etc/sudoers
}

echo -n "Start Boot Script: " >> /var/log/bootscript.log
date >> /var/log/bootscript.log

export DEBIAN_FRONTEND=noninteractive

ssh_keys="{{.SSHKeys}}"
setup_user quilt "$ssh_keys"

sudo mkdir /run/docker/plugins
sudo chmod -R /run/docker/plugins 0755

install_docker
initialize_ovs
initialize_docker
initialize_minion

# Allow the user to use docker without sudo
sudo usermod -aG docker quilt

# Reload because we replaced the docker.service provided by the package
systemctl daemon-reload

# Enable our services to run on boot
systemctl enable {docker,ovs,minion}.service

# Start our services
systemctl restart {docker,ovs,minion}.service

echo -n "Completed Boot Script: " >> /var/log/bootscript.log
date >> /var/log/bootscript.log
    `
