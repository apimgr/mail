package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/apimgr/mail/src/common/theme"
	projectconfig "github.com/apimgr/mail/src/config"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type clientAction struct {
	label, method, route string
	operation            string
	args                 []string
	body, destructive    bool
}

func clientActions() []clientAction {
	return []clientAction{
		{label: "users list", method: "GET", route: "/users"},
		{label: "users get", method: "GET", route: "/users/%s", args: []string{"User ID"}},
		{label: "users create", method: "POST", route: "/users", body: true, destructive: true},
		{label: "users update", method: "PUT", route: "/users/%s", args: []string{"User ID"}, body: true, destructive: true},
		{label: "users delete", method: "DELETE", route: "/users/%s", args: []string{"User ID"}, destructive: true},
		{label: "users password", method: "POST", route: "/users/%s/password", args: []string{"User ID"}, body: true, destructive: true},
		{label: "users import", method: "POST", route: "/users/import", body: true, destructive: true},
		{label: "domains list", method: "GET", route: "/domains"},
		{label: "domains get", method: "GET", route: "/domains/%s", args: []string{"Domain"}},
		{label: "domains create", method: "POST", route: "/domains", body: true, destructive: true},
		{label: "domains update", method: "PUT", route: "/domains/%s", args: []string{"Domain"}, body: true, destructive: true},
		{label: "domains delete", method: "DELETE", route: "/domains/%s", args: []string{"Domain"}, destructive: true},
		{label: "domains DNS", method: "GET", route: "/domains/%s/dns", args: []string{"Domain"}},
		{label: "domains DKIM", method: "GET", route: "/domains/%s/dkim", args: []string{"Domain"}},
		{label: "domains generate DKIM", method: "POST", route: "/domains/%s/dkim/generate", args: []string{"Domain"}, destructive: true},
		{label: "aliases list", method: "GET", route: "/aliases"},
		{label: "aliases create", method: "POST", route: "/aliases", body: true, destructive: true},
		{label: "aliases delete", method: "DELETE", route: "/aliases/%s", args: []string{"Alias ID"}, destructive: true},
		{label: "forwards list", method: "GET", route: "/forwards"},
		{label: "forwards create", method: "POST", route: "/forwards", body: true, destructive: true},
		{label: "forwards delete", method: "DELETE", route: "/forwards/%s", args: []string{"Forward ID"}, destructive: true},
		{label: "queue list", method: "GET", route: "/queue"},
		{label: "queue get", method: "GET", route: "/queue/%s", args: []string{"Queue ID"}},
		{label: "queue flush", method: "POST", route: "/queue/%s/flush", args: []string{"Queue ID"}, destructive: true},
		{label: "queue hold", method: "POST", route: "/queue/%s/hold", args: []string{"Queue ID"}, destructive: true},
		{label: "queue release", method: "POST", route: "/queue/%s/release", args: []string{"Queue ID"}, destructive: true},
		{label: "queue delete", method: "DELETE", route: "/queue/%s", args: []string{"Queue ID"}, destructive: true},
		{label: "queue flush all", method: "POST", route: "/queue/flush-all", destructive: true},
		{label: "config get", method: "GET", route: "/config/%s", args: []string{"Component"}},
		{label: "config set", method: "PUT", route: "/config/%s", args: []string{"Component"}, body: true, destructive: true},
		{label: "config validate", method: "POST", route: "/config/%s/validate", args: []string{"Component"}, body: true},
		{label: "config templates", method: "GET", route: "/config/%s/templates", args: []string{"Component"}},
		{label: "config backup", method: "POST", route: "/config/%s/backup", args: []string{"Component"}},
		{label: "config restore", method: "POST", route: "/config/%s/restore", args: []string{"Component"}, body: true, destructive: true},
		{label: "stats delivery", method: "GET", route: "/stats/delivery"},
		{label: "stats spam", method: "GET", route: "/stats/spam"},
		{label: "stats queue", method: "GET", route: "/stats/queue"},
		{label: "logs", method: "GET", route: "/logs/%s", args: []string{"Component"}},
		{label: "services list", method: "GET", route: "/services"},
		{label: "services get", method: "GET", route: "/services/%s", args: []string{"Service"}},
		{label: "services start", method: "POST", route: "/services/%s/operations", operation: "start", args: []string{"Service"}, destructive: true},
		{label: "services stop", method: "POST", route: "/services/%s/operations", operation: "stop", args: []string{"Service"}, destructive: true},
		{label: "services restart", method: "POST", route: "/services/%s/operations", operation: "restart", args: []string{"Service"}, destructive: true},
		{label: "services reload", method: "POST", route: "/services/%s/operations", operation: "reload", args: []string{"Service"}, destructive: true},
		{label: "health", method: "GET", route: "/health"},
		{label: "server status", method: "GET", route: "/server/healthz"},
		{label: "server about", method: "GET", route: "/server/about"},
	}
}

