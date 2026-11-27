package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const operatorTokenAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// loadSecrets applies the secret file after the main config and drop-ins.
func loadSecrets(path string, cfg *Config) error {
	if err := secureSecretFile(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("secure secrets file permissions: %w", err)
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read secrets file: %w", err)
	}
	candidate, err := cloneConfig(cfg)
	if err != nil {
		return fmt.Errorf("clone configuration before applying secrets: %w", err)
	}
	if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(data))), candidate); err != nil {
		return fmt.Errorf("parse secrets file: %w", err)
	}
	*cfg = *candidate
	return nil
}

func secureSecretFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("secrets path is not a regular file")
	}
	if info.Mode().Perm() != 0o600 {
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// EnsureOperatorToken persists server secrets in secrets.yml and removes
// operator-token or encryption-key copies from server.yml and yaml.d drop-ins.
func EnsureOperatorToken(configDir string, cfg *Config) (bool, error) {
	if cfg == nil {
		return false, fmt.Errorf("configuration is required")
	}
	secretPath := filepath.Join(configDir, "secrets.yml")
	if err := secureSecretFile(secretPath); err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("secure secrets file permissions: %w", err)
	}
	secretNode, err := readYAMLNode(secretPath)
	if err != nil {
		return false, fmt.Errorf("load secrets file: %w", err)
	}
	tokenNode := nestedMappingValue(secretNode, "server", "token")
	token := ""
	if tokenNode != nil {
		token = strings.TrimSpace(tokenNode.Value)
	}
	if token == "" {
		token = strings.TrimSpace(cfg.Server.Token)
	}
	generated := false
	if token == "" {
		token, err = generateOperatorToken()
		if err != nil {
			return false, fmt.Errorf("generate operator token: %w", err)
		}
		generated = true
	}
	if !validOperatorToken(token) {
		return false, fmt.Errorf("server.token must be adm_ followed by 32 base62 characters")
	}
	setNestedMappingValue(secretNode, "server", "token", token)
	encryptionKey := ""
	if keyNode := nestedMappingValue(secretNode, "server", "security", "encryption_key"); keyNode != nil {
		encryptionKey = strings.TrimSpace(keyNode.Value)
	}
	if encryptionKey == "" {
		encryptionKey = strings.TrimSpace(cfg.Server.Security.EncryptionKey)
	}
	if encryptionKey == "" {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return false, fmt.Errorf("generate server encryption key: %w", err)
		}
		encryptionKey = base64.RawStdEncoding.EncodeToString(key)
	}
	decodedKey, err := base64.RawStdEncoding.DecodeString(encryptionKey)
	if err != nil || len(decodedKey) != 32 {
		return false, fmt.Errorf("server.security.encryption_key must be base64-encoded 32-byte key material")
	}
	setNestedMappingValue(secretNode, "server", "security", "encryption_key", encryptionKey)
	setNestedMappingValue(secretNode, "server", "security", "encryption_key_version", "1")
	nestedMappingValue(secretNode, "server", "security", "encryption_key_version").Tag = "!!int"
	if err := writeYAMLNodeAtomic(secretPath, secretNode); err != nil {
		return false, fmt.Errorf("write secrets file: %w", err)
	}

	configFiles, err := operatorTokenSourceFiles(configDir)
	if err != nil {
		return false, err
	}
	for _, path := range configFiles {
		node, err := readYAMLNode(path)
		if err != nil {
			return false, fmt.Errorf("read config file for secret migration: %w", err)
		}
		changed := removeNestedMappingValue(node, "server", "token")
		changed = removeNestedMappingValue(node, "server", "security", "encryption_key") || changed
		changed = removeNestedMappingValue(node, "server", "security", "encryption_key_version") || changed
		if changed {
			if err := writeYAMLNodeAtomic(path, node); err != nil {
				return false, fmt.Errorf("remove server.token from config file: %w", err)
			}
		}
	}
	cfg.Server.Token = token
	cfg.Server.Security.EncryptionKey = encryptionKey
	cfg.Server.Security.EncryptionKeyVersion = 1
	return generated, nil
}

func operatorTokenSourceFiles(configDir string) ([]string, error) {
	files := []string{filepath.Join(configDir, "server.yml")}
	dropinDir := filepath.Join(configDir, "yaml.d")
	entries, err := os.ReadDir(dropinDir)
	if os.IsNotExist(err) {
		return files, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config drop-ins for secret migration: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".yml") && !strings.HasSuffix(entry.Name(), ".yaml")) {
			continue
		}
		files = append(files, filepath.Join(dropinDir, entry.Name()))
	}
	return files, nil
}

func readYAMLNode(path string) (*yaml.Node, error) {
	node := &yaml.Node{}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		node.Kind = yaml.DocumentNode
		node.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
		return node, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, node); err != nil {
		return nil, err
	}
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("YAML root must be a mapping")
	}
	return node, nil
}

