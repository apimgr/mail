package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// StartupSettings contains only the values needed before a privileged bind.
type StartupSettings struct {
	Port      int
	Address   string
	Daemonize bool
	PIDFile   bool
	Logs      LogsConfig
}

// PeekStartup reads only startup keys from server.yml and ordered YAML drop-ins.
// It intentionally does not read secrets.yml or validate unrelated settings.
func PeekStartup(path string) StartupSettings {
	settings := StartupSettings{Address: "[::]", PIDFile: true, Logs: defaultConfig().Server.Logs}
	files := []string{path}
	dropinDir := filepath.Join(filepath.Dir(path), "yaml.d")
	entries, err := os.ReadDir(dropinDir)
	if err != nil && !os.IsNotExist(err) {
		slog.Warn("cannot read startup config drop-ins", "error", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || entry.IsDir() || (!strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml")) {
			continue
		}
		file := filepath.Join(dropinDir, name)
		if entry.Type()&os.ModeSymlink != 0 {
			resolved, resolveErr := filepath.EvalSymlinks(file)
			directory, directoryErr := filepath.EvalSymlinks(dropinDir)
			if resolveErr != nil || directoryErr != nil || !withinDirectory(directory, resolved) {
				slog.Warn("startup config drop-in symlink escapes its directory; skipping", "file", file)
				continue
			}
		}
		info, statErr := os.Stat(file)
		if statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, file)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			if !os.IsNotExist(err) {
				slog.Warn("cannot read startup config file", "file", file, "error", err)
			}
			continue
		}
		var root yaml.Node
		if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(data))), &root); err != nil {
			slog.Warn("invalid startup config file; skipping early values", "file", file)
			continue
		}
		if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
			continue
		}
		serverNode := mappingValue(root.Content[0], "server")
		if serverNode == nil || serverNode.Kind != yaml.MappingNode {
			continue
		}
		applyStartupValue(&settings, serverNode, file)
	}
	return settings
}

func applyStartupValue(settings *StartupSettings, server *yaml.Node, file string) {
	if value, duplicate := uniqueStartupMappingValue(server.Content, "logs"); duplicate {
		slog.Warn("duplicate startup config key; retaining prior value", "file", file, "key", "server.logs")
	} else if value != nil {
		if value.Kind != yaml.MappingNode || value.Decode(&settings.Logs) != nil {
			slog.Warn("invalid startup config value; retaining prior value", "file", file, "key", "server.logs")
		} else {
			validateLogConfig(&settings.Logs)
		}
	}
	if value, duplicate := uniqueStartupMappingValue(server.Content, "port"); duplicate {
		slog.Warn("duplicate startup config key; retaining prior value", "file", file, "key", "server.port")
	} else if value != nil {
		port, err := strconv.Atoi(strings.TrimSpace(value.Value))
		if value.Kind != yaml.ScalarNode || err != nil || port < 1 || port > 65535 {
			slog.Warn("invalid startup config value; retaining prior value", "file", file, "key", "server.port")
		} else {
			settings.Port = port
		}
	}
	if value, duplicate := uniqueStartupMappingValue(server.Content, "address"); duplicate {
		slog.Warn("duplicate startup config key; retaining prior value", "file", file, "key", "server.address")
	} else if value != nil {
		if value.Kind != yaml.ScalarNode || strings.TrimSpace(value.Value) == "" {
			slog.Warn("invalid startup config value; retaining prior value", "file", file, "key", "server.address")
		} else {
			settings.Address = strings.TrimSpace(value.Value)
		}
	}
	if value, duplicate := uniqueStartupMappingValue(server.Content, "daemonize"); duplicate {
		slog.Warn("duplicate startup config key; retaining prior value", "file", file, "key", "server.daemonize")
	} else if value != nil {
		parsed, err := strconv.ParseBool(strings.TrimSpace(value.Value))
		if value.Kind != yaml.ScalarNode || err != nil {
			slog.Warn("invalid startup config value; retaining prior value", "file", file, "key", "server.daemonize")
		} else {
			settings.Daemonize = parsed
		}
	}
	if value, duplicate := uniqueStartupMappingValue(server.Content, "pidfile"); duplicate {
		slog.Warn("duplicate startup config key; retaining prior value", "file", file, "key", "server.pidfile")
	} else if value != nil {
		parsed, err := strconv.ParseBool(strings.TrimSpace(value.Value))
		if value.Kind != yaml.ScalarNode || err != nil {
			slog.Warn("invalid startup config value; retaining prior value", "file", file, "key", "server.pidfile")
		} else {
			settings.PIDFile = parsed
		}
	}
}

func uniqueStartupMappingValue(nodes []*yaml.Node, key string) (*yaml.Node, bool) {
	var found *yaml.Node
	for index := 0; index+1 < len(nodes); index += 2 {
		if nodes[index].Value != key {
			continue
		}
		if found != nil {
			return nil, true
		}
		found = nodes[index+1]
	}
	return found, false
}

func withinDirectory(directory, path string) bool {
	relative, err := filepath.Rel(directory, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