type clientTUI struct {
	state                          *cliState
	setup                          bool
	serverInput, tokenInput, input textinput.Model
	setupStep                      int
	menu, settings                 []string
	actions                        []clientAction
	filtered                       []int
	cursor, scroll, width, height  int
	screen, prompt                 string
	entryIndex                     int
	values                         []string
	entryField                     int
	search                         textinput.Model
	settingIndex                   int
	result, problem                string
	busy                           bool
}

type tuiRequestResult struct {
	output string
	err    error
}
type tuiSetupResult struct{ err error }

func runClientTUI(ctx context.Context, state *cliState) error {
	model := newClientTUI(state)
	program := tea.NewProgram(model, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	result, err := program.Run()
	if err != nil {
		return fmt.Errorf("run CLI TUI: %w", err)
	}
	if final, ok := result.(clientTUI); ok && final.problem != "" && final.setup {
		return errors.New(final.problem)
	}
	return nil
}

func newClientTUI(state *cliState) clientTUI {
	urlInput := textinput.New()
	urlInput.Placeholder = "https://mail.example"
	urlInput.CharLimit = 2048
	urlInput.Width = 64
	tokenInput := textinput.New()
	tokenInput.Placeholder = "Optional owner or operator token"
	tokenInput.CharLimit = 512
	tokenInput.Width = 64
	tokenInput.EchoMode = textinput.EchoPassword
	input := textinput.New()
	input.CharLimit = 4096
	input.Width = 64
	search := textinput.New()
	search.Placeholder = "Filter commands"
	search.CharLimit = 128
	search.Width = 64
	setup := strings.TrimSpace(state.server) == ""
	if setup {
		urlInput.Focus()
	}
	menu := make([]string, 0, len(clientActions())+2)
	for _, action := range clientActions() {
		menu = append(menu, action.label)
	}
	menu = append(menu, "Preferences", "Help")
	settings := []string{"Server URL", "API token", "Token file", "Output format", "Color output", "Pager", "Language", "Display mode", "TUI enabled", "TUI theme", "TUI mouse", "TUI unicode", "Log level", "Log file", "Log max size", "Log file count", "Cache enabled", "Cache TTL", "Cache max size", "Request timeout", "Retry attempts", "Retry delay", "Page limit", "Automatic updates", "Update check interval", "Update channel", "Back"}
	model := clientTUI{state: state, setup: setup, serverInput: urlInput, tokenInput: tokenInput, input: input, menu: menu, settings: settings, actions: clientActions(), screen: "menu", width: 80, height: 24, search: search}
	model.rebuildMenu()
	return model
}

func (m clientTUI) Init() tea.Cmd { return textinput.Blink }

func (m clientTUI) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tuiSetupResult:
		m.busy = false
		if msg.err != nil {
			m.problem = msg.err.Error()
			return m, nil
		}
		m.state.server = strings.TrimSpace(m.serverInput.Value())
		m.state.token = strings.TrimSpace(m.tokenInput.Value())
		if m.state.config.Server.Primary == "" || !validServerURL(m.state.config.Server.Primary) {
			m.state.config.Server.Primary = m.state.server
		}
		if m.state.token != "" && m.state.config.Auth.Token == "" {
			m.state.config.Auth.Token = m.state.token
		}
		if err := saveCLIConfig(m.state.configFile, m.state.config); err != nil {
			m.problem = err.Error()
			return m, nil
		}
		m.setup = false
		m.screen = "menu"
		m.problem = "Connected. Choose a mail operation."
		m.rebuildMenu()
	case tuiRequestResult:
		m.busy = false
		if msg.err != nil {
			m.problem = msg.err.Error()
		} else {
			m.result = msg.output
			m.problem = ""
		}
		m.screen = "result"
	case tea.KeyMsg:
		if m.busy {
			return m, nil
		}
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.setup {
			cmd := m.updateSetup(msg)
			return m, cmd
		}
		switch m.screen {
		case "menu":
			switch key {
			case "q":
				return m, tea.Quit
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.filtered)-1 {
					m.cursor++
				}
			case "g":
				m.cursor = 0
			case "G":
				if len(m.filtered) > 0 {
					m.cursor = len(m.filtered) - 1
				}
			case "?":
				m.screen = "help"
			case "/":
				m.screen = "search"
				m.search.SetValue("")
				m.search.Focus()
			case "enter":
				cmd := m.selectMenu()
				return m, cmd
			}
			m.ensureVisible()
		case "search":
			if key == "esc" {
				m.screen = "menu"
				m.search.Blur()
				m.cursor = 0
				m.rebuildMenu()
				return m, nil
			}
			if key == "enter" {
				m.screen = "menu"
				m.search.Blur()
				m.cursor = 0
				m.rebuildMenu()
				return m, nil
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			m.rebuildMenu()
			return m, cmd
		case "settings":
			switch key {
			case "esc", "q":
				m.screen = "menu"
			case "up", "k":
				if m.settingIndex > 0 {
					m.settingIndex--
				}
			case "down", "j":
				if m.settingIndex < len(m.settings)-1 {
					m.settingIndex++
				}
			case "enter":
				cmd := m.startSetting()
				return m, cmd
			}
		case "input":
			if key == "esc" {
				m.screen = "menu"
				return m, nil
			}
			if key == "enter" {
				next, cmd := m.submitInput()
				return next, cmd
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		case "confirm":
			if key == "y" || key == "Y" {
				cmd := m.performAction(true)
				return m, cmd
			}
			if key == "n" || key == "N" || key == "esc" {
				m.screen = "menu"
				m.problem = "Operation cancelled."
			}
		case "result", "help":
			if key == "esc" || key == "enter" || key == "q" {
				m.screen = "menu"
				m.result = ""
			}
		}
	}
	return m, nil
}