func nestedMappingValue(root *yaml.Node, path ...string) *yaml.Node {
	if root == nil || len(root.Content) == 0 {
		return nil
	}
	node := root.Content[0]
	for _, key := range path {
		node = mappingValue(node, key)
		if node == nil {
			return nil
		}
	}
	return node
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func setNestedMappingValue(root *yaml.Node, path ...string) {
	if root == nil || len(root.Content) == 0 || len(path) < 2 {
		return
	}
	node := root.Content[0]
	for _, key := range path[:len(path)-2] {
		child := mappingValue(node, key)
		if child == nil {
			child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, child)
		}
		node = child
	}
	key, value := path[len(path)-2], path[len(path)-1]
	valueNode := mappingValue(node, key)
	if valueNode == nil {
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
		return
	}
	valueNode.Kind = yaml.ScalarNode
	valueNode.Tag = "!!str"
	valueNode.Value = value
}

func removeNestedMappingValue(root *yaml.Node, path ...string) bool {
	if root == nil || len(root.Content) == 0 {
		return false
	}
	if len(path) < 2 {
		return false
	}
	mapping := root.Content[0]
	for _, key := range path[:len(path)-1] {
		mapping = mappingValue(mapping, key)
		if mapping == nil || mapping.Kind != yaml.MappingNode {
			return false
		}
	}
	key := path[len(path)-1]
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return true
		}
	}
	return false
}

func writeYAMLNodeAtomic(path string, node *yaml.Node) error {
	dir := filepath.Dir(path)
	data, err := yaml.Marshal(node)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".secrets-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	if err := directory.Sync(); err != nil {
		return err
	}
	return nil
}

// SaveDetectedSMTP persists the relay selected by automatic SMTP detection
// without replacing unrelated server settings or comments.
func SaveDetectedSMTP(path, host string, port int) error {
	if strings.TrimSpace(host) == "" || port < 1 || port > 65535 {
		return fmt.Errorf("detected SMTP endpoint is invalid")
	}
	node, err := readYAMLNode(path)
	if err != nil {
		return fmt.Errorf("read server configuration: %w", err)
	}
	if err := setSMTPYAMLValue(node, "host", strings.TrimSpace(host), "!!str"); err != nil {
		return err
	}
	if err := setSMTPYAMLValue(node, "port", strconv.Itoa(port), "!!int"); err != nil {
		return err
	}
	return writeYAMLNodeAtomic(path, node)
}

// SaveMode changes only server.mode while preserving unrelated YAML content.
func SaveMode(path, mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "production" && mode != "development" {
		return fmt.Errorf("unsupported server mode %q", mode)
	}
	node, err := readYAMLNode(path)
	if err != nil {
		return fmt.Errorf("read server configuration: %w", err)
	}
	root := node.Content[0]
	server := mappingValue(root, "server")
	if server == nil {
		server = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "server"}, server)
	} else if server.Kind != yaml.MappingNode {
		return fmt.Errorf("server configuration must be a mapping")
	}
	modeNode := mappingValue(server, "mode")
	if modeNode == nil {
		server.Content = append(server.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "mode"}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: mode})
	} else {
		modeNode.Kind = yaml.ScalarNode
		modeNode.Tag = "!!str"
		modeNode.Value = mode
		modeNode.Content = nil
	}
	return writeYAMLNodeAtomic(path, node)
}

func setSMTPYAMLValue(root *yaml.Node, name, value, tag string) error {
	if root == nil || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("server configuration root must be a mapping")
	}
	path := []string{"server", "notifications", "email", "smtp"}
	mapping := root.Content[0]
	for _, key := range path {
		child := mappingValue(mapping, key)
		if child == nil {
			child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, child)
		} else if child.Kind != yaml.MappingNode {
			return fmt.Errorf("server configuration %s must be a mapping", strings.Join(path, "."))
		}
		mapping = child
	}
	valueNode := mappingValue(mapping, name)
	if valueNode == nil {
		valueNode = &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
		mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, valueNode)
	} else {
		valueNode.Kind = yaml.ScalarNode
		valueNode.Tag = tag
		valueNode.Value = value
		valueNode.Content = nil
	}
	return nil
}

func generateOperatorToken() (string, error) {
	const limit = byte(248)
	value := make([]byte, 0, 32)
	buffer := make([]byte, 64)
	for len(value) < 32 {
		if _, err := rand.Read(buffer); err != nil {
			return "", err
		}
		for _, candidate := range buffer {
			if candidate < limit {
				value = append(value, operatorTokenAlphabet[int(candidate)%len(operatorTokenAlphabet)])
				if len(value) == 32 {
					break
				}
			}
		}
	}
	return "adm_" + string(value), nil
}

func validOperatorToken(value string) bool {
	if len(value) != 36 || !strings.HasPrefix(value, "adm_") {
		return false
	}
	for _, char := range value[4:] {
		if !strings.ContainsRune(operatorTokenAlphabet, char) {
			return false
		}
	}
	return true
}
