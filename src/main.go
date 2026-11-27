package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/apimgr/mail/src/common/api"
	"github.com/apimgr/mail/src/common/display"
	"github.com/apimgr/mail/src/common/i18n"
	shellintegration "github.com/apimgr/mail/src/common/shell"
	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
	"github.com/apimgr/mail/src/email"
	"github.com/apimgr/mail/src/logger"
	"github.com/apimgr/mail/src/paths"
	"github.com/apimgr/mail/src/pidfile"
	"github.com/apimgr/mail/src/scheduler"
	"github.com/apimgr/mail/src/server"
	"github.com/apimgr/mail/src/service"
	"github.com/apimgr/mail/src/update"
	"github.com/mattn/go-isatty"
)

// Build info - BuildEpoch is set via -ldflags at build time; BuildDate is
// derived from it (per AI.md PART 26)
var (
	Version  = "dev"
	CommitID = "unknown"
	// BuildDate is derived from BuildEpoch in init(); "unknown" when BuildEpoch is unset
	BuildDate = "unknown"
	// BuildEpoch is the Unix build timestamp (seconds, UTC) set via -ldflags; "0" when unset
	BuildEpoch = "0"
	// OfficialSite is the default server URL; empty means users must use --server flag
	OfficialSite = ""
)

// buildEpoch parses the embedded BuildEpoch ldflag; 0 when unset or invalid.
func buildEpoch() int64 {
	n, err := strconv.ParseInt(BuildEpoch, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func backupDirectoryFailure(path string, err error) string {
	owner, mode, serviceUser := "unknown", "unknown", "unknown"
	if info, statErr := os.Stat(path); statErr == nil {
		owner = pathOwner(info)
		mode = fmt.Sprintf("%04o", info.Mode().Perm())
	}
	if current, currentErr := user.Current(); currentErr == nil {
		serviceUser = current.Username
	}
	return fmt.Sprintf("backup directory %q is not writable (owner=%s mode=%s service_user=%s): %v", path, owner, mode, serviceUser, err)
}

// init derives BuildDate (RFC 3339 UTC) from the embedded BuildEpoch
func init() {
	if n := buildEpoch(); n > 0 {
		BuildDate = time.Unix(n, 0).UTC().Format("2006-01-02T15:04:05Z")
	}
}

// Binary name (for display in help/error messages per AI.md PART 8)
var (
	binaryName   = filepath.Base(os.Args[0])
	internalName = "mail"
	debugFlag    bool
)

type serverOptions struct {
	configDir, dataDir, cacheDir, logDir, backupDir, pidFile string
	address, mode, baseURL, color, language                  string
	port                                                     int
	portSet                                                  bool
	status, daemon, debug, i2p                               bool
	shell, service, maintenance, update                      string
	commandArgs                                              []string
}

var cliOptions serverOptions

func stdoutIsTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

func main() {
	if name, ok := commandHelp(os.Args[1:], "shell", "service", "maintenance", "update"); ok {
		printCommandHelp(name)
		return
	}
	options, err := parseServerArgs(os.Args[1:], io.Discard)
	if err != nil {
		if err == flag.ErrHelp {
			return
		}
		fmt.Fprintf(os.Stderr, "%s: %v\nRun '%s --help' for usage.\n", binaryName, err, binaryName)
		os.Exit(2)
	}
	cliOptions = options
	debugFlag = options.debug
	switch {
	case options.shell != "":
		os.Args = append([]string{os.Args[0], "--shell", options.shell}, options.commandArgs...)
		handleShell()
	case options.service != "":
		os.Args = append([]string{os.Args[0], "--service", options.service}, options.commandArgs...)
		handleService()
	case options.maintenance != "":
		os.Args = append([]string{os.Args[0], "--maintenance", options.maintenance}, options.commandArgs...)
		handleMaintenance()
	case options.update != "":
		os.Args = append([]string{os.Args[0], "--update", options.update}, options.commandArgs...)
		handleUpdate()
	case options.status:
		checkStatus()
	default:
		if options.daemon && !paths.GetDefaultPaths().Container {
			executable, err := os.Executable()
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: cannot locate executable: %v\n", binaryName, err)
				os.Exit(1)
			}
			detached, pid, err := startDetachedProcess(executable, os.Args[1:])
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: daemon startup failed: %v\n", binaryName, err)
				os.Exit(1)
			}
			if detached {
				fmt.Printf("%s started in background (PID %d)\n", binaryName, pid)
				return
			}
		}
		if exitCode := startServer(); exitCode != 0 {
			os.Exit(exitCode)
		}
	}
}

// commandHelp detects command-specific help before flag parsing consumes it as a value.
func commandHelp(args []string, commands ...string) (string, bool) {
	for i := 0; i+1 < len(args); i++ {
		next := args[i+1]
		if next != "--help" && next != "-h" && next != "help" {
			continue
		}
		for _, command := range commands {
			if args[i] == "--"+command {
				return command, true
			}
		}
	}
	return "", false
}

func printCommandHelp(name string) {
	switch name {
	case "shell":
		printShellHelp()
	case "service":
		printServiceHelp()
	case "maintenance":
		printMaintenanceHelp()
	case "update":
		printUpdateHelp()
	}
}

func parseServerArgs(args []string, output io.Writer) (serverOptions, error) {
	args = append([]string(nil), args...)
	for i := 0; i < len(args); i++ {
		if args[i] == "--update" && (i+1 == len(args) || strings.HasPrefix(args[i+1], "-")) {
			args[i] = "--update=yes"
		}
		if i+1 < len(args) && strings.HasPrefix(args[i+1], "--") {
			switch args[i] {
			case "--service", "--maintenance", "--shell":
				args[i] += "=" + args[i+1]
				args = append(args[:i+1], args[i+2:]...)
			}
		}
	}
	fs := flag.NewFlagSet(binaryName, flag.ContinueOnError)
	fs.SetOutput(output)
	var o serverOptions
	var showHelp, showVersion bool
	fs.BoolVar(&showHelp, "help", false, "Show help")
	fs.BoolVar(&showVersion, "version", false, "Show version")
	fs.BoolVar(&o.status, "status", false, "Show server status and health")
	fs.StringVar(&o.shell, "shell", "", "Shell integration command")
	fs.StringVar(&o.service, "service", "", "Service management command")
	fs.StringVar(&o.maintenance, "maintenance", "", "Maintenance operation")
	fs.StringVar(&o.update, "update", "", "Check or perform updates")
	fs.StringVar(&o.mode, "mode", "", "Application mode")
	fs.StringVar(&o.configDir, "config", "", "Config directory")
	fs.StringVar(&o.dataDir, "data", "", "Data directory")
	fs.StringVar(&o.cacheDir, "cache", "", "Cache directory")
	fs.StringVar(&o.logDir, "log", "", "Log directory")
	fs.StringVar(&o.backupDir, "backup", "", "Backup directory")
	fs.StringVar(&o.pidFile, "pid", "", "PID file path")
	fs.StringVar(&o.address, "address", "", "Listen address")
	fs.IntVar(&o.port, "port", 0, "Listen port")
	fs.StringVar(&o.baseURL, "baseurl", "", "URL path prefix")
	fs.BoolVar(&o.daemon, "daemon", false, "Run as background daemon")
	fs.BoolVar(&o.debug, "debug", false, "Enable debug mode")
	fs.BoolVar(&o.i2p, "i2p", false, "Enable I2P eepsite")
	fs.StringVar(&o.color, "color", "auto", "Color output: auto, yes, no")
	fs.StringVar(&o.language, "lang", "", "Language for output")
	fs.Usage = func() { printHelp() }
	for i := range args {
		if args[i] == "-h" || args[i] == "-v" {
			args[i] = map[string]string{"-h": "--help", "-v": "--version"}[args[i]]
		}
	}
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	o.commandArgs = fs.Args()
	fs.Visit(func(parsed *flag.Flag) {
		if parsed.Name == "port" {
			o.portSet = true
		}
	})
	if len(o.commandArgs) > 0 && o.shell == "" && o.service == "" && o.maintenance == "" && o.update == "" {
		return o, fmt.Errorf("unexpected argument %q", o.commandArgs[0])
	}
	if showHelp {
		printHelp()
		return o, flag.ErrHelp
	}
	if showVersion {
		printVersion()
		return o, flag.ErrHelp
	}
	if o.mode != "" && o.mode != "production" && o.mode != "development" && o.mode != "debug" {
		return o, fmt.Errorf("invalid mode %q", o.mode)
	}
	if o.port < 0 || o.port > 65535 {
		return o, fmt.Errorf("port must be between 1 and 65535")
	}
	if o.color != "auto" && o.color != "yes" && o.color != "no" {
		return o, fmt.Errorf("invalid color mode %q", o.color)
	}
	return o, nil
}

// printHelp displays the main help message per AI.md PART 8
func printHelp() {
	fmt.Printf("%s %s - Email Infrastructure Management Panel\n\n", binaryName, Version)
	fmt.Printf("Usage:\n")
	fmt.Printf("  %s [flags]\n\n", binaryName)
	fmt.Printf("Information:\n")
	fmt.Printf("  -h, --help                        Show help\n")
	fmt.Printf("  -v, --version                     Show version\n")
	fmt.Printf("      --status                      Show server status and health\n\n")
	fmt.Printf("Shell Integration:\n")
	fmt.Printf("      --shell completions [SHELL]   Print shell completions\n")
	fmt.Printf("      --shell init [SHELL]          Print shell init command\n")
	fmt.Printf("      --shell --help                Show shell help\n\n")
	fmt.Printf("Server Configuration:\n")
	fmt.Printf("      --mode {production|development}  Application mode (default: production)\n")
	fmt.Printf("      --config DIR                  Config directory\n")
	fmt.Printf("      --data DIR                    Data directory\n")
	fmt.Printf("      --cache DIR                   Cache directory\n")
	fmt.Printf("      --log DIR                     Log directory\n")
	fmt.Printf("      --backup DIR                  Backup directory\n")
	fmt.Printf("      --pid FILE                    PID file path\n")
	fmt.Printf("      --address ADDR                Listen address (default: 0.0.0.0)\n")
	fmt.Printf("      --port PORT                   Listen port (default: random 64xxx)\n")
	fmt.Printf("      --daemon                      Run as daemon (detach from terminal)\n")
	fmt.Printf("      --debug                       Enable debug mode\n")
	fmt.Printf("      --i2p                         Enable the I2P eepsite\n")
	fmt.Printf("      --color {auto|yes|no}         Color output (default: auto)\n")
	fmt.Printf("      --lang CODE                   Language for output (default: auto)\n\n")
	fmt.Printf("Service Management:\n")
	fmt.Printf("      --service CMD                 Service management (--service --help for details)\n")
	fmt.Printf("      --maintenance CMD             Maintenance operations (--maintenance --help for details)\n")
	fmt.Printf("      --update [CMD]                Check/perform updates (--update --help for details)\n\n")
	fmt.Printf("Run '%s <command> --help' for detailed help on any command.\n", binaryName)
}

