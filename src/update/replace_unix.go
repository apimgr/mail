//go:build !windows

package update

import (
	"fmt"
	"os"
	"syscall"
)

func replaceBinary(currentPath, newPath string) error {
	info, err := os.Stat(currentPath)
	if err != nil {
		return fmt.Errorf("stat current binary: %w", err)
	}
	if err := os.Chmod(newPath, info.Mode()); err != nil {
		return fmt.Errorf("set binary permissions: %w", err)
	}
	if err := os.Rename(newPath, currentPath); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}
	return nil
}

func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}

// RestartSelf re-executes the installed binary after a privileged replacement.
func RestartSelf() error {
	return restartSelf()
}
