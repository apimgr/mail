package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (m *Manager) commandAction(ctx context.Context, manager ManagerType, scope Scope, action string) error {
	if action == "install" {
		return m.enableAndStart(ctx, manager, scope)
	}
	if action == "disable" || action == "uninstall" {
		status, err := m.Status(ctx, scope)
		if err != nil {
			return err
		}
		if status.Running {
			if err := m.commandAction(ctx, manager, scope, "stop"); err != nil {
				return err
			}
		}
		if err := m.disableDefinition(ctx, manager, scope); err != nil {
			return err
		}
		if action == "disable" {
			return nil
		}
		return m.removeDefinition(ctx, manager, scope)
	}
	user := scope == User
	unit := m.opts.InternalName + ".service"
	switch manager {
	case Systemd:
		verb := action
		if verb == "reload" {
			verb = "restart"
		}
		args := []string{verb, unit}
		if user {
			args = append([]string{"--user"}, args...)
		}
		_, err := m.runTimeout(ctx, "systemctl", args...)
		return wrapAction(action, manager, err)
	case OpenRC:
		verb := action
		if verb == "reload" {
			verb = "restart"
		}
		_, err := m.runTimeout(ctx, "rc-service", m.opts.InternalName, verb)
		return wrapAction(action, manager, err)
	case SysV:
		verb := action
		if verb == "reload" {
			verb = "restart"
		}
		_, err := m.runTimeout(ctx, m.sysvPath(), verb)
		return wrapAction(action, manager, err)
	case Runit:
		verb := map[string]string{"start": "up", "stop": "down", "restart": "restart", "reload": "restart"}[action]
		_, err := m.runTimeout(ctx, "sv", verb, filepath.Join("/etc/service", m.opts.InternalName))
		return wrapAction(action, manager, err)
	case S6:
		scan, err := s6ScanDirectory()
		if err != nil {
			return err
		}
		verb := map[string]string{"start": "-u", "stop": "-d", "restart": "-r", "reload": "-r"}[action]
		_, err = m.runTimeout(ctx, "s6-svc", verb, filepath.Join(scan, m.opts.InternalName))
		return wrapAction(action, manager, err)
	case RCd:
		verb := action
		if verb == "reload" {
			verb = "restart"
		}
		_, err := m.runTimeout(ctx, "service", m.opts.InternalName, verb)
		return wrapAction(action, manager, err)
	case Launchd:
		return m.launchdAction(ctx, scope, action)
	case WindowsService:
		return m.windowsAction(ctx, action, false)
	case WindowsTask:
		return m.windowsTaskAction(ctx, action)
	default:
		return fmt.Errorf("unsupported service manager %q", manager)
	}
}

func (m *Manager) enableAndStart(ctx context.Context, manager ManagerType, scope Scope) error {
	user := scope == User
	switch manager {
	case Systemd:
		args := []string{"enable", "--now", m.opts.InternalName + ".service"}
		if user {
			args = append([]string{"--user"}, args...)
		}
		_, err := m.runTimeout(ctx, "systemctl", args...)
		return wrapAction("enable and start", manager, err)
	case OpenRC:
		if _, err := m.runTimeout(ctx, "rc-update", "add", m.opts.InternalName, "default"); err != nil {
			return wrapAction("enable", manager, err)
		}
		_, err := m.runTimeout(ctx, "rc-service", m.opts.InternalName, "start")
		return wrapAction("start", manager, err)
	case SysV:
		if executable("update-rc.d") {
			if _, err := m.runTimeout(ctx, "update-rc.d", m.opts.InternalName, "defaults"); err != nil {
				return wrapAction("enable", manager, err)
			}
		} else if _, err := m.runTimeout(ctx, "chkconfig", "--add", m.opts.InternalName); err != nil {
			return wrapAction("enable", manager, err)
		}
		_, err := m.runTimeout(ctx, m.sysvPath(), "start")
		return wrapAction("start", manager, err)
	case Runit:
		_, err := m.runTimeout(ctx, "sv", "up", filepath.Join("/etc/service", m.opts.InternalName))
		return wrapAction("start", manager, err)
	case S6:
		scan, err := s6ScanDirectory()
		if err != nil {
			return err
		}
		_, err = m.runTimeout(ctx, "s6-svc", "-u", filepath.Join(scan, m.opts.InternalName))
		return wrapAction("start", manager, err)
	case RCd:
		if _, err := m.runTimeout(ctx, "sysrc", m.opts.InternalName+"_enable=YES"); err != nil {
			return wrapAction("enable", manager, err)
		}
		_, err := m.runTimeout(ctx, "service", m.opts.InternalName, "start")
		return wrapAction("start", manager, err)
	case Launchd:
		return m.launchdAction(ctx, scope, "start")
	case WindowsService:
		return m.windowsAction(ctx, "start", true)
	case WindowsTask:
		return m.windowsTaskAction(ctx, "start")
	default:
		return fmt.Errorf("unsupported service manager %q", manager)
	}
}