// printVersion displays version information per AI.md PART 8 and PART 26
func printVersion() {
	fmt.Printf("%s version %s\n", binaryName, Version)
	fmt.Println("Email Infrastructure Management Panel")
	fmt.Printf("Commit: %s\n", CommitID)
	fmt.Printf("Built: %s\n", BuildDate)
	fmt.Printf("Go: %s\n", runtime.Version())
	fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	if OfficialSite != "" {
		fmt.Printf("Site: %s\n", OfficialSite)
	}
}

// checkStatus queries the running server and displays status
// Per AI.md PART 8: Exit 0 if healthy, 1 if unhealthy
func checkStatus() {
	port, err := statusPort()
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		host := statusAddress()
		pathPrefix := strings.TrimRight(cliOptions.baseURL, "/")
		err = checkHealthEndpoint(ctx, "http://"+net.JoinHostPort(host, strconv.Itoa(port))+pathPrefix+api.APIBasePath()+"/server/healthz")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: unhealthy: %v\n", binaryName, err)
		os.Exit(1)
	}
	fmt.Printf("%s is healthy\n", binaryName)
}

func statusPort() (int, error) {
	if cliOptions.port > 0 {
		return cliOptions.port, nil
	}
	if value := os.Getenv("PORT"); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return 0, fmt.Errorf("invalid PORT value %q", value)
		}
		return port, nil
	}
	appPaths := paths.GetDefaultPaths()
	if cliOptions.configDir != "" {
		appPaths.Config = filepath.Clean(cliOptions.configDir)
	}
	configPath := filepath.Join(appPaths.Config, "server.yml")
	cfg, err := config.Load(configPath)
	if os.IsNotExist(err) {
		return 64500, nil
	}
	if err != nil {
		return 0, fmt.Errorf("load server configuration: %w", err)
	}
	return cfg.Server.Port, nil
}

func statusAddress() string {
	address := strings.TrimSpace(cliOptions.address)
	if address == "" {
		if value := strings.TrimSpace(os.Getenv("LISTEN")); value != "" {
			address = value
		}
	}
	if address == "" {
		appPaths := paths.GetDefaultPaths()
		if cliOptions.configDir != "" {
			appPaths.Config = filepath.Clean(cliOptions.configDir)
		}
		if cfg, err := config.Load(filepath.Join(appPaths.Config, "server.yml")); err == nil {
			address = strings.TrimSpace(cfg.Server.Address)
		}
	}
	switch strings.Trim(address, "[]") {
	case "", "0.0.0.0":
		return "127.0.0.1"
	case "::":
		return "::1"
	default:
		return strings.Trim(address, "[]")
	}
}

func checkHealthEndpoint(ctx context.Context, endpoint string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("server did not respond: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return fmt.Errorf("invalid health response: %w", err)
	}
	if response.StatusCode != http.StatusOK || body.Status != "healthy" {
		return fmt.Errorf("server returned %s status %q", response.Status, body.Status)
	}
	return nil
}

// handleShell manages shell integration commands
func handleShell() {
	if len(os.Args) < 3 {
		printShellHelp()
		os.Exit(1)
	}

	subcmd := os.Args[2]
	switch subcmd {
	case "--help":
		printShellHelp()
		os.Exit(0)
	}
	shellName := ""
	if len(os.Args) > 3 {
		shellName = os.Args[3]
	} else if value := os.Getenv("SHELL"); value != "" {
		shellName = filepath.Base(value)
	}
	output, err := shellintegration.Render(subcmd, shellName, binaryName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", binaryName, err)
		fmt.Fprintf(os.Stderr, "Run '%s --shell --help' for usage.\n", binaryName)
		os.Exit(1)
	}
	fmt.Print(output)
}

// printShellHelp displays shell integration help
func printShellHelp() {
	fmt.Printf("%s shell integration\n\n", binaryName)
	fmt.Printf("Usage:\n")
	fmt.Printf("  %s --shell <command> [options]\n\n", binaryName)
	fmt.Printf("Commands:\n")
	fmt.Printf("  completions [SHELL]   Generate shell completions (bash, zsh, fish, powershell)\n")
	fmt.Printf("  init [SHELL]          Print shell initialization command\n")
	fmt.Printf("  --help                Show this help\n")
}

// handleService manages service operations
func handleService() {
	if len(os.Args) < 3 {
		printServiceHelp()
		os.Exit(1)
	}

	subcmd := os.Args[2]
	if subcmd == "--help" || subcmd == "help" {
		printServiceHelp()
		os.Exit(0)
	}
	manager, err := newServiceManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", binaryName, err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	scope := service.System
	if !manager.IsElevated() {
		scope = service.User
	}
	var result service.Result
	switch strings.TrimLeft(subcmd, "-") {
	case "status":
		status, statusErr := manager.Status(ctx, scope)
		if statusErr != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", binaryName, statusErr)
			os.Exit(1)
		}
		printServiceStatus(status)
		return
	case "install":
		result, err = manager.Install(ctx, scope)
	case "start":
		result, err = manager.Start(ctx, scope)
	case "stop":
		result, err = manager.Stop(ctx, scope)
	case "restart":
		result, err = manager.Restart(ctx, scope)
	case "reload":
		result, err = manager.Reload(ctx, scope)
	case "disable":
		result, err = manager.Disable(ctx, scope)
	case "uninstall":
		if !confirmServiceUninstall() {
			fmt.Fprintln(os.Stderr, "Service uninstall cancelled.")
			os.Exit(1)
		}
		result, err = manager.Uninstall(ctx, scope, true)
	default:
		fmt.Fprintf(os.Stderr, "%s: unknown service command %q\n", binaryName, subcmd)
		printServiceHelp()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", binaryName, err)
		os.Exit(1)
	}
	if result.Message != "" {
		fmt.Println(result.Message)
	}
	if result.Available {
		printServiceStatus(result.Status)
	}
}

func newServiceManager() (*service.Manager, error) {
	appPaths := paths.GetDefaultPaths()
	if cliOptions.configDir != "" {
		appPaths.Config = filepath.Clean(cliOptions.configDir)
	}
	if cliOptions.dataDir != "" {
		appPaths.Data = filepath.Clean(cliOptions.dataDir)
	}
	if cliOptions.cacheDir != "" {
		appPaths.Cache = filepath.Clean(cliOptions.cacheDir)
	}
	if cliOptions.logDir != "" {
		appPaths.Log = filepath.Clean(cliOptions.logDir)
	}
	if cliOptions.backupDir != "" {
		appPaths.Backup = filepath.Clean(cliOptions.backupDir)
	}
	if cliOptions.pidFile != "" {
		appPaths.PID = filepath.Clean(cliOptions.pidFile)
	}
	return service.New(service.Options{
		ProjectName:  "mail",
		InternalName: internalName,
		InternalOrg:  "apimgr",
		Paths:        appPaths,
	})
}

func printServiceStatus(status service.Status) {
	state := "stopped"
	if status.Running {
		state = "running"
	}
	installed := "not installed"
	if status.Installed {
		installed = "installed"
	}
	enabled := "disabled"
	if status.Enabled {
		enabled = "enabled"
	}
	fmt.Printf("Service:    %s (%s)\nState:      %s\nAuto-start: %s\n", installed, status.Manager, state, enabled)
	if status.PID > 0 {
		fmt.Printf("PID:        %d\n", status.PID)
	}
}

func confirmServiceUninstall() bool {
	if !stdoutIsTerminal() || !isatty.IsTerminal(os.Stdin.Fd()) {
		return false
	}
	fmt.Fprint(os.Stderr, "This will delete ALL data, configs, and the system user. Continue? [y/N] ")
	var response string
	if _, err := fmt.Fscanln(os.Stdin, &response); err != nil {
		return false
	}
	return strings.EqualFold(response, "y") || strings.EqualFold(response, "yes")
}

// printServiceHelp displays service management help
func printServiceHelp() {
	fmt.Printf("%s service management commands:\n\n", binaryName)
	fmt.Printf("  status                                - Show service status\n")
	fmt.Printf("  start                                 - Start the service\n")
	fmt.Printf("  stop                                  - Stop the service\n")
	fmt.Printf("  restart                               - Restart the service\n")
	fmt.Printf("  reload                                - Reload configuration\n\n")
	fmt.Printf("  --install                             - Install, enable, and start service\n")
	fmt.Printf("  --disable                             - Stop and disable service (keeps data)\n")
	fmt.Printf("  --uninstall                           - Stop, disable, and remove everything (keeps binary)\n\n")
	fmt.Printf("  --help                                - Show this help\n\nCurrent status:\n")
	manager, err := newServiceManager()
	if err != nil {
		fmt.Printf("  Service:    unavailable (%v)\n", err)
		return
	}
	scope := service.System
	if !manager.IsElevated() {
		scope = service.User
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, err := manager.Status(ctx, scope)
	if err != nil {
		fmt.Printf("  Service:    unavailable (%v)\n", err)
		return
	}
	printServiceStatus(status)
}

// handleMaintenance manages maintenance operations
func handleMaintenance() {
	if len(os.Args) < 3 {
		printMaintenanceHelp()
		os.Exit(1)
	}

	subcmd := os.Args[2]
	switch subcmd {
	case "--help":
		printMaintenanceHelp()
		os.Exit(0)
	case "update":
		os.Args = maintenanceUpdateArgs(os.Args[0], os.Args[3:])
		handleUpdate()
	case "email":
		code := handleEmailMaintenance(os.Args[3:])
		if code != 0 {
			os.Exit(code)
		}
	case "mode":
		code := handleModeMaintenance(os.Args[3:])
		if code != 0 {
			os.Exit(code)
		}
	case "security":
		code := handleSecurityMaintenance(os.Args[3:])
		if code != 0 {
			os.Exit(code)
		}
	case "pgp":
		code := handlePGPMaintenance(os.Args[3:])
		if code != 0 {
			os.Exit(code)
		}
	default:
		fmt.Fprintf(os.Stderr, "%s: maintenance operation %q is not implemented\n", binaryName, subcmd)
		os.Exit(1)
	}
}

func handleSecurityMaintenance(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: %s --maintenance security <list|show|update|export> [options]\n", binaryName)
		return 2
	}
	if args[0] == "show" {
		return handleSecurityReportShow(args[1:])
	}
	if args[0] == "update" {
		return handleSecurityReportUpdate(args[1:])
	}
	if args[0] == "export" {
		return handleSecurityReportExport(args[1:])
	}
	if args[0] != "list" {
		fmt.Fprintf(os.Stderr, "%s: unknown security maintenance action %q\n", binaryName, args[0])
		return 2
	}
	flags := flag.NewFlagSet("maintenance security list", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	status := flags.String("status", "", "Filter by report status")
	showHelp := flags.Bool("help", false, "Show security report list help")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if *showHelp {
		fmt.Printf("Usage: %s --maintenance security list [--status received|triaged|confirmed|patching|disclosed|wont_fix]\n", binaryName)
		return 0
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "%s: unexpected security list argument %q\n", binaryName, flags.Arg(0))
		return 2
	}
	validStatus := map[string]bool{"": true, "received": true, "triaged": true, "confirmed": true, "patching": true, "disclosed": true, "wont_fix": true}
	if !validStatus[*status] {
		fmt.Fprintf(os.Stderr, "%s: invalid security report status %q\n", binaryName, *status)
		return 2
	}
	appPaths := configuredPaths()
	cfg, err := loadConfig(appPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load configuration: %v\n", binaryName, err)
		return 1
	}
	if !authorizeMaintenance(cfg) {
		fmt.Fprintln(os.Stderr, "maintenance security requires the operator token or an elevated account")
		return 77
	}
	db, err := initDatabase(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: connect to database: %v\n", binaryName, err)
		return 1
	}
	defer func() { _ = db.Close() }()
	reports, err := db.ListSecurityReportSummaries(context.Background(), *status)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: list security reports: %v\n", binaryName, err)
		return 1
	}
	fmt.Println("TRACKING ID\tSTATUS\tSEVERITY\tCOMPONENT\tCREATED\tUPDATED")
	for _, report := range reports {
		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\n", report.TrackingID, report.Status, report.Severity, report.Component, report.CreatedAt.UTC().Format(time.RFC3339), report.UpdatedAt.UTC().Format(time.RFC3339))
	}
	return 0
}

