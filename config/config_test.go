package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadAppliesDefaultsAndExpandsEnvironment(t *testing.T) {
	t.Setenv("MAIL_TEST_DB", filepath.Join(t.TempDir(), "server.db"))
	path := filepath.Join(t.TempDir(), "server.yml")
	if err := os.WriteFile(path, []byte("database:\n  driver: sqlite\n  name: ${MAIL_TEST_DB}\nserver:\n  admin_path: /control/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Name != os.Getenv("MAIL_TEST_DB") || cfg.Server.AdminPath != "control" {
		t.Fatalf("loaded config = %#v", cfg)
	}
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 || cfg.Database.Pool.MaxOpen != 25 {
		t.Fatalf("defaults not applied: %#v", cfg)
	}
	if Get() != cfg || GetConfigPath() != path {
		t.Fatal("global config state was not updated")
	}
}

func TestLoadRejectsMalformedAndMissingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.yml")
	if err := os.WriteFile(path, []byte("database: ["), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "parse config YAML") {
		t.Fatalf("Load malformed config error = %v", err)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yml")); err == nil {
		t.Fatal("Load missing config succeeded")
	}
}

func TestDatabaseDSNAndDuration(t *testing.T) {
	for _, tc := range []struct {
		db   DatabaseConfig
		want string
	}{
		{DatabaseConfig{Driver: "sqlite", URL: "file:test.db"}, "file:test.db"},
		{DatabaseConfig{Driver: "sqlite", Name: "server.db"}, "server.db"},
		{DatabaseConfig{Driver: "libsql", URL: "libsql://db", Token: "secret"}, "libsql://db?authToken=secret"},
		{DatabaseConfig{Driver: "libsql", URL: "libsql://db?mode=rw", Token: "secret"}, "libsql://db?mode=rw&authToken=secret"},
		{DatabaseConfig{Driver: "pgx", Host: "db", Port: 5432, Username: "u", Password: "p", Name: "n"}, "host=db port=5432 user=u password=p dbname=n sslmode=prefer"},
		{DatabaseConfig{Driver: "mysql", Host: "db", Port: 3306, Username: "u", Password: "p", Name: "n"}, "u:p@tcp(db:3306)/n?charset=utf8mb4&parseTime=True&loc=Local"},
	} {
		if got := tc.db.DSN(); got != tc.want {
			t.Errorf("DSN() = %q, want %q", got, tc.want)
		}
	}
	if got, err := ParseDuration("5m"); err != nil || got != 5*time.Minute {
		t.Fatalf("ParseDuration = %v, %v", got, err)
	}
	if _, err := ParseDuration("bad"); err == nil {
		t.Fatal("ParseDuration accepted an invalid duration")
	}
}
