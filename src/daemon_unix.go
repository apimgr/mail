//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func startDetachedProcess(executable string, args []string) (bool, int, error) {
	if os.Getenv("_DAEMON_CHILD") != "" {
		return false, 0, nil
	}
	command := exec.Command(executable, filterDaemonFlag(args)...)
	env := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "_DAEMON_CHILD=") {
			env = append(env, value)
		}
	}
	command.Env = append(env, "_DAEMON_CHILD=1")
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	if err := command.Start(); err != nil {
		return false, 0, fmt.Errorf("start detached process: %w", err)
	}
	pid := command.Process.Pid
	if err := command.Process.Release(); err != nil {
		_ = command.Process.Kill()
		return false, 0, fmt.Errorf("release detached process: %w", err)
	}
	return true, pid, nil
}

func filterDaemonFlag(args []string) []string {
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg != "--daemon" {
			filtered = append(filtered, arg)
		}
	}
	return filtered
}