func handleSecurityReportExport(args []string) int {
	if len(args) == 0 || !validSecurityTrackingID(args[0]) {
		fmt.Fprintf(os.Stderr, "usage: %s --maintenance security export TRACKING_ID [--output FILE|-]\n", binaryName)
		return 2
	}
	flags := flag.NewFlagSet("maintenance security export", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("output", args[0]+".asc", "Write the encrypted report to FILE, or - for stdout")
	showHelp := flags.Bool("help", false, "Show encrypted security report export help")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if *showHelp {
		fmt.Printf("Usage: %s --maintenance security export TRACKING_ID [--output FILE|-]\n", binaryName)
		return 0
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected security report export argument")
		return 2
	}
	appPaths := configuredPaths()
	cfg, err := loadConfig(appPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load configuration: %v\n", binaryName, err)
		return 1
	}
	if !authorizeMaintenance(cfg) {
		fmt.Fprintln(os.Stderr, "maintenance security requires the operator token or an elevated account")
		return 77
	}
	if err := logger.Init(loggerConfig(cfg.Server.Logs, appPaths.Log)); err != nil {
		fmt.Fprintf(os.Stderr, "%s: initialize logging: %v\n", binaryName, err)
		return 1
	}
	defer func() { _ = logger.Close() }()
	db, err := initDatabase(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: connect to database: %v\n", binaryName, err)
		return 1
	}
	defer func() { _ = db.Close() }()
	report, err := db.GetEncryptedSecurityReport(context.Background(), args[0])
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fmt.Fprintf(os.Stderr, "%s: security report not found\n", binaryName)
			return 66
		}
		fmt.Fprintf(os.Stderr, "%s: get encrypted security report: %v\n", binaryName, err)
		return 1
	}
	if report.Algorithm != "pgp" && report.Algorithm != "aes-256-gcm" {
		fmt.Fprintf(os.Stderr, "%s: unsupported security report encryption algorithm\n", binaryName)
		return 1
	}
	if _, err := writeEncryptedSecurityExport(*output, report.Body, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "%s: write encrypted security report: %v\n", binaryName, err)
		return 1
	}
	logger.Security("security.report_exported tracking_id="+report.TrackingID, "local")
	if *output != "-" {
		fmt.Printf("Exported encrypted security report %s to %s\n", report.TrackingID, *output)
	}
	return 0
}

func writeEncryptedSecurityExport(output string, encrypted []byte, stdout io.Writer) (string, error) {
	if output == "-" {
		_, err := stdout.Write(encrypted)
		return "-", err
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(output)
		return "", err
	}
	if _, err := file.Write(encrypted); err != nil {
		_ = file.Close()
		_ = os.Remove(output)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(output)
		return "", err
	}
	return output, nil
}

func handleSecurityReportUpdate(args []string) int {
	if len(args) == 0 || !validSecurityTrackingID(args[0]) {
		fmt.Fprintf(os.Stderr, "usage: %s --maintenance security update TRACKING_ID [--status STATUS] [--comment TEXT] [--disclosure-date YYYY-MM-DD] [--severity LEVEL]\n", binaryName)
		return 2
	}
	flags := flag.NewFlagSet("maintenance security update", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	status := flags.String("status", "", "Advance report status")
	comment := flags.String("comment", "", "Set the researcher-facing comment")
	disclosureDate := flags.String("disclosure-date", "", "Set the expected disclosure date (YYYY-MM-DD)")
	severity := flags.String("severity", "", "Set the triaged severity")
	showHelp := flags.Bool("help", false, "Show security report update help")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if *showHelp {
		fmt.Printf("Usage: %s --maintenance security update TRACKING_ID [--status STATUS] [--comment TEXT] [--disclosure-date YYYY-MM-DD] [--severity LEVEL]\n", binaryName)
		return 0
	}
	if flags.NArg() != 0 || (*status == "" && *comment == "" && *disclosureDate == "" && *severity == "") {
		fmt.Fprintln(os.Stderr, "security report update requires at least one update field and no extra arguments")
		return 2
	}
	appPaths := configuredPaths()
	cfg, err := loadConfig(appPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load configuration: %v\n", binaryName, err)
		return 1
	}
	if !authorizeMaintenance(cfg) {
		fmt.Fprintln(os.Stderr, "maintenance security requires the operator token or an elevated account")
		return 77
	}
	if err := logger.Init(loggerConfig(cfg.Server.Logs, appPaths.Log)); err != nil {
		fmt.Fprintf(os.Stderr, "%s: initialize logging: %v\n", binaryName, err)
		return 1
	}
	defer func() { _ = logger.Close() }()
	db, err := initDatabase(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: connect to database: %v\n", binaryName, err)
		return 1
	}
	defer func() { _ = db.Close() }()
	update := database.SecurityReportUpdate{Status: *status, Comment: *comment, DisclosureDate: *disclosureDate, Severity: *severity}
	oldStatus, err := db.UpdateSecurityReport(context.Background(), args[0], update, time.Now().UTC())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fmt.Fprintf(os.Stderr, "%s: security report not found\n", binaryName)
			return 66
		}
		if errors.Is(err, database.ErrInvalidSecurityReportUpdate) {
			fmt.Fprintln(os.Stderr, "security report update has invalid values or attempts a backward status transition")
			return 2
		}
		fmt.Fprintf(os.Stderr, "%s: update security report: %v\n", binaryName, err)
		return 1
	}
	newStatus := oldStatus
	if update.Status != "" {
		newStatus = update.Status
	}
	logger.Security(fmt.Sprintf("security.report_updated tracking_id=%s old_status=%s new_status=%s", args[0], oldStatus, newStatus), "local")
	fmt.Printf("Updated security report %s (status: %s)\n", args[0], newStatus)
	return 0
}

func validSecurityTrackingID(value string) bool {
	if len(value) != 20 || !strings.HasPrefix(value, "sec_") {
		return false
	}
	for _, char := range value[4:] {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}

func handleSecurityReportShow(args []string) int {
	if len(args) != 1 || !validSecurityTrackingID(args[0]) {
		fmt.Fprintf(os.Stderr, "usage: %s --maintenance security show TRACKING_ID\n", binaryName)
		return 2
	}
	appPaths := configuredPaths()
	cfg, err := loadConfig(appPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load configuration: %v\n", binaryName, err)
		return 1
	}
	if !authorizeMaintenance(cfg) {
		fmt.Fprintln(os.Stderr, "maintenance security requires the operator token or an elevated account")
		return 77
	}
	db, err := initDatabase(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: connect to database: %v\n", binaryName, err)
		return 1
	}
	defer func() { _ = db.Close() }()
	report, err := db.GetSecurityReportDetails(context.Background(), args[0])
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fmt.Fprintf(os.Stderr, "%s: security report not found\n", binaryName)
			return 66
		}
		fmt.Fprintf(os.Stderr, "%s: get security report: %v\n", binaryName, err)
		return 1
	}
	fmt.Printf("Tracking ID: %s\nStatus: %s\nSeverity: %s\nComponent: %s\nCreated: %s\nUpdated: %s\nResearcher comment: %s\n",
		report.TrackingID, report.Status, report.Severity, report.Component,
		report.CreatedAt.UTC().Format(time.RFC3339), report.UpdatedAt.UTC().Format(time.RFC3339), report.ResearcherComment)
	if report.DisclosureDate.Valid {
		fmt.Printf("Disclosure date: %s\n", report.DisclosureDate.String)
	}
	return 0
}

func maintenanceUpdateArgs(program string, args []string) []string {
	subcommand := "yes"
	if len(args) > 0 {
		subcommand = args[0]
		args = args[1:]
	}
	return append([]string{program, "--update", subcommand}, args...)
}

func handleEmailMaintenance(args []string) int {
	if len(args) == 0 || args[0] != "test" {
		fmt.Fprintf(os.Stderr, "usage: %s --maintenance email test [--to ADDRESS] [--dry-run]\n", binaryName)
		return 2
	}
	flags := flag.NewFlagSet("maintenance email test", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	recipientFlag := flags.String("to", "", "Test email recipient")
	dryRun := flags.Bool("dry-run", false, "Render and validate templates without sending")
	showHelp := flags.Bool("help", false, "Show email test help")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if *showHelp {
		fmt.Printf("Usage: %s --maintenance email test [--to ADDRESS] [--dry-run]\n", binaryName)
		return 0
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "%s: unexpected email test argument %q\n", binaryName, flags.Arg(0))
		return 2
	}
	appPaths := configuredPaths()
	cfg, err := loadConfig(appPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load configuration: %v\n", binaryName, err)
		return 1
	}
	if !authorizeMaintenance(cfg) {
		fmt.Fprintln(os.Stderr, "maintenance email test requires the operator token or an elevated account")
		return 77
	}
	if *dryRun {
		return previewEmailTemplates(appPaths.Config)
	}

	startup := config.PeekStartup(filepath.Join(appPaths.Config, "server.yml"))
	if err := logger.Init(loggerConfig(startup.Logs, appPaths.Log)); err != nil {
		fmt.Fprintf(os.Stderr, "%s: initialize logging: %v\n", binaryName, err)
		return 1
	}
	defer func() { _ = logger.Close() }()
	mailer := initializeMailer(cfg, appPaths)
	if !mailer.Status().Enabled {
		fmt.Fprintln(os.Stderr, "email test unavailable: SMTP is not configured and working")
		return 69
	}
	recipient := strings.TrimSpace(*recipientFlag)
	if recipient == "" {
		recipient = strings.TrimSpace(cfg.Server.Notifications.Email.ReplyTo)
	}
	if recipient == "" {
		fmt.Fprintln(os.Stderr, "set server.notifications.email.reply_to or pass --to ADDRESS")
		return 1
	}
	if _, err := email.ParseAddress(recipient); err != nil {
		fmt.Fprintf(os.Stderr, "%s: invalid recipient address\n", binaryName)
		return 1
	}
	templates := email.NewTemplates(appPaths.Config, nil)
	values := emailPreviewValues(cfg)
	rendered, err := templates.Render("test", values)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: validate test template: %v\n", binaryName, err)
		return 65
	}
	fmt.Printf("Sending test email to %s\nTemplate: %s\nSubject: %s\n", recipient, rendered.Name, "[TEST] "+rendered.Subject)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := mailer.Send(ctx, "test", recipient, values); err != nil {
		fmt.Fprintf(os.Stderr, "%s: test email failed: %v\n", binaryName, err)
		return 1
	}
	logger.Audit("email.test_sent", "operator", map[string]interface{}{"recipient": recipient, "template": "test"})
	return 0
}

