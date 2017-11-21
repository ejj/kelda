// Code generated by mockery v1.0.1 DO NOT EDIT.

package mocks

import godo "github.com/digitalocean/godo"
import mock "github.com/stretchr/testify/mock"

// Client is an autogenerated mock type for the Client type
type Client struct {
	mock.Mock
}

// AddRules provides a mock function with given fields: _a0, _a1
func (_m *Client) AddRules(_a0 string, _a1 []godo.InboundRule) (*godo.Response, error) {
	ret := _m.Called(_a0, _a1)

	var r0 *godo.Response
	if rf, ok := ret.Get(0).(func(string, []godo.InboundRule) *godo.Response); ok {
		r0 = rf(_a0, _a1)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*godo.Response)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string, []godo.InboundRule) error); ok {
		r1 = rf(_a0, _a1)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// AssignFloatingIP provides a mock function with given fields: _a0, _a1
func (_m *Client) AssignFloatingIP(_a0 string, _a1 int) (*godo.Action, *godo.Response, error) {
	ret := _m.Called(_a0, _a1)

	var r0 *godo.Action
	if rf, ok := ret.Get(0).(func(string, int) *godo.Action); ok {
		r0 = rf(_a0, _a1)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*godo.Action)
		}
	}

	var r1 *godo.Response
	if rf, ok := ret.Get(1).(func(string, int) *godo.Response); ok {
		r1 = rf(_a0, _a1)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*godo.Response)
		}
	}

	var r2 error
	if rf, ok := ret.Get(2).(func(string, int) error); ok {
		r2 = rf(_a0, _a1)
	} else {
		r2 = ret.Error(2)
	}

	return r0, r1, r2
}

// CreateDroplets provides a mock function with given fields: _a0
func (_m *Client) CreateDroplets(_a0 *godo.DropletMultiCreateRequest) ([]godo.Droplet, *godo.Response, error) {
	ret := _m.Called(_a0)

	var r0 []godo.Droplet
	if rf, ok := ret.Get(0).(func(*godo.DropletMultiCreateRequest) []godo.Droplet); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]godo.Droplet)
		}
	}

	var r1 *godo.Response
	if rf, ok := ret.Get(1).(func(*godo.DropletMultiCreateRequest) *godo.Response); ok {
		r1 = rf(_a0)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*godo.Response)
		}
	}

	var r2 error
	if rf, ok := ret.Get(2).(func(*godo.DropletMultiCreateRequest) error); ok {
		r2 = rf(_a0)
	} else {
		r2 = ret.Error(2)
	}

	return r0, r1, r2
}

// CreateFirewall provides a mock function with given fields: _a0, _a1, _a2
func (_m *Client) CreateFirewall(_a0 string, _a1 []godo.OutboundRule, _a2 []godo.InboundRule) (*godo.Firewall, *godo.Response, error) {
	ret := _m.Called(_a0, _a1, _a2)

	var r0 *godo.Firewall
	if rf, ok := ret.Get(0).(func(string, []godo.OutboundRule, []godo.InboundRule) *godo.Firewall); ok {
		r0 = rf(_a0, _a1, _a2)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*godo.Firewall)
		}
	}

	var r1 *godo.Response
	if rf, ok := ret.Get(1).(func(string, []godo.OutboundRule, []godo.InboundRule) *godo.Response); ok {
		r1 = rf(_a0, _a1, _a2)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*godo.Response)
		}
	}

	var r2 error
	if rf, ok := ret.Get(2).(func(string, []godo.OutboundRule, []godo.InboundRule) error); ok {
		r2 = rf(_a0, _a1, _a2)
	} else {
		r2 = ret.Error(2)
	}

	return r0, r1, r2
}

// CreateTag provides a mock function with given fields: _a0
func (_m *Client) CreateTag(_a0 string) (*godo.Tag, *godo.Response, error) {
	ret := _m.Called(_a0)

	var r0 *godo.Tag
	if rf, ok := ret.Get(0).(func(string) *godo.Tag); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*godo.Tag)
		}
	}

	var r1 *godo.Response
	if rf, ok := ret.Get(1).(func(string) *godo.Response); ok {
		r1 = rf(_a0)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*godo.Response)
		}
	}

	var r2 error
	if rf, ok := ret.Get(2).(func(string) error); ok {
		r2 = rf(_a0)
	} else {
		r2 = ret.Error(2)
	}

	return r0, r1, r2
}

