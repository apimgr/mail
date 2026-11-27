package paths

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/apimgr/mail/src/common/security"
)

// Paths holds all application paths
type Paths struct {
	Config    string
	Data      string
	Cache     string
	Log       string
	Backup    string
	PID       string
	SSL       string
	Security  string
	DBDir     string
	Container bool
}

// GetDefaultPaths returns OS and privilege-specific default paths
// Per AI.md PART 4: OS-SPECIFIC PATHS
func GetDefaultPaths() *Paths {
	isRoot := isRootUser()
	isDocker := isContainer()
	var appPaths *Paths
	if isDocker {
		appPaths = getDockerPaths()
	} else {
		switch runtime.GOOS {
		case "linux":
			appPaths = getLinuxPaths(isRoot)
		case "darwin":
			appPaths = getDarwinPaths(isRoot)
		case "freebsd", "openbsd", "netbsd":
			appPaths = getBSDPaths(isRoot)
		case "windows":
			appPaths = getWindowsPaths(isRoot)
		default:
			appPaths = getLinuxPaths(isRoot)
		}
	}
	applyPathOverrides(appPaths)
	return appPaths
}

// IsRoot reports whether this process has the host's root identity.
func IsRoot() bool { return isRootUser() }

// IsContainer reports whether this process runs in an application container.
func IsContainer() bool { return isContainer() }

func applyPathOverrides(appPaths *Paths) {
	if value := os.Getenv("CONFIG_DIR"); value != "" {
		appPaths.Config = safePathOverride("CONFIG_DIR", value, appPaths.Config)
	}
	if value := os.Getenv("DATA_DIR"); value != "" {
		appPaths.Data = safePathOverride("DATA_DIR", value, appPaths.Data)
	}
	if value := os.Getenv("CACHE_DIR"); value != "" {
		appPaths.Cache = safePathOverride("CACHE_DIR", value, appPaths.Cache)
	}
	if value := os.Getenv("LOG_DIR"); value != "" {
		appPaths.Log = safePathOverride("LOG_DIR", value, appPaths.Log)
	}
	if value := os.Getenv("BACKUP_DIR"); value != "" {
		appPaths.Backup = safePathOverride("BACKUP_DIR", value, appPaths.Backup)
	}
	if value := os.Getenv("PID_FILE"); value != "" {
		appPaths.PID = safePathOverride("PID_FILE", value, appPaths.PID)
	}
	if value := os.Getenv("SSL_DIR"); value != "" {
		appPaths.SSL = safePathOverride("SSL_DIR", value, appPaths.SSL)
	} else if appPaths.Config != "" {
		appPaths.SSL = filepath.Join(appPaths.Config, "ssl")
	}
	if value := os.Getenv("SECURITY_DIR"); value != "" {
		appPaths.Security = safePathOverride("SECURITY_DIR", value, appPaths.Security)
	} else if appPaths.Data != "" {
		appPaths.Security = filepath.Join(appPaths.Data, "security")
	}
	if value := os.Getenv("DATABASE_DIR"); value != "" {
		appPaths.DBDir = safePathOverride("DATABASE_DIR", value, appPaths.DBDir)
	}
}

func safePathOverride(name, value, fallback string) string {
	cleaned, err := security.SafeDirPath(value)
	if err != nil {
		slog.Warn("invalid path override; using platform default", "setting", name, "error", err)
		return fallback
	}
	return cleaned
}

// isContainer detects application containers after allowing system service managers to take precedence.
// Per AI.md PART 8: Detect container vs local
func isContainer() bool {
	if os.Getenv("INVOCATION_ID") != "" || os.Getenv("RC_SVCNAME") != "" || (os.Getppid() == 1 && fileExists("/run/openrc")) {
		return false
	}
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv", "/dev/lxc"} {
		if fileExists(marker) {
			return true
		}
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}
	if os.Getenv("container") != "" || fileExists("/dev/lxc") {
		return true
	}
	if isContainerParent(parentProcessName()) {
		return true
	}
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		value := strings.ToLower(string(data))
		for _, marker := range []string{"docker", "kubepods", "lxc"} {
			if strings.Contains(value, marker) {
				return true
			}
		}
	}
	return false
}

func isContainerParent(name string) bool {
	switch strings.TrimSuffix(filepath.Base(strings.TrimSpace(name)), ".exe") {
	case "tini", "dumb-init", "s6-svscan", "runsv", "runsvdir", "catatonit", "mail":
		return true
	default:
		return false
	}
}

