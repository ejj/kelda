package plugin

import (
	"os/exec"

	"github.com/quilt/quilt/minion/network/openflow"
)

type vsctlReq struct {
	err  chan error
	cmds [][]string
}

type ofcReq struct {
	err chan error
	ofc openflow.Container
}

var vsctlChan = make(chan vsctlReq)
var ofcChan = make(chan ofcReq)

func vsctlImpl(cmds [][]string) error {
	err := make(chan error)
	vsctlChan <- vsctlReq{cmds: cmds, err: err}
	return <-err
}

func ofctlImpl(ofc openflow.Container) error {
	err := make(chan error)
	ofcChan <- ofcReq{ofc: ofc, err: err}
	return <-err
}

func vsctlRun() {
	var reqs []vsctlReq
	for {
		if len(reqs) == 0 {
			reqs = append(reqs, <-vsctlChan)
		}

		select {
		case req := <-vsctlChan:
			reqs = append(reqs, req)
			continue
		default:
		}

		var args []string
		for _, req := range reqs {
			for _, cmd := range req.cmds {
				args = append(args, "--")
				args = append(args, cmd...)
			}
		}

		err := execRun("ovs-vsctl", args...)
		for _, req := range reqs {
			req.err <- err
		}
		reqs = nil
	}
}

func ofctlRun() {
	var reqs []ofcReq
	for {
		if len(reqs) == 0 {
			reqs = append(reqs, <-ofcChan)
		}

		select {
		case req := <-ofcChan:
			reqs = append(reqs, req)
			continue
		default:
		}

		var ofcs []openflow.Container
		for _, req := range reqs {
			ofcs = append(ofcs, req.ofc)
		}

		err := addFlows(ofcs)
		for _, req := range reqs {
			req.err <- err
		}
		reqs = nil
	}
}

var execRun = func(name string, arg ...string) error {
	return exec.Command(name, arg...).Run()
}

var addFlows = openflow.AddFlows

var vsctl = vsctlImpl
var ofctl = ofctlImpl