func (m *clientTUI) updateSetup(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	if key == "ctrl+c" {
		return tea.Quit
	}
	if key == "esc" {
		m.problem = "Enter a server URL to continue."
		return nil
	}
	if key == "enter" {
		if m.setupStep == 0 {
			server := strings.TrimSpace(m.serverInput.Value())
			if !validServerURL(server) {
				m.problem = "Enter an absolute http:// or https:// server URL."
				return nil
			}
			m.setupStep = 1
			m.problem = ""
			m.tokenInput.Focus()
			m.serverInput.Blur()
			return nil
		}
		server := strings.TrimSpace(m.serverInput.Value())
		m.busy = true
		m.problem = "Testing server connection…"
		return func() tea.Msg {
			return tuiSetupResult{err: testSetupConnection(server, strings.TrimSpace(m.tokenInput.Value()), m.state.config.Server.APIVersion)}
		}
	}
	if m.setupStep == 0 {
		var cmd tea.Cmd
		m.serverInput, cmd = m.serverInput.Update(msg)
		return cmd
	}
	var cmd tea.Cmd
	m.tokenInput, cmd = m.tokenInput.Update(msg)
	return cmd
}

func testSetupConnection(server, token, apiVersion string) error {
	if !validServerURL(server) {
		return errors.New("invalid server URL")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	request, err := http.NewRequest(http.MethodGet, strings.TrimRight(server, "/")+apiBasePathFor(apiVersion)+"/server/about", nil)
	if err != nil {
		return fmt.Errorf("create connection request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", ProjectName+"-cli/"+Version)
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("cannot connect to server: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read server response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("server connection check returned %s", response.Status)
	}
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("server returned invalid JSON: %w", err)
	}
	if token == "" {
		return nil
	}
	check, err := http.NewRequest(http.MethodGet, strings.TrimRight(server, "/")+apiBasePathFor(apiVersion)+"/server/auth/check", nil)
	if err != nil {
		return err
	}
	check.Header.Set("Accept", "application/json")
	check.Header.Set("Authorization", "Bearer "+token)
	check.Header.Set("User-Agent", ProjectName+"-cli/"+Version)
	verified, err := client.Do(check)
	if err != nil {
		return fmt.Errorf("validate API token: %w", err)
	}
	defer func() { _ = verified.Body.Close() }()
	if verified.StatusCode == http.StatusUnauthorized || verified.StatusCode == http.StatusForbidden {
		return errors.New("API token is invalid or unauthorized")
	}
	if verified.StatusCode < 200 || verified.StatusCode >= 300 {
		return fmt.Errorf("token check returned %s", verified.Status)
	}
	return nil
}

func validServerURL(value string) bool {
	u, err := url.ParseRequestURI(strings.TrimSpace(value))
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && u.Fragment == ""
}

func (m *clientTUI) selectMenu() tea.Cmd {
	if len(m.filtered) == 0 {
		return nil
	}
	item := m.menu[m.filtered[m.cursor]]
	if item == "Help" {
		m.screen = "help"
		return nil
	}
	if item == "Preferences" {
		m.screen = "settings"
		m.settingIndex = 0
		return nil
	}
	for i, action := range m.actions {
		if action.label == item {
			m.entryIndex = i
			m.values = nil
			m.entryField = 0
			if len(action.args) > 0 || action.body {
				m.beginInput()
				return nil
			}
			if action.destructive {
				m.screen = "confirm"
				return nil
			}
			return m.performAction(false)
		}
	}
	return nil
}

func (m *clientTUI) beginInput() {
	action := m.actions[m.entryIndex]
	m.screen = "input"
	m.entryField = 0
	if len(action.args) > 0 {
		m.prompt = action.args[0]
	} else {
		m.prompt = "JSON request body"
	}
	m.input.SetValue("")
	m.input.Focus()
}

func (m *clientTUI) submitInput() (tea.Model, tea.Cmd) {
	if m.entryIndex < 0 {
		if err := m.saveSetting(); err != nil {
			m.problem = err.Error()
			return m, nil
		}
		m.problem = "Preference saved."
		m.screen = "settings"
		m.input.Blur()
		return m, nil
	}
	action := m.actions[m.entryIndex]
	value := strings.TrimSpace(m.input.Value())
	if m.entryField < len(action.args) && value == "" {
		m.problem = m.prompt + " is required."
		return m, nil
	}
	if m.entryField >= len(action.args) && action.body && !json.Valid([]byte(value)) {
		m.problem = "Enter a valid JSON object for the request body."
		return m, nil
	}
	m.values = append(m.values, value)
	m.problem = ""
	fields := len(action.args)
	if action.body {
		fields++
	}
	if len(m.values) < fields {
		m.entryField = len(m.values)
		if m.entryField < len(action.args) {
			m.prompt = action.args[m.entryField]
		} else {
			m.prompt = "JSON request body"
		}
		m.input.SetValue("")
		return m, nil
	}
	m.input.Blur()
	if action.destructive {
		m.screen = "confirm"
		return m, nil
	}
	cmd := m.performAction(false)
	return m, cmd
}

func (m *clientTUI) performAction(force bool) tea.Cmd {
	action := m.actions[m.entryIndex]
	if action.destructive && !force {
		m.screen = "confirm"
		return nil
	}
	route := action.route
	for _, value := range m.values[:min(len(m.values), len(action.args))] {
		route = strings.Replace(route, "%s", url.PathEscape(value), 1)
	}
	var body io.Reader
	if action.operation != "" {
		body = strings.NewReader(`{"operation":"` + action.operation + `"}`)
	} else if action.body && len(m.values) > len(action.args) {
		body = bytes.NewBufferString(m.values[len(action.args)])
	}
	m.busy = true
	m.screen = "result"
	m.problem = "Working…"
	state := m.state
	return func() tea.Msg {
		var output bytes.Buffer
		original := state.stdout
		state.stdout = &output
		err := state.do(action.method, route, body)
		state.stdout = original
		return tuiRequestResult{output: output.String(), err: err}
	}
}

func (m *clientTUI) startSetting() tea.Cmd {
	if m.settings[m.settingIndex] == "Back" {
		m.screen = "menu"
		return nil
	}
	m.screen = "input"
	m.entryIndex = -1
	m.prompt = m.settings[m.settingIndex]
	value := m.settingValue(m.prompt)
	if m.prompt == "API token" {
		value = ""
		m.input.Placeholder = "Leave blank to keep current token; type clear to remove it"
	} else {
		m.input.Placeholder = ""
	}
	m.input.SetValue(value)
	m.input.Focus()
	if m.prompt == "API token" {
		m.input.EchoMode = textinput.EchoPassword
	} else {
		m.input.EchoMode = textinput.EchoNormal
	}
	return nil
}

func (m *clientTUI) saveSetting() error {
	value := strings.TrimSpace(m.input.Value())
	switch m.prompt {
	case "Server URL":
		if !validServerURL(value) {
			return errors.New("invalid server URL")
		}
		m.state.config.Server.Primary = value
		m.state.server = value
	case "API token":
		if value == "" {
			return nil
		}
		if strings.EqualFold(value, "clear") {
			m.state.config.Auth.Token = ""
			m.state.token = ""
		} else {
			m.state.config.Auth.Token = value
			m.state.token = value
		}
	case "Token file":
		m.state.config.Auth.TokenFile = value
	case "Output format":
		if value != "table" && value != "json" && value != "yaml" && value != "plain" && value != "csv" {
			return errors.New("format must be table, json, yaml, plain, or csv")
		}
		m.state.config.Output.Format = value
		m.state.output = value
	case "Color output":
		if value != "auto" && value != "yes" && value != "no" {
			return errors.New("color must be auto, yes, or no")
		}
		m.state.config.Output.Color = value
		m.state.color = value
	case "Pager":
		if value != "auto" && value != "always" && value != "never" {
			return errors.New("pager must be auto, always, or never")
		}
		m.state.config.Output.Pager = value
	case "Language":
		m.state.config.Defaults.Lang = value
		m.state.lang = value
	case "Display mode":
		if value != "auto" && value != "gui" && value != "tui" {
			return errors.New("display mode must be auto, gui, or tui")
		}
		m.state.config.Display.Mode = value
	case "TUI enabled":
		parsed, err := projectconfig.ParseBool(value, m.state.config.TUI.Enabled)
		if err != nil {
			return err
		}
		m.state.config.TUI.Enabled = parsed
	case "TUI theme":
		if value != "dark" && value != "light" && value != "system" {
			return errors.New("TUI theme must be dark, light, or system")
		}
		m.state.config.TUI.Theme = value
	case "TUI mouse":
		parsed, err := projectconfig.ParseBool(value, m.state.config.TUI.Mouse)
		if err != nil {
			return err
		}
		m.state.config.TUI.Mouse = parsed
	case "TUI unicode":
		parsed, err := projectconfig.ParseBool(value, m.state.config.TUI.Unicode)
		if err != nil {
			return err
		}
		m.state.config.TUI.Unicode = parsed
	case "Log level":
		if value != "debug" && value != "info" && value != "warn" && value != "error" {
			return errors.New("log level must be debug, info, warn, or error")
		}
		m.state.config.Logging.Level = value
	case "Log file":
		m.state.config.Logging.File = value
	case "Log max size":
		if value == "" {
			return errors.New("log max size is required")
		}
		m.state.config.Logging.MaxSize = value
	case "Log file count":
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			return errors.New("log file count must be positive")
		}
		m.state.config.Logging.MaxFiles = parsed
	case "Cache enabled":
		parsed, err := projectconfig.ParseBool(value, m.state.config.Cache.Enabled)
		if err != nil {
			return err
		}
		m.state.config.Cache.Enabled = parsed
	case "Cache TTL":
		if _, err := time.ParseDuration(value); err != nil {
			return err
		}
		m.state.config.Cache.TTL = value
	case "Cache max size":
		if value == "" {
			return errors.New("cache max size is required")
		}
		m.state.config.Cache.MaxSize = value
	case "Request timeout":
		if _, err := time.ParseDuration(value); err != nil {
			return err
		}
		m.state.config.Server.Timeout = value
	case "Retry attempts":
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return errors.New("retry attempts must be zero or positive")
		}
		m.state.config.Server.Retry = parsed
	case "Retry delay":
		if _, err := time.ParseDuration(value); err != nil {
			return err
		}
		m.state.config.Server.RetryDelay = value
	case "Page limit":
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			return errors.New("page limit must be positive")
		}
		m.state.config.Defaults.Limit = parsed
	case "Automatic updates":
		parsed, err := projectconfig.ParseBool(value, m.state.config.Update.Auto)
		if err != nil {
			return err
		}
		m.state.config.Update.Auto = parsed
	case "Update check interval":
		if value != "per_invocation" {
			return errors.New("update interval must be per_invocation")
		}
		m.state.config.Update.CheckInterval = value
	case "Update channel":
		if value != "stable" && value != "beta" && value != "daily" {
			return errors.New("update channel must be stable, beta, or daily")
		}
		m.state.config.Update.Channel = value
	}
	return saveCLIConfig(m.state.configFile, m.state.config)
}

