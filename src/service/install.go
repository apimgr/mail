package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/apimgr/mail/src/paths"
)

func (m *Manager) install(ctx context.Context, manager ManagerType, scope Scope) (Result, error) {
	if scope == System && runtime.GOOS != "windows" {
		identity, err := paths.EnsureServiceIdentity(m.opts.InternalName, m.opts.Paths.Data)
		if err != nil {
			return Result{}, fmt.Errorf("prepare service identity: %w", err)
		}
		if err := m.opts.Paths.EnsureAllDirs(true); err != nil {
			return Result{}, fmt.Errorf("prepare service directories: %w", err)
		}
		if err := m.opts.Paths.EnsureBackupDir(true); err != nil {
			return Result{}, fmt.Errorf("prepare service backup directory: %w", err)
		}
		if err := m.opts.Paths.PrepareServicePaths(identity); err != nil {
			return Result{}, fmt.Errorf("prepare service-owned paths: %w", err)
		}
	} else {
		if err := m.opts.Paths.EnsureAllDirs(false); err != nil {
			return Result{}, fmt.Errorf("prepare user service directories: %w", err)
		}
		if err := m.opts.Paths.EnsureBackupDir(false); err != nil {
			return Result{}, fmt.Errorf("prepare user service backup directory: %w", err)
		}
	}
	if err := m.installDefinition(ctx, manager, scope); err != nil {
		return Result{}, err
	}
	if err := m.commandAction(ctx, manager, scope, "install"); err != nil {
		return Result{}, err
	}
	status, err := m.Status(ctx, scope)
	return Result{Available: true, Status: status}, err
}

func (m *Manager) installDefinition(ctx context.Context, manager ManagerType, scope Scope) error {
	switch manager {
	case Systemd:
		return m.writeUnit(ctx, scope)
	case OpenRC:
		return m.writeExecutable(filepath.Join("/etc/init.d", m.opts.InternalName), openRCDefinition(m.opts))
	case SysV:
		return m.writeExecutable(m.sysvPath(), sysVDefinition(m.opts))
	case Runit:
		return m.writeRunit(ctx)
	case S6:
		return m.writeS6(ctx)
	case RCd:
		return m.writeExecutable(filepath.Join("/usr/local/etc/rc.d", m.opts.InternalName), rcDefinition(m.opts))
	case Launchd:
		return m.writeDefinition(m.launchdPath(scope), launchdDefinition(m.opts))
	case WindowsService:
		return m.installWindowsService(ctx)
	case WindowsTask:
		return m.installWindowsTask(ctx)
	default:
		return fmt.Errorf("unsupported service manager %q", manager)
	}
}

func (m *Manager) writeUnit(ctx context.Context, scope Scope) error {
	if err := m.writeDefinition(m.unitPath(scope), systemdDefinition(m.opts, scope)); err != nil {
		return err
	}
	args := []string{"daemon-reload"}
	if scope == User {
		args = append([]string{"--user"}, args...)
	}
	if _, err := m.runTimeout(ctx, "systemctl", args...); err != nil {
		return commandError("reload unit", "systemd", "", err)
	}
	return nil
}

func (m *Manager) writeDefinition(path, content string) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return fmt.Errorf("create service definition directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".service-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary service definition: %w", err)
	}
	tempPath := temporary.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := temporary.WriteString(content); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write service definition: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync service definition: %w", err)
	}
	if err := temporary.Chmod(0644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set service definition mode: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close service definition: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("install service definition: %w", err)
	}
	return nil
}

func (m *Manager) writeExecutable(path, content string) error {
	if err := m.writeDefinition(path, content); err != nil {
		return err
	}
	if err := os.Chmod(path, 0755); err != nil {
		return fmt.Errorf("make service script executable: %w", err)
	}
	return nil
}

