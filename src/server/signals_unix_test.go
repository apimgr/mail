//go:build !windows

package server

import (
	"syscall"
	"testing"
)

func TestOperationalSignalActions(t *testing.T) {
	if got := operationalSignalAction(syscall.SIGUSR1); got != "reopen" {
		t.Fatalf("SIGUSR1 action = %q, want reopen", got)
	}
	if got := operationalSignalAction(syscall.SIGUSR2); got != "status" {
		t.Fatalf("SIGUSR2 action = %q, want status", got)
	}
	if got := operationalSignalAction(syscall.SIGTERM); got != "" {
		t.Fatalf("SIGTERM action = %q, want no operational action", got)
	}
}