func parentProcessName() string {
	parentPID := os.Getppid()
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", parentPID)); err == nil {
		return strings.TrimSpace(string(data))
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// getDockerPaths returns Docker container paths
// Per AI.md PART 4: Docker/Container section
func getDockerPaths() *Paths {
	return &Paths{
		Config:    "/config/mail",
		Data:      "/data/mail",
		Cache:     "/data/mail/cache",
		Log:       "/data/log/mail",
		Backup:    "/data/backups/mail",
		PID:       "",
		SSL:       "/config/mail/ssl",
		Security:  "/data/mail/security",
		DBDir:     "/data/db/sqlite",
		Container: true,
	}
}

// getLinuxPaths returns Linux-specific paths
// Per AI.md PART 4: Linux section
func getLinuxPaths(isRoot bool) *Paths {
	if isRoot {
		return &Paths{
			Config:   "/etc/apimgr/mail",
			Data:     "/var/lib/apimgr/mail",
			Cache:    "/var/cache/apimgr/mail",
			Log:      "/var/log/apimgr/mail",
			Backup:   "/mnt/Backups/apimgr/mail",
			PID:      "/var/run/apimgr/mail/mail.pid",
			SSL:      "/etc/apimgr/mail/ssl",
			Security: "/var/lib/apimgr/mail/security",
			DBDir:    "/var/lib/apimgr/mail/db",
		}
	}

	home := os.Getenv("HOME")
	return &Paths{
		Config:   filepath.Join(home, ".config/apimgr/mail"),
		Data:     filepath.Join(home, ".local/share/apimgr/mail"),
		Cache:    filepath.Join(home, ".cache/apimgr/mail"),
		Log:      filepath.Join(home, ".local/log/apimgr/mail"),
		Backup:   filepath.Join(home, ".local/share/Backups/apimgr/mail"),
		PID:      filepath.Join(home, ".local/share/apimgr/mail/mail.pid"),
		SSL:      filepath.Join(home, ".config/apimgr/mail/ssl"),
		Security: filepath.Join(home, ".local/share/apimgr/mail/security"),
		DBDir:    filepath.Join(home, ".local/share/apimgr/mail/db"),
	}
}

// getDarwinPaths returns macOS-specific paths
// Per AI.md PART 4: macOS section
func getDarwinPaths(isRoot bool) *Paths {
	if isRoot {
		return &Paths{
			Config:   "/Library/Application Support/apimgr/mail",
			Data:     "/Library/Application Support/apimgr/mail/data",
			Cache:    "/Library/Caches/apimgr/mail",
			Log:      "/Library/Logs/apimgr/mail",
			Backup:   "/Library/Backups/apimgr/mail",
			PID:      "/var/run/apimgr/mail/mail.pid",
			SSL:      "/Library/Application Support/apimgr/mail/ssl",
			Security: "/Library/Application Support/apimgr/mail/data/security",
			DBDir:    "/Library/Application Support/apimgr/mail/db",
		}
	}

	home := os.Getenv("HOME")
	return &Paths{
		Config:   filepath.Join(home, "Library/Application Support/apimgr/mail"),
		Data:     filepath.Join(home, "Library/Application Support/apimgr/mail"),
		Cache:    filepath.Join(home, "Library/Caches/apimgr/mail"),
		Log:      filepath.Join(home, "Library/Logs/apimgr/mail"),
		Backup:   filepath.Join(home, "Library/Backups/apimgr/mail"),
		PID:      filepath.Join(home, "Library/Application Support/apimgr/mail/mail.pid"),
		SSL:      filepath.Join(home, "Library/Application Support/apimgr/mail/ssl"),
		Security: filepath.Join(home, "Library/Application Support/apimgr/mail/security"),
		DBDir:    filepath.Join(home, "Library/Application Support/apimgr/mail/db"),
	}
}

// getBSDPaths returns BSD-specific paths
// Per AI.md PART 4: BSD section
func getBSDPaths(isRoot bool) *Paths {
	if isRoot {
		return &Paths{
			Config:   "/usr/local/etc/apimgr/mail",
			Data:     "/var/db/apimgr/mail",
			Cache:    "/var/cache/apimgr/mail",
			Log:      "/var/log/apimgr/mail",
			Backup:   "/var/backups/apimgr/mail",
			PID:      "/var/run/apimgr/mail/mail.pid",
			SSL:      "/usr/local/etc/apimgr/mail/ssl",
			Security: "/var/db/apimgr/mail/security",
			DBDir:    "/var/db/apimgr/mail/db",
		}
	}

	home := os.Getenv("HOME")
	return &Paths{
		Config:   filepath.Join(home, ".config/apimgr/mail"),
		Data:     filepath.Join(home, ".local/share/apimgr/mail"),
		Cache:    filepath.Join(home, ".cache/apimgr/mail"),
		Log:      filepath.Join(home, ".local/log/apimgr/mail"),
		Backup:   filepath.Join(home, ".local/share/Backups/apimgr/mail"),
		PID:      filepath.Join(home, ".local/share/apimgr/mail/mail.pid"),
		SSL:      filepath.Join(home, ".config/apimgr/mail/ssl"),
		Security: filepath.Join(home, ".local/share/apimgr/mail/security"),
		DBDir:    filepath.Join(home, ".local/share/apimgr/mail/db"),
	}
}

// getWindowsPaths returns Windows-specific paths
// Per AI.md PART 4: Windows section
func getWindowsPaths(isRoot bool) *Paths {
	if isRoot {
		programData := os.Getenv("ProgramData")
		return &Paths{
			Config:   filepath.Join(programData, "apimgr/mail"),
			Data:     filepath.Join(programData, "apimgr/mail/data"),
			Cache:    filepath.Join(programData, "apimgr/mail/cache"),
			Log:      filepath.Join(programData, "apimgr/mail/logs"),
			Backup:   filepath.Join(programData, "Backups/apimgr/mail"),
			PID:      filepath.Join(programData, "apimgr/mail/mail.pid"),
			SSL:      filepath.Join(programData, "apimgr/mail/ssl"),
			Security: filepath.Join(programData, "apimgr/mail/data/security"),
			DBDir:    filepath.Join(programData, "apimgr/mail/db"),
		}
	}

	appData := os.Getenv("AppData")
	localAppData := os.Getenv("LocalAppData")
	return &Paths{
		Config:   filepath.Join(appData, "apimgr/mail"),
		Data:     filepath.Join(localAppData, "apimgr/mail"),
		Cache:    filepath.Join(localAppData, "apimgr/mail/cache"),
		Log:      filepath.Join(localAppData, "apimgr/mail/logs"),
		Backup:   filepath.Join(localAppData, "Backups/apimgr/mail"),
		PID:      filepath.Join(localAppData, "apimgr/mail/mail.pid"),
		SSL:      filepath.Join(appData, "apimgr/mail/ssl"),
		Security: filepath.Join(localAppData, "apimgr/mail/security"),
		DBDir:    filepath.Join(localAppData, "apimgr/mail/db"),
	}
}

// EnsureDir creates directory with proper permissions if it doesn't exist
// Per AI.md PART 8: Directory Validation Rules
func EnsureDir(path string, isRoot bool) error {
	perm := os.FileMode(0o700)
	if isRoot {
		perm = 0o755
	}

	if err := os.MkdirAll(path, perm); err != nil {
		return err
	}
	if err := os.Chmod(path, perm); err != nil {
		return fmt.Errorf("set directory mode %s: %w", path, err)
	}

	// Verify writable
	file, err := os.CreateTemp(path, ".write-test-*")
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return fmt.Errorf("close directory write test: %w", err)
	}
	if err := os.Remove(file.Name()); err != nil {
		return fmt.Errorf("remove directory write test: %w", err)
	}

	return nil
}

