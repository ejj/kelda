package cfg

var cfgTemplate = `#!/bin/bash

initialize_ovs() {
	echo "net.ipv4.ip_forward=1" >> /etc/sysctl.conf
	sysctl --system

	cat <<- EOF > /etc/systemd/system/ovs.service
	[Unit]
	Description=OVS
	After=docker.service
	Requires=docker.service

	[Service]
	Type=oneshot
	ExecStartPre=/sbin/modprobe gre
	ExecStartPre=/sbin/modprobe nf_nat_ipv6
	ExecStart=/usr/bin/docker run --rm --privileged --net=host {{.KeldaImage}} \
	bash -c "if [ ! -d /modules/$(uname -r) ]; then \
			echo WARN No usable pre-built kernel module. Building now... >&2; \
			/bin/bootstrap kernel_modules $(uname -r); \
		fi ; \
		insmod /modules/$(uname -r)/openvswitch.ko \
	         && insmod /modules/$(uname -r)/vport-geneve.ko \
	         && insmod /modules/$(uname -r)/vport-stt.ko"

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
	ExecStart=/usr/bin/dockerd --ip-forward=false --bridge=none \
	--insecure-registry 10.0.0.0/8 --insecure-registry 172.16.0.0/12 --insecure-registry 192.168.0.0/16 \
	-H unix:///var/run/docker.sock


	[Install]
	WantedBy=multi-user.target
	EOF
}

initialize_minion() {
	# Create the Kelda directory now so that it will exist when the minion starts,
	# and attempts to mount it as a volume. If the directory didn't exist, then
	# Docker would automatically create it, resulting in it being owned by root.
	# The TLS credential installation code running on the daemon would then be
	# unable to write to the directory.
	install -d -o kelda -m 755 {{.KeldaHome}}

	# Make the Kubelet directory. It must be configured as a shared mount so
	# that the mounts created by the Kubelet get propogated to other containers.
	mkdir -p /var/lib/kubelet
	mount -o bind /var/lib/kubelet /var/lib/kubelet/
	mount --make-shared /var/lib/kubelet/

	# Create the etcd data directory.
	mkdir -p /var/lib/etcd

	cat <<- EOF > /etc/systemd/system/minion.service
	[Unit]
	Description=Kelda Minion
	After=ovs.service
	Requires=ovs.service

	[Service]
	TimeoutSec=1000
	ExecStartPre=-/usr/bin/docker kill minion
	ExecStartPre=-/usr/bin/docker rm minion
	ExecStartPre=/usr/bin/docker pull {{.KeldaImage}}
	ExecStart=/usr/bin/docker run --net=host --name=minion --privileged \
	-v /var/run/docker.sock:/var/run/docker.sock \
	-v /etc/ssl/certs/ca-certificates.crt:/etc/ssl/certs/ca-certificates.crt \
	-v /home/kelda/.ssh:/home/kelda/.ssh:rw \
	-v {{.KeldaHome}}:{{.KeldaHome}}:ro \
	-v /run/docker:/run/docker:rw {{.KeldaImage}} \
	kelda -l {{.LogLevel}} minion {{.MinionOpts}}
	Restart=on-failure

	[Install]
	WantedBy=multi-user.target
	EOF
}

install_docker() (
	# Fail immediately if any of commands error. If this flag were not set,
	# every command would have to check whether it failed in order to
	# propagate the proper exit status to the caller. Note that we set this
	# within a subshell, so commands outside this function will not cause
	# the shell to exit on failure.
	set -e

	# The expected key is documented by Docker here:
	# https://docs.docker.com/engine/installation/linux/docker-ce/ubuntu/#install-using-the-repository
	curl -fsSL https://download.docker.com/linux/ubuntu/gpg | apt-key add -
	expKey="9DC858229FC7DD38854AE2D88D81803C0EBFCD88"
	actualKey=$(apt-key adv --with-colons --fingerprint 0EBFCD88 | grep ^fpr: | cut -d ':' -f 10)
	if [ $actualKey != $expKey ] ; then
	    echo "ERROR Failed to verify Docker's GPG key."
	    echo "This could mean that an attacker is injecting a malicious version of docker-engine. Bailing."
	    exit 1
	fi

	add-apt-repository "deb [arch=amd64] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable"
	apt-get update
	apt-get install docker-ce=17.12.0~ce-0~ubuntu -y
	systemctl stop docker.service
)

setup_user() {
	user=$1
	ssh_keys=$2
	sudo groupadd $user
	sudo useradd $user -s /bin/bash -g $user
	sudo usermod -aG sudo $user

	user_dir=/home/$user

	# Create dirs and files with correct users and permissions
	install -d -o $user -m 755 $user_dir
	install -d -o $user -m 700 $user_dir/.ssh
	install -o $user -m 600 /dev/null $user_dir/.ssh/authorized_keys
	printf "$ssh_keys" >> $user_dir/.ssh/authorized_keys
	printf "$user ALL = (ALL) NOPASSWD: ALL\n" >> /etc/sudoers
}

echo -n "Start Boot Script: " >> /var/log/bootscript.log
date >> /var/log/bootscript.log

export DEBIAN_FRONTEND=noninteractive

# Increase the maximum number of concurrent sessions per SSH connection so that
# the integration tests can take advantage of connection multiplexing.
printf '\nMaxSessions 512\n' >> /etc/ssh/sshd_config
systemctl restart ssh

ssh_keys="{{.SSHKeys}}"
setup_user kelda "$ssh_keys"

# Docker sometimes fails to install because of temporary network issues
# connecting to the Docker apt server.
while ! install_docker ; do
  echo "Docker failed to install. Retrying in 30 seconds."
  sleep 30
done

initialize_ovs
initialize_docker
initialize_minion

# Allow the user to use docker without sudo
sudo usermod -aG docker kelda

# Reload because we replaced the docker.service provided by the package
systemctl daemon-reload

# Enable our services to run on boot
systemctl enable {docker,ovs,minion}.service

# Start our services
systemctl restart {docker,ovs,minion}.service

echo -n "Completed Boot Script: " >> /var/log/bootscript.log
date >> /var/log/bootscript.log
    `
