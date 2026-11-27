package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (m *Manager) inspectStatus(ctx context.Context, status *Status) error {
	scope := status.Scope
	switch status.Manager {
	case Systemd:
		status.Installed = exists(m.unitPath(scope))
		if !status.Installed {
			return nil
		}
		prefix := []string{}
		if scope == User {
			prefix = append(prefix, "--user")
		}
		active, _ := m.runTimeout(ctx, "systemctl", append(append([]string{}, prefix...), "show", "--property=ActiveState", "--value", m.opts.InternalName+".service")...)
		status.Running = strings.TrimSpace(active) == "active"
		enabled, _ := m.runTimeout(ctx, "systemctl", append(append([]string{}, prefix...), "is-enabled", m.opts.InternalName+".service")...)
		status.Enabled = strings.TrimSpace(enabled) == "enabled" || strings.TrimSpace(enabled) == "static"
		pid, _ := m.runTimeout(ctx, "systemctl", append(append([]string{}, prefix...), "show", "--property=MainPID", "--value", m.opts.InternalName+".service")...)
		status.PID, _ = strconv.Atoi(strings.TrimSpace(pid))
	case OpenRC:
		status.Installed = exists(filepath.Join("/etc/init.d", m.opts.InternalName))
		if !status.Installed {
			return nil
		}
		out, _ := m.runTimeout(ctx, "rc-service", m.opts.InternalName, "status")
		status.Running = containsRunning(out)
		out, _ = m.runTimeout(ctx, "rc-update", "show", "default")
		status.Enabled = strings.Contains(out, m.opts.InternalName)
	case SysV:
		status.Installed = exists(m.sysvPath())
		if !status.Installed {
			return nil
		}
		out, _ := m.runTimeout(ctx, m.sysvPath(), "status")
		status.Running = containsRunning(out)
		status.Enabled = sysVEnabled(m.opts.InternalName)
	case Runit:
		link := filepath.Join("/etc/service", m.opts.InternalName)
		status.Installed = exists(filepath.Join("/etc/sv", m.opts.InternalName, "run"))
		status.Enabled = exists(link)
		if !status.Installed {
			return nil
		}
		out, _ := m.runTimeout(ctx, "sv", "status", link)
		status.Running = strings.HasPrefix(strings.TrimSpace(out), "run:")
		status.PID = numberAfter(out, "pid ")
	case S6:
		scan, err := s6ScanDirectory()
		if err != nil {
			return err
		}
		link := filepath.Join(scan, m.opts.InternalName)
		status.Installed = exists(filepath.Join("/etc/s6/sv", m.opts.InternalName, "run"))
		status.Enabled = exists(link)
		if !status.Installed {
			return nil
		}
		out, _ := m.runTimeout(ctx, "s6-svstat", link)
		status.Running = strings.Contains(out, "up")
		status.PID = numberAfter(out, "pid ")
	case RCd:
		status.Installed = exists(filepath.Join("/usr/local/etc/rc.d", m.opts.InternalName))
		if !status.Installed {
			return nil
		}
		out, _ := m.runTimeout(ctx, "service", m.opts.InternalName, "onestatus")
		status.Running = containsRunning(out)
		out, _ = m.runTimeout(ctx, "sysrc", "-n", m.opts.InternalName+"_enable")
		status.Enabled = strings.TrimSpace(out) == "YES"
	case Launchd:
		status.Installed = exists(m.launchdPath(scope))
		if !status.Installed {
			return nil
		}
		target := "system"
		if scope == User {
			target = "gui/" + currentUID()
		}
		out, _ := m.runTimeout(ctx, "launchctl", "print", target+"/"+m.opts.InternalOrg+"."+m.opts.InternalName)
		status.Running = strings.Contains(out, "state = running") || strings.Contains(out, "pid = ")
		status.Enabled = true
	case WindowsService:
		return m.inspectWindowsService(ctx, status)
	case WindowsTask:
		return m.inspectWindowsTask(ctx, status)
	default:
		return fmt.Errorf("unsupported service manager %q", status.Manager)
	}
	return nil
}

func containsRunning(output string) bool {
	value := strings.ToLower(output)
	return strings.Contains(value, "started") || strings.Contains(value, "running") || strings.Contains(value, "is running")
}

func numberAfter(value, marker string) int {
	index := strings.Index(value, marker)
	if index < 0 {
		return 0
	}
	value = value[index+len(marker):]
	var digits strings.Builder
	for _, char := range value {
		if char < '0' || char > '9' {
			break
		}
		digits.WriteRune(char)
	}
	number, _ := strconv.Atoi(digits.String())
	return number
}

func sysVEnabled(name string) bool {
	for _, directory := range []string{"/etc/rc2.d", "/etc/rc3.d", "/etc/rc4.d", "/etc/rc5.d"} {
		entries, _ := os.ReadDir(directory)
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "S") && strings.Contains(entry.Name(), name) {
				return true
			}
		}
	}
	return false
}

func s6Running() bool { _, err := s6ScanDirectory(); return err == nil }

func s6ScanDirectory() (string, error) {
	entries, err := os.ReadDir("/proc")
	procUnavailable := err != nil
	if err == nil {
		for _, entry := range entries {
			if _, parseErr := strconv.Atoi(entry.Name()); parseErr != nil {
				continue
			}
			data, readErr := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
			if readErr != nil {
				continue
			}
			parts := strings.Split(string(data), "\x00")
			if len(parts) == 0 || !strings.Contains(filepath.Base(parts[0]), "s6-svscan") {
				continue
			}
			if len(parts) > 1 && filepath.IsAbs(parts[1]) {
				return parts[1], nil
			}
			if filepath.IsAbs(parts[0]) && filepath.Base(filepath.Dir(parts[0])) != "bin" {
				return filepath.Dir(parts[0]), nil
			}
		}
	}
	if procUnavailable && exists("/run/service") {
		return "/run/service", nil
	}
	return "", fmt.Errorf("could not locate the running s6 scan directory")
}
