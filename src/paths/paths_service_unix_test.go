//go:build !windows

package paths

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestPrepareServicePathsDoesNotChangeBackupContents(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("ownership preparation requires root")
	}
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	backupDir := filepath.Join(dataDir, "backup")
	backupFile := filepath.Join(backupDir, "existing.tar.gz")
	for _, dir := range []string{
		filepath.Join(root, "config"), dataDir, filepath.Join(root, "cache"),
		filepath.Join(root, "logs"), backupDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(backupFile, []byte("preserve ownership"), 0o600); err != nil {
		t.Fatal(err)
	}

	appPaths := &Paths{
		Config: filepath.Join(root, "config"), Data: dataDir,
		Cache: filepath.Join(root, "cache"), Log: filepath.Join(root, "logs"),
		Backup: backupDir, SSL: filepath.Join(root, "config", "ssl"),
		Security: filepath.Join(dataDir, "security"), DBDir: filepath.Join(dataDir, "db"),
	}
	if err := appPaths.EnsureAllDirs(true); err != nil {
		t.Fatal(err)
	}
	if err := appPaths.PrepareServicePaths(ServiceIdentity{Name: "test-service", Home: dataDir, UID: 65534, GID: 65534}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(backupFile)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("backup file has no Unix ownership metadata")
	}
	if stat.Uid != uint32(os.Getuid()) || stat.Gid != uint32(os.Getgid()) {
		t.Fatalf("backup file ownership changed to %d:%d", stat.Uid, stat.Gid)
	}
	backupInfo, err := os.Stat(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	backupStat := backupInfo.Sys().(*syscall.Stat_t)
	if backupStat.Uid != 65534 || backupStat.Gid != 65534 {
		t.Fatalf("backup directory ownership = %d:%d, want 65534:65534", backupStat.Uid, backupStat.Gid)
	}
}

func TestVerifyBackupWritableUsesConfiguredDirectory(t *testing.T) {
	backupDir := t.TempDir()
	appPaths := &Paths{Backup: backupDir}
	if err := appPaths.VerifyBackupWritable(); err != nil {
		t.Fatalf("VerifyBackupWritable() error = %v", err)
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("write probe left %d artifacts", len(entries))
	}
}
