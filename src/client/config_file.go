package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

func defaultCLIConfig() cliConfig {
	var cfg cliConfig
	cfg.Server.APIVersion = "v1"
	cfg.Server.Timeout = "30s"
	cfg.Server.Retry = 3
	cfg.Server.RetryDelay = "1s"
	cfg.Output.Format = "table"
	cfg.Output.Color = "auto"
	cfg.Output.Pager = "auto"
	cfg.TUI.Enabled = true
	cfg.TUI.Theme = "dark"
	cfg.TUI.Mouse = true
	cfg.TUI.Unicode = true
	cfg.Logging.Level = "warn"
	cfg.Logging.MaxSize = "10MB"
	cfg.Logging.MaxFiles = 5
	cfg.Cache.Enabled = true
	cfg.Cache.TTL = "5m"
	cfg.Cache.MaxSize = "100MB"
	cfg.Update.CheckInterval = "per_invocation"
	cfg.Update.Channel = "stable"
	cfg.Defaults.Lang = "auto"
	cfg.Defaults.Output = "table"
	cfg.Defaults.Limit = 20
	return cfg
}

func cliConfigFile(configFlag string) (string, error) {
	if configFlag == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("locating user config directory: %w", err)
		}
		return filepath.Join(configDir, "apimgr", "mail", "cli.yml"), nil
	}
	return resolveConfigPath(configFlag)
}

func saveCLIConfig(path string, cfg cliConfig) error {
	if path == "" {
		return errors.New("CLI configuration path is empty")
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode CLI configuration: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create CLI configuration directory: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("secure CLI configuration directory: %w", err)
		}
	}
	temp, err := os.CreateTemp(dir, ".cli.yml-*")
	if err != nil {
		return fmt.Errorf("create temporary CLI configuration: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("secure temporary CLI configuration: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write CLI configuration: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync CLI configuration: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close CLI configuration: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace CLI configuration: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("secure CLI configuration: %w", err)
		}
	}
	return nil
}
