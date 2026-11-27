package mailservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	ErrServiceNotFound = errors.New("mail service not found")
	ErrServiceMissing  = errors.New("mail service is not installed")
	ErrManagerMissing  = errors.New("host service manager is unsupported")
	ErrInvalidAction   = errors.New("unsupported service action")
)

type serviceManager struct {
	name string
}

// Inspect returns installed components and status where the host service
// manager can provide it without changing host state.
func Inspect() []Component {
	components := Discover()
	manager := detectServiceManager()
	for index := range components {
		components[index] = inspectComponent(manager, components[index])
	}
	return components
}

// InspectOne returns the current state for one supported mail component.
func InspectOne(name string) (Component, error) {
	for _, component := range Discover() {
		if component.Name == name {
			return inspectComponent(detectServiceManager(), component), nil
		}
	}
	return Component{}, ErrServiceNotFound
}

// Operate performs one explicit service-manager action on an installed,
// allowlisted mail component.
func Operate(ctx context.Context, name, action string) (Component, error) {
	component, err := InspectOne(name)
	if err != nil {
		return Component{}, err
	}
	if !component.Installed {
		return component, ErrServiceMissing
	}
	if action != "start" && action != "stop" && action != "restart" && action != "reload" {
		return component, ErrInvalidAction
	}
	manager := detectServiceManager()
	if manager.name == "" {
		return component, ErrManagerMissing
	}
	command, args, err := managerCommand(manager, serviceUnit(name), action)
	if err != nil {
		return component, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, command, args...).Run(); err != nil {
		return component, fmt.Errorf("service operation failed")
	}
	return InspectOne(name)
}

func detectServiceManager() serviceManager {
	switch runtime.GOOS {
	case "linux":
		if _, err := os.Stat("/run/systemd/system"); err == nil && commandExists("systemctl") {
			return serviceManager{name: "systemd"}
		}
		if _, err := os.Stat("/run/openrc"); err == nil && commandExists("rc-service") {
			return serviceManager{name: "openrc"}
		}
		if _, err := os.Stat("/etc/init.d"); err == nil && commandExists("service") {
			return serviceManager{name: "sysv"}
		}
	case "freebsd":
		if commandExists("service") {
			return serviceManager{name: "freebsd"}
		}
	}
	return serviceManager{}
}

func inspectComponent(manager serviceManager, component Component) Component {
	if !component.Installed {
		component.Status = "not_installed"
		return component
	}
	component.Version = componentVersion(component.Name)
	if manager.name == "" {
		component.Status = "unknown"
		return component
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unit := serviceUnit(component.Name)
	switch manager.name {
	case "systemd":
		output, err := runCommand(ctx, "systemctl", "is-active", unit)
		state := strings.TrimSpace(output)
		if err != nil && state != "inactive" && state != "failed" {
			component.Status = "unknown"
			return component
		}
		running := err == nil && state == "active"
		component.Running = &running
		component.Status = statusName(running)
		if output, err := runCommand(ctx, "systemctl", "is-enabled", unit); err == nil || output != "" {
			value := err == nil && strings.TrimSpace(output) == "enabled"
			component.Enabled = &value
		}
		if output, err := runCommand(ctx, "systemctl", "show", "--property=MainPID", "--value", unit); err == nil {
			component.PID, _ = strconv.Atoi(strings.TrimSpace(output))
		}
		if running {
			if output, err := runCommand(ctx, "systemctl", "show", "--property=ActiveEnterTimestamp", "--value", unit); err == nil {
				component.Uptime = elapsedSince(parseSystemdTimestamp(output))
			}
		}
	case "openrc":
		_, err := runCommand(ctx, "rc-service", unit, "status")
		running := err == nil
		component.Running = &running
		component.Status = statusName(running)
		if output, err := runCommand(ctx, "rc-update", "show", "-v"); err == nil {
			value := strings.Contains(output, unit)
			component.Enabled = &value
		}
	case "sysv", "freebsd":
		output, err := runCommand(ctx, "service", unit, "status")
		running := err == nil
		component.Running = &running
		component.Status = statusName(running)
		component.PID = parsePID(output)
		if manager.name == "freebsd" {
			if output, err := runCommand(ctx, "sysrc", "-n", unit+"_enable"); err == nil {
				value := strings.EqualFold(strings.TrimSpace(output), "yes")
				component.Enabled = &value
			}
		}
	}
	return component
}

func managerCommand(manager serviceManager, unit, action string) (string, []string, error) {
	switch manager.name {
	case "systemd":
		return "systemctl", []string{action, unit}, nil
	case "openrc":
		return "rc-service", []string{unit, action}, nil
	case "sysv", "freebsd":
		return "service", []string{unit, action}, nil
	default:
		return "", nil, ErrManagerMissing
	}
}

func serviceUnit(name string) string {
	if name == "amavis" {
		return "amavisd"
	}
	return name
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(output), err
}

func statusName(running bool) string {
	if running {
		return "running"
	}
	return "stopped"
}

var processIDPattern = regexp.MustCompile(`(?i)pid\s+([0-9]+)`)

func parsePID(output string) int {
	match := processIDPattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return 0
	}
	value, _ := strconv.Atoi(match[1])
	return value
}

func componentVersion(name string) string {
	commands := map[string]struct {
		name string
		args []string
	}{
		"postfix":      {"postconf", []string{"-h", "mail_version"}},
		"dovecot":      {"dovecot", []string{"--version"}},
		"spamassassin": {"spamassassin", []string{"--version"}},
		"rspamd":       {"rspamd", []string{"--version"}},
		"clamav":       {"clamscan", []string{"--version"}},
		"opendkim":     {"opendkim", []string{"-V"}},
	}
	command, ok := commands[name]
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, command.name, command.args...).Output()
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(output)), "\n")
	line = strings.TrimSpace(line)
	if len(line) > 128 {
		line = line[:128]
	}
	return line
}

func parseSystemdTimestamp(value string) time.Time {
	for _, layout := range []string{"Mon 2006-01-02 15:04:05 MST", "Mon 2006-01-02 15:04:05 -0700"} {
		if parsed, err := time.ParseInLocation(layout, strings.TrimSpace(value), time.Local); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func elapsedSince(start time.Time) string {
	if start.IsZero() || time.Now().Before(start) {
		return ""
	}
	duration := time.Since(start).Round(time.Second)
	days := int(duration.Hours()) / 24
	duration -= time.Duration(days) * 24 * time.Hour
	if days > 0 {
		return fmt.Sprintf("%dd %s", days, duration)
	}
	return duration.String()
}
