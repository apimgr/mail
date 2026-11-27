package service

import (
	"context"
	"fmt"
)

func (m *Manager) launchdAction(ctx context.Context, scope Scope, action string) error {
	label := m.opts.InternalOrg + "." + m.opts.InternalName
	target := "system"
	if scope == User {
		target = "gui/" + currentUID()
	}
	switch action {
	case "install", "start":
		_, err := m.runTimeout(ctx, "launchctl", "bootstrap", target, m.launchdPath(scope))
		if err != nil {
			output, kickErr := m.runTimeout(ctx, "launchctl", "kickstart", "-k", target+"/"+label)
			if kickErr == nil {
				return nil
			}
			return commandError(action, string(Launchd), output, err)
		}
		return nil
	case "stop", "disable", "uninstall":
		_, err := m.runTimeout(ctx, "launchctl", "bootout", target+"/"+label)
		return wrapAction(action, Launchd, err)
	case "restart", "reload":
		status := Status{Manager: Launchd, Scope: scope}
		if err := m.inspectStatus(ctx, &status); err != nil {
			return err
		}
		if status.Running {
			if err := m.launchdAction(ctx, scope, "stop"); err != nil {
				return err
			}
		}
		return m.launchdAction(ctx, scope, "start")
	default:
		return fmt.Errorf("unsupported launchd action %q", action)
	}
}