// EnsureAllDirs creates all application directories
// Per AI.md PART 8: Step 8b/9 - Create ALL directories
func (p *Paths) EnsureAllDirs(isRoot bool) error {
	dirs := []string{
		p.Config,
		p.Data,
		p.Cache,
		p.Log,
		p.SSL,
		p.DBDir,
	}

	for _, dir := range dirs {
		if err := EnsureDir(dir, isRoot); err != nil {
			return err
		}
	}
	privateDirs := []string{filepath.Join(p.Config, "tor"), filepath.Join(p.Config, "i2p"), filepath.Join(p.Config, "yaml.d"), filepath.Join(p.Config, "assets"), p.SSL, p.Security, filepath.Join(p.Data, "tor"), filepath.Join(p.Data, "tor/site")}
	for _, dir := range privateDirs {
		if err := EnsurePrivateDir(dir); err != nil {
			return err
		}
	}

	if p.PID != "" {
		pidDir := filepath.Dir(p.PID)
		if err := EnsureDir(pidDir, isRoot); err != nil {
			return err
		}
	}

	return nil
}

// EnsureBackupDir validates the backup path separately because startup treats
// an unavailable backup location as warning-only.
func (p *Paths) EnsureBackupDir(isRoot bool) error {
	if p.Backup == "" {
		return fmt.Errorf("backup directory is not configured")
	}
	if err := EnsureDir(p.Backup, isRoot); err != nil {
		return fmt.Errorf("ensure backup directory %s: %w", p.Backup, err)
	}
	return nil
}

