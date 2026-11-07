package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlatformPathSets(t *testing.T) {
	if got := getDockerPaths(); got.Config != "/config/mail" || got.Data != "/data/mail" || got.DBDir != "/data/db/sqlite" {
		t.Fatalf("Docker paths = %#v", got)
	}
	if got := getLinuxPaths(true); got.Config != "/etc/apimgr/mail" || got.DBDir != "/var/lib/apimgr/mail/db" {
		t.Fatalf("root Linux paths = %#v", got)
	}
	t.Setenv("HOME", "/home/test")
	if got := getLinuxPaths(false); got.Config != "/home/test/.config/apimgr/mail" || got.PID != "/home/test/.local/share/apimgr/mail/mail.pid" {
		t.Fatalf("user Linux paths = %#v", got)
	}
	if getDarwinPaths(true).Config == "" || getDarwinPaths(false).Data == "" || getBSDPaths(false).Data == "" || getBSDPaths(true).Data == "" || getWindowsPaths(false).Config == "" || getWindowsPaths(true).Config == "" {
		t.Fatal("platform path sets are incomplete")
	}
	if isInDocker() {
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
	for _, dir := range []string{p.Config, p.Data, p.Cache, p.Log, p.Backup, p.SSL, p.Security, p.DBDir} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Errorf("directory %q: info=%v err=%v", dir, info, err)
		}
	}
}
