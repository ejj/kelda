package ipdef

import (
	"fmt"
	"math"
	"net"
	"syscall"
)

var (
	// QuiltSubnet is the subnet under which quilt containers are given IP addresses.
	QuiltSubnet = net.IPNet{
		IP:   net.IPv4(10, 0, 0, 0).To4(),
		Mask: net.CIDRMask(8, 32),
	}

	// SubMask is the subnet mask that minions can assign container IPs within. It
	// resprents a /20 subnet.
	SubMask = net.CIDRMask(20, 32)

	// LabelSubnet is the subnet that is reserved for label IPs.
	LabelSubnet = net.IPNet{IP: QuiltSubnet.IP, Mask: SubMask}

	minionMaskBits, _ = SubMask.Size()
	quiltMaskBits, _  = QuiltSubnet.Mask.Size()

	// MaxMinionCount is the largest number of minions that can exist, based
	// on the number of available subnets
	MaxMinionCount = int(math.Pow(2, float64(minionMaskBits-quiltMaskBits))+0.5) - 1

	// GatewayIP is the address of the border router in the logical network.
	GatewayIP = net.IPv4(10, 0, 0, 1).To4()

	// GatewayMac is the Mac address of the default gateway.
	GatewayMac = IPToMac(GatewayIP)
)

// IPStrToMac converts the given IP address string into a MAC address.
func IPStrToMac(ipStr string) string {
	parsedIP := net.ParseIP(ipStr)
	if parsedIP == nil {
		return ""
	}
	return IPToMac(parsedIP)
}

// IPToMac converts the given IP address into a MAC address.
func IPToMac(ip net.IP) string {
	ip = ip.To4()
	return fmt.Sprintf("02:00:%02x:%02x:%02x:%02x", ip[0], ip[1], ip[2], ip[3])
}

// Allow mocking out for unit tests.
var ifNameSize = syscall.IFNAMSIZ

// IFName transforms a string into something suitable for an interface name.
func IFName(name string) string {
	// The IFNAMESIZ #define is the size of a C buffer, not the length of a string.
	// Thus, it assumes one NULL character at the end which we can't overwrite.
	size := ifNameSize - 1

	if len(name) < size {
		return name
	}
	return name[0:size]
}