func handleModeMaintenance(args []string) int {
	if len(args) != 1 || (args[0] != "production" && args[0] != "development") {
		fmt.Fprintf(os.Stderr, "usage: %s --maintenance mode <production|development>\n", binaryName)
		return 2
	}
	appPaths := configuredPaths()
	cfg, err := loadConfig(appPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load configuration: %v\n", binaryName, err)
		return 1
	}
	if !authorizeMaintenance(cfg) {
		fmt.Fprintln(os.Stderr, "maintenance mode requires the operator token or an elevated account")
		return 77
	}
	manager, managerErr := newServiceManager()
	if args[0] == "development" && managerErr == nil && manager.IsElevated() {
		fmt.Fprintln(os.Stderr, "warning: development mode enables verbose logging and should not be used on a public server")
	}
	if err := config.SaveMode(filepath.Join(appPaths.Config, "server.yml"), args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "%s: save runtime mode: %v\n", binaryName, err)
		return 1
	}
	if err := logger.Init(loggerConfig(config.PeekStartup(filepath.Join(appPaths.Config, "server.yml")).Logs, appPaths.Log)); err != nil {
		fmt.Fprintf(os.Stderr, "%s: initialize logging: %v\n", binaryName, err)
		return 1
	}
	defer func() { _ = logger.Close() }()
	logger.Audit("config.mode_changed", "operator", map[string]interface{}{"mode": args[0]})
	fmt.Printf("Application mode set to %s. Restart the service to apply it.\n", args[0])
	return 0
}

func previewEmailTemplates(configDir string) int {
	templates := email.NewTemplates(configDir, nil)
	issues := templates.ValidateOverrides()
	blocking := false
	for _, issue := range issues {
		level := "ERROR"
		if issue.Warning {
			level = "WARN"
		} else {
			blocking = true
		}
		fmt.Printf("%s %s: %s\n", level, issue.Name, issue.Message)
	}
	values := emailPreviewValues(nil)
	for _, name := range templates.Names() {
		rendered, err := templates.Render(name, values)
		if err != nil {
			fmt.Printf("ERROR %s: %v\n", name, err)
			blocking = true
			continue
		}
		fmt.Printf("\n[%s]\nSubject: %s\n%s\n", name, rendered.Subject, rendered.Text)
	}
	if blocking {
		return 65
	}
	return 0
}

func emailPreviewValues(cfg *config.Config) map[string]string {
	appName := "Mail"
	if cfg != nil && cfg.Server.FQDN != "" {
		appName = binaryName
	}
	return map[string]string{
		"app_name": appName, "app_url": "https://mail.example.test", "fqdn": "mail.example.test",
		"onion_url": "", "onion_address": "", "i2p_url": "", "i2p_address": "",
		"notification_reply_to": "operator@example.test", "timestamp": time.Now().UTC().Format(time.RFC3339),
		"year": strconv.Itoa(time.Now().UTC().Year()), "event": "test event", "ip": "192.0.2.1", "details": "Sample details",
		"filename": "mail_backup.tar.gz", "size": "1 MiB", "error": "Sample error", "expires_in": "7 days",
		"expiry_date": "2030-01-01", "valid_until": "2030-01-01", "next_retry": "in 1 hour", "task_name": "sample task",
		"next_run": "in 1 hour", "hostname": "mail.example.test", "version": Version, "mode": "production",
		"reason": "maintenance", "path": "/var", "used_percent": "85", "free": "1 GiB", "threshold_percent": "85",
		"since": time.Now().UTC().Format(time.RFC3339), "current_version": Version, "new_version": Version,
		"channel": "stable", "previous_version": Version,
	}
}

func authorizeMaintenance(cfg *config.Config) bool {
	return authorizeMaintenanceWithReader(cfg, os.Stdin)
}

func authorizeMaintenanceWithReader(cfg *config.Config, input io.Reader) bool {
	manager, err := newServiceManager()
	if err == nil && manager.IsElevated() {
		return true
	}
	if cfg == nil || cfg.Server.Token == "" || !stdoutIsTerminal() || !isatty.IsTerminal(os.Stdin.Fd()) {
		return false
	}
	fmt.Fprint(os.Stderr, "Enter operator token: ")
	var supplied string
	if _, err := fmt.Fscanln(input, &supplied); err != nil {
		return false
	}
	if len(supplied) != len(cfg.Server.Token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(supplied), []byte(cfg.Server.Token)) == 1
}

func configuredPaths() *paths.Paths {
	appPaths := paths.GetDefaultPaths()
	if cliOptions.configDir != "" {
		appPaths.Config = filepath.Clean(cliOptions.configDir)
	}
	if cliOptions.dataDir != "" {
		appPaths.Data = filepath.Clean(cliOptions.dataDir)
	}
	if cliOptions.cacheDir != "" {
		appPaths.Cache = filepath.Clean(cliOptions.cacheDir)
	}
	if cliOptions.logDir != "" {
		appPaths.Log = filepath.Clean(cliOptions.logDir)
	}
	if cliOptions.backupDir != "" {
		appPaths.Backup = filepath.Clean(cliOptions.backupDir)
	}
	if cliOptions.pidFile != "" {
		appPaths.PID = filepath.Clean(cliOptions.pidFile)
	}
	return appPaths
}

// printMaintenanceHelp displays maintenance operations help
func printMaintenanceHelp() {
	fmt.Printf("%s maintenance operations\n\n", binaryName)
	fmt.Printf("Usage:\n")
	fmt.Printf("  %s --maintenance <command> [options]\n\n", binaryName)
	fmt.Printf("Commands:\n")
	fmt.Printf("  backup [FILE]     Create backup\n")
	fmt.Printf("  restore FILE      Restore from backup\n")
	fmt.Printf("  update            Update application\n")
	fmt.Printf("  mode MODE         Set application mode\n")
	fmt.Printf("  setup             Run setup wizard\n")
	fmt.Printf("  email test        Send a test email (--to ADDRESS, --dry-run)\n")
	fmt.Printf("  security list|show|update|export Manage reports without exposing plaintext bodies\n")
	fmt.Printf("  pgp generate      Generate the project security keypair\n")
	fmt.Printf("  pgp export public [FILE] Export the public security key\n")
	fmt.Printf("  --help            Show this help\n")
}

// handleUpdate manages update operations
func handleUpdate() {
	subcmd := "yes"
	if len(os.Args) >= 3 {
		subcmd = os.Args[2]
	}

	switch subcmd {
	case "--help":
		printUpdateHelp()
		os.Exit(0)
	case "branch":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "usage: mail --update branch <stable|beta|devel>")
			os.Exit(1)
		}
		path := updateConfigPath()
		if err := update.SaveBranch(path, os.Args[3]); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", binaryName, err)
			os.Exit(1)
		}
		fmt.Printf("Update branch set to %s\n", update.ReadBranch(path))
		os.Exit(0)
	case "check", "yes":
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		branch := update.ReadBranch(updateConfigPath())
		release, err := update.Check(ctx, Version, branch, buildEpoch())
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: update check failed: %v\n", binaryName, err)
			os.Exit(1)
		}
		if release == nil {
			fmt.Printf("%s is up to date (%s, %s)\n", binaryName, Version, branch)
			os.Exit(0)
		}
		if subcmd == "check" {
			fmt.Printf("Update available: %s (current %s, %s)\n", release.TagName, Version, branch)
			os.Exit(0)
		}
		if err := update.Install(ctx, release); err != nil {
			fmt.Fprintf(os.Stderr, "%s: update install failed: %v\n", binaryName, err)
			os.Exit(1)
		}
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "%s: unknown update command %q\n", binaryName, subcmd)
		os.Exit(1)
	}
}

func updateConfigPath() string {
	appPaths := paths.GetDefaultPaths()
	if cliOptions.configDir != "" {
		appPaths.Config = filepath.Clean(cliOptions.configDir)
	}
	return filepath.Join(appPaths.Config, "server.yml")
}

// printUpdateHelp displays update operations help
func printUpdateHelp() {
	fmt.Printf("%s update operations\n\n", binaryName)
	fmt.Printf("Usage:\n")
	fmt.Printf("  %s --update [command]\n\n", binaryName)
	fmt.Printf("Commands:\n")
	fmt.Printf("  check              Check for updates\n")
	fmt.Printf("  yes                Download and install update\n")
	fmt.Printf("  branch BRANCH      Switch to branch (stable, beta, devel)\n")
	fmt.Printf("  --help             Show this help\n")
}

