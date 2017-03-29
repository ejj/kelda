// +build !windows

package quiltctl

import "github.com/quilt/quilt/quiltctl/command"

func init() {
	commands["minion"] = command.NewMinionCommand()
}
