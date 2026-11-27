package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/apimgr/mail/src/paths"
)

type Scope string

const (
	System Scope = "system"
	User   Scope = "user"
)

type ManagerType string

const (
	Systemd        ManagerType = "systemd"
	OpenRC         ManagerType = "openrc"
	Runit          ManagerType = "runit"
	S6             ManagerType = "s6"
	SysV           ManagerType = "sysv"
	RCd            ManagerType = "rc.d"
	Launchd        ManagerType = "launchd"
	WindowsService ManagerType = "windows-service"
	WindowsTask    ManagerType = "windows-task"
)

var (
	ErrElevationRequired      = errors.New("managing the system service requires administrator privileges")
	ErrConfirmationRequired   = errors.New("service uninstall requires explicit confirmation")
	ErrUserServiceUnavailable = errors.New("no user service manager is available on this platform")
	ErrServiceNotInstalled    = errors.New("service is not installed")
	ErrServiceNotRunning      = errors.New("service is not running")
)

type Options struct {
	ProjectName        string
	InternalName       string
	InternalOrg        string
	BinaryPath         string
	DocumentationURL   string
	Paths              *paths.Paths
	ExtraWritablePaths []string
	RunCommand         func(context.Context, string, ...string) (string, error)
}

type Status struct {
	Manager   ManagerType `json:"manager"`
	Scope     Scope       `json:"scope"`
	Installed bool        `json:"installed"`
	Running   bool        `json:"running"`
	Enabled   bool        `json:"enabled"`
	PID       int         `json:"pid,omitempty"`
}

type Result struct {
	Available bool
	Message   string
	Status    Status
}

type Manager struct{ opts Options }

func New(opts Options) (*Manager, error) {
	for key, value := range map[string]string{"project_name": opts.ProjectName, "internal_name": opts.InternalName, "internal_org": opts.InternalOrg} {
		if !validIdentifier(value) {
			return nil, fmt.Errorf("invalid %s %q", key, value)
		}
	}
	if opts.BinaryPath == "" {
		value, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("locate server executable: %w", err)
		}
		opts.BinaryPath = value
	}
	binary, err := filepath.Abs(opts.BinaryPath)
	if err != nil {
		return nil, fmt.Errorf("resolve server executable: %w", err)
	}
	opts.BinaryPath = filepath.Clean(binary)
	if opts.Paths == nil {
		opts.Paths = paths.GetDefaultPaths()
	}
	if opts.DocumentationURL == "" {
		opts.DocumentationURL = "https://" + opts.InternalOrg + ".github.io/" + opts.ProjectName
	}
	return &Manager{opts: opts}, nil
}

func validIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9' && index > 0) || (index > 0 && (char == '-' || char == '_')) {
			continue
		}
		return false
	}
	return true
}

func (m *Manager) Detect() (ManagerType, error) { return detectManager(m.run) }

func detectManager(run func(context.Context, string, ...string) (string, error)) (ManagerType, error) {
	if runtime.GOOS == "windows" {
		return WindowsService, nil
	}
	if runtime.GOOS == "darwin" {
		return Launchd, nil
	}
	if runtime.GOOS == "freebsd" || runtime.GOOS == "openbsd" || runtime.GOOS == "netbsd" {
		return RCd, nil
	}
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("service management is unsupported on %s", runtime.GOOS)
	}
	if exists("/run/systemd/system") {
		return Systemd, nil
	}
	if exists("/sbin/openrc-run") && exists("/run/openrc") {
		return OpenRC, nil
	}
	if exists("/etc/runit") || executable("sv") {
		return Runit, nil
	}
	if executable("s6-svc") && s6Running() {
		return S6, nil
	}
	if exists("/etc/init.d") && (executable("update-rc.d") || executable("chkconfig")) {
		return SysV, nil
	}
	return "", errors.New("no supported running service manager was detected")
}

func (m *Manager) run(ctx context.Context, name string, args ...string) (string, error) {
	if m.opts.RunCommand != nil {
		return m.opts.RunCommand(ctx, name, args...)
	}
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s: %w", name, err)
	}
	return string(output), nil
}

func executable(name string) bool { _, err := exec.LookPath(name); return err == nil }
func exists(name string) bool     { _, err := os.Stat(name); return err == nil }

func (m *Manager) elevated() bool {
	if runtime.GOOS == "windows" {
		return windowsElevated()
	}
	return paths.IsRoot()
}

// IsElevated reports whether this process can manage the system service directly.
func (m *Manager) IsElevated() bool { return m.elevated() }