// startServer initializes and starts the HTTP server
// Per AI.md PART 8: This is PHASE 6 - actual server startup
func startServer() int {
	fmt.Printf("%s - Email Infrastructure Management Panel\n", binaryName)
	fmt.Printf("Version: %s (commit: %s)\n\n", Version, CommitID)

	// PHASE 6 per AI.md PART 8 Server Startup Sequence

	// Step 6: Determine run context
	isRoot := paths.IsRoot()
	startedElevated := isRoot
	fmt.Printf("[INFO] Running as: ")
	if isRoot {
		fmt.Println("root (privileged)")
	} else {
		fmt.Println("user (non-privileged)")
	}

	// Step 7: Resolve all paths
	appPaths := paths.GetDefaultPaths()
	if cliOptions.configDir != "" {
		appPaths.Config = filepath.Clean(cliOptions.configDir)
	}
	if cliOptions.dataDir != "" {
		appPaths.Data = filepath.Clean(cliOptions.dataDir)
	}
	if cliOptions.cacheDir != "" {
		appPaths.Cache = filepath.Clean(cliOptions.cacheDir)
	}
	if cliOptions.logDir != "" {
		appPaths.Log = filepath.Clean(cliOptions.logDir)
	}
	if cliOptions.backupDir != "" {
		appPaths.Backup = filepath.Clean(cliOptions.backupDir)
	}
	if cliOptions.pidFile != "" {
		appPaths.PID = filepath.Clean(cliOptions.pidFile)
	}
	if cliOptions.dataDir != "" {
		appPaths.DBDir = filepath.Join(appPaths.Data, "db")
		appPaths.Security = filepath.Join(appPaths.Data, "security")
	}
	if appPaths.Config != "" {
		appPaths.SSL = filepath.Join(appPaths.Config, "ssl")
	}
	var stagedUpdateFailure error
	stagedUpdateID := "unknown"
	if isRoot {
		updateDir := filepath.Join(appPaths.Data, "update")
		if _, statErr := os.Stat(updateDir); statErr == nil {
			stagedVersion, _ := os.ReadFile(filepath.Join(updateDir, "version"))
			stagedRelease := &update.Release{TagName: strings.TrimSpace(string(stagedVersion))}
			if publishedAt, readErr := os.ReadFile(filepath.Join(updateDir, "published_at")); readErr == nil {
				stagedRelease.PublishedAt, _ = time.Parse(time.RFC3339Nano, strings.TrimSpace(string(publishedAt)))
			}
			if id := update.ReleaseID(stagedRelease); id != "" {
				stagedUpdateID = id
			}
			if executable, executableErr := os.Executable(); executableErr != nil {
				stagedUpdateFailure = fmt.Errorf("locate executable for staged update: %w", executableErr)
			} else if executable, executableErr = filepath.EvalSymlinks(executable); executableErr != nil {
				stagedUpdateFailure = fmt.Errorf("resolve installed executable for staged update: %w", executableErr)
			} else if version, applyErr := update.ApplyStaged(context.Background(), updateDir, executable, Version, buildEpoch()); applyErr == nil {
				fmt.Printf("[INFO] Applying staged update %s and restarting\n", version)
				if err := os.Setenv("APIMGR_UPDATE_INSTALLED", Version+"\n"+version); err != nil {
					stagedUpdateFailure = fmt.Errorf("record installed update %s for restart notification: %w", version, err)
				}
				if stagedUpdateFailure == nil {
					if err := update.RestartSelf(); err != nil {
						_ = os.Unsetenv("APIMGR_UPDATE_INSTALLED")
						stagedUpdateFailure = fmt.Errorf("restart after applying staged update %s: %w", version, err)
					} else {
						return 0
					}
				}
			} else {
				stagedUpdateFailure = applyErr
			}
			if stagedUpdateFailure != nil {
				stagedUpdateFailure = fmt.Errorf("release %s: %w", stagedUpdateID, stagedUpdateFailure)
				_ = os.RemoveAll(updateDir)
			}
		} else if !os.IsNotExist(statErr) {
			stagedUpdateFailure = fmt.Errorf("inspect staged update directory: %w", statErr)
		}
	}
	var serviceIdentity *paths.ServiceIdentity
	if isRoot {
		identity, err := paths.EnsureServiceIdentity(internalName, appPaths.Data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Could not prepare service identity: %v\n", err)
			os.Exit(1)
		}
		serviceIdentity = &identity
	}
	fmt.Printf("[INFO] Config dir: %s\n", appPaths.Config)
	fmt.Printf("[INFO] Data dir: %s\n", appPaths.Data)
	fmt.Printf("[INFO] Log dir: %s\n", appPaths.Log)
	fmt.Printf("[INFO] Database dir: %s\n", appPaths.DBDir)

	// Step 8/9: Setup directories
	fmt.Println("[INFO] Creating directories...")
	if err := appPaths.EnsureAllDirs(isRoot); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to create directories: %v\n", err)
		os.Exit(1)
	}
	backupDirErr := appPaths.EnsureBackupDir(isRoot)
	startup := config.PeekStartup(filepath.Join(appPaths.Config, "server.yml"))
	var privilegedListener net.Listener
	if serviceIdentity != nil {
		if err := appPaths.PrepareServicePaths(*serviceIdentity); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Could not prepare service-owned paths: %v\n", err)
			os.Exit(1)
		}
	}
	if (cliOptions.daemon || startup.Daemonize) && !appPaths.Container && cliOptions.service != "start" {
		executable, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: cannot locate executable: %v\n", binaryName, err)
			os.Exit(1)
		}
		detached, pid, err := startDetachedProcess(executable, os.Args[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: daemon startup failed: %v\n", binaryName, err)
			os.Exit(1)
		}
		if detached {
			fmt.Printf("%s started in background (PID %d)\n", binaryName, pid)
			return 0
		}
	}
	listenAddress := strings.TrimSpace(cliOptions.address)
	if listenAddress == "" {
		listenAddress = strings.TrimSpace(os.Getenv("LISTEN"))
	}
	if listenAddress == "" {
		listenAddress = startup.Address
	}
	if listenAddress == "" {
		listenAddress = "[::]"
	}
	listenPort := cliOptions.port
	if cliOptions.portSet && listenPort == 0 {
		fmt.Fprintln(os.Stderr, "[WARN] port 0 is invalid; selecting the context default")
	}
	if listenPort == 0 {
		if value := strings.TrimSpace(os.Getenv("PORT")); value != "" {
			parsed, parseErr := strconv.Atoi(value)
			if parseErr == nil && parsed >= 1 && parsed <= 65535 {
				listenPort = parsed
			}
		}
	}
	if listenPort == 0 {
		listenPort = startup.Port
	}
	if listenPort == 0 {
		if appPaths.Container {
			listenPort = 80
		} else {
			listenPort = config.AvailablePort()
		}
	}
	cliOptions.port = listenPort
	cliOptions.address = listenAddress
	if serviceIdentity != nil && listenPort < 1024 {
		listener, err := server.CreateListener(listenAddress, listenPort)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Could not bind privileged listener: %v\n", err)
			os.Exit(1)
		}
		privilegedListener = listener
	}
	if serviceIdentity != nil {
		if err := paths.DropPrivileges(*serviceIdentity); err != nil {
			if privilegedListener != nil {
				_ = privilegedListener.Close()
			}
			fmt.Fprintf(os.Stderr, "[ERROR] Could not drop privileges to service account %q: %v\n", serviceIdentity.Name, err)
			os.Exit(1)
		}
		if paths.IsRoot() {
			fmt.Fprintf(os.Stderr, "[ERROR] Privilege drop to service account %q did not change the process identity\n", serviceIdentity.Name)
			os.Exit(1)
		}
	}
	backupCheckErr := appPaths.VerifyBackupWritable()
	if backupDirErr != nil {
		fmt.Fprintln(os.Stderr, "[ERROR]", backupDirectoryFailure(appPaths.Backup, backupDirErr))
	} else if backupCheckErr != nil {
		fmt.Fprintln(os.Stderr, "[ERROR]", backupDirectoryFailure(appPaths.Backup, backupCheckErr))
	}

	// Step 10: Initialize logging per AI.md PART 11
	fmt.Println("[INFO] Initializing logging...")
	logConfig := loggerConfig(startup.Logs, appPaths.Log)

	if err := logger.Init(logConfig); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to initialize logging: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := logger.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "[WARN] Failed to close logging: %v\n", err)
		}
	}()

	logger.Info("Server starting...")
	logger.Infof("Version: %s (commit: %s)", Version, CommitID)

	// Step 11-12: Check and write PID file
	pidPath := appPaths.PID
	pidPathOverridden := cliOptions.pidFile != "" || os.Getenv("PID_FILE") != ""
	if (!startup.PIDFile && !pidPathOverridden) || appPaths.Container || runtime.GOOS == "windows" {
		pidPath = ""
	}
	if pidPath != "" {
		logger.Infof("PID file: %s", pidPath)
		pid, isRunning, err := pidfile.Check(pidPath)
		if err != nil {
			logger.Warnf("PID file check failed: %v", err)
		} else if isRunning {
			logger.Errorf("Server already running (PID %d)", pid)
			fmt.Fprintf(os.Stderr, "[ERROR] Server already running (PID %d)\n", pid)
			os.Exit(1)
		} else if pid > 0 {
			logger.Infof("Removing stale PID file (process %d not running)", pid)
			if err := pidfile.Remove(pidPath); err != nil {
				logger.Warnf("Failed to remove stale PID file: %v", err)
			}
		}

		if err := pidfile.Write(pidPath); err != nil {
			logger.Errorf("Failed to write PID file: %v", err)
			fmt.Fprintf(os.Stderr, "[ERROR] Failed to write PID file: %v\n", err)
			os.Exit(1)
		}
		defer func() {
			if err := pidfile.Remove(pidPath); err != nil {
				logger.Warnf("Failed to remove PID file: %v", err)
			}
		}()
	}

	// Step 13: Load configuration
	cfg, err := loadConfig(appPaths)
	if err != nil {
		logger.Errorf("Failed to load config: %v", err)
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to load config: %v\n", err)
		os.Exit(1)
	}
	if cliOptions.address != "" {
		cfg.Server.Address = cliOptions.address
	}
	if cliOptions.port > 0 {
		cfg.Server.Port = cliOptions.port
	}
	if cliOptions.mode != "" {
		cfg.Server.Mode = cliOptions.mode
	}
	if cliOptions.language != "" {
		if !i18n.IsRegistered(cliOptions.language) {
			fmt.Fprintf(os.Stderr, "%s: unsupported language %q\n", binaryName, cliOptions.language)
			os.Exit(78)
		}
		cfg.Server.I18n.DefaultLanguage = cliOptions.language
	}
	if cliOptions.debug || cliOptions.mode == "debug" {
		cfg.Server.Debug = true
	}

	// Step 14: Reconfigure logging from config
	logLevel := cfg.Server.Logs.Level
	if logLevel == "" {
		logLevel = "info"
		if cfg.Server.Mode == "development" || cfg.Server.Mode == "debug" {
			logLevel = "debug"
		}
	}
	if err := logger.SetLevel(logLevel); err != nil {
		logger.Warn("could not apply configured log level; using info")
		if fallbackErr := logger.SetLevel("info"); fallbackErr != nil {
			logger.Errorf("failed to configure logging: %v", fallbackErr)
			fmt.Fprintf(os.Stderr, "[ERROR] Failed to configure logging: %v\n", fallbackErr)
			os.Exit(1)
		}
	}
	logger.Info("Configuration loaded successfully")
	mailer := initializeMailer(cfg, appPaths)

	// Step 15: Initialize database
	logger.Info("Connecting to database...")
	db, err := initDatabase(cfg)
	if err != nil {
		logger.Errorf("Database initialization failed: %v", err)
		fmt.Fprintf(os.Stderr, "[ERROR] Database initialization failed: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Errorf("Database shutdown failed: %v", err)
		}
	}()
	logger.Info("Database connected successfully")
	if installedUpdate := strings.TrimSpace(os.Getenv("APIMGR_UPDATE_INSTALLED")); installedUpdate != "" {
		_ = os.Unsetenv("APIMGR_UPDATE_INSTALLED")
		versions := strings.SplitN(installedUpdate, "\n", 2)
		previousVersion, installedVersion := "unknown", strings.TrimSpace(versions[len(versions)-1])
		if len(versions) == 2 {
			previousVersion = strings.TrimSpace(versions[0])
		}
		if err := db.AppendAuditEvent(context.Background(), database.AuditEvent{Category: "update", Action: "installed", ActorType: "system", TargetType: "release", TargetID: installedVersion, Details: map[string]any{"version": installedVersion}, Success: true}); err != nil {
			logger.Warnf("Could not record installed update %s", installedVersion)
		}
		if cfg.Server.Notifications.Email.Events.UpdateInstalled {
			sendLifecycleEmail(mailer, cfg, "update_installed", map[string]string{"new_version": installedVersion, "previous_version": previousVersion})
		}
	}
	if stagedUpdateFailure != nil {
		logger.Errorf("Staged update failed: %v", stagedUpdateFailure)
		_ = db.AppendAuditEvent(context.Background(), database.AuditEvent{Category: "update", Action: "failed", ActorType: "system", TargetType: "release", TargetID: stagedUpdateID, Details: map[string]any{"reason": "apply_failed"}, Success: false})
		if cfg.Server.Notifications.Email.Events.UpdateFailed {
			sendLifecycleEmail(mailer, cfg, "update_failed", map[string]string{"version": stagedUpdateID, "error": "staged update could not be applied"})
		}
	}

	// Step 16: Start scheduler per AI.md PART 19
	logger.Info("Initializing scheduler...")
	sched, err := scheduler.New(db, cfg.Server.Scheduler)
	if err != nil {
		logger.Errorf("Scheduler initialization failed: %v", err)
		fmt.Fprintf(os.Stderr, "[ERROR] Scheduler initialization failed: %v\n", err)
		os.Exit(1)
	}
	if err := sched.SetSecurityReportRetention(cfg.Server.Security.Reports.RetentionDays); err != nil {
		logger.Errorf("Could not configure security report retention: %v", err)
		fmt.Fprintf(os.Stderr, "[ERROR] Could not configure security report retention: %v\n", err)
		os.Exit(1)
	}
	if err := sched.SetPGPKeyserverPublisher(appPaths.Config, cfg.Web.Security.Keyservers); err != nil {
		logger.Errorf("Could not configure PGP keyserver retries: %v", err)
		fmt.Fprintf(os.Stderr, "[ERROR] Could not configure PGP keyserver retries: %v\n", err)
		os.Exit(1)
	}
	updateRestart := make(chan struct{}, 1)
	if err := sched.SetTaskHandler("update_check", newScheduledUpdateCheck(cfg, mailer, db, startedElevated, filepath.Join(appPaths.Data, "update"), updateRestart)); err != nil {
		logger.Errorf("Could not configure update check task: %v", err)
		fmt.Fprintf(os.Stderr, "[ERROR] Could not configure update check task: %v\n", err)
		os.Exit(1)
	}
	sched.Start()
	defer sched.Stop()
	logger.Info("Scheduler initialized successfully")

	// Step 17: Start Tor (if available)
	// TODO: Initialize Tor per AI.md PART 32

	// Step 18: Start HTTP server
	logger.Info("Starting HTTP server...")
	srv, err := startHTTPServer(cfg, db, sched, privilegedListener)
	if err != nil {
		logger.Errorf("Failed to start server: %v", err)
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to start server: %v\n", err)
		os.Exit(1)
	}
	srv.SetMailer(mailer)
	var updateRestarting atomic.Bool
	go func() {
		<-updateRestart
		updateRestarting.Store(true)
		srv.RequestShutdown()
	}()

	// Step 19: Register signal handlers (handled by srv.WaitForShutdown)

	// Step 20: Log startup complete per AI.md PART 17
	logger.Infof("Server started successfully on %s:%d", cfg.Server.Address, cfg.Server.Port)
	if cfg.Server.Notifications.Email.Events.Startup {
		sendLifecycleEmail(mailer, cfg, "startup", map[string]string{"hostname": smtpFacts().FQDN, "version": Version, "mode": cfg.Server.Mode})
	}

	// Display startup banner per AI.md PART 17
	displayStartupBanner(cfg)

	// Step 21: Enter main loop
	srv.WaitForShutdown()
	shutdownReason := "signal"
	if updateRestarting.Load() {
		shutdownReason = "update"
	}
	if cfg.Server.Notifications.Email.Events.Shutdown {
		sendLifecycleEmail(mailer, cfg, "shutdown", map[string]string{"hostname": smtpFacts().FQDN, "version": Version, "mode": cfg.Server.Mode, "reason": shutdownReason})
	}

	logger.Info("Server stopped")
	fmt.Println("[INFO] Server stopped")
	if updateRestarting.Load() {
		return 75
	}
	return 0
}

