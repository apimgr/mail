//go:build windows

package service

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
)

func windowsElevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

func (m *Manager) installWindowsService(ctx context.Context) error {
	quotedBinary := `"` + strings.ReplaceAll(m.opts.BinaryPath, `"`, `\"`) + `"`
	if _, err := m.runTimeout(ctx, "sc.exe", "create", m.opts.InternalName, "binPath=", quotedBinary, "start=", "auto", "DisplayName=", m.opts.ProjectName); err != nil {
		return wrapAction("create", WindowsService, err)
	}
	account := `NT SERVICE\` + m.opts.InternalName
	if _, err := m.runTimeout(ctx, "sc.exe", "config", m.opts.InternalName, "obj=", account, "password=", `""`); err != nil {
		return wrapAction("configure virtual service account", WindowsService, err)
	}
	for _, directory := range writablePaths(m.opts) {
		if _, err := m.runTimeout(ctx, "icacls.exe", directory, "/grant", account+":(OI)(CI)F", "/T", "/C"); err != nil {
			return fmt.Errorf("grant virtual service account access to %q: %w", directory, err)
		}
	}
	_, err := m.runTimeout(ctx, "sc.exe", "description", m.opts.InternalName, m.opts.ProjectName+" service")
	return wrapAction("set description", WindowsService, err)
}

func (m *Manager) installWindowsTask(ctx context.Context) error {
	command := `"` + strings.ReplaceAll(m.opts.BinaryPath, `"`, `\"`) + `"`
	_, err := m.runTimeout(ctx, "schtasks.exe", "/Create", "/SC", "ONLOGON", "/TN", m.opts.InternalName, "/TR", command, "/F", "/RL", "LIMITED")
	return wrapAction("create user logon task", WindowsTask, err)
}

func (m *Manager) windowsAction(ctx context.Context, action string, install bool) error {
	if install {
		action = "start"
	}
	switch action {
	case "start":
		_, err := m.runTimeout(ctx, "sc.exe", "start", m.opts.InternalName)
		return wrapAction(action, WindowsService, err)
	case "stop":
		_, err := m.runTimeout(ctx, "sc.exe", "stop", m.opts.InternalName)
		return wrapAction(action, WindowsService, err)
	case "restart", "reload":
		status := Status{Manager: WindowsService, Scope: System}
		if err := m.inspectWindowsService(ctx, &status); err != nil {
			return err
		}
		if status.Running {
			if _, err := m.runTimeout(ctx, "sc.exe", "stop", m.opts.InternalName); err != nil {
				return wrapAction("stop", WindowsService, err)
			}
		}
		_, err := m.runTimeout(ctx, "sc.exe", "start", m.opts.InternalName)
		return wrapAction("start", WindowsService, err)
	case "disable":
		_, err := m.runTimeout(ctx, "sc.exe", "config", m.opts.InternalName, "start=", "disabled")
		return wrapAction(action, WindowsService, err)
	default:
		return fmt.Errorf("unsupported Windows service action %q", action)
	}
}

func (m *Manager) windowsTaskAction(ctx context.Context, action string) error {
	switch action {
	case "start":
		_, err := m.runTimeout(ctx, "schtasks.exe", "/Run", "/TN", m.opts.InternalName)
		return wrapAction(action, WindowsTask, err)
	case "stop":
		_, err := m.runTimeout(ctx, "schtasks.exe", "/End", "/TN", m.opts.InternalName)
		return wrapAction(action, WindowsTask, err)
	case "restart", "reload":
		_, _ = m.runTimeout(ctx, "schtasks.exe", "/End", "/TN", m.opts.InternalName)
		_, err := m.runTimeout(ctx, "schtasks.exe", "/Run", "/TN", m.opts.InternalName)
		return wrapAction("run", WindowsTask, err)
	case "disable":
		_, err := m.runTimeout(ctx, "schtasks.exe", "/Change", "/TN", m.opts.InternalName, "/Disable")
		return wrapAction(action, WindowsTask, err)
	default:
		return fmt.Errorf("unsupported Windows task action %q", action)
	}
}

func (m *Manager) inspectWindowsService(ctx context.Context, status *Status) error {
	out, err := m.runTimeout(ctx, "sc.exe", "query", m.opts.InternalName)
	if err != nil {
		if strings.Contains(out, "1060") || strings.Contains(strings.ToLower(out), "does not exist") {
			return nil
		}
		return wrapAction("query", WindowsService, err)
	}
	status.Installed = true
	status.Running = strings.Contains(out, "RUNNING")
	status.PID = numberAfter(out, "PID")
	config, _ := m.runTimeout(ctx, "sc.exe", "qc", m.opts.InternalName)
	status.Enabled = strings.Contains(config, "AUTO_START")
	return nil
}

func (m *Manager) inspectWindowsTask(ctx context.Context, status *Status) error {
	out, err := m.runTimeout(ctx, "schtasks.exe", "/Query", "/TN", m.opts.InternalName, "/FO", "LIST")
	if err != nil {
		return nil
	}
	status.Installed = true
	status.Running = strings.Contains(strings.ToLower(out), "running")
	status.Enabled = !strings.Contains(strings.ToLower(out), "disabled")
	return nil
}
