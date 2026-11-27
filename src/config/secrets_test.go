package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureOperatorTokenGeneratesAndPersistsSecret(t *testing.T) {
	dir := t.TempDir()
	serverPath := filepath.Join(dir, "server.yml")
	if err := os.WriteFile(serverPath, []byte("server:\n  address: 127.0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{}
	generated, err := EnsureOperatorToken(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !generated || !validOperatorToken(cfg.Server.Token) {
		t.Fatal("first initialization did not generate a valid operator token")
	}
	secretPath := filepath.Join(dir, "secrets.yml")
	info, err := os.Stat(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secrets.yml permissions = %04o, want 0600", info.Mode().Perm())
	}
	secretData, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(secretData), cfg.Server.Token) {
		t.Fatal("secrets.yml does not contain the generated operator token")
	}
	if len(mustDecodeSecretKey(t, cfg.Server.Security.EncryptionKey)) != 32 || cfg.Server.Security.EncryptionKeyVersion != 1 {
		t.Fatal("startup did not create a version 1 AES-256 encryption key")
	}
	if !strings.Contains(string(secretData), "encryption_key_version: 1") {
		t.Fatal("secrets.yml does not contain the encryption key version")
	}
	serverData, err := os.ReadFile(serverPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serverData), "token:") {
		t.Fatal("server.yml contains the operator token key")
	}
	previous := cfg.Server.Token
	previousEncryptionKey := cfg.Server.Security.EncryptionKey
	generated, err = EnsureOperatorToken(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if generated || cfg.Server.Token != previous {
		t.Fatal("restart initialization did not preserve the saved operator token")
	}
	if cfg.Server.Security.EncryptionKey != previousEncryptionKey || cfg.Server.Security.EncryptionKeyVersion != 1 {
		t.Fatal("restart initialization did not preserve the saved encryption key")
	}
}

func mustDecodeSecretKey(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		t.Fatal("encryption key is not base64 encoded")
	}
	return decoded
}

func TestEnsureOperatorTokenMigratesConfigAndDropinValues(t *testing.T) {
	dir := t.TempDir()
	token, err := generateOperatorToken()
	if err != nil {
		t.Fatal(err)
	}
	serverPath := filepath.Join(dir, "server.yml")
	encryptionKey := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	if err := os.WriteFile(serverPath, []byte("server:\n  token: "+token+"\n  security:\n    encryption_key: "+encryptionKey+"\n    encryption_key_version: 1\n  address: 127.0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dropins := filepath.Join(dir, "yaml.d")
	if err := os.Mkdir(dropins, 0o700); err != nil {
		t.Fatal(err)
	}
	dropinPath := filepath.Join(dropins, "10-security.yml")
	if err := os.WriteFile(dropinPath, []byte("server:\n  token: "+token+"\n  security:\n    encryption_key: "+encryptionKey+"\n    encryption_key_version: 1\n  debug: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{}
	cfg.Server.Token = token
	cfg.Server.Security.EncryptionKey = encryptionKey
	cfg.Server.Security.EncryptionKeyVersion = 1
	generated, err := EnsureOperatorToken(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if generated || cfg.Server.Token != token {
		t.Fatal("operator-supplied token was not preserved")
	}
	for _, path := range []string{serverPath, dropinPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "token:") {
			t.Fatalf("operator token was not removed from %s", filepath.Base(path))
		}
	}
	loaded, err := Load(serverPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.Token != token {
		t.Fatal("secrets.yml did not override the config files")
	}
	if loaded.Server.Security.EncryptionKey != encryptionKey || loaded.Server.Security.EncryptionKeyVersion != 1 {
		t.Fatal("secrets.yml did not preserve and reload the migrated encryption key")
	}
	for _, path := range []string{serverPath, dropinPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "encryption_key:") || strings.Contains(string(data), "encryption_key_version:") {
			t.Fatalf("encryption key material was not removed from %s", filepath.Base(path))
		}
	}
}

func TestLoadSecretsRepairsFilePermissions(t *testing.T) {
	dir := t.TempDir()
	serverPath := filepath.Join(dir, "server.yml")
	if err := os.WriteFile(serverPath, []byte("server:\n  address: 127.0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secretPath := filepath.Join(dir, "secrets.yml")
	token, err := generateOperatorToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretPath, []byte("server:\n  token: "+token+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(serverPath); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secrets.yml permissions = %04o, want 0600", info.Mode().Perm())
	}
}

func TestEnsureOperatorTokenRejectsInvalidConfiguredValue(t *testing.T) {
	cfg := &Config{}
	cfg.Server.Token = "not-a-valid-token"
	if _, err := EnsureOperatorToken(t.TempDir(), cfg); err == nil {
		t.Fatal("invalid configured operator token was accepted")
	}
}

func TestEnsureOperatorTokenRejectsInvalidEncryptionKey(t *testing.T) {
	cfg := &Config{}
	cfg.Server.Security.EncryptionKey = "too-short"
	if _, err := EnsureOperatorToken(t.TempDir(), cfg); err == nil {
		t.Fatal("invalid server encryption key was accepted")
	}
}

func TestSaveDetectedSMTPPreservesOtherConfiguration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yml")
	original := "# operator comment\nserver:\n  address: 127.0.0.1\n  notifications:\n    email:\n      smtp:\n        tls: starttls\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveDetectedSMTP(path, "relay.example.test", 465); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"# operator comment", "address: 127.0.0.1", "tls: starttls", "host: relay.example.test", "port: 465"} {
		if !strings.Contains(string(data), expected) {
			t.Errorf("saved configuration missing %q:\n%s", expected, data)
		}
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.Notifications.Email.SMTP.Host != "relay.example.test" || loaded.Server.Notifications.Email.SMTP.Port != 465 {
		t.Fatalf("saved SMTP endpoint was not reloadable: %#v", loaded.Server.Notifications.Email.SMTP)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("server.yml permissions = %04o, want 0600", info.Mode().Perm())
	}
}

func TestSaveDetectedSMTPLeavesMalformedConfigurationUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yml")
	original := []byte("server: invalid\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveDetectedSMTP(path, "relay.example.test", 587); err == nil {
		t.Fatal("SMTP persistence accepted a malformed server section")
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(original) {
		t.Fatalf("malformed configuration was changed: %q", actual)
	}
}

func TestSaveModePreservesConfigurationAndRejectsInvalidSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yml")
	original := "# operator comment\nserver:\n  address: 127.0.0.1\n  mode: production\nnotifications:\n  enabled: true\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveMode(path, "development"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"# operator comment", "address: 127.0.0.1", "mode: development", "enabled: true"} {
		if !strings.Contains(string(data), expected) {
			t.Errorf("saved mode configuration missing %q:\n%s", expected, data)
		}
	}
	if err := SaveMode(path, "debug"); err == nil {
		t.Fatal("unsupported mode was accepted")
	}
	malformed := filepath.Join(t.TempDir(), "server.yml")
	if err := os.WriteFile(malformed, []byte("server: invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveMode(malformed, "production"); err == nil {
		t.Fatal("malformed server mapping was accepted")
	}
}