func (m *Manager) writeRunit(ctx context.Context) error {
	serviceDir := filepath.Join("/etc/sv", m.opts.InternalName)
	if err := os.MkdirAll(filepath.Join(serviceDir, "log"), 0755); err != nil {
		return fmt.Errorf("create runit service directory: %w", err)
	}
	if err := m.writeDefinition(filepath.Join(serviceDir, "run"), "#!/bin/sh\nexec "+shellQuote(m.opts.BinaryPath)+" 2>&1\n"); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Join(serviceDir, "run"), 0755); err != nil {
		return err
	}
	if err := m.writeDefinition(filepath.Join(serviceDir, "log", "run"), "#!/bin/sh\nexec svlogd -tt "+shellQuote(m.opts.Paths.Log)+"\n"); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Join(serviceDir, "log", "run"), 0755); err != nil {
		return err
	}
	return enableSymlink(filepath.Join("/etc/service", m.opts.InternalName), serviceDir)
}

func (m *Manager) writeS6(ctx context.Context) error {
	scanDir, err := s6ScanDirectory()
	if err != nil {
		return err
	}
	serviceDir := filepath.Join("/etc/s6/sv", m.opts.InternalName)
	if err := os.MkdirAll(filepath.Join(serviceDir, "log"), 0755); err != nil {
		return err
	}
	if err := m.writeDefinition(filepath.Join(serviceDir, "run"), "#!/bin/sh\nexec "+shellQuote(m.opts.BinaryPath)+" 2>&1\n"); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Join(serviceDir, "run"), 0755); err != nil {
		return err
	}
	if err := m.writeDefinition(filepath.Join(serviceDir, "log", "run"), "#!/bin/sh\nexec s6-log n10 s1000000 "+shellQuote(m.opts.Paths.Log)+"\n"); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Join(serviceDir, "log", "run"), 0755); err != nil {
		return err
	}
	if err := enableSymlink(filepath.Join(scanDir, m.opts.InternalName), serviceDir); err != nil {
		return err
	}
	if _, err := m.runTimeout(ctx, "s6-svscanctl", "-a", scanDir); err != nil {
		return commandError("rescan", "s6", "", err)
	}
	return nil
}

