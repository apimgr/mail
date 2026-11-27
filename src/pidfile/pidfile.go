package pidfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/apimgr/mail/src/paths"
)

// Check checks if a PID file exists and if the process is running
// Per AI.md PART 8 Step 11: Check PID file
// Returns (pid, isRunning, error)
func Check(pidPath string) (int, bool, error) {
	if paths.IsContainer() {
		return 0, false, nil
	}
	return checkPIDFile(pidPath)
}

func checkPIDFile(pidPath string) (int, bool, error) {
	// Check if PID file exists
	data, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No PID file - not running
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("reading PID file: %w", err)
	}

	// Parse PID from file
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		// Invalid PID file - consider stale
		if removeErr := Remove(pidPath); removeErr != nil {
			return 0, false, fmt.Errorf("invalid PID in file and cannot remove stale file: %w", removeErr)
		}
		return 0, false, nil
	}
	if pid <= 0 {
		if removeErr := Remove(pidPath); removeErr != nil {
			return 0, false, fmt.Errorf("invalid PID in file and cannot remove stale file: %w", removeErr)
		}
		return 0, false, nil
	}

	// Check if process is running
	// Per AI.md PART 8: Send signal 0 to check if process exists
	if !isProcessRunning(pid) || !isOurProcess(pid) {
		if removeErr := Remove(pidPath); removeErr != nil {
			return pid, false, fmt.Errorf("remove stale PID file: %w", removeErr)
		}
		return pid, false, nil
	}
	return pid, true, nil
}

// Write writes the current process ID to the PID file
// Per AI.md PART 8 Step 12: Write PID file
func Write(pidPath string) error {
	if paths.IsContainer() {
		return nil
	}
	return writePIDFile(pidPath)
}

func writePIDFile(pidPath string) error {
	if pid, running, err := checkPIDFile(pidPath); err != nil {
		return err
	} else if running {
		return fmt.Errorf("already running (pid %d)", pid)
	}
	// Ensure directory exists
	dir := filepath.Dir(pidPath)
	perm := os.FileMode(0o700)
	if paths.IsRoot() {
		perm = 0o755
	}
	if err := os.MkdirAll(dir, perm); err != nil {
		return fmt.Errorf("creating PID directory: %w", err)
	}

	// Write PID to file
	pid := os.Getpid()
	data := fmt.Sprintf("%d\n", pid)

	filePerm := os.FileMode(0o600)
	if paths.IsRoot() {
		filePerm = 0o644
	}
	if err := os.WriteFile(pidPath, []byte(data), filePerm); err != nil {
		return fmt.Errorf("writing PID file: %w", err)
	}

	return nil
}

// Remove removes the PID file
// Per AI.md PART 8: Remove PID file during graceful shutdown
func Remove(pidPath string) error {
	if err := os.Remove(pidPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing PID file: %w", err)
	}
	return nil
}
