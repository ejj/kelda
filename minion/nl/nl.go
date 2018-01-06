//go:generate mockery -name=I -outpkg=nlmock -output=./nlmock

package nl

import (
	"net"

	"github.com/kelda/kelda/counter"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

// I implements a mock interface netlink.
type I interface {
	AddVeth(name, alias, peer string, mtu int) error
	AddrAdd(link Link, ip net.IPNet) error

	LinkSetUp(link Link) error
	LinkDel(link Link) error
	LinkSetNs(link Link, nsh netns.NsHandle) error
	LinkSetName(link Link, name string) error
	LinkByName(name string) (Link, error)
	LinkByAlias(alias string) (Link, error)
	LinkByIndex(index int) (Link, error)
	LinkSetHardwareAddr(link Link, hwaddr net.HardwareAddr) error

	RouteList(family int) ([]Route, error)
	RouteAdd(r Route) error

	GetNetns() (netns.NsHandle, error)
	GetNetnsFromPath(string) (netns.NsHandle, error)
	SetNetns(ns netns.NsHandle) error
	CloseNsHandle(ns netns.NsHandle) error
}

// N holds a global instance of I.
var N I = n{}

type n struct{}

// Link wraps netlink.Link.
type Link netlink.Link

// Route wraps netlink.Route.
type Route netlink.Route

// ScopeLink represents link scope for linux routing table entires.
var ScopeLink netlink.Scope // Overwritten in nl_linux.go

var c = counter.New("Netlink")

func (n n) AddVeth(name, alias, peer string, mtu int) error {
	c.Inc("Add Veth")
	return netlink.LinkAdd(&netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{Name: name, Alias: alias, MTU: mtu},
		PeerName:  peer})
}

func (n n) LinkSetUp(link Link) error {
	c.Inc("Link Up")
	return netlink.LinkSetUp(link)
}

func (n n) LinkDel(link Link) error {
	c.Inc("Link Del")
	return netlink.LinkDel(link)
}

func (n n) LinkSetNs(link Link, nsh netns.NsHandle) error {
	c.Inc("LinkSetNs")
	return netlink.LinkSetNsFd(link, int(nsh))
}

func (n n) LinkSetName(link Link, name string) error {
	c.Inc("LinkSetName")
	return netlink.LinkSetName(link, name)
}

func (n n) LinkByName(name string) (Link, error) {
	c.Inc("Get LinkByName")
	return netlink.LinkByName(name)
}

func (n n) LinkByAlias(alias string) (Link, error) {
	c.Inc("Get LinkByAlias")
	return netlink.LinkByAlias(alias)
}

func (n n) LinkByIndex(index int) (Link, error) {
	c.Inc("Get Link")
	return netlink.LinkByIndex(index)
}

func (n n) LinkSetHardwareAddr(link Link, hwaddr net.HardwareAddr) error {
	return netlink.LinkSetHardwareAddr(link, hwaddr)
}

func (n n) AddrAdd(link Link, ip net.IPNet) error {
	c.Inc("Add Address")
	return netlink.AddrAdd(link, &netlink.Addr{IPNet: &ip})
}

func (n n) RouteList(family int) ([]Route, error) {
	c.Inc("List Routes")
	res, err := netlink.RouteList(nil, family)

	var routes []Route
	for _, r := range res {
		routes = append(routes, Route(r))
	}
	return routes, err
}

func (n n) RouteAdd(r Route) error {
	c.Inc("Add Route")

	nlr := netlink.Route(r)
	return netlink.RouteAdd(&nlr)
}

func (n n) GetNetns() (netns.NsHandle, error) {
	c.Inc("Get Namespace")
	return netns.Get()
}

func (n n) GetNetnsFromPath(path string) (netns.NsHandle, error) {
	c.Inc("Get Namespace")
	return netns.GetFromPath(path)
}

func (n n) SetNetns(ns netns.NsHandle) error {
	c.Inc("Set Namespace")
	return netns.Set(ns)
}

func (n n) CloseNsHandle(ns netns.NsHandle) error {
	return ns.Close()
}
