package plugin

import (
	"os/exec"
)

type vsctlReq struct {
	err  chan error
	cmds [][]string
}

var vsctlChan = make(chan vsctlReq)

func vsctlImpl(cmds [][]string) error {
	err := make(chan error)
	vsctlChan <- vsctlReq{cmds: cmds, err: err}
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

var execRun = func(name string, arg ...string) error {
	return exec.Command(name, arg...).Run()
}

var vsctl = vsctlImpl