func initializeMailer(cfg *config.Config, appPaths *paths.Paths) *email.Mailer {
	if cfg.Server.Contact.Admin.Email == "" {
		logger.Warn("server.contact.admin.email is empty; email notifications are disabled")
	}
	settings := cfg.Server.Notifications.Email
	facts := smtpFacts()
	mailerConfig := email.Config{
		Host: settings.SMTP.Host, Port: settings.SMTP.Port, Username: settings.SMTP.Username,
		Password: settings.SMTP.Password, TLSMode: settings.SMTP.TLS, FromName: settings.From.Name,
		FromEmail: settings.From.Email, ReplyTo: settings.ReplyTo, AppName: binaryName,
		FQDN: facts.FQDN, Timeout: 2 * time.Second,
	}
	if appPaths != nil {
		mailerConfig.ConfigDir = appPaths.Config
	}
	warn := func(key, message string) { logger.Warnf("%s: %s", key, message) }
	mailer := email.NewMailer(mailerConfig, warn, logger.Info)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	detected, err := mailer.DetectSMTP(ctx, facts)
	if err != nil {
		logger.Warnf("email.configured=false; email features are disabled: %s", err)
		return mailer
	}
	_, hostOverridden := os.LookupEnv("SMTP_HOST")
	if settings.SMTP.Host == "" && !hostOverridden && appPaths != nil {
		if err := config.SaveDetectedSMTP(filepath.Join(appPaths.Config, "server.yml"), detected.Host, detected.Port); err != nil {
			logger.Warnf("could not persist detected SMTP endpoint: %v", err)
		}
	}
	if mailer.Status().Enabled {
		logger.Infof("email.configured=true; detected SMTP %s:%d", detected.Host, detected.Port)
	} else {
		logger.Warnf("email.configured=false; SMTP relay detected at %s:%d but sender configuration is incomplete", detected.Host, detected.Port)
	}
	return mailer
}

func newScheduledUpdateCheck(cfg *config.Config, mailer *email.Mailer, db *database.DB, startedElevated bool, updateDir string, restart chan<- struct{}) scheduler.TaskHandler {
	return func(ctx context.Context) error {
		if cfg == nil || db == nil {
			return fmt.Errorf("scheduled update check requires configuration and database")
		}
		release, err := update.CheckWithDefer(ctx, Version, cfg.Server.Update.Branch, buildEpoch(), cfg.Server.Update.DeferDays, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("check scheduled release updates: %w", err)
		}
		if release == nil {
			return nil
		}
		releaseID := update.ReleaseID(release)
		if cfg.Server.Update.AutoInstall {
			if !startedElevated {
				alreadySkipped, err := db.HasAuditEvent(ctx, "update", "install_skipped", "release", releaseID)
				if err != nil {
					return err
				}
				if !alreadySkipped {
					if err := db.AppendAuditEvent(ctx, database.AuditEvent{Category: "update", Action: "install_skipped", ActorType: "system", TargetType: "release", TargetID: releaseID, Details: map[string]any{"reason": "not_elevated", "version": release.TagName}, Success: true}); err != nil {
						return err
					}
				}
			} else {
				previouslyFailed, err := db.HasAuditEvent(ctx, "update", "failed", "release", releaseID)
				if err != nil {
					return err
				}
				alreadyStaged, err := db.HasAuditEvent(ctx, "update", "staged", "release", releaseID)
				if err != nil {
					return err
				}
				if !alreadyStaged && !previouslyFailed {
					if _, err := update.StageRelease(ctx, release, updateDir); err != nil {
						_ = db.AppendAuditEvent(ctx, database.AuditEvent{Category: "update", Action: "failed", ActorType: "system", TargetType: "release", TargetID: releaseID, Details: map[string]any{"reason": "stage_failed", "version": release.TagName}, Success: false})
						if cfg.Server.Notifications.Email.Events.UpdateFailed && mailer != nil && cfg.Server.Contact.Admin.Email != "" && mailer.Status().Enabled {
							_ = mailer.Send(ctx, "update_failed", cfg.Server.Contact.Admin.Email, map[string]string{"version": release.TagName, "error": err.Error()})
						}
						return fmt.Errorf("stage release %s: %w", release.TagName, err)
					}
					if err := db.AppendAuditEvent(ctx, database.AuditEvent{Category: "update", Action: "staged", ActorType: "system", TargetType: "release", TargetID: releaseID, Details: map[string]any{"version": release.TagName}, Success: true}); err != nil {
						return err
					}
					if cfg.Server.Notifications.Email.Events.UpdateStaged && mailer != nil && cfg.Server.Contact.Admin.Email != "" && mailer.Status().Enabled {
						_ = mailer.Send(ctx, "update_staged", cfg.Server.Contact.Admin.Email, map[string]string{"version": release.TagName})
					}
					restart <- struct{}{}
				}
			}
		}
		notified, err := db.HasAuditEvent(ctx, "update", "available", "release", releaseID)
		if err != nil {
			return err
		}
		if notified {
			return nil
		}
		if err := db.AppendAuditEvent(ctx, database.AuditEvent{
			Category: "update", Action: "available", ActorType: "system", TargetType: "release", TargetID: releaseID,
			Details: map[string]any{"current_version": Version, "channel": cfg.Server.Update.Branch}, Success: true,
		}); err != nil {
			return err
		}
		logger.Warnf("Update available: current=%s new=%s channel=%s", Version, release.TagName, cfg.Server.Update.Branch)
		if mailer != nil && cfg.Server.Notifications.Email.Events.UpdateAvailable && cfg.Server.Contact.Admin.Email != "" && mailer.Status().Enabled {
			if err := mailer.Send(ctx, "update_available", cfg.Server.Contact.Admin.Email, map[string]string{
				"current_version": Version, "new_version": release.TagName, "channel": cfg.Server.Update.Branch,
			}); err != nil {
				logger.Warnf("Update notification email could not be sent for %s", release.TagName)
			}
		}
		return nil
	}
}

