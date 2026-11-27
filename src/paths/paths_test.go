package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlatformPathSets(t *testing.T) {
	if got := getDockerPaths(); got.Config != "/config/mail" || got.Data != "/data/mail" || got.DBDir != "/data/db/sqlite" {
		t.Fatalf("Docker paths = %#v", got)
	}
	if got := getLinuxPaths(true); got.Config != "/etc/apimgr/mail" || got.DBDir != "/var/lib/apimgr/mail/db" || got.Security != "/var/lib/apimgr/mail/security" || got.PID != "/var/run/apimgr/mail/mail.pid" {
		t.Fatalf("root Linux paths = %#v", got)
	}
	t.Setenv("HOME", "/home/test")
	if got := getLinuxPaths(false); got.Config != "/home/test/.config/apimgr/mail" || got.PID != "/home/test/.local/share/apimgr/mail/mail.pid" || got.Security != "/home/test/.local/share/apimgr/mail/security" {
		t.Fatalf("user Linux paths = %#v", got)
	}
	if got := getDarwinPaths(true); got.Config != "/Library/Application Support/apimgr/mail" || got.Security != "/Library/Application Support/apimgr/mail/data/security" || got.PID != "/var/run/apimgr/mail/mail.pid" {
		t.Fatalf("root macOS paths = %#v", got)
	}
	if got := getBSDPaths(true); got.Config != "/usr/local/etc/apimgr/mail" || got.Security != "/var/db/apimgr/mail/security" || got.PID != "/var/run/apimgr/mail/mail.pid" {
		t.Fatalf("root BSD paths = %#v", got)
	}
	if got := getBSDPaths(false); got.Config != "/home/test/.config/apimgr/mail" || got.Security != "/home/test/.local/share/apimgr/mail/security" {
		t.Fatalf("user BSD paths = %#v", got)
	}
	if got := getWindowsPaths(true); got.Security != filepath.Join(os.Getenv("ProgramData"), "apimgr/mail/data/security") {
		t.Fatalf("root Windows paths = %#v", got)
	}
	if got := getWindowsPaths(false); got.Security != filepath.Join(os.Getenv("LocalAppData"), "apimgr/mail/security") {
		t.Fatalf("user Windows paths = %#v", got)
	}
	if getDarwinPaths(false).Data == "" || getWindowsPaths(false).Config == "" || getWindowsPaths(true).Config == "" {
		t.Fatal("platform path sets are incomplete")
	}
	if isContainer() {
		if got := GetDefaultPaths(); got.Config != "/config/mail" {
			t.Fatalf("default container paths = %#v", got)
		}
	}
}

func TestEnsureDirAndEnsureAllDirs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested", "directory")
	if err := EnsureDir(path, false); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatalf("EnsureDir did not create directory: info=%v err=%v", info, err)
	}
	p := &Paths{Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Log: filepath.Join(root, "log"), Backup: filepath.Join(root, "backup"), SSL: filepath.Join(root, "ssl"), Security: filepath.Join(root, "security"), DBDir: filepath.Join(root, "db")}
	if err := p.EnsureAllDirs(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.Backup); !os.IsNotExist(err) {
		t.Fatalf("EnsureAllDirs should defer backup validation, stat err=%v", err)
	}
	if err := p.EnsureBackupDir(false); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{p.Config, p.Data, p.Cache, p.Log, p.Backup, p.SSL, p.Security, p.DBDir} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Errorf("directory %q: info=%v err=%v", dir, info, err)
		}
	}
	backupFile := filepath.Join(root, "backup-file")
	if err := os.WriteFile(backupFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.EnsureAllDirs(false); err != nil {
		t.Fatalf("unavailable backup path made required directory setup fail: %v", err)
	}
	p.Backup = backupFile
	if err := p.EnsureBackupDir(false); err == nil {
		t.Fatal("backup validation accepted a file in place of a directory")
	}
}