func (m clientTUI) settingValue(name string) string {
	c := m.state.config
	switch name {
	case "Server URL":
		return c.Server.Primary
	case "API token":
		return ""
	case "Token file":
		return c.Auth.TokenFile
	case "Output format":
		return c.Output.Format
	case "Color output":
		return c.Output.Color
	case "Pager":
		return c.Output.Pager
	case "Language":
		return c.Defaults.Lang
	case "Display mode":
		return c.Display.Mode
	case "TUI enabled":
		return fmt.Sprint(c.TUI.Enabled)
	case "TUI theme":
		return c.TUI.Theme
	case "TUI mouse":
		return fmt.Sprint(c.TUI.Mouse)
	case "TUI unicode":
		return fmt.Sprint(c.TUI.Unicode)
	case "Log level":
		return c.Logging.Level
	case "Log file":
		return c.Logging.File
	case "Log max size":
		return c.Logging.MaxSize
	case "Log file count":
		return fmt.Sprint(c.Logging.MaxFiles)
	case "Cache enabled":
		return fmt.Sprint(c.Cache.Enabled)
	case "Cache TTL":
		return c.Cache.TTL
	case "Cache max size":
		return c.Cache.MaxSize
	case "Request timeout":
		return c.Server.Timeout
	case "Retry attempts":
		return fmt.Sprint(c.Server.Retry)
	case "Retry delay":
		return c.Server.RetryDelay
	case "Page limit":
		return fmt.Sprint(c.Defaults.Limit)
	case "Automatic updates":
		return fmt.Sprint(c.Update.Auto)
	case "Update check interval":
		return c.Update.CheckInterval
	case "Update channel":
		return c.Update.Channel
	default:
		return ""
	}
}