func sendLifecycleEmail(mailer *email.Mailer, cfg *config.Config, template string, values map[string]string) {
	if mailer == nil || cfg.Server.Contact.Admin.Email == "" || !mailer.Status().Enabled {
		return
	}
	timeout := 15 * time.Second
	if template == "shutdown" {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := mailer.Send(ctx, template, cfg.Server.Contact.Admin.Email, values); err != nil {
		logger.Warnf("could not send %s notification: %v", template, err)
	}
}

func smtpFacts() email.SMTPFacts {
	facts := email.SMTPFacts{FQDN: strings.TrimSpace(os.Getenv("DOMAIN"))}
	if facts.FQDN == "" {
		if hostname, err := os.Hostname(); err == nil {
			facts.FQDN = strings.TrimSpace(hostname)
		}
	}
	if facts.FQDN == "" {
		facts.FQDN = strings.TrimSpace(os.Getenv("HOSTNAME"))
	}
	facts.GatewayIP = defaultGatewayIPv4()
	facts.GlobalIPv4 = globalIPv4()
	return facts
}

func defaultGatewayIPv4() string {
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile("/proc/net/route")
		if err != nil {
			return ""
		}
		return parseLinuxDefaultGateway(string(data))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var command *exec.Cmd
	if runtime.GOOS == "windows" {
		command = exec.CommandContext(ctx, "route", "print", "-4")
	} else {
		command = exec.CommandContext(ctx, "route", "-n", "get", "default")
	}
	output, err := command.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(strings.ReplaceAll(line, ":", " "))
		if runtime.GOOS == "windows" && len(fields) >= 5 && fields[0] == "0.0.0.0" && fields[1] == "0.0.0.0" {
			if gateway := net.ParseIP(fields[2]); gateway != nil && gateway.To4() != nil {
				return gateway.String()
			}
		}
		for index, field := range fields {
			if strings.EqualFold(field, "gateway") && index+1 < len(fields) {
				if gateway := net.ParseIP(fields[index+1]); gateway != nil && gateway.To4() != nil {
					return gateway.String()
				}
			}
		}
	}
	return ""
}

func parseLinuxDefaultGateway(routeTable string) string {
	lines := strings.Split(routeTable, "\n")
	if len(lines) > 0 {
		lines = lines[1:]
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[1] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(fields[3], 16, 16)
		if err != nil || flags&1 == 0 {
			continue
		}
		bytes, err := hex.DecodeString(fields[2])
		if err != nil || len(bytes) != 4 {
			continue
		}
		return net.IPv4(bytes[3], bytes[2], bytes[1], bytes[0]).String()
	}
	return ""
}

func globalIPv4() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			var ip net.IP
			switch value := address.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			parsed, ok := netip.ParseAddr(ip.String())
			if ok == nil && parsed.Is4() && parsed.IsGlobalUnicast() && !parsed.IsPrivate() && !parsed.IsLinkLocalUnicast() {
				return parsed.String()
			}
		}
	}
	return ""
}

// displayStartupBanner displays the startup information per AI.md PART 17
func displayStartupBanner(cfg *config.Config) {
	if display.IsDumbTerminal(os.Getenv("TERM")) {
		fmt.Printf("%s %s\nListening on http://%s:%d\nServer started successfully\n", binaryName, Version, cfg.Server.Address, cfg.Server.Port)
		return
	}
	emoji := display.EmojiEnabled(stdoutIsTerminal(), os.Getenv("TERM"), os.Getenv("NO_COLOR"))
	appMark, packageMark, networkMark, successMark := "", "", "", ""
	if emoji {
		appMark, packageMark, networkMark, successMark = "🚀 ", " 📦", "📡 ", "✅ "
	}
	fmt.Println()
	fmt.Println("╭───────────────────────────────────────────────────────────╮")
	fmt.Printf("│  %sMAIL ·%s %s%-39s│\n", appMark, packageMark, Version, "")
	fmt.Println("├───────────────────────────────────────────────────────────┤")
	fmt.Printf("│  %sListening on http://%s:%d%-23s│\n", networkMark, cfg.Server.Address, cfg.Server.Port, "")
	fmt.Printf("│  %sServer started successfully                           │\n", successMark)
	fmt.Println("╰───────────────────────────────────────────────────────────╯")
	fmt.Println()
}

func loggerConfig(settings config.LogsConfig, logDir string) logger.Config {
	formats := map[string]string{}
	custom := map[string]string{}
	disabled := map[string]bool{}
	policies := map[string]logger.LogPolicy{}
	for name, kind := range map[string]config.LogKindConfig{
		"access":   settings.Access,
		"server":   settings.Server,
		"error":    settings.Error,
		"audit":    settings.Audit,
		"security": settings.Security,
		"debug":    settings.Debug,
	} {
		formats[name] = kind.Format
		custom[name] = kind.Custom
		enabled := name != "debug"
		if kind.Enabled != nil {
			enabled = *kind.Enabled
		}
		disabled[name] = !enabled
		rotate := kind.Rotate
		if rotate == "" {
			rotate = settings.Defaults.Rotate
		}
		keep := kind.Keep
		if keep == "" {
			keep = settings.Defaults.Keep
		}
		policies[name] = logger.LogPolicy{Rotate: rotate, Keep: keep, Compress: kind.Compress}
	}
	return logger.Config{
		Level: "info", LogDir: logDir, Format: "text", Console: true,
		Formats: formats, Custom: custom, Disabled: disabled, Policies: policies,
	}
}

// loadConfig loads the configuration file
// Per AI.md PART 8: Step 13
func loadConfig(appPaths *paths.Paths) (*config.Config, error) {
	configFile := filepath.Join(appPaths.Config, "server.yml")

	// Check if config file exists
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		// First run - create default config
		fmt.Println("[INFO] First run detected - creating default configuration")
		cfg := createDefaultConfig(appPaths)
		if err := applyEnvironmentOverrides(cfg); err != nil {
			return nil, err
		}
		if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
			cfg.Server.Port = config.AvailablePort()
		}

		// Generate one-time setup token per AI.md PART 8 Step 13
		setupToken, err := generateSetupToken()
		if err != nil {
			return nil, fmt.Errorf("generating setup token: %w", err)
		}
		cfg.Server.SetupToken = setupToken

		// Save config to file per AI.md PART 5
		if err := saveConfig(cfg, configFile); err != nil {
			return nil, fmt.Errorf("saving initial config: %w", err)
		}
		if err := ensureOperatorToken(appPaths, cfg); err != nil {
			return nil, err
		}

		// Display setup token in console per AI.md PART 17
		displaySetupToken(setupToken, cfg)

		return cfg, nil
	}

	// Load existing config from YAML file
	fmt.Printf("[INFO] Loading config from %s\n", configFile)
	cfg, err := config.Load(configFile)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	if err := applyEnvironmentOverrides(cfg); err != nil {
		return nil, err
	}
	if err := ensureOperatorToken(appPaths, cfg); err != nil {
		return nil, err
	}

	// Update database path if it's relative or empty
	if cfg.Database.Driver == "sqlite" && cfg.Database.Name == "" {
		cfg.Database.Name = filepath.Join(appPaths.DBDir, "server.db")
	}

	return cfg, nil
}

func ensureOperatorToken(appPaths *paths.Paths, cfg *config.Config) error {
	encryptionKeyMissing := strings.TrimSpace(cfg.Server.Security.EncryptionKey) == ""
	generated, err := config.EnsureOperatorToken(appPaths.Config, cfg)
	if err != nil {
		return fmt.Errorf("initializing operator token: %w", err)
	}
	if encryptionKeyMissing {
		logger.Audit("config.secret_generated", "system", map[string]interface{}{"key": "server.security.encryption_key"})
	}
	if generated {
		fmt.Printf("[INFO] Generated server.token (shown once): %s\n", cfg.Server.Token)
		logger.Audit("config.secret_generated", "system", map[string]interface{}{"key": "server.token"})
		return nil
	}
	token := cfg.Server.Token
	logger.Infof("Operator token: adm_****%s", token[len(token)-4:])
	return nil
}

func applyEnvironmentOverrides(cfg *config.Config) error {
	if value := os.Getenv("DOMAIN"); value != "" {
		cfg.Server.FQDN = strings.TrimSpace(value)
	}
	if value := os.Getenv("LISTEN"); value != "" {
		cfg.Server.Address = value
	}
	if value := os.Getenv("PORT"); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			logger.Warnf("invalid configuration value for %s; using default", "PORT")
		} else {
			cfg.Server.Port = port
		}
	}
	if value, ok := os.LookupEnv("MODE"); ok && strings.TrimSpace(value) != "" {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "prod", "production":
			cfg.Server.Mode = "production"
		case "dev", "devel", "development":
			cfg.Server.Mode = "development"
		case "debug":
			cfg.Server.Mode = "debug"
		default:
			logger.Warnf("invalid configuration value for %s; using default", "MODE")
		}
	}
	debugEnvironmentSet := false
	if value, ok := os.LookupEnv("DEBUG"); ok {
		debugEnvironmentSet = true
		cfg.Server.Debug = config.ParseBoolOrDefault("DEBUG", value, false)
	}
	if debugFlag {
		cfg.Server.Debug = true
	} else if cfg.Server.Mode == "debug" && !debugEnvironmentSet {
		cfg.Server.Debug = true
	}
	smtp := &cfg.Server.Notifications.Email
	if value, ok := os.LookupEnv("SMTP_HOST"); ok {
		smtp.SMTP.Host = strings.TrimSpace(value)
	}
	if value, ok := os.LookupEnv("SMTP_PORT"); ok {
		port, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || port < 1 || port > 65535 {
			logger.Warnf("invalid configuration value for %s; using default", "SMTP_PORT")
			smtp.SMTP.Port = 587
		} else {
			smtp.SMTP.Port = port
		}
	}
	if value, ok := os.LookupEnv("SMTP_USERNAME"); ok {
		smtp.SMTP.Username = value
	}
	if value, ok := os.LookupEnv("SMTP_PASSWORD"); ok {
		smtp.SMTP.Password = value
	}
	if value, ok := os.LookupEnv("SMTP_TLS"); ok {
		mode := strings.ToLower(strings.TrimSpace(value))
		switch mode {
		case "", "auto", "starttls", "tls", "none":
			smtp.SMTP.TLS = mode
		default:
			logger.Warnf("invalid configuration value for %s; using default", "SMTP_TLS")
			smtp.SMTP.TLS = "auto"
		}
	}
	if value, ok := os.LookupEnv("SMTP_FROM_NAME"); ok {
		smtp.From.Name = value
	}
	if value, ok := os.LookupEnv("SMTP_FROM_EMAIL"); ok {
		smtp.From.Email = value
	}
	cfg.Server.Database = cfg.Database
	return nil
}

