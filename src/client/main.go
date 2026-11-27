package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	shellintegration "github.com/apimgr/mail/src/common/shell"

	"gopkg.in/yaml.v3"
)

var (
	Version   = "devel"
	CommitID  = "unknown"
	BuildDate = "unknown"
)

// ProjectName is the stable client identity, independent of the executable's filename.
var (
	ProjectName  = "mail"
	OfficialSite = ""
)

type cliConfig struct {
	Server struct {
		Primary    string `yaml:"primary"`
		APIVersion string `yaml:"api_version"`
		Timeout    string `yaml:"timeout"`
		Retry      int    `yaml:"retry"`
		RetryDelay string `yaml:"retry_delay"`
	} `yaml:"server"`
	Auth struct {
		Token     string `yaml:"token"`
		TokenFile string `yaml:"token_file"`
	} `yaml:"auth"`
	Output struct {
		Format  string `yaml:"format"`
		Color   string `yaml:"color"`
		Pager   string `yaml:"pager"`
		Quiet   bool   `yaml:"quiet"`
		Verbose bool   `yaml:"verbose"`
	} `yaml:"output"`
	Display struct {
		Mode string `yaml:"mode"`
	} `yaml:"display"`
	TUI struct {
		Enabled bool   `yaml:"enabled"`
		Theme   string `yaml:"theme"`
		Mouse   bool   `yaml:"mouse"`
		Unicode bool   `yaml:"unicode"`
	} `yaml:"tui"`
	Logging struct {
		Level    string `yaml:"level"`
		File     string `yaml:"file"`
		MaxSize  string `yaml:"max_size"`
		MaxFiles int    `yaml:"max_files"`
	} `yaml:"logging"`
	Cache struct {
		Enabled bool   `yaml:"enabled"`
		TTL     string `yaml:"ttl"`
		MaxSize string `yaml:"max_size"`
	} `yaml:"cache"`
	Update struct {
		Auto          bool   `yaml:"auto"`
		CheckInterval string `yaml:"check_interval"`
		Channel       string `yaml:"channel"`
	} `yaml:"update"`
	Defaults struct {
		Lang   string `yaml:"lang"`
		Output string `yaml:"output"`
		Limit  int    `yaml:"limit"`
	} `yaml:"defaults"`
	Debug bool `yaml:"debug"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		code := 1
		var commandErr cliCommandError
		if errors.As(err, &commandErr) {
			code = commandErr.code
		}
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "unknown command") || strings.Contains(message, "requires") || strings.Contains(message, "accepts") || strings.Contains(message, "unknown flag") {
			code = 2
		}
		if strings.Contains(message, "configuration") || strings.Contains(message, "cli.yml") {
			code = 78
		}
		os.Exit(code)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	name := filepath.Base(os.Args[0])
	for i, arg := range args {
		if arg == "--shell" {
			if i+1 < len(args) && (args[i+1] == "--help" || args[i+1] == "-h" || args[i+1] == "help") {
				return shellCommand([]string{"help"}, name, stdout, stderr)
			}
			return shellCommand(args[i+1:], name, stdout, stderr)
		}
	}
	root := newRootCommand(stdout, stderr)
	root.SetArgs(args)
	return root.Execute()
}

func loadConfig(path string) (cliConfig, error) {
	cfg := defaultCLIConfig()
	defaultPath := path == ""
	if path == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return cfg, fmt.Errorf("locating user config directory: %w", err)
		}
		path = filepath.Join(configDir, "apimgr", "mail", "cli.yml")
	} else {
		var err error
		path, err = resolveConfigPath(path)
		if err != nil {
			return cfg, err
		}
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if defaultPath {
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return cfg, fmt.Errorf("create config directory: %w", err)
			}
			if runtime.GOOS != "windows" {
				if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
					return cfg, fmt.Errorf("secure config directory: %w", err)
				}
			}
			contents, marshalErr := yaml.Marshal(cfg)
			if marshalErr != nil {
				return cfg, fmt.Errorf("encode default CLI config: %w", marshalErr)
			}
			if err := os.WriteFile(path, contents, 0o600); err != nil {
				return cfg, fmt.Errorf("create CLI config: %w", err)
			}
			if runtime.GOOS != "windows" {
				if err := os.Chmod(path, 0o600); err != nil {
					return cfg, fmt.Errorf("secure CLI config: %w", err)
				}
			}
		}
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("reading CLI config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing CLI config: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o600); err != nil {
			return cfg, fmt.Errorf("secure CLI config: %w", err)
		}
	}
	return cfg, nil
}

func ensureDirs() error {
	base, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("locating user config directory: %w", err)
	}
	var dirs []string
	if runtime.GOOS == "windows" {
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return errors.New("LOCALAPPDATA is not set")
		}
		dirs = []string{
			filepath.Join(base, "apimgr", "mail"),
			filepath.Join(local, "apimgr", "mail", "data"),
			filepath.Join(local, "apimgr", "mail", "cache"),
			filepath.Join(local, "apimgr", "mail", "log"),
		}
	} else {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return fmt.Errorf("locating user home directory: %w", homeErr)
		}
		dirs = []string{
			filepath.Join(base, "apimgr", "mail"),
			filepath.Join(home, ".local", "share", "apimgr", "mail"),
			filepath.Join(home, ".cache", "apimgr", "mail"),
			filepath.Join(home, ".local", "log", "apimgr", "mail"),
		}
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create user directory %s: %w", dir, err)
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(dir, 0o700); err != nil {
				return fmt.Errorf("secure user directory %s: %w", dir, err)
			}
		}
	}
	return nil
}

func resolveConfigPath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locating home directory: %w", err)
		}
		path = filepath.Join(home, filepath.FromSlash(path[2:]))
	}
	if !filepath.IsAbs(path) {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("locating user config directory: %w", err)
		}
		path = filepath.Join(base, "apimgr", "mail", path)
	}
	if filepath.Ext(path) == "" {
		if _, err := os.Stat(path + ".yml"); err == nil {
			return path + ".yml", nil
		}
		if _, err := os.Stat(path + ".yaml"); err == nil {
			return path + ".yaml", nil
		}
		return path + ".yml", nil
	}
	return path, nil
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(path, "~/")))
		}
	}
	return path
}

func shellCommand(args []string, binaryName string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: --shell [completions|init|help] [SHELL]")
	}
	action := args[0]
	shell := ""
	if len(args) > 1 {
		shell = args[1]
	}
	if shell == "" {
		shell = filepath.Base(os.Getenv("SHELL"))
	}
	if shell == "" {
		shell = "bash"
	}
	if action == "help" {
		_, err := fmt.Fprintf(stdout, "Shell integration for %s:\n  --shell completions [SHELL]  Print shell completions\n  --shell init [SHELL]         Print shell init command\n", binaryName)
		return err
	}
	if action != "completions" && action != "init" {
		return errors.New("usage: --shell [completions|init|help] [SHELL]")
	}
	output, err := shellintegration.Render(action, shell, binaryName)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return err
	}
	_, err = io.WriteString(stdout, output)
	return err
}

func promptCommand(input io.Reader, output io.Writer) (string, error) {
	if _, err := io.WriteString(output, "Mail CLI\n1) status\n2) about\n3) exit\nSelect: "); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	switch strings.TrimSpace(line) {
	case "1", "status":
		return "status", nil
	case "2", "about":
		return "about", nil
	case "3", "exit", "":
		return "", nil
	default:
		return "", fmt.Errorf("unknown menu selection %q", strings.TrimSpace(line))
	}
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