// PrepareServicePaths transfers root-created runtime paths to the service
// identity before the process drops privileges.
func (p *Paths) PrepareServicePaths(identity ServiceIdentity) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if !isRootUser() {
		return fmt.Errorf("preparing service paths requires root")
	}
	if identity.UID <= 0 || identity.GID <= 0 {
		return fmt.Errorf("service identity has invalid numeric ownership")
	}

	generalPaths := uniquePaths(p.Config, p.Data, p.Cache, p.Log, p.SSL, p.DBDir)
	if p.PID != "" {
		generalPaths = uniquePaths(append(generalPaths, filepath.Dir(p.PID))...)
		if _, err := os.Lstat(p.PID); err == nil {
			generalPaths = uniquePaths(append(generalPaths, p.PID)...)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect PID path %s: %w", p.PID, err)
		}
	}
	skipBackup := ""
	if p.Backup != "" {
		skipBackup = filepath.Clean(p.Backup)
	}
	for _, root := range generalPaths {
		if err := changeTreeOwner(root, identity.UID, identity.GID, skipBackup); err != nil {
			return err
		}
		if err := changeTreeMode(root, 0o755, 0o644, skipBackup); err != nil {
			return err
		}
	}
	if p.PID != "" {
		if info, err := os.Lstat(p.PID); err == nil && info.Mode().IsRegular() {
			if err := os.Chmod(p.PID, 0o644); err != nil {
				return fmt.Errorf("secure PID file %s: %w", p.PID, err)
			}
		}
	}

	privatePaths := []string{
		filepath.Join(p.Config, "server.yml"), filepath.Join(p.Config, "secrets.yml"),
		filepath.Join(p.Config, "yaml.d"), filepath.Join(p.Config, "assets"), p.SSL,
		filepath.Join(p.Config, "tor"), filepath.Join(p.Config, "i2p"), p.Security,
		filepath.Join(p.Data, "tor"),
	}
	for _, root := range uniquePaths(privatePaths...) {
		if _, err := os.Lstat(root); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return fmt.Errorf("inspect private path %s: %w", root, err)
		}
		if err := changeTreeMode(root, 0o700, 0o600, skipBackup); err != nil {
			return err
		}
	}

	if p.Backup != "" {
		backupInfo, statErr := os.Lstat(p.Backup)
		if statErr != nil {
			slog.Warn("could not inspect backup directory; service-user write check will determine availability", "path", p.Backup, "error", statErr)
		} else if backupInfo.Mode()&os.ModeSymlink != 0 || !backupInfo.IsDir() {
			slog.Warn("backup path is not a real directory; service-user write check will determine availability", "path", p.Backup)
		} else {
			if err := os.Chown(p.Backup, identity.UID, identity.GID); err != nil {
				slog.Warn("could not change backup directory ownership; service-user write check will determine availability", "path", p.Backup, "error", err)
			}
			if err := os.Chmod(p.Backup, 0o755); err != nil {
				slog.Warn("could not set backup directory mode; service-user write check will determine availability", "path", p.Backup, "error", err)
			}
		}
	}
	return nil
}

// VerifyBackupWritable probes the configured backup path without choosing a
// replacement location when the configured path is unavailable.
func (p *Paths) VerifyBackupWritable() error {
	if p.Backup == "" {
		return fmt.Errorf("backup directory is not configured")
	}
	file, err := os.CreateTemp(p.Backup, ".mail-write-check-*")
	if err != nil {
		return fmt.Errorf("backup directory %s is not writable: %w", p.Backup, err)
	}
	name := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(name)
		return fmt.Errorf("secure backup write check in %s: %w", p.Backup, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("close backup write check in %s: %w", p.Backup, err)
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("remove backup write check in %s: %w", p.Backup, err)
	}
	return nil
}

func uniquePaths(paths ...string) []string {
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		clean := filepath.Clean(path)
		if _, exists := seen[clean]; exists {
			continue
		}
		seen[clean] = struct{}{}
		result = append(result, clean)
	}
	return result
}

func changeTreeOwner(root string, uid, gid int, skipPath string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("inspect service path %s: %w", path, walkErr)
		}
		if skipPath != "" && filepath.Clean(path) == skipPath {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in service path: %s", path)
		}
		if err := os.Chown(path, uid, gid); err != nil {
			return fmt.Errorf("set service ownership on %s: %w", path, err)
		}
		return nil
	})
}

func changeTreeMode(root string, directoryMode, fileMode os.FileMode, skipPath string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("inspect service path %s: %w", path, walkErr)
		}
		if skipPath != "" && filepath.Clean(path) == skipPath {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in service path: %s", path)
		}
		mode := fileMode
		if info.IsDir() {
			mode = directoryMode
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing special file in service path: %s", path)
		}
		if err := os.Chmod(path, mode); err != nil {
			return fmt.Errorf("set service path mode on %s: %w", path, err)
		}
		return nil
	})
}

// EnsurePrivateDir creates a private directory with owner-only access.
func EnsurePrivateDir(path string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create private directory %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure private directory %s: %w", path, err)
	}
	return nil
}
