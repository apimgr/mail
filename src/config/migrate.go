package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func migrateLegacyDatabaseSection(path string) error {
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(original, &document); err != nil || len(document.Content) == 0 {
		return nil
	}
	root := document.Content[0]
	legacy := mappingValue(root, "database")
	if legacy == nil {
		return nil
	}
	server := mappingValue(root, "server")
	if server == nil {
		server = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "server"}, server)
	}
	if server.Kind != yaml.MappingNode {
		return fmt.Errorf("server section is not a mapping")
	}

	backupPath := path + ".bak"
	if err := writePrivateBackup(backupPath, original); err != nil {
		return fmt.Errorf("backup legacy configuration: %w", err)
	}
	if mappingValue(server, "database") == nil {
		for i := 0; i+1 < len(legacy.Content); i += 2 {
			switch legacy.Content[i].Value {
			case "type":
				legacy.Content[i].Value = "driver"
			case "name":
				legacy.Content[i].Value = "url"
			}
		}
		server.Content = append(server.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "database"}, legacy)
	}
	removeMappingKey(root, "database")
	if err := writeYAMLNodeAtomic(path, &document); err != nil {
		return fmt.Errorf("write migrated configuration: %w", err)
	}
	return nil
}

func writePrivateBackup(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return file.Sync()
}

func removeMappingKey(mapping *yaml.Node, key string) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}