// DeleteDroplet provides a mock function with given fields: _a0
func (_m *Client) DeleteDroplet(_a0 int) (*godo.Response, error) {
	ret := _m.Called(_a0)

	var r0 *godo.Response
	if rf, ok := ret.Get(0).(func(int) *godo.Response); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*godo.Response)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(int) error); ok {
		r1 = rf(_a0)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// GetDroplet provides a mock function with given fields: _a0
func (_m *Client) GetDroplet(_a0 int) (*godo.Droplet, *godo.Response, error) {
	ret := _m.Called(_a0)

	var r0 *godo.Droplet
	if rf, ok := ret.Get(0).(func(int) *godo.Droplet); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*godo.Droplet)
		}
	}

	var r1 *godo.Response
	if rf, ok := ret.Get(1).(func(int) *godo.Response); ok {
		r1 = rf(_a0)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*godo.Response)
		}
	}

	var r2 error
	if rf, ok := ret.Get(2).(func(int) error); ok {
		r2 = rf(_a0)
	} else {
		r2 = ret.Error(2)
	}

	return r0, r1, r2
}

// ListDroplets provides a mock function with given fields: _a0, _a1
func (_m *Client) ListDroplets(_a0 *godo.ListOptions, _a1 string) ([]godo.Droplet, *godo.Response, error) {
	ret := _m.Called(_a0, _a1)

	var r0 []godo.Droplet
	if rf, ok := ret.Get(0).(func(*godo.ListOptions, string) []godo.Droplet); ok {
		r0 = rf(_a0, _a1)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]godo.Droplet)
		}
	}

	var r1 *godo.Response
	if rf, ok := ret.Get(1).(func(*godo.ListOptions, string) *godo.Response); ok {
		r1 = rf(_a0, _a1)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*godo.Response)
		}
	}

	var r2 error
	if rf, ok := ret.Get(2).(func(*godo.ListOptions, string) error); ok {
		r2 = rf(_a0, _a1)
	} else {
		r2 = ret.Error(2)
	}

	return r0, r1, r2
}

// ListFirewalls provides a mock function with given fields: _a0
func (_m *Client) ListFirewalls(_a0 *godo.ListOptions) ([]godo.Firewall, *godo.Response, error) {
	ret := _m.Called(_a0)

	var r0 []godo.Firewall
	if rf, ok := ret.Get(0).(func(*godo.ListOptions) []godo.Firewall); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]godo.Firewall)
		}
	}

	var r1 *godo.Response
	if rf, ok := ret.Get(1).(func(*godo.ListOptions) *godo.Response); ok {
		r1 = rf(_a0)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*godo.Response)
		}
	}

	var r2 error
	if rf, ok := ret.Get(2).(func(*godo.ListOptions) error); ok {
		r2 = rf(_a0)
	} else {
		r2 = ret.Error(2)
	}

	return r0, r1, r2
}

// ListFloatingIPs provides a mock function with given fields: _a0
func (_m *Client) ListFloatingIPs(_a0 *godo.ListOptions) ([]godo.FloatingIP, *godo.Response, error) {
	ret := _m.Called(_a0)

	var r0 []godo.FloatingIP
	if rf, ok := ret.Get(0).(func(*godo.ListOptions) []godo.FloatingIP); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]godo.FloatingIP)
		}
	}

	var r1 *godo.Response
	if rf, ok := ret.Get(1).(func(*godo.ListOptions) *godo.Response); ok {
		r1 = rf(_a0)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*godo.Response)
		}
	}

	var r2 error
	if rf, ok := ret.Get(2).(func(*godo.ListOptions) error); ok {
		r2 = rf(_a0)
	} else {
		r2 = ret.Error(2)
	}

	return r0, r1, r2
}

// RemoveRules provides a mock function with given fields: _a0, _a1
func (_m *Client) RemoveRules(_a0 string, _a1 []godo.InboundRule) (*godo.Response, error) {
	ret := _m.Called(_a0, _a1)

	var r0 *godo.Response
	if rf, ok := ret.Get(0).(func(string, []godo.InboundRule) *godo.Response); ok {
		r0 = rf(_a0, _a1)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*godo.Response)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string, []godo.InboundRule) error); ok {
		r1 = rf(_a0, _a1)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// UnassignFloatingIP provides a mock function with given fields: _a0
func (_m *Client) UnassignFloatingIP(_a0 string) (*godo.Action, *godo.Response, error) {
	ret := _m.Called(_a0)

	var r0 *godo.Action
	if rf, ok := ret.Get(0).(func(string) *godo.Action); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*godo.Action)
		}
	}

	var r1 *godo.Response
	if rf, ok := ret.Get(1).(func(string) *godo.Response); ok {
		r1 = rf(_a0)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*godo.Response)
		}
	}

	var r2 error
	if rf, ok := ret.Get(2).(func(string) error); ok {
		r2 = rf(_a0)
	} else {
		r2 = ret.Error(2)
	}

	return r0, r1, r2
}
