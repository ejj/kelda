FROM ubuntu:16.04

RUN ovs_ver="2.6.1" linux="linux-headers-4.4.0-63-generic" \
build_deps="${linux} build-essential libssl-dev python python-six wget" \
&& mkdir /build && cd /build \
&& apt-get update \
&& apt-get install -y ${build_deps} \
&& wget http://openvswitch.org/releases/openvswitch-${ovs_ver}.tar.gz \
&& tar -xf openvswitch-${ovs_ver}.tar.gz \
&& cd openvswitch-${ovs_ver} \
&& ./configure --localstatedir="/var" --sysconfdir="/etc" --prefix="/usr" \
    --with-linux=/lib/modules/4.4.0-63-generic/build --enable-ssl \
&& make all install \
&& mkdir /modules \
&& cp datapath/linux/openvswitch.ko \
      datapath/linux/vport-geneve.ko \
      datapath/linux/vport-stt.ko /modules \
&& apt-get remove --purge -y ${build_deps} \
&& apt-get autoremove -y --purge \
&& apt-get install -y --no-install-recommends openssl ca-certificates kmod \
      iproute2 iptables\
&& rm -rf /var/lib/apt/lists/* \
&& rm -rf /build \
&& ovsdb-tool create /etc/openvswitch/conf.db /usr/share/openvswitch/vswitch.ovsschema \
&& ovsdb-tool create /etc/openvswitch/ovnnb.db /usr/share/openvswitch/ovn-nb.ovsschema \
&& ovsdb-tool create /etc/openvswitch/ovnsb.db /usr/share/openvswitch/ovn-sb.ovsschema

COPY run /bin/run

VOLUME ["/var/log/openvswitch", "/var/lib/openvswitch", "/var/run/openvswitch", "/etc/openvswitch"]
ENTRYPOINT ["run"]