func (m *Manager) disableDefinition(ctx context.Context, manager ManagerType, scope Scope) error {
	user := scope == User
	switch manager {
	case Systemd:
		args := []string{"disable", "--now", m.opts.InternalName + ".service"}
		if user {
			args = append([]string{"--user"}, args...)
		}
		_, err := m.runTimeout(ctx, "systemctl", args...)
		return wrapAction("disable", manager, err)
	case OpenRC:
		_, _ = m.runTimeout(ctx, "rc-update", "del", m.opts.InternalName, "default")
		return nil
	case SysV:
		if executable("update-rc.d") {
			_, err := m.runTimeout(ctx, "update-rc.d", "-f", m.opts.InternalName, "remove")
			return wrapAction("disable", manager, err)
		}
		_, err := m.runTimeout(ctx, "chkconfig", m.opts.InternalName, "off")
		return wrapAction("disable", manager, err)
	case Runit:
		link := filepath.Join("/etc/service", m.opts.InternalName)
		_, _ = m.runTimeout(ctx, "sv", "down", link)
		return removeExpectedSymlink(link, filepath.Join("/etc/sv", m.opts.InternalName))
	case S6:
		scan, err := s6ScanDirectory()
		if err != nil {
			return err
		}
		link := filepath.Join(scan, m.opts.InternalName)
		_, _ = m.runTimeout(ctx, "s6-svc", "-d", link)
		if err := removeExpectedSymlink(link, filepath.Join("/etc/s6/sv", m.opts.InternalName)); err != nil {
			return err
		}
		_, err = m.runTimeout(ctx, "s6-svscanctl", "-a", scan)
		return wrapAction("disable", manager, err)
	case RCd:
		_, _ = m.runTimeout(ctx, "sysrc", "-x", m.opts.InternalName+"_enable")
		return nil
	case Launchd:
		return m.launchdAction(ctx, scope, "stop")
	case WindowsService:
		return m.windowsAction(ctx, "disable", false)
	case WindowsTask:
		return m.windowsTaskAction(ctx, "disable")
	default:
		return fmt.Errorf("unsupported service manager %q", manager)
	}
}

func (m *Manager) removeDefinition(ctx context.Context, manager ManagerType, scope Scope) error {
	switch manager {
	case Systemd:
		if err := removeFile(m.unitPath(scope)); err != nil {
			return err
		}
		args := []string{"daemon-reload"}
		if scope == User {
			args = append([]string{"--user"}, args...)
		}
		_, err := m.runTimeout(ctx, "systemctl", args...)
		return wrapAction("remove unit", manager, err)
	case OpenRC, SysV:
		return removeFile(m.sysvPath())
	case RCd:
		return removeFile(filepath.Join("/usr/local/etc/rc.d", m.opts.InternalName))
	case Runit:
		return os.RemoveAll(filepath.Join("/etc/sv", m.opts.InternalName))
	case S6:
		return os.RemoveAll(filepath.Join("/etc/s6/sv", m.opts.InternalName))
	case Launchd:
		return removeFile(m.launchdPath(scope))
	case WindowsService:
		_, err := m.runTimeout(ctx, "sc.exe", "delete", m.opts.InternalName)
		return wrapAction("delete", manager, err)
	case WindowsTask:
		_, err := m.runTimeout(ctx, "schtasks.exe", "/Delete", "/TN", m.opts.InternalName, "/F")
		return wrapAction("delete", manager, err)
	default:
		return fmt.Errorf("unsupported service manager %q", manager)
	}
}

func (m *Manager) uninstall(ctx context.Context, manager ManagerType, scope Scope) (Result, error) {
	if err := m.validateRemovalPaths(); err != nil {
		return Result{}, err
	}
	if err := m.commandAction(ctx, manager, scope, "uninstall"); err != nil {
		return Result{}, err
	}
	if err := m.removeApplicationData(); err != nil {
		return Result{}, err
	}
	return Result{Available: true, Message: "Service uninstalled. Delete the binary manually: " + m.opts.BinaryPath, Status: Status{Manager: manager, Scope: scope}}, nil
}

func (m *Manager) removeApplicationData() error {
	paths := []string{m.opts.Paths.Config, m.opts.Paths.Data, m.opts.Paths.Cache, m.opts.Paths.Log, m.opts.Paths.Backup, m.opts.Paths.PID}
	seen := make(map[string]bool)
	for _, target := range paths {
		if target == "" {
			continue
		}
		absolute, err := filepath.Abs(target)
		if err != nil {
			return fmt.Errorf("resolve uninstall path: %w", err)
		}
		absolute = filepath.Clean(absolute)
		if seen[absolute] {
			continue
		}
		seen[absolute] = true
		if !safeRemovalPath(absolute, m.opts.InternalName) {
			return fmt.Errorf("refusing to remove unsafe application path %q", absolute)
		}
		if filepath.Clean(m.opts.Paths.PID) == absolute {
			if err := removeFile(absolute); err != nil {
				return err
			}
		} else if err := os.RemoveAll(absolute); err != nil {
			return fmt.Errorf("remove application data %q: %w", absolute, err)
		}
	}
	return nil
}

func (m *Manager) validateRemovalPaths() error {
	for _, target := range []string{m.opts.Paths.Config, m.opts.Paths.Data, m.opts.Paths.Cache, m.opts.Paths.Log, m.opts.Paths.Backup, m.opts.Paths.PID} {
		if target == "" {
			continue
		}
		absolute, err := filepath.Abs(target)
		if err != nil {
			return fmt.Errorf("resolve uninstall path: %w", err)
		}
		if !safeRemovalPath(filepath.Clean(absolute), m.opts.InternalName) {
			return fmt.Errorf("refusing to remove unsafe application path %q", absolute)
		}
	}
	return nil
}

func safeRemovalPath(path, name string) bool {
	volume := filepath.VolumeName(path)
	if path == string(filepath.Separator) || path == volume+string(filepath.Separator) || path == "." {
		return false
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.Clean(home) == path {
		return false
	}
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if strings.EqualFold(part, name) {
			return true
		}
	}
	return false
}

func removeExpectedSymlink(link, target string) error {
	current, err := os.Readlink(link)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if current != target {
		return fmt.Errorf("refusing to remove service link %q pointing to %q", link, current)
	}
	return os.Remove(link)
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove service definition %q: %w", path, err)
	}
	return nil
}
func wrapAction(action string, manager ManagerType, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s service using %s: %w", action, manager, err)
}
