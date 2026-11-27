package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apimgr/mail/src/paths"
)

func serviceTestOptions() Options {
	return Options{
		ProjectName: "mail", InternalName: "mail", InternalOrg: "apimgr",
		BinaryPath: "/usr/local/bin/mail", DocumentationURL: "https://apimgr.github.io/mail",
		Paths: &paths.Paths{Config: "/etc/mail", Data: "/var/lib/mail", Cache: "/var/cache/mail", Log: "/var/log/mail", PID: "/run/mail/mail.pid", Backup: "/var/backups/mail"},
	}
}

func TestNewValidatesIdentifiersAndNormalizesExecutable(t *testing.T) {
	opts := serviceTestOptions()
	manager, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if manager.opts.BinaryPath != filepath.Clean(opts.BinaryPath) || manager.opts.DocumentationURL != opts.DocumentationURL {
		t.Fatalf("normalized options = %+v", manager.opts)
	}
	for _, field := range []string{"ProjectName", "InternalName", "InternalOrg"} {
		invalid := serviceTestOptions()
		switch field {
		case "ProjectName":
			invalid.ProjectName = "../mail"
		case "InternalName":
			invalid.InternalName = "-mail"
		case "InternalOrg":
			invalid.InternalOrg = "org/name"
		}
		if _, err := New(invalid); err == nil {
			t.Errorf("New accepted invalid %s", field)
		}
	}
}

func TestValidIdentifierAndScopeSelection(t *testing.T) {
	for _, value := range []string{"mail", "m1", "mail-server", "mail_server"} {
		if !validIdentifier(value) {
			t.Errorf("validIdentifier(%q) = false", value)
		}
	}
	for _, value := range []string{"", "1mail", "-mail", "mail/name", "mail name"} {
		if validIdentifier(value) {
			t.Errorf("validIdentifier(%q) = true", value)
		}
	}
	if got, err := managerForScope("darwin", User); err != nil || got != Launchd {
		t.Errorf("user manager on macOS = %q, %v", got, err)
	}
	if got, err := managerForScope("windows", User); err != nil || got != WindowsTask {
		t.Errorf("user manager on Windows = %q, %v", got, err)
	}
	if _, err := managerForScope("plan9", User); !errors.Is(err, ErrUserServiceUnavailable) {
		t.Errorf("unsupported user manager error = %v", err)
	}
	if _, err := managerForScope("plan9", Scope("invalid")); err == nil {
		t.Error("invalid service scope was accepted")
	}
}

func TestServiceDefinitionEscapingAndWritablePathDeduplication(t *testing.T) {
	opts := serviceTestOptions()
	opts.BinaryPath = `/opt/mail/with space/it's "binary"`
	opts.ExtraWritablePaths = []string{"/var/lib/mail/", "/custom/path", ""}
	paths := writablePaths(opts)
	if len(paths) != 7 || paths[1] != filepath.Clean(opts.Paths.Data) {
		t.Fatalf("writable paths = %#v", paths)
	}
	for manager, definition := range map[string]string{
		"systemd": systemdDefinition(opts, System), "openrc": openRCDefinition(opts),
		"sysv": sysVDefinition(opts), "rc.d": rcDefinition(opts), "launchd": launchdDefinition(opts),
	} {
		if !strings.Contains(definition, "mail") {
			t.Errorf("%s definition omitted service identity", manager)
		}
	}
	if !strings.Contains(systemdDefinition(opts, User), "WantedBy=default.target") {
		t.Error("user systemd definition omitted default target")
	}
	if !strings.Contains(shellQuote("it's"), `'it'"'"'s'`) || !strings.Contains(xmlEscape(`<&>"'`), "&lt;&amp;&gt;&quot;&apos;") {
		t.Error("service definition escaping is incorrect")
	}
	if !strings.Contains(systemdEscape(`a%"b`), `a%%\"b`) {
		t.Errorf("systemdEscape = %q", systemdEscape(`a%"b`))
	}
}

