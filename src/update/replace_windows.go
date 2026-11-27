//go:build windows

package update

import (
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/sys/windows"
)

func replaceBinary(currentPath, newPath string) error {
	oldPath := currentPath + ".old"
	_ = os.Remove(oldPath)
	if err := os.Rename(currentPath, oldPath); err != nil {
		return fmt.Errorf("rename current binary: %w", err)
	}
	if err := os.Rename(newPath, currentPath); err != nil {
		_ = os.Rename(oldPath, currentPath)
		return fmt.Errorf("install new binary: %w", err)
	}
	old, err := windows.UTF16PtrFromString(oldPath)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(old, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT); err != nil {
		return fmt.Errorf("schedule old binary removal: %w", err)
	}
	return nil
}

func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start updated process: %w", err)
	}
	os.Exit(0)
	return nil
}

// RestartSelf re-executes the installed binary after a privileged replacement.
func RestartSelf() error {
	return restartSelf()
}
