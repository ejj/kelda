package nl

import "github.com/vishvananda/netlink"

func init() {
	ScopeLink = netlink.SCOPE_LINK
}