func TestStatusParsersAndErrorWrapping(t *testing.T) {
	if !containsRunning("service is running") || containsRunning("service stopped") {
		t.Error("running-state parser returned an incorrect result")
	}
	if got := numberAfter("run: mail (pid 5432) 4s", "pid "); got != 5432 {
		t.Errorf("parsed PID = %d", got)
	}
	if got := numberAfter("no process", "pid "); got != 0 {
		t.Errorf("missing PID = %d", got)
	}
	wrapped := wrapAction("restart", Systemd, errors.New("permission denied"))
	if wrapped == nil || !strings.Contains(wrapped.Error(), "restart service using systemd") {
		t.Errorf("wrapped action error = %v", wrapped)
	}
	if wrapAction("restart", Systemd, nil) != nil {
		t.Error("successful action produced an error")
	}
	if _, err := New(Options{ProjectName: "mail", InternalName: "mail", InternalOrg: "apimgr", BinaryPath: "/mail"}); err != nil {
		t.Fatal(err)
	}
}

func TestUninstallRequiresExplicitConfirmation(t *testing.T) {
	manager, err := New(serviceTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Uninstall(context.Background(), User, false); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("unconfirmed uninstall error = %v", err)
	}
}

func TestPathSafetyAndSymlinkManagement(t *testing.T) {
	if safeRemovalPath(string(filepath.Separator), "mail") || safeRemovalPath(".", "mail") {
		t.Fatal("root or current directory accepted for removal")
	}
	if !safeRemovalPath(filepath.Join(string(filepath.Separator), "tmp", "mail", "data"), "mail") {
		t.Fatal("application path rejected for removal")
	}
	if safeRemovalPath(filepath.Join(string(filepath.Separator), "tmp", "mailer", "data"), "mail") {
		t.Fatal("similarly named path accepted for removal")
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "mail-link")
	target := filepath.Join(dir, "mail-service")
	if err := enableSymlink(link, target); err != nil {
		t.Fatal(err)
	}
	if err := enableSymlink(link, target); err != nil {
		t.Fatalf("idempotent symlink creation failed: %v", err)
	}
	if err := enableSymlink(link, filepath.Join(dir, "other")); err == nil {
		t.Fatal("symlink pointing elsewhere was accepted")
	}
	if err := removeExpectedSymlink(link, filepath.Join(dir, "other")); err == nil {
		t.Fatal("unexpected symlink target was removed")
	}
	if err := removeExpectedSymlink(link, target); err != nil {
		t.Fatal(err)
	}
	if err := removeExpectedSymlink(link, target); err != nil {
		t.Fatalf("removing an already absent symlink failed: %v", err)
	}
	file := filepath.Join(dir, "definition")
	if err := os.WriteFile(file, []byte("unit"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeFile(file); err != nil {
		t.Fatal(err)
	}
	if err := removeFile(file); err != nil {
		t.Fatalf("removing an already absent file failed: %v", err)
	}
}

func TestUnixWindowsOperationsReturnUnsupportedErrors(t *testing.T) {
	manager, err := New(serviceTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	if windowsElevated() {
		t.Fatal("Unix platform reported Windows elevation")
	}
	status := &Status{}
	operations := []func() error{
		func() error { return manager.installWindowsService(context.Background()) },
		func() error { return manager.installWindowsTask(context.Background()) },
		func() error { return manager.windowsAction(context.Background(), "start", false) },
		func() error { return manager.windowsTaskAction(context.Background(), "start") },
		func() error { return manager.inspectWindowsService(context.Background(), status) },
		func() error { return manager.inspectWindowsTask(context.Background(), status) },
	}
	for index, operation := range operations {
		if err := operation(); err == nil {
			t.Errorf("Windows-only operation %d succeeded on Unix", index)
		}
	}
}

func TestUserSystemdInstallLifecycleAndSafeUninstall(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	dataRoot := filepath.Join(root, "mail")
	opts := serviceTestOptions()
	opts.BinaryPath = filepath.Join(root, "bin", "mail")
	opts.Paths = &paths.Paths{
		Config: filepath.Join(dataRoot, "config"), Data: filepath.Join(dataRoot, "data"),
		Cache: filepath.Join(dataRoot, "cache"), Log: filepath.Join(dataRoot, "log"),
		PID: filepath.Join(dataRoot, "run", "mail.pid"), Backup: filepath.Join(dataRoot, "backup"),
		SSL: filepath.Join(dataRoot, "config", "ssl"), Security: filepath.Join(dataRoot, "data", "security"),
		DBDir: filepath.Join(dataRoot, "data", "db"),
	}
	var commands [][]string
	opts.RunCommand = func(_ context.Context, name string, args ...string) (string, error) {
		if name != "systemctl" {
			return "", errors.New("unexpected command: " + name)
		}
		commands = append(commands, append([]string(nil), args...))
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "ActiveState"):
			return "active\n", nil
		case strings.Contains(joined, "MainPID"):
			return "4321\n", nil
		case strings.Contains(joined, "is-enabled"):
			return "enabled\n", nil
		default:
			return "", nil
		}
	}
	manager, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	installed, err := manager.Install(ctx, User)
	if err != nil || !installed.Available || !installed.Status.Installed || !installed.Status.Running || !installed.Status.Enabled || installed.Status.PID != 4321 {
		t.Fatalf("user service install = %+v, %v", installed, err)
	}
	unitPath := manager.unitPath(User)
	if contents, err := os.ReadFile(unitPath); err != nil || !strings.Contains(string(contents), "WantedBy=default.target") {
		t.Fatalf("installed unit content = %q, %v", contents, err)
	}
	if _, err := manager.Restart(ctx, User); err != nil {
		t.Fatalf("restart user service: %v", err)
	}
	if _, err := manager.Uninstall(ctx, User, false); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("unconfirmed uninstall = %v", err)
	}
	uninstalled, err := manager.Uninstall(ctx, User, true)
	if err != nil || !uninstalled.Available {
		t.Fatalf("confirmed uninstall = %+v, %v", uninstalled, err)
	}
	if _, err := os.Stat(unitPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unit remains after uninstall: %v", err)
	}
	for _, removed := range []string{opts.Paths.Config, opts.Paths.Data, opts.Paths.Cache, opts.Paths.Log, opts.Paths.Backup} {
		if _, err := os.Stat(removed); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("application path remains after uninstall (%s): %v", removed, err)
		}
	}
	if len(commands) < 10 {
		t.Fatalf("service lifecycle ran too few manager commands: %d", len(commands))
	}
}

