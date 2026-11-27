//go:build windows

package main

import (
	"fmt"
	"os"
)

func startDetachedProcess(_ string, _ []string) (bool, int, error) {
	_, _ = fmt.Fprintln(os.Stderr, "Warning: --daemon is not supported on Windows")
	_, _ = fmt.Fprintln(os.Stderr, "Use --service --install to install and start a Windows Service")
	return false, 0, nil
}

func filterDaemonFlag(args []string) []string { return args }
