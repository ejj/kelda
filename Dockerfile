From gcr.io/google_containers/hyperkube-amd64:v1.9.1
Maintainer Ethan J. Jackson

From keldaio/ovs
Copy --from=0 /hyperkube /hyperkube
Copy --from=0 /opt/cni/bin/loopback /opt/cni/bin/loopback

RUN ln -s /hyperkube /apiserver \
        && ln -s /hyperkube /controller-manager \
        && ln -s /hyperkube /kubectl \
        && ln -s /hyperkube /kubelet \
        && ln -s /hyperkube /proxy \
        && ln -s /hyperkube /scheduler \
        && ln -s /hyperkube /aggerator \
        && ln -s /hyperkube /usr/local/bin/kube-apiserver \
        && ln -s /hyperkube /usr/local/bin/kube-controller-manager \
        && ln -s /hyperkube /usr/local/bin/kubectl \
        && ln -s /hyperkube /usr/local/bin/kubelet \
        && ln -s /hyperkube /usr/local/bin/kube-proxy \
        && ln -s /hyperkube /usr/local/bin/kube-scheduler

Copy ./buildinfo /buildinfo
Copy ./kelda_linux /usr/bin/kelda
Copy ./minion/network/cni/kelda.sh /opt/cni/bin/kelda
Copy ./minion/network/cni/*.conf /etc/cni/net.d/
Copy ./minion/network/resolv.conf /kelda_resolv.conf
ENTRYPOINT []