func (m clientTUI) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(m.primaryColor())
	title := titleStyle.Render("MAIL CLI")
	if m.busy {
		return title + "\n\n" + m.problem + "\n"
	}
	if m.setup {
		if m.setupStep == 0 {
			return title + " SETUP\n\nNo server is configured. Enter the server URL, then press Enter.\n\nServer URL: " + m.serverInput.View() + "\n\nEnter continue · Ctrl+C quit\n"
		}
		return title + " SETUP\n\nServer URL: " + m.serverInput.Value() + "\nAPI token (optional): " + m.tokenInput.View() + "\n\nEnter to test connection · Esc to cancel\n" + m.problem + "\n"
	}
	switch m.screen {
	case "menu", "search":
		lines := []string{title + "  " + m.state.server, "↑/↓ or j/k move · Enter open · / filter · ? help · q quit"}
		if m.problem != "" {
			lines = append(lines, m.problem)
		}
		if m.screen == "search" {
			lines = append(lines, "Filter: "+m.search.View())
		}
		start := m.scroll
		end := min(len(m.filtered), start+m.visibleRows())
		for i := start; i < end; i++ {
			index := m.filtered[i]
			marker := "  "
			if i == m.cursor {
				marker = "> "
			}
			line := truncateTUI(marker+m.menu[index], m.width)
			if i == m.cursor && m.colorEnabled() {
				line = lipgloss.NewStyle().Foreground(m.primaryColor()).Bold(true).Render(line)
			}
			lines = append(lines, line)
		}
		lines = append(lines, fmt.Sprintf("%d/%d", m.cursor+1, len(m.filtered)))
		return strings.Join(lines, "\n")
	case "settings":
		lines := []string{title + "  Preferences", "Select a preference to edit · Esc back"}
		visible := m.height - 4
		if visible < 1 {
			visible = 1
		}
		start := 0
		if m.settingIndex >= visible {
			start = m.settingIndex - visible + 1
		}
		end := min(len(m.settings), start+visible)
		for i := start; i < end; i++ {
			item := m.settings[i]
			marker := "  "
			if i == m.settingIndex {
				marker = "> "
			}
			lines = append(lines, truncateTUI(marker+item+": "+m.settingValue(item), m.width))
		}
		return strings.Join(lines, "\n")
	case "input":
		return title + "\n\n" + m.prompt + ":\n" + m.input.View() + "\n\nEnter save/continue · Esc cancel\n" + m.problem
	case "confirm":
		return title + "\n\nThis action changes mail-server state. Continue?\n\n[y] confirm   [n] cancel\n"
	case "result":
		return title + "  " + m.actions[m.entryIndex].label + "\n\n" + m.result + "\n" + m.problem + "\nEnter/Esc back\n"
	case "help":
		return title + " HELP\n\nThis menu mirrors the API command tree. Select an operation, enter the requested values, and review the server response. Search with /; navigate with arrows or j/k; press q to quit.\n\nEsc or Enter back\n"
	}
	return title
}