func TestPathOverridesUseSafePathsAndDerivedDirectories(t *testing.T) {
	paths := &Paths{Config: "/default/config", Data: "/default/data", Cache: "/default/cache", Log: "/default/log", Backup: "/default/backup", PID: "/default/server.pid", SSL: "/default/ssl", Security: "/default/security", DBDir: "/default/db"}
	t.Setenv("CONFIG_DIR", "/custom/config")
	t.Setenv("DATA_DIR", "/custom/data")
	t.Setenv("CACHE_DIR", "../unsafe")
	t.Setenv("LOG_DIR", "/custom/log")
	t.Setenv("BACKUP_DIR", "/custom/backup")
	t.Setenv("PID_FILE", "/custom/server.pid")
	t.Setenv("SSL_DIR", "")
	t.Setenv("SECURITY_DIR", "")
	t.Setenv("DATABASE_DIR", "/custom/db")
	applyPathOverrides(paths)
	if paths.Config != "/custom/config" || paths.Data != "/custom/data" || paths.Cache != "/default/cache" || paths.Log != "/custom/log" || paths.Backup != "/custom/backup" || paths.PID != "/custom/server.pid" || paths.SSL != filepath.Join(paths.Config, "ssl") || paths.Security != filepath.Join(paths.Data, "security") || paths.DBDir != "/custom/db" {
		t.Fatalf("path override result = %+v", paths)
	}
}

func TestContainerParentRecognition(t *testing.T) {
	for _, parent := range []string{"tini", "dumb-init", "s6-svscan", "runsv", "runsvdir", "catatonit", "mail", "mail.exe"} {
		if !isContainerParent(parent) {
			t.Errorf("isContainerParent(%q) = false", parent)
		}
	}
	for _, parent := range []string{"", "bash", "systemd", "mail-service"} {
		if isContainerParent(parent) {
			t.Errorf("isContainerParent(%q) = true", parent)
		}
	}
}

func TestUniquePathsPrivateDirectoriesAndTreeSafety(t *testing.T) {
	got := uniquePaths("", "/tmp/mail/../mail", "/tmp/mail", "/tmp/mail/data")
	want := []string{"/tmp/mail", "/tmp/mail/data"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("uniquePaths() = %v, want %v", got, want)
	}
	if err := EnsurePrivateDir(""); err != nil {
		t.Fatalf("empty private directory: %v", err)
	}
	private := filepath.Join(t.TempDir(), "private")
	if err := EnsurePrivateDir(private); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(private)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("private directory mode = %v, %v", info, err)
	}
	root := filepath.Join(t.TempDir(), "tree")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "nested", "config")
	if err := os.WriteFile(file, []byte("config"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := changeTreeOwner(root, os.Getuid(), os.Getgid(), ""); err != nil {
		t.Fatal(err)
	}
	if err := changeTreeMode(root, 0o750, 0o640, ""); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("tree file mode = %v, %v", info, err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if err := changeTreeOwner(root, os.Getuid(), os.Getgid(), ""); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("changeTreeOwner followed a symlink: %v", err)
	}
	if err := changeTreeMode(root, 0o750, 0o640, ""); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("changeTreeMode followed a symlink: %v", err)
	}
}

func TestDirectoryValidationReportsUnavailablePaths(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDir(filepath.Join(file, "child"), false); err == nil {
		t.Fatal("EnsureDir accepted a path below a regular file")
	}
	if err := (&Paths{}).EnsureBackupDir(false); err == nil {
		t.Fatal("EnsureBackupDir accepted an empty path")
	}
	if err := (&Paths{Backup: file}).EnsureBackupDir(false); err == nil {
		t.Fatal("EnsureBackupDir accepted a regular file")
	}
	if err := (&Paths{}).VerifyBackupWritable(); err == nil {
		t.Fatal("VerifyBackupWritable accepted an empty path")
	}
}
