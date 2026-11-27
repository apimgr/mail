//go:build !windows

package service

import (
	"context"
	"errors"
)

func windowsElevated() bool { return false }
func (m *Manager) installWindowsService(context.Context) error {
	return errors.New("windows services are unavailable on this operating system")
}
func (m *Manager) installWindowsTask(context.Context) error {
	return errors.New("windows scheduled tasks are unavailable on this operating system")
}
func (m *Manager) windowsAction(context.Context, string, bool) error {
	return errors.New("windows services are unavailable on this operating system")
}
func (m *Manager) windowsTaskAction(context.Context, string) error {
	return errors.New("windows scheduled tasks are unavailable on this operating system")
}
func (m *Manager) inspectWindowsService(context.Context, *Status) error {
	return errors.New("windows services are unavailable on this operating system")
}
func (m *Manager) inspectWindowsTask(context.Context, *Status) error {
	return errors.New("windows scheduled tasks are unavailable on this operating system")
}
