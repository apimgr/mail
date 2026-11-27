//go:build windows

package pidfile

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

const stillActive = 259

func processImage(pid int) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return "", err
	}
	if exitCode != stillActive {
		return "", windows.ERROR_INVALID_PARAMETER
	}
	var buffer [windows.MAX_PATH]uint16
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buffer[:size]), nil
}

func isProcessRunning(pid int) bool {
	_, err := processImage(pid)
	return err == nil
}

func isOurProcess(pid int) bool {
	image, err := processImage(pid)
	if err != nil {
		return false
	}
	base := filepath.Base(image)
	return strings.EqualFold(base, "mail.exe") || strings.EqualFold(base, "mail")
}
