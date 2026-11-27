package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPeekStartupMergesOnlyOrderedStartupKeys(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "server.yml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: 8080\n  address: 127.0.0.1\n  daemonize: true\n  logs:\n    defaults:\n      rotate: daily,100MB\n    access:\n      format: json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dropins := filepath.Join(root, "yaml.d")
	if err := os.Mkdir(dropins, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"10-start.yml":   "server:\n  port: 8081\n  pidfile: false\n",
		"20-start.yaml":  "server:\n  port: 8082\n  address: '[::1]'\n  logs:\n    security:\n      format: cef\n",
		".ignored.yml":   "server:\n  port: 9999\n",
		"30-invalid.yml": "server: [\n",
	} {
		if err := os.WriteFile(filepath.Join(dropins, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "secrets.yml"), []byte("server:\n  port: 9997\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	settings := PeekStartup(configPath)
	if settings.Port != 8082 || settings.Address != "[::1]" || !settings.Daemonize || settings.PIDFile {
		t.Fatalf("PeekStartup() = %#v", settings)
	}
	if settings.Logs.Access.Format != "json" || settings.Logs.Security.Format != "cef" || settings.Logs.Defaults.Rotate != "daily,100MB" {
		t.Fatalf("PeekStartup() log settings = %+v", settings.Logs)
	}
}

func TestPeekStartupRejectsInvalidAndDuplicateValues(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "server.yml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: 8080\n  address: 127.0.0.1\n  daemonize: false\n  pidfile: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dropins := filepath.Join(filepath.Dir(configPath), "yaml.d")
	if err := os.Mkdir(dropins, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dropins, "10-invalid.yml"), []byte("server:\n  port: 80\n  address: ''\n  daemonize: maybe\n  pidfile: null\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dropins, "20-duplicate.yml"), []byte("server:\n  port: 8081\n  port: 8082\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	settings := PeekStartup(configPath)
	if settings.Port != 80 || settings.Address != "127.0.0.1" || settings.Daemonize || !settings.PIDFile {
		t.Fatalf("invalid early values changed startup settings: %#v", settings)
	}
}