func enableSymlink(link, target string) error {
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		return err
	}
	if current, err := os.Readlink(link); err == nil {
		if current != target {
			return fmt.Errorf("service link already points elsewhere: %s", link)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Symlink(target, link); err != nil {
		return fmt.Errorf("enable service link: %w", err)
	}
	return nil
}

func systemdDefinition(options Options, scope Scope) string {
	if scope == User {
		return "[Unit]\nDescription=" + options.ProjectName + " service\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nExecStart=" + systemdEscape(options.BinaryPath) + "\nRestart=on-failure\nRestartSec=5\n\n[Install]\nWantedBy=default.target\n"
	}
	paths := writablePaths(options)
	readWrite := make([]string, 0, len(paths)+1)
	for _, path := range paths {
		readWrite = append(readWrite, "ReadWritePaths=-"+systemdEscape(path))
	}
	readWrite = append(readWrite, "ReadWritePaths=-/usr/local/bin")
	wantedBy := "multi-user.target"
	if scope == User {
		wantedBy = "default.target"
	}
	documentation := ""
	if options.DocumentationURL != "" {
		documentation = "Documentation=" + systemdEscape(options.DocumentationURL) + "\n"
	}
	return "[Unit]\nDescription=" + options.ProjectName + " service\n" + documentation + "After=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nExecStart=" + systemdEscape(options.BinaryPath) + "\nRestart=on-failure\nRestartSec=5\nStandardOutput=journal\nStandardError=journal\nProtectSystem=true\nProtectHome=yes\nPrivateTmp=yes\nExecStartPre=+/usr/bin/install -d -m 0755 " + installDirArgs(options) + "\n" + strings.Join(readWrite, "\n") + "\n\n[Install]\nWantedBy=" + wantedBy + "\n"
}

func openRCDefinition(options Options) string {
	return "#!/sbin/openrc-run\n\nname=" + shellQuote(options.InternalName) + "\ndescription=" + shellQuote(options.ProjectName) + "\ncommand=" + shellQuote(options.BinaryPath) + "\ncommand_args=\"\"\npidfile=" + shellQuote("/var/run/"+options.InternalOrg+"-"+options.InternalName+".pid") + "\ncommand_background=true\noutput_log=" + shellQuote(filepath.Join(options.Paths.Log, "server.log")) + "\nerror_log=" + shellQuote(filepath.Join(options.Paths.Log, "error.log")) + "\n\ndepend() {\n    need net\n    after firewall\n    use dns logger\n}\n\nstart_pre() {\n    checkpath -d -m 0755 " + shellQuote(filepath.Dir(options.Paths.PID)) + "\n    checkpath -d -m 0755 " + shellQuote(options.Paths.Log) + "\n}\n"
}

func sysVDefinition(options Options) string {
	name := options.InternalName
	return "#!/bin/sh\n### BEGIN INIT INFO\n# Provides:          " + name + "\n# Required-Start:    $network $remote_fs $syslog\n# Required-Stop:     $network $remote_fs $syslog\n# Default-Start:     2 3 4 5\n# Default-Stop:      0 1 6\n# Short-Description: " + options.ProjectName + "\n### END INIT INFO\nNAME=" + name + "\nDAEMON=" + shellQuote(options.BinaryPath) + "\nPIDFILE=" + shellQuote("/var/run/"+options.InternalOrg+"-"+name+".pid") + "\nLOGFILE=" + shellQuote(filepath.Join(options.Paths.Log, "server.log")) + "\ncase \"$1\" in\n start) mkdir -p \"$(dirname \"$LOGFILE\")\"; start-stop-daemon --start --quiet --background --make-pidfile --pidfile \"$PIDFILE\" --exec \"$DAEMON\" --no-close >> \"$LOGFILE\" 2>&1 ;;\n stop) start-stop-daemon --stop --quiet --pidfile \"$PIDFILE\" --retry 30; rm -f \"$PIDFILE\" ;;\n restart) \"$0\" stop; sleep 1; \"$0\" start ;;\n status) start-stop-daemon --status --pidfile \"$PIDFILE\" ;;\n *) echo \"Usage: $0 {start|stop|restart|status}\" >&2; exit 2 ;;\nesac\n"
}

func rcDefinition(options Options) string {
	name := options.InternalName
	pid := "/var/run/" + options.InternalOrg + "-" + name + ".pid"
	return "#!/bin/sh\n\n# PROVIDE: " + name + "\n# REQUIRE: NETWORKING\n# KEYWORD: shutdown\n\n. /etc/rc.subr\n\nname=" + name + "\nrcvar=" + name + "_enable\npidfile=" + shellQuote(pid) + "\ncommand=\"/usr/sbin/daemon\"\ncommand_args=\"-P ${pidfile} -r -f " + options.BinaryPath + "\"\nstart_precmd=" + name + "_prestart\n" + name + "_prestart() { install -d -m 0755 " + shellQuote(filepath.Dir(options.Paths.PID)) + " " + shellQuote(options.Paths.Log) + "; }\nload_rc_config $name\nrun_rc_command \"$1\"\n"
}

func launchdDefinition(options Options) string {
	label := options.InternalOrg + "." + options.InternalName
	return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict><key>Label</key><string>" + xmlEscape(label) + "</string><key>ProgramArguments</key><array><string>" + xmlEscape(options.BinaryPath) + "</string></array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>StandardOutPath</key><string>" + xmlEscape(filepath.Join(options.Paths.Log, "stdout.log")) + "</string><key>StandardErrorPath</key><string>" + xmlEscape(filepath.Join(options.Paths.Log, "stderr.log")) + "</string></dict></plist>\n"
}

func writablePaths(options Options) []string {
	values := []string{options.Paths.Config, options.Paths.Data, options.Paths.Cache, options.Paths.Log, filepath.Dir(options.Paths.PID), options.Paths.Backup}
	values = append(values, options.ExtraWritablePaths...)
	result := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		if value != "" {
			value = filepath.Clean(value)
			if !seen[value] {
				seen[value] = true
				result = append(result, value)
			}
		}
	}
	return result
}

func installDirArgs(options Options) string {
	values := writablePaths(options)
	args := make([]string, 0, len(values))
	for _, value := range values {
		args = append(args, systemdEscape(value))
	}
	return strings.Join(args, " ")
}
func systemdEscape(value string) string {
	return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "%", "%%").Replace(value) + "\""
}
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
func xmlEscape(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;").Replace(value)
}
