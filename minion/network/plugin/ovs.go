package plugin

import "os/exec"

type ovsReq struct {
	vsctlCmds [][]string
	err       chan error
}

var ovsChan = make(chan ovsReq)

func ovsRequestImpl(cmds [][]string) error {
	err := make(chan error)
	ovsChan <- ovsReq{vsctlCmds: cmds, err: err}
	return <-err
}

func ovsRun() {
	var reqs []ovsReq
	for {
		if len(reqs) == 0 {
			reqs = append(reqs, <-ovsChan)
		}

		select {
		case req := <-ovsChan:
			reqs = append(reqs, req)
			continue
		default:
		}

		err := ovsRunOnce(reqs)
		for _, req := range reqs {
			req.err <- err
		}
		reqs = nil
	}
}

func ovsRunOnceImpl(reqs []ovsReq) error {
	args := []string{}
	for _, req := range reqs {
		for _, cmd := range req.vsctlCmds {
			args = append(args, "--")
			args = append(args, cmd...)
		}
	}
	return execRun("ovs-vsctl", args...)
}

var execRun = func(name string, arg ...string) error {
	return exec.Command(name, arg...).Run()
}

var ovsRunOnce = ovsRunOnceImpl
var ovsRequest = ovsRequestImpl
