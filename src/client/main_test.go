package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRunHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--version"}} {
		var output bytes.Buffer
		if err := run(args, &output, io.Discard); err != nil {
			t.Fatalf("run(%q): %v", args, err)
		}
		if output.Len() == 0 {
			t.Errorf("run(%q) produced no output", args)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cli.yml")
	data := []byte("server:\n  primary: https://mail.example\nauth:\n  token: token-value\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Primary != "https://mail.example" || cfg.Auth.Token != "token-value" {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestLoadConfigMissingAndInvalid(t *testing.T) {
	if _, err := loadConfig(filepath.Join(t.TempDir(), "missing.yml")); err != nil {
		t.Fatalf("missing config error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "invalid.yml")
	if err := os.WriteFile(path, []byte("server: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("invalid YAML config succeeded")
	}
}

func TestRunUsesConfigAndCallsAbout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/server/about" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		if got := r.Header.Get("User-Agent"); got != "mail-cli/"+Version {
			t.Errorf("User-Agent = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-value" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "cli.yml")
	configData := []byte("server:\n  primary: " + server.URL + "\n  api_version: v2\nauth:\n  token: token-value\n")
	if err := os.WriteFile(configPath, configData, 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"about", "--config", configPath, "--output", "json"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "\"ok\": true") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestRunImportsMailboxCSVAsTextCSV(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	csvPath := filepath.Join(t.TempDir(), "mailboxes.csv")
	const csvBody = "email,password,quota_bytes,enabled\ninfo@example.test,correct horse battery staple,0,true\n"
	if err := os.WriteFile(csvPath, []byte(csvBody), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/users/import" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "text/csv" {
			t.Errorf("Content-Type = %q, want text/csv", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer operator-token" {
			t.Errorf("Authorization = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != csvBody {
			t.Errorf("CSV body = %q, err=%v", body, err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"data":{"created":1}}`))
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "cli.yml")
	configData := []byte("server:\n  primary: " + server.URL + "\nauth:\n  token: operator-token\n")
	if err := os.WriteFile(configPath, configData, 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"users", "import", csvPath, "--config", configPath, "--output", "json"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"created": 1`) {
		t.Fatalf("import output = %q", output.String())
	}
}

func TestFormatCSV(t *testing.T) {
	rows := []any{
		map[string]any{"name": "Alice", "email": "alice@example.test"},
		"ignored non-record row",
		map[string]any{"name": "Bob", "email": "bob,ops@example.test"},
	}
	if got, want := string(formatCSV(rows)), "email,name\nalice@example.test,Alice\n\"bob,ops@example.test\",Bob"; got != want {
		t.Fatalf("formatCSV(rows) = %q, want %q", got, want)
	}
	for _, test := range []struct {
		name  string
		value any
		want  string
	}{
		{name: "empty list", value: []any{}, want: ""},
		{name: "map without data", value: map[string]any{"name": "Alice"}, want: "name\nAlice"},
		{name: "single data value", value: map[string]any{"data": map[string]any{"name": "Alice"}}, want: "name\nAlice"},
		{name: "scalar data", value: map[string]any{"data": "ready"}, want: `["ready"]`},
		{name: "non-record list", value: []any{"ready"}, want: `["ready"]`},
		{name: "scalar", value: 42, want: "42"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := string(formatCSV(test.value)); got != test.want {
				t.Fatalf("formatCSV(%#v) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func TestFormatTableAndWriteOutput(t *testing.T) {
	rows := []any{
		map[string]any{"name": "Alice", "email": "alice@example.test"},
		"ignored non-record row",
		map[string]any{"name": "Bob", "email": "bob@example.test"},
	}
	formatted := string(formatTable(rows))
	for _, expected := range []string{"email", "name", "alice@example.test", "Alice", "bob@example.test", "Bob"} {
		if !strings.Contains(formatted, expected) {
			t.Errorf("formatTable output %q does not contain %q", formatted, expected)
		}
	}
	for _, test := range []struct {
		name  string
		value any
		want  string
	}{
		{name: "empty list", value: []any{}, want: "No results"},
		{name: "map without data", value: map[string]any{"name": "Alice"}},
		{name: "single data value", value: map[string]any{"data": map[string]any{"name": "Alice"}}},
		{name: "scalar data", value: map[string]any{"data": "ready"}, want: "[\n  \"ready\"\n]"},
		{name: "non-record list", value: []any{"ready"}, want: "[\n  \"ready\"\n]"},
		{name: "scalar", value: 42, want: "42"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := string(formatTable(test.value))
			if test.want != "" && got != test.want {
				t.Fatalf("formatTable(%#v) = %q, want %q", test.value, got, test.want)
			}
			if test.want == "" && !strings.Contains(got, "Alice") {
				t.Fatalf("formatTable(%#v) = %q, want Alice row", test.value, got)
			}
		})
	}

	for _, test := range []struct {
		name    string
		data    []byte
		format  string
		want    string
		wantErr bool
	}{
		{name: "json", data: []byte(`{"ok":true}`), format: "json", want: "{\n  \"ok\": true\n}\n"},
		{name: "table", data: []byte(`{"data":[{"name":"Alice"}]}`), format: "table", want: "name\nAlice\n"},
		{name: "csv", data: []byte(`{"data":[{"name":"Alice"}]}`), format: "csv", want: "name\nAlice\n"},
		{name: "yaml", data: []byte(`{"ok":true}`), format: "yaml", want: "ok: true\n\n"},
		{name: "plain", data: []byte("  \"value\"  \n"), format: "plain", want: "\"value\"\n"},
		{name: "invalid json", data: []byte("{"), format: "json", wantErr: true},
		{name: "unknown format", data: []byte(`{"ok":true}`), format: "toml", wantErr: true},
	} {
		t.Run("output_"+test.name, func(t *testing.T) {
			var output bytes.Buffer
			err := writeOutput(&output, test.data, test.format)
			if test.wantErr {
				if err == nil {
					t.Fatal("writeOutput() error = nil, want error")
				}
				return
			}
			if err != nil || output.String() != test.want {
				t.Fatalf("writeOutput() = %q, %v; want %q", output.String(), err, test.want)
			}
		})
	}
}

func TestValidAPIVersion(t *testing.T) {
	for _, value := range []string{"v1", "v2", "v10", "v2-beta"} {
		if !validAPIVersion(value) {
			t.Errorf("validAPIVersion(%q) = false", value)
		}
	}
	for _, value := range []string{"", "v", "1", "../v1", "v1/../admin", "v1?x=y", "v!"} {
		if validAPIVersion(value) {
			t.Errorf("validAPIVersion(%q) = true", value)
		}
	}
}

func TestTUITokenPreferenceDoesNotOverwriteSavedTokenOnBlank(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cli.yml")
	cfg := defaultCLIConfig()
	cfg.Auth.Token = "owner-secret"
	state := &cliState{config: cfg, configFile: path, token: cfg.Auth.Token}
	model := newClientTUI(state)
	for index, setting := range model.settings {
		if setting == "API token" {
			model.settingIndex = index
			break
		}
	}
	model.startSetting()
	if model.input.Value() != "" {
		t.Fatalf("token input exposed existing credential: %q", model.input.Value())
	}
	if err := model.saveSetting(); err != nil {
		t.Fatal(err)
	}
	if state.config.Auth.Token != "owner-secret" || state.token != "owner-secret" {
		t.Fatal("blank token preference replaced the saved credential")
	}
	model.input.SetValue("clear")
	if err := model.saveSetting(); err != nil {
		t.Fatal(err)
	}
	if state.config.Auth.Token != "" || state.token != "" {
		t.Fatal("clear token preference retained the credential")
	}
}

func TestRunRequiresServerAndRejectsUnknownCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	var output bytes.Buffer
	if err := run([]string{"status", "--config", filepath.Join(t.TempDir(), "missing.yml")}, &output, io.Discard); err == nil || !strings.Contains(err.Error(), "no server configured") {
		t.Fatalf("missing server error = %v", err)
	}
	if err := run([]string{"--server", "https://mail.example", "unknown"}, &output, io.Discard); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("unknown command error = %v", err)
	}
}

func TestCLIStateDoValidatesURLAndHTTPStatus(t *testing.T) {
	for _, invalid := range []string{"file:///tmp/mail", "https://", "https://user:pass@mail.example", "https://mail.example/path#fragment"} {
		state := cliState{server: invalid, stdout: io.Discard}
		if err := state.do(http.MethodGet, "/server/about", nil); err == nil {
			t.Errorf("do(%q) succeeded", invalid)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	state := cliState{server: server.URL, output: "json", stdout: io.Discard}
	if err := state.do(http.MethodGet, "/server/about", nil); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("non-success error = %v", err)
	}
}

func TestResolveConfigPath(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	path, err := resolveConfigPath("stage")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "apimgr", "mail", "stage.yml"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "apimgr", "mail", "stage.yaml"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err = resolveConfigPath("stage")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "apimgr", "mail", "stage.yaml"); path != want {
		t.Fatalf("existing YAML path = %q, want %q", path, want)
	}
}

func TestShellCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := shellCommand([]string{"init", "fish"}, "renamed-mail", &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "'renamed-mail' --shell completions fish | source\n" {
		t.Fatalf("output = %q", got)
	}
	if err := shellCommand([]string{"completions", "unsupported"}, "mail-cli", &out, &errOut); err == nil {
		t.Fatal("unsupported shell succeeded")
	}
}

func TestPromptCommandAndTerminalDetection(t *testing.T) {
	var output bytes.Buffer
	command, err := promptCommand(strings.NewReader("2\n"), &output)
	if err != nil || command != "about" {
		t.Fatalf("promptCommand() = %q, %v", command, err)
	}
	file, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if isTerminal(file) {
		t.Fatal("regular file detected as a terminal")
	}
	if _, err := promptCommand(strings.NewReader("unknown\n"), &output); err == nil {
		t.Fatal("unknown menu selection succeeded")
	}
}

func TestClientTUIKeyboardNavigationAndViews(t *testing.T) {
	state := &cliState{server: "https://mail.example", config: defaultCLIConfig(), color: "no"}
	model := newClientTUI(state)
	update := func(message tea.Msg) {
		next, _ := model.Update(message)
		model = next.(clientTUI)
	}
	model.height = 8
	if !strings.Contains(model.View(), "https://mail.example") {
		t.Fatal("menu view omitted configured server")
	}
	update(tea.KeyMsg{Type: tea.KeyEnd})
	update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if model.scroll == 0 {
		t.Fatal("short terminal view did not scroll to the selected entry")
	}
	update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if model.screen != "help" || !strings.Contains(model.View(), "HELP") {
		t.Fatal("help key did not show the help view")
	}
	update(tea.KeyMsg{Type: tea.KeyEscape})
	update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if model.screen != "search" || !strings.Contains(model.View(), "Filter:") {
		t.Fatal("search key did not show the filter view")
	}
	update(tea.KeyMsg{Type: tea.KeyEscape})
	if model.screen != "menu" {
		t.Fatal("escape did not return from search to the menu")
	}
	model.screen = "settings"
	if !strings.Contains(model.View(), "Preferences") {
		t.Fatal("settings view omitted its title")
	}
	if got := truncateTUI("long command", 6); got != "lon..." {
		t.Fatalf("truncateTUI() = %q", got)
	}
	model.screen = "confirm"
	if !strings.Contains(model.View(), "This action changes mail-server state") {
		t.Fatal("confirmation view omitted the state-change warning")
	}
	model.screen = "input"
	model.prompt = "Domain"
	if !strings.Contains(model.View(), "Domain:") {
		t.Fatal("input view omitted its prompt")
	}
	model.screen = "menu"
	model.cursor = len(model.actions)
	update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.screen != "settings" {
		t.Fatal("Preferences menu item did not open settings")
	}
	model.screen = "menu"
	model.cursor = len(model.actions) + 1
	update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.screen != "help" {
		t.Fatal("Help menu item did not open help")
	}
}
