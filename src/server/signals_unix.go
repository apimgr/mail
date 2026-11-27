//go:build !windows

package server

import (
	"os"
	"syscall"
)

func shutdownSignals() []os.Signal {
	return []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT}
}

func operationalSignals() []os.Signal {
	return []os.Signal{syscall.SIGUSR1, syscall.SIGUSR2}
}

func ignoredSignals() []os.Signal {
	return []os.Signal{syscall.SIGHUP}
}

func operationalSignalAction(sig os.Signal) string {
	switch sig {
	case syscall.SIGUSR1:
		return "reopen"
	case syscall.SIGUSR2:
		return "status"
	default:
		return ""
	}
}
