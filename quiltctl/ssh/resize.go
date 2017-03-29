// +build !windows

package ssh

import (
	"os"
	"os/signal"
	"syscall"
)

func setupResizeSignal(sig chan os.Signal) {
	signal.Notify(sig, syscall.SIGWINCH)
}
