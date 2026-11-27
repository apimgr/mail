package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/apimgr/mail/src/common/api"
	projectconfig "github.com/apimgr/mail/src/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type cliState struct {
	server, token, configPath, tokenFile, output string
	configFile                                   string
	debug, quiet, verbose                        bool
	color, lang                                  string
	config                                       cliConfig
	stdout, stderr                               io.Writer
}

type cliCommandError struct {
	code int
	err  error
}

func (e cliCommandError) Error() string { return e.err.Error() }
func (e cliCommandError) Unwrap() error { return e.err }

func newRootCommand(stdout, stderr io.Writer) *cobra.Command {
	state := &cliState{stdout: stdout, stderr: stderr}
	name := filepath.Base(os.Args[0])
	root := &cobra.Command{
		Use:           name + " [flags] <command>",
		Short:         "Manage mail infrastructure through the mail API",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				if !isTerminal(os.Stdin) {
					return errors.New("a command is required in non-interactive mode")
				}
				return runClientTUI(cmd.Context(), state)
			}
			return fmt.Errorf("unknown command %q", strings.Join(args, " "))
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	flags := root.PersistentFlags()
	flags.StringVar(&state.server, "server", "", "Server base URL")
	flags.StringVar(&state.token, "token", "", "Owner or operator API token")
	flags.StringVar(&state.tokenFile, "token-file", "", "Read API token from a file")
	flags.StringVar(&state.configPath, "config", "", "CLI configuration file or profile")
	flags.StringVar(&state.output, "output", "", "Output format: table, json, yaml, plain, csv")
	flags.StringVar(&state.color, "color", "", "Color output: auto, yes, no")
	flags.StringVar(&state.lang, "lang", "", "Language for output")
	flags.BoolVar(&state.debug, "debug", false, "Enable debug output")
	flags.BoolVar(&state.quiet, "quiet", false, "Suppress non-essential output")
	flags.BoolVar(&state.verbose, "verbose", false, "Enable verbose output")
	root.Version = Version
	root.SetVersionTemplate("{{.Name}} {{.Version}} (" + shortCommitID() + ")\nBuilt: " + BuildDate + "\nGo: " + runtime.Version() + "\nOS/Arch: " + runtime.GOOS + "/" + runtime.GOARCH + "\n")
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if err := ensureDirs(); err != nil {
			return err
		}
		cfg, err := loadConfig(state.configPath)
		if err != nil {
			return err
		}
		state.config = cfg
		state.configFile, err = cliConfigFile(state.configPath)
		if err != nil {
			return err
		}
		if !validAPIVersion(cfg.Server.APIVersion) {
			return cliCommandError{code: 78, err: fmt.Errorf("invalid API version %q in cli.yml", cfg.Server.APIVersion)}
		}
		serverFlag := cmd.Flags().Changed("server")
		if !serverFlag {
			state.server = strings.TrimSpace(os.Getenv("MAIL_SERVER_PRIMARY"))
		}
		if state.server == "" {
			state.server = strings.TrimSpace(cfg.Server.Primary)
		}
		if state.server == "" {
			state.server = strings.TrimSpace(os.Getenv("OFFICIAL_SITE"))
		}
		if state.server == "" {
			state.server = strings.TrimSpace(OfficialSite)
		}
		if state.server != "" && !validServerURL(state.server) {
			return cliCommandError{code: 78, err: errors.New("invalid server URL; use an absolute http:// or https:// URL without credentials or a fragment")}
		}
		if serverFlag && validServerURL(state.server) && !validServerURL(cfg.Server.Primary) {
			state.config.Server.Primary = state.server
			if err := saveCLIConfig(state.configFile, state.config); err != nil {
				return err
			}
		}
		if state.server == "" && cmd != cmd.Root() {
			return errors.New("no server configured; pass --server URL or configure server.primary in cli.yml")
		}
		if state.token == "" {
			state.token = strings.TrimSpace(os.Getenv("MAIL_TOKEN"))
		}
		if state.token == "" {
			state.token = strings.TrimSpace(cfg.Auth.Token)
		}
		if cmd.Flags().Changed("token") && state.token != "" && cfg.Auth.Token == "" {
			state.config.Auth.Token = state.token
			if err := saveCLIConfig(state.configFile, state.config); err != nil {
				return err
			}
		}
		file := state.tokenFile
		if file == "" {
			file = cfg.Auth.TokenFile
		}
		if state.token == "" && file != "" {
			b, readErr := os.ReadFile(expandHome(file))
			if readErr != nil {
				return fmt.Errorf("reading token file: %w", readErr)
			}
			state.token = strings.TrimSpace(string(b))
		}
		if state.output == "" {
			state.output = strings.TrimSpace(os.Getenv("MAIL_OUTPUT_FORMAT"))
		}
		if state.output == "" {
			state.output = cfg.Output.Format
		}
		if state.output == "" {
			state.output = cfg.Defaults.Output
		}
		if state.output == "" {
			state.output = "table"
		}
		if state.color == "" {
			state.color = strings.TrimSpace(os.Getenv("MAIL_COLOR"))
		}
		if state.color == "" {
			state.color = cfg.Output.Color
		}
		if state.color == "" {
			state.color = "auto"
		}
		if state.color != "auto" && state.color != "yes" && state.color != "no" {
			return cliCommandError{code: 2, err: fmt.Errorf("invalid --color value %q: use auto, yes, or no", state.color)}
		}
		if state.lang == "" {
			state.lang = strings.TrimSpace(os.Getenv("MAIL_LANG"))
		}
		if state.lang == "" {
			state.lang = cfg.Defaults.Lang
		}
		if state.lang == "" || state.lang == "auto" {
			state.lang = strings.TrimSpace(os.Getenv("LANG"))
			if cut, _, ok := strings.Cut(state.lang, "."); ok {
				state.lang = cut
			}
			state.lang = strings.ReplaceAll(state.lang, "_", "-")
		}
		if !cmd.Flags().Changed("debug") {
			state.debug = projectconfig.ParseBoolOrDefault("MAIL_DEBUG", os.Getenv("MAIL_DEBUG"), cfg.Debug)
		}
		if !cmd.Flags().Changed("quiet") {
			state.quiet = projectconfig.ParseBoolOrDefault("MAIL_QUIET", os.Getenv("MAIL_QUIET"), cfg.Output.Quiet)
		}
		if !cmd.Flags().Changed("verbose") {
			state.verbose = projectconfig.ParseBoolOrDefault("MAIL_VERBOSE", os.Getenv("MAIL_VERBOSE"), cfg.Output.Verbose)
		}
		if len(args) == 0 && cfg.Display.Mode == "gui" {
			return cliCommandError{code: 69, err: errors.New("GUI is unavailable in this static client build; set display.mode to tui or auto")}
		}
		if len(args) == 0 && !cfg.TUI.Enabled && cfg.Display.Mode != "gui" {
			return errors.New("TUI is disabled in cli.yml")
		}
		if state.server == "" && (len(args) == 0) && !isTerminal(os.Stdin) {
			return errors.New("CLI setup requires an interactive terminal or a configured server")
		}
		return nil
	}
	addAPICommands(root, state)
	return root
}

