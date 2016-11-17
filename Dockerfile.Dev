From quilt/ovs
Maintainer Ethan J. Jackson

RUN mkdir -p /var/run/netns \
&& apt-get update \
&& apt-get install -y --no-install-recommends iproute2 iptables \
&& rm -rf /var/lib/apt/lists/*

Copy ./quilt /usr/local/bin/quilt
ENTRYPOINT []