func (m *Manager) CanEscalate(ctx context.Context) bool {
	if m.elevated() {
		return true
	}
	if runtime.GOOS == "windows" {
		return false
	}
	for _, program := range []string{"sudo", "doas"} {
		if executable(program) {
			if _, err := m.run(ctx, program, "-n", "true"); err == nil {
				return true
			}
		}
	}
	if runtime.GOOS == "darwin" && executable("osascript") {
		return true
	}
	return false
}

func (m *Manager) RequiresEscalation(ctx context.Context, scope Scope) bool {
	return scope == System && !m.elevated()
}

func (m *Manager) Status(ctx context.Context, scope Scope) (Status, error) {
	manager, err := managerForScope(runtime.GOOS, scope)
	if err != nil {
		return Status{Scope: scope}, err
	}
	status := Status{Manager: manager, Scope: scope}
	if err := m.inspectStatus(ctx, &status); err != nil {
		return status, err
	}
	return status, nil
}

func managerForScope(goos string, scope Scope) (ManagerType, error) {
	if scope == System {
		return detectManager(execRun)
	}
	if scope != User {
		return "", fmt.Errorf("invalid service scope %q", scope)
	}
	switch goos {
	case "linux":
		if executable("systemctl") {
			return Systemd, nil
		}
	case "darwin":
		return Launchd, nil
	case "windows":
		return WindowsTask, nil
	}
	return "", ErrUserServiceUnavailable
}

func execRun(ctx context.Context, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	return string(output), err
}

func (m *Manager) target(scope Scope) (ManagerType, error) {
	if scope == System {
		return m.Detect()
	}
	return managerForScope(runtime.GOOS, scope)
}

func (m *Manager) act(ctx context.Context, action string, scope Scope) (Result, error) {
	manager, err := m.target(scope)
	if err != nil {
		if scope == User && errors.Is(err, ErrUserServiceUnavailable) && action == "install" {
			return Result{Available: false, Message: userServiceUnavailableMessage()}, nil
		}
		return Result{}, err
	}
	if scope == System && !m.elevated() {
		return Result{}, ErrElevationRequired
	}
	if action == "install" {
		return m.install(ctx, manager, scope)
	}
	status, err := m.Status(ctx, scope)
	if err != nil {
		return Result{}, err
	}
	if !status.Installed {
		return Result{}, ErrServiceNotInstalled
	}
	if action == "uninstall" {
		return m.uninstall(ctx, manager, scope)
	}
	if err := m.commandAction(ctx, manager, scope, action); err != nil {
		return Result{}, err
	}
	updated, err := m.Status(ctx, scope)
	return Result{Available: true, Status: updated}, err
}

func (m *Manager) Install(ctx context.Context, scope Scope) (Result, error) {
	return m.act(ctx, "install", scope)
}
func (m *Manager) Start(ctx context.Context, scope Scope) (Result, error) {
	return m.act(ctx, "start", scope)
}
func (m *Manager) Stop(ctx context.Context, scope Scope) (Result, error) {
	return m.act(ctx, "stop", scope)
}
func (m *Manager) Restart(ctx context.Context, scope Scope) (Result, error) {
	return m.act(ctx, "restart", scope)
}
func (m *Manager) Reload(ctx context.Context, scope Scope) (Result, error) {
	return m.act(ctx, "reload", scope)
}
func (m *Manager) Disable(ctx context.Context, scope Scope) (Result, error) {
	return m.act(ctx, "disable", scope)
}

func (m *Manager) Uninstall(ctx context.Context, scope Scope, confirmed bool) (Result, error) {
	if !confirmed {
		return Result{}, ErrConfirmationRequired
	}
	return m.act(ctx, "uninstall", scope)
}

func (m *Manager) runTimeout(ctx context.Context, name string, args ...string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return m.run(commandCtx, name, args...)
}

func userServiceUnavailableMessage() string {
	return "No per-user service manager is available on this platform. Start the server with --daemon or ask an administrator to install the system service."
}

func currentUID() string {
	current, err := user.Current()
	if err != nil {
		return "0"
	}
	return current.Uid
}

func (m *Manager) unitPath(scope Scope) string {
	if scope == User {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "systemd", "user", m.opts.InternalName+".service")
	}
	return filepath.Join("/etc/systemd/system", m.opts.InternalName+".service")
}

func (m *Manager) launchdPath(scope Scope) string {
	label := m.opts.InternalOrg + "." + m.opts.InternalName
	if scope == User {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	}
	return filepath.Join("/Library/LaunchDaemons", label+".plist")
}

func (m *Manager) sysvPath() string { return filepath.Join("/etc/init.d", m.opts.InternalName) }

func commandError(action, manager string, output string, err error) error {
	message := strings.TrimSpace(output)
	if message == "" {
		message = err.Error()
	}
	return fmt.Errorf("%s service: %s: %s", action, manager, message)
}