func shortCommitID() string {
	if len(CommitID) > 7 {
		return CommitID[:7]
	}
	return CommitID
}

func validAPIVersion(value string) bool {
	if len(value) < 2 || len(value) > 16 || value[0] != 'v' {
		return false
	}
	for _, char := range value[1:] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'z') && char != '-' {
			return false
		}
	}
	return true
}

func (s *cliState) apiBasePath() string {
	return apiBasePathFor(s.config.Server.APIVersion)
}

func apiBasePathFor(value string) string {
	version := strings.TrimSpace(value)
	if !validAPIVersion(version) {
		version = api.Version
	}
	return "/api/" + url.PathEscape(version)
}

func addAPICommands(root *cobra.Command, state *cliState) {
	add := func(parent *cobra.Command, use, method, route string, args cobra.PositionalArgs) *cobra.Command {
		cmd := &cobra.Command{Use: use, Args: args, RunE: func(cmd *cobra.Command, argv []string) error {
			if requiresCLIConfirmation(method, route) {
				force, _ := cmd.Flags().GetBool("force")
				if !force {
					if !isTerminal(os.Stdin) {
						return errors.New("this operation is destructive; pass --force to confirm")
					}
					if _, err := fmt.Fprintf(state.stderr, "Confirm %s? [y/N] ", strings.TrimSpace(use)); err != nil {
						return fmt.Errorf("write confirmation prompt: %w", err)
					}
					answer, readErr := bufio.NewReader(os.Stdin).ReadString('\n')
					if readErr != nil && readErr != io.EOF {
						return readErr
					}
					if !strings.EqualFold(strings.TrimSpace(answer), "y") && !strings.EqualFold(strings.TrimSpace(answer), "yes") {
						return errors.New("operation cancelled")
					}
				}
			}
			path := route
			for i, arg := range argv {
				path = strings.ReplaceAll(path, fmt.Sprintf("{%d}", i), url.PathEscape(arg))
			}
			bodyText, _ := cmd.Flags().GetString("data")
			var body io.Reader
			if bodyText != "" {
				body = strings.NewReader(bodyText)
			}
			return state.do(method, path, body)
		}}
		cmd.Flags().String("data", "", "JSON request body")
		cmd.Flags().Bool("force", false, "Skip the confirmation prompt for destructive operations")
		parent.AddCommand(cmd)
		return cmd
	}
	noArgs := cobra.NoArgs
	oneArg := cobra.ExactArgs(1)
	users := &cobra.Command{Use: "users", Short: "Manage mail users"}
	root.AddCommand(users)
	add(users, "list", "GET", "/users", noArgs)
	add(users, "get {id}", "GET", "/users/{0}", oneArg)
	add(users, "create", "POST", "/users", noArgs).Flags().Lookup("data").Usage = "JSON user fields"
	add(users, "update {id}", "PUT", "/users/{0}", oneArg)
	add(users, "delete {id}", "DELETE", "/users/{0}", oneArg)
	add(users, "password {id}", "POST", "/users/{0}/password", oneArg)
	users.AddCommand(&cobra.Command{Use: "import <csv-file>", Short: "Import virtual mailboxes from CSV", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		file, err := os.Open(args[0])
		if err != nil {
			return fmt.Errorf("open CSV file: %w", err)
		}
		defer func() { _ = file.Close() }()
		return state.doContentType(http.MethodPost, "/users/import", file, "text/csv")
	}})
	domains := &cobra.Command{Use: "domains", Short: "Manage virtual domains"}
	root.AddCommand(domains)
	add(domains, "list", "GET", "/domains", noArgs)
	add(domains, "get {domain}", "GET", "/domains/{0}", oneArg)
	add(domains, "create", "POST", "/domains", noArgs)
	add(domains, "update {domain}", "PUT", "/domains/{0}", oneArg)
	add(domains, "delete {domain}", "DELETE", "/domains/{0}", oneArg)
	add(domains, "dns {domain}", "GET", "/domains/{0}/dns", oneArg)
	add(domains, "dkim {domain}", "GET", "/domains/{0}/dkim", oneArg)
	add(domains, "generate-dkim {domain}", "POST", "/domains/{0}/dkim/generate", oneArg)
	for _, spec := range []struct{ name, route string }{{"aliases", "/aliases"}, {"forwards", "/forwards"}} {
		resource := &cobra.Command{Use: spec.name, Short: "Manage " + spec.name}
		root.AddCommand(resource)
		add(resource, "list", "GET", spec.route, noArgs)
		add(resource, "create", "POST", spec.route, noArgs)
		add(resource, "delete {id}", "DELETE", spec.route+"/{0}", oneArg)
	}
	queue := &cobra.Command{Use: "queue", Short: "Inspect and operate the mail queue"}
	root.AddCommand(queue)
	add(queue, "list", "GET", "/queue", noArgs)
	add(queue, "get {id}", "GET", "/queue/{0}", oneArg)
	for _, action := range []string{"flush", "hold", "release"} {
		add(queue, action+" {id}", "POST", "/queue/{0}/"+action, oneArg)
	}
	add(queue, "delete {id}", "DELETE", "/queue/{0}", oneArg)
	add(queue, "flush-all", "POST", "/queue/flush-all", noArgs)
	config := &cobra.Command{Use: "config", Short: "Inspect and validate component configuration"}
	root.AddCommand(config)
	add(config, "get {component}", "GET", "/config/{0}", oneArg)
	add(config, "set {component}", "PUT", "/config/{0}", oneArg)
	add(config, "validate {component}", "POST", "/config/{0}/validate", oneArg)
	add(config, "templates {component}", "GET", "/config/{0}/templates", oneArg)
	add(config, "backup {component}", "POST", "/config/{0}/backup", oneArg)
	add(config, "restore {component}", "POST", "/config/{0}/restore", oneArg)
	stats := &cobra.Command{Use: "stats", Short: "Show mail statistics"}
	root.AddCommand(stats)
	for _, kind := range []string{"delivery", "spam", "queue"} {
		add(stats, kind, "GET", "/stats/"+kind, noArgs)
	}
	logs := &cobra.Command{Use: "logs", Short: "Search component logs", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return state.do("GET", "/logs/"+url.PathEscape(args[0]), nil)
	}}
	root.AddCommand(logs)
	services := &cobra.Command{Use: "services", Short: "Inspect and control mail services"}
	root.AddCommand(services)
	add(services, "list", "GET", "/services", noArgs)
	add(services, "get {service}", "GET", "/services/{0}", oneArg)
	for _, action := range []string{"start", "stop", "restart", "reload"} {
		add(services, action+" {service}", "POST", "/services/{0}/"+action, oneArg)
	}
	health := &cobra.Command{Use: "health", Short: "Show mail-system health", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error { return state.do("GET", "/health", nil) }}
	root.AddCommand(health)
	for _, spec := range []struct{ name, path string }{{"status", "/server/healthz"}, {"about", "/server/about"}} {
		path := spec.path
		root.AddCommand(&cobra.Command{Use: spec.name, Short: "Show server " + spec.name, Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error { return state.do("GET", path, nil) }})
	}
}

