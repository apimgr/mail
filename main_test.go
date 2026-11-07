package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/apimgr/mail/config"
	"github.com/apimgr/mail/paths"
)

func TestBuildEpochAndSetupToken(t *testing.T) {
	previous := BuildEpoch
	defer func() { BuildEpoch = previous }()
	for value, want := range map[string]int64{"0": 0, "123": 123, "invalid": 0} {
		BuildEpoch = value
		if got := buildEpoch(); got != want {
			t.Errorf("buildEpoch(%q) = %d, want %d", value, got, want)
		}
	}
	token, err := generateSetupToken()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(token) {
		t.Fatalf("setup token has invalid format: %q", token)
	}
}

func TestConfigSetupAndPIDPaths(t *testing.T) {
	root := t.TempDir()
	appPaths := &paths.Paths{Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"), DBDir: filepath.Join(root, "db")}
	cfg := createDefaultConfig(appPaths)
	if cfg.Server.Address != "0.0.0.0" || cfg.Server.Port != 64500 || cfg.Database.Name != filepath.Join(appPaths.DBDir, "server.db") {
		t.Fatalf("default config = %#v", cfg)
	}
	if got := getPIDPath(appPaths, true); got != "/var/run/apimgr/mail.pid" {
		t.Fatalf("root PID path = %q", got)
	}
	if got := getPIDPath(appPaths, false); got != filepath.Join(appPaths.Data, "mail.pid") {
		t.Fatalf("user PID path = %q", got)
	}
	if err := os.MkdirAll(appPaths.Config, 0700); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(appPaths.Config, "server.yml")
	cfg.Server.SetupToken = "setup-token"
	if err := saveConfig(cfg, configFile); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("config permissions = %o, want 600", info.Mode().Perm())
	}
	loaded, err := loadConfig(appPaths)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.SetupToken != cfg.Server.SetupToken || loaded.Database.Name != cfg.Database.Name {
		t.Fatalf("loaded config = %#v", loaded)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	fn()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = previous
	data, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestHelpAndVersionOutput(t *testing.T) {
	for name, fn := range map[string]func(){
		"help":        printHelp,
		"version":     printVersion,
		"shell":       printShellHelp,
		"service":     printServiceHelp,
		"maintenance": printMaintenanceHelp,
		"update":      printUpdateHelp,
	} {
		output := captureStdout(t, fn)
		if output == "" {
			t.Errorf("%s output is empty", name)
		}
	}
	output := captureStdout(t, func() { displayStartupBanner(createDefaultConfig(&paths.Paths{})) })
	if output == "" {
		t.Fatal("startup banner output is empty")
	}
	output = captureStdout(t, func() { displaySetupToken("test-token", &config.Config{}) })
	if output == "" {
		t.Fatal("setup token output is empty")
	}
}