func TestCommandActionAndEnableFlowsAcrossUnixManagers(t *testing.T) {
	manager, err := New(serviceTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	var commands [][]string
	manager.opts.RunCommand = func(_ context.Context, name string, args ...string) (string, error) {
		commands = append(commands, append([]string{name}, args...))
		return "", nil
	}
	ctx := context.Background()
	cases := []struct {
		manager ManagerType
		scope   Scope
		action  string
		want    string
	}{
		{Systemd, User, "reload", "systemctl --user restart mail.service"},
		{OpenRC, System, "reload", "rc-service mail restart"},
		{SysV, System, "reload", "/etc/init.d/mail restart"},
		{Runit, System, "reload", "sv restart /etc/service/mail"},
		{RCd, System, "reload", "service mail restart"},
	}
	for _, test := range cases {
		commands = nil
		if err := manager.commandAction(ctx, test.manager, test.scope, test.action); err != nil {
			t.Errorf("%s %s: %v", test.manager, test.action, err)
			continue
		}
		if len(commands) != 1 || strings.Join(commands[0], " ") != test.want {
			t.Errorf("%s %s commands = %#v, want %q", test.manager, test.action, commands, test.want)
		}
	}
	for _, test := range []struct {
		manager ManagerType
		want    string
	}{
		{Systemd, "systemctl --user enable --now mail.service"},
		{OpenRC, "rc-update add mail default"},
		{RCd, "sysrc mail_enable=YES"},
	} {
		commands = nil
		if err := manager.enableAndStart(ctx, test.manager, User); err != nil {
			t.Errorf("enable and start %s: %v", test.manager, err)
			continue
		}
		if len(commands) == 0 || strings.Join(commands[0], " ") != test.want {
			t.Errorf("enable %s commands = %#v, want first command %q", test.manager, commands, test.want)
		}
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "update-rc.d"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	commands = nil
	if err := manager.enableAndStart(ctx, SysV, System); err != nil {
		t.Fatalf("enable and start SysV: %v", err)
	}
	if len(commands) != 2 || strings.Join(commands[0], " ") != "update-rc.d mail defaults" || strings.Join(commands[1], " ") != "/etc/init.d/mail start" {
		t.Errorf("SysV enable commands = %#v", commands)
	}
	for _, test := range []struct {
		manager ManagerType
		want    string
	}{
		{OpenRC, "rc-update del mail default"},
		{RCd, "sysrc -x mail_enable"},
	} {
		commands = nil
		if err := manager.disableDefinition(ctx, test.manager, User); err != nil {
			t.Errorf("disable %s: %v", test.manager, err)
		}
		if len(commands) == 0 || strings.Join(commands[0], " ") != test.want {
			t.Errorf("disable %s commands = %#v", test.manager, commands)
		}
	}
	if err := manager.commandAction(ctx, ManagerType("unknown"), User, "start"); err == nil {
		t.Error("unsupported manager action succeeded")
	}
}

func TestLaunchdActionAndInstallDefinition(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	manager, err := New(serviceTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	var commands [][]string
	manager.opts.RunCommand = func(_ context.Context, name string, args ...string) (string, error) {
		if name != "launchctl" {
			return "", errors.New("unexpected command: " + name)
		}
		commands = append(commands, append([]string(nil), args...))
		return "", nil
	}
	ctx := context.Background()
	if err := manager.installDefinition(ctx, Launchd, User); err != nil {
		t.Fatal(err)
	}
	definition, err := os.ReadFile(manager.launchdPath(User))
	if err != nil || !strings.Contains(string(definition), "apimgr.mail") {
		t.Fatalf("launchd definition = %q, %v", definition, err)
	}
	if err := manager.launchdAction(ctx, User, "start"); err != nil {
		t.Fatal(err)
	}
	if err := manager.launchdAction(ctx, User, "stop"); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || !strings.HasPrefix(strings.Join(commands[0], " "), "bootstrap gui/") || !strings.HasPrefix(strings.Join(commands[1], " "), "bootout gui/") {
		t.Fatalf("launchd commands = %#v", commands)
	}
	if err := manager.launchdAction(ctx, User, "unsupported"); err == nil {
		t.Fatal("unsupported launchd operation succeeded")
	}
}

func TestStatusAndRemovalGuards(t *testing.T) {
	opts := serviceTestOptions()
	opts.Paths = &paths.Paths{Config: "/var/lib/app/config", Data: "/var/lib/app/data", Cache: "/var/cache/app", Log: "/var/log/app", Backup: "/var/backups/app", PID: "/run/app.pid"}
	manager, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	status := Status{Manager: Systemd, Scope: User}
	if err := manager.inspectStatus(context.Background(), &status); err != nil || status.Installed {
		t.Fatalf("status for absent user unit = %+v, %v", status, err)
	}
	if err := manager.validateRemovalPaths(); err == nil {
		t.Fatal("uninstall accepted unrelated default paths")
	}
	if err := manager.commandAction(context.Background(), ManagerType("unknown"), User, "start"); err == nil {
		t.Error("unsupported service manager action succeeded")
	}
}

func TestAbsentUnixServiceDefinitionsAndExecutableWriters(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	opts := serviceTestOptions()
	opts.InternalName = "mail-test-uninstalled"
	manager, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, managerType := range []ManagerType{Systemd, OpenRC, SysV, Runit, RCd, Launchd} {
		status := Status{Manager: managerType, Scope: User}
		if err := manager.inspectStatus(context.Background(), &status); err != nil {
			t.Errorf("inspect absent %s service: %v", managerType, err)
		}
		if status.Installed {
			t.Errorf("absent %s service was reported installed", managerType)
		}
	}
	definition := filepath.Join(root, "init.d", "mail")
	if err := manager.writeExecutable(definition, "#!/bin/sh\nexit 0\n"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(definition)
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("executable service definition mode = %v, %v", info, err)
	}
	if err := manager.removeDefinition(context.Background(), Launchd, User); err != nil {
		t.Fatalf("removing an already absent launchd definition failed: %v", err)
	}
}