// generateSetupToken generates a one-time setup token per AI.md PART 17
// Format: 32 hexadecimal characters (128-bit random)
func generateSetupToken() (string, error) {
	// Use crypto/rand for secure random bytes per AI.md PART 17
	bytes := make([]byte, 16) // 16 bytes = 32 hex chars
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	return fmt.Sprintf("%x", bytes), nil
}

// displaySetupToken displays the setup token banner per AI.md PART 17
func displaySetupToken(token string, cfg *config.Config) {
	if display.IsDumbTerminal(os.Getenv("TERM")) {
		fmt.Printf("SETUP REQUIRED\nSetup Token: %s\nGo to http://%s:%d/admin/server/setup and enter this token to complete setup.\nThis token will only be shown ONCE.\n", token, cfg.Server.Address, cfg.Server.Port)
		return
	}
	setupMark := ""
	if display.EmojiEnabled(stdoutIsTerminal(), os.Getenv("TERM"), os.Getenv("NO_COLOR")) {
		setupMark = "🔑 "
	}
	fmt.Println()
	fmt.Println("┌───────────────────────────────────────────────────────────┐")
	fmt.Printf("│  %sSETUP REQUIRED                                        │\n", setupMark)
	fmt.Println("├───────────────────────────────────────────────────────────┤")
	fmt.Printf("│  Setup Token: %-42s  │\n", token)
	fmt.Println("│                                                           │")
	fmt.Printf("│  Go to http://%s:%d/admin/server/setup         │\n", cfg.Server.Address, cfg.Server.Port)
	fmt.Println("│  and enter this token to complete setup.                 │")
	fmt.Println("│                                                           │")
	fmt.Println("│  This token will only be shown ONCE.                      │")
	fmt.Println("└───────────────────────────────────────────────────────────┘")
	fmt.Println()
}

// createDefaultConfig creates default configuration
func createDefaultConfig(appPaths *paths.Paths) *config.Config {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address:   "[::]",
			Port:      config.AvailablePort(),
			PIDFile:   true,
			Daemonize: false,
			Mode:      "production",
			Update:    config.UpdateConfig{Branch: "stable"},
		},
	}
	cfg.Server.Security.Reports.RetentionDays = 365
	logs := config.LogsConfig{
		Defaults: config.LogDefaultsConfig{Rotate: "weekly,50MB", Keep: "none"},
		Access:   config.LogKindConfig{Format: "apache", Rotate: "monthly", Keep: "none"},
		Server:   config.LogKindConfig{Format: "text"},
		Error:    config.LogKindConfig{Format: "text"},
		Audit:    config.LogKindConfig{Format: "json", Rotate: "daily"},
		Security: config.LogKindConfig{Format: "fail2ban"},
		Debug:    config.LogKindConfig{Format: "text"},
	}
	logs.Access.Enabled = boolConfig(true)
	logs.Server.Enabled = boolConfig(true)
	logs.Error.Enabled = boolConfig(true)
	logs.Audit.Enabled = boolConfig(true)
	logs.Security.Enabled = boolConfig(true)
	logs.Debug.Enabled = boolConfig(false)
	cfg.Server.Logs = logs
	cfg.Database = config.DatabaseConfig{Driver: "sqlite", Name: filepath.Join(appPaths.DBDir, "server.db")}
	cfg.Server.Database = cfg.Database
	cfg.Server.Privacy.Data.StoredOnServer = true
	cfg.Server.Privacy.Consent.ShowUntilAcknowledged = true
	cfg.Server.Privacy.Consent.ShowPreferences = true
	cfg.Server.Privacy.Cookies.Preferences.Enabled = true
	cfg.Server.Privacy.Cookies.Analytics.Enabled = true
	cfg.Server.Privacy.Cookies.Essential.Enabled = true
	cfg.Server.Privacy.Retention.ExportAvailable = true
	cfg.Server.Privacy.Retention.DeletionAvailable = true
	cfg.Server.I18n.DefaultLanguage = "en"
	cfg.Server.I18n.AvailableLanguages = i18n.LanguageCodes()
	return cfg
}

func boolConfig(value bool) *bool { return &value }

// saveConfig saves configuration to YAML file
// Per AI.md PART 5: Comments MUST be above settings, never inline
func saveConfig(cfg *config.Config, path string) error {
	var languages strings.Builder
	for _, language := range cfg.Server.I18n.AvailableLanguages {
		fmt.Fprintf(&languages, "      - %s\n", strconv.Quote(language))
	}
	if languages.Len() == 0 {
		for _, language := range i18n.LanguageCodes() {
			fmt.Fprintf(&languages, "      - %s\n", strconv.Quote(language))
		}
	}
	data := fmt.Sprintf(`# Mail Panel Configuration
# Generated automatically on first run

server:
  # Server listening address ([::] = all interfaces)
  address: %s
  
  # Server listening port (64000-64999 range recommended)
  port: %d

  # Create and check a PID file outside containers
  pidfile: %t

  # Detach from the terminal for manual starts
  daemonize: %t

  # Runtime mode (production or development)
  mode: %s

  logs:
    # Global log level: empty follows the selected runtime mode
    level: ""
    defaults:
      custom: ""
      rotate: weekly,50MB
      keep: none
    access:
      format: apache
      rotate: monthly
      keep: none
    server:
      format: text
    error:
      format: text
    audit:
      enabled: true
      format: json
      rotate: daily
      compress: false
    security:
      format: fail2ban
    debug:
      enabled: false
      format: text
  
  # Admin panel path (e.g., /admin)
  admin_path: admin
  
  # One-time setup token (generated on first run)
  setup_token: %s

  update:
    # Release channel used by --update check and --update yes
    branch: stable
    # Scheduled updates remain notify-only until the staged service flow is enabled
    auto_install: false
    # Hold stable and beta scheduled updates until this many days after publication
    defer_days: 0
    # Minimum compatible client version returned by autodiscovery
    cli_min_version: ""

  notifications:
    email:
      smtp:
        host: ""
        port: 587
        username: ""
        password: ""
        tls: auto
      from:
        name: ""
        email: ""
      reply_to: ""
      events:
        startup: false
        shutdown: false
        backup_complete: false
        backup_failed: true
        ssl_expiring: true
        ssl_renewed: false
        ssl_renewal_failed: true
        security_alert: true
        scheduler_error: true
        update_available: false
        update_installed: true
        update_failed: true
        update_staged: true
        disk_low: true
        database_error: true

  contact:
    admin:
      email: ""

  i18n:
    # Default language for server-rendered pages and responses
    default_language: %s
    # Complete embedded locale set required by AI.md PART 30
    available_languages:
%s

  cors:
    # Wildcard origins never receive credentialed CORS responses
    allowed_origins: ["*"]
    allow_credentials: true
    max_age: 86400

  database:
    # SQLite (default) or libSQL/Turso remote
    driver: %s
    # The database URL remains local for SQLite and is remote for libSQL
    url: %s

  privacy:
    data:
      sold: false
      stored_on_server: true
      sharing:
        - condition: "analytics"
          when: "Tracking is configured and the visitor consents"
          data: "Anonymized page views, browser type and country"
        - condition: "email"
          when: "SMTP is configured for sending"
          data: "Email address and message content"
        - condition: "user_initiated"
          when: "The visitor explicitly shares content"
          data: "Information the visitor chooses to share"
    consent:
      show_until_acknowledged: true
      message: "We use essential cookies and, with your consent, preference and analytics cookies. Your data is stored on our servers and is never sold."
      message_if_sold: "We use essential cookies and, with your consent, preference and analytics cookies. Your data may be shared with or sold to third parties as described in our Privacy Policy."
      policy:
        text: "Privacy Policy"
        url: "/server/privacy"
        version: "1"
      buttons:
        decline: "Decline"
        accept: "I Agree"
      position: bottom
      show_preferences: true
      preferences_text: "Manage Preferences"
    cookies:
      essential:
        enabled: true
        description: "Required for site operation, including security, consent, and automatic build-version cookies."
      preferences:
        enabled: true
        description: "Remember theme, language, and interface settings. Disabling this resets them to defaults on each visit."
      analytics:
        enabled: true
        description: "Help us understand how visitors use the site."
        description_suffix_not_sold: "Analytics data is anonymized and never sold."
        description_suffix_sold: "Analytics data may be shared with third parties."
    retention:
      period: "Mail resources remain until the operator deletes them. Preferences are stored only in your browser. Operational logs are retained for up to 30 days. Anonymized analytics may be retained for up to 12 months."
      export_available: true
      deletion_available: true

web:
  robots:
    allow:
      - /
      - /api
    deny: []

`, strconv.Quote(cfg.Server.Address), cfg.Server.Port, cfg.Server.PIDFile, cfg.Server.Daemonize, strconv.Quote(cfg.Server.Mode), strconv.Quote(cfg.Server.SetupToken), strconv.Quote(cfg.Server.I18n.DefaultLanguage), languages.String(), strconv.Quote(cfg.Database.Driver), strconv.Quote(cfg.Database.Name))

	return os.WriteFile(path, []byte(data), 0o600)
}

// initDatabase initializes database connection and schema
// Per AI.md PART 8: Step 15
func initDatabase(cfg *config.Config) (*database.DB, error) {
	// Connect to database
	db, err := database.Connect(cfg)
	if err != nil {
		return nil, fmt.Errorf("database connection: %w", err)
	}

	// Create schema (idempotent per AI.md PART 10)
	if err := db.EnsureSchema(); err != nil {
		closeErr := db.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("schema creation: %w (database close: %v)", err, closeErr)
		}
		return nil, fmt.Errorf("schema creation: %w", err)
	}

	return db, nil
}

// startHTTPServer creates and starts the HTTP server
// Per AI.md PART 8: Step 18
func startHTTPServer(cfg *config.Config, db *database.DB, sched *scheduler.Scheduler, listener net.Listener) (*server.Server, error) {
	appPaths := configuredPaths()
	installationSecret, err := db.GeneratedSecret(context.Background(), "installation_secret")
	if err != nil {
		return nil, fmt.Errorf("read installation secret before PGP recovery: %w", err)
	}
	securityDir := filepath.Join(appPaths.Config, "security")
	if err := recoverPGPRotationFiles(context.Background(), db, securityDir, installationSecret); err != nil {
		return nil, fmt.Errorf("recover pending PGP rotation before server startup: %w", err)
	}
	server.SetBuildMetadata(Version, CommitID, BuildDate)
	srv := server.New(cfg, db, BuildDate)
	srv.SetScheduler(sched)

	if listener == nil {
		err = srv.Start()
	} else {
		err = srv.StartWithListener(listener)
	}
	if err != nil {
		return nil, err
	}

	return srv, nil
}