func requiresCLIConfirmation(method, route string) bool {
	return method == http.MethodDelete || method == http.MethodPost && strings.HasPrefix(route, "/queue/")
}

func (s *cliState) do(method, endpoint string, body io.Reader) error {
	return s.doContentType(method, endpoint, body, "application/json")
}

func (s *cliState) doContentType(method, endpoint string, body io.Reader, contentType string) error {
	base := strings.TrimRight(strings.TrimSpace(s.server), "/")
	u, err := url.ParseRequestURI(base)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return errors.New("server URL must start with http:// or https://")
	}
	path := s.apiBasePath() + endpoint
	request, err := http.NewRequest(method, base+path, body)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", ProjectName+"-cli/"+Version)
	if body != nil {
		request.Header.Set("Content-Type", contentType)
	}
	if s.token != "" {
		request.Header.Set("Authorization", "Bearer "+s.token)
	}
	timeout := 30 * time.Second
	if s.config.Server.Timeout != "" {
		if parsed, parseErr := time.ParseDuration(s.config.Server.Timeout); parseErr == nil && parsed > 0 {
			timeout = parsed
		}
	}
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return cliCommandError{code: 69, err: fmt.Errorf("request failed: %w", err)}
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		code := 1
		switch response.StatusCode {
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			code = 65
		case http.StatusNotFound:
			code = 66
		case http.StatusUnauthorized, http.StatusForbidden:
			code = 77
		}
		return cliCommandError{code: code, err: fmt.Errorf("server returned %s: %s", response.Status, strings.TrimSpace(string(data)))}
	}
	return writeOutput(s.stdout, data, s.output)
}

