From quilt/ovs
Maintainer Ethan J. Jackson

ENV build_deps \
    build-essential \
    git \
    wget

RUN VER=1.7 \
&& export GOROOT=/tmp/build/go GOPATH=/tmp/build/gowork \
&& export PATH=$PATH:$GOROOT/bin \
&& mkdir -p /var/run/netns \
&& mkdir /tmp/build && cd /tmp/build \
&& apt-get update \
&& apt-get install -y ${build_deps} \
&& apt-get install -y --no-install-recommends iproute2 iptables \
&& wget https://storage.googleapis.com/golang/go$VER.linux-amd64.tar.gz \
&& gunzip -c go$VER.linux-amd64.tar.gz | tar x \
&& go get -u github.com/NetSys/quilt \
&& go test github.com/NetSys/quilt/... \
&& go install github.com/NetSys/quilt \
&& cp $GOPATH/bin/* /usr/local/bin \
&& git -C "$GOPATH/src/github.com/NetSys/quilt/" show --pretty=medium \
    --no-patch > /buildinfo \
&& rm -rf /tmp/build \
&& apt-get remove --purge -y ${build_deps} \
&& apt-get autoremove -y --purge \
&& rm -rf /var/lib/apt/lists/*