func (m clientTUI) primaryColor() lipgloss.TerminalColor {
	if m.state.config.TUI.Theme == "system" {
		light := theme.TerminalPaletteLight
		dark := theme.TerminalPaletteDark
		return lipgloss.AdaptiveColor{Light: light.Primary, Dark: dark.Primary}
	}
	palette := theme.TerminalPaletteFor(m.state.config.TUI.Theme)
	return lipgloss.Color(palette.Primary)
}

func (m *clientTUI) rebuildMenu() {
	query := strings.ToLower(strings.TrimSpace(m.search.Value()))
	m.filtered = m.filtered[:0]
	for i, item := range m.menu {
		if query == "" || strings.Contains(strings.ToLower(item), query) {
			m.filtered = append(m.filtered, i)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
	m.ensureVisible()
}

func (m *clientTUI) ensureVisible() {
	rows := m.visibleRows()
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	}
	if m.cursor >= m.scroll+rows {
		m.scroll = m.cursor - rows + 1
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

func (m clientTUI) visibleRows() int {
	rows := m.height - 6
	if rows < 1 {
		return 1
	}
	return rows
}

func (m clientTUI) colorEnabled() bool {
	if m.state.color == "yes" {
		return true
	}
	if m.state.color == "no" || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return isTerminal(os.Stdout)
}

func truncateTUI(value string, width int) string {
	if width < 1 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	runes := []rune(value)
	for len(runes) > 0 && lipgloss.Width(string(runes))+3 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "..."
}