func writeOutput(out io.Writer, data []byte, format string) error {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("server returned invalid JSON: %w", err)
	}
	var formatted []byte
	switch strings.ToLower(format) {
	case "json":
		formatted, _ = json.MarshalIndent(value, "", "  ")
	case "table":
		formatted = formatTable(value)
	case "csv":
		formatted = formatCSV(value)
	case "yaml":
		formatted, _ = yaml.Marshal(value)
	case "plain":
		formatted = []byte(strings.TrimSpace(string(data)))
	default:
		return fmt.Errorf("unsupported output format %q", format)
	}
	_, err := out.Write(append(formatted, '\n'))
	return err
}

func formatCSV(value any) []byte {
	var rows []any
	switch typed := value.(type) {
	case []any:
		rows = typed
	case map[string]any:
		if data, ok := typed["data"]; ok {
			if list, ok := data.([]any); ok {
				rows = list
			} else {
				rows = []any{data}
			}
		} else {
			rows = []any{typed}
		}
	default:
		return []byte(fmt.Sprint(value))
	}
	if len(rows) == 0 {
		return nil
	}
	first, ok := rows[0].(map[string]any)
	if !ok {
		data, _ := json.Marshal(rows)
		return data
	}
	keys := make([]string, 0, len(first))
	for key := range first {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out bytes.Buffer
	writer := csv.NewWriter(&out)
	if err := writer.Write(keys); err != nil {
		return nil
	}
	for _, row := range rows {
		record, ok := row.(map[string]any)
		if !ok {
			continue
		}
		values := make([]string, len(keys))
		for i, key := range keys {
			values[i] = fmt.Sprint(record[key])
		}
		if err := writer.Write(values); err != nil {
			return nil
		}
	}
	writer.Flush()
	return bytes.TrimRight(out.Bytes(), "\n")
}

func formatTable(value any) []byte {
	var rows []any
	switch typed := value.(type) {
	case []any:
		rows = typed
	case map[string]any:
		if data, ok := typed["data"]; ok {
			if list, ok := data.([]any); ok {
				rows = list
			} else {
				rows = []any{data}
			}
		} else {
			rows = []any{typed}
		}
	default:
		return []byte(fmt.Sprint(value))
	}
	if len(rows) == 0 {
		return []byte("No results")
	}
	first, ok := rows[0].(map[string]any)
	if !ok {
		b, _ := json.MarshalIndent(rows, "", "  ")
		return b
	}
	keys := make([]string, 0, len(first))
	for key := range first {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out bytes.Buffer
	w := tabwriter.NewWriter(&out, 0, 4, 2, ' ', 0)
	for i, key := range keys {
		if i > 0 {
			_, _ = io.WriteString(w, "\t")
		}
		_, _ = io.WriteString(w, key)
	}
	_, _ = io.WriteString(w, "\n")
	for _, row := range rows {
		record, ok := row.(map[string]any)
		if !ok {
			continue
		}
		for i, key := range keys {
			if i > 0 {
				_, _ = io.WriteString(w, "\t")
			}
			_, _ = fmt.Fprint(w, record[key])
		}
		_, _ = io.WriteString(w, "\n")
	}
	_ = w.Flush()
	return bytes.TrimRight(out.Bytes(), "\n")
}
