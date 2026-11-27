package pidfile

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/apimgr/mail/src/paths"
)

func TestWriteCheckAndRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "server.pid")
	if err := Write(path); err != nil {
		t.Fatal(err)
	}
	if paths.IsContainer() {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("container PID file should not exist: %v", err)
		}
		return
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != fmt.Sprintf("%d\n", os.Getpid()) {
		t.Fatalf("PID file = %q, %v", data, err)
	}
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("PID file still exists: %v", err)
	}
	if err := Remove(path); err != nil {
		t.Fatalf("removing missing PID file: %v", err)
	}
}

func TestCheckMissingInvalidAndCurrentPID(t *testing.T) {
	if paths.IsContainer() {
		t.Skip("PID files are disabled in containers")
	}
	path := filepath.Join(t.TempDir(), "server.pid")
	if pid, running, err := Check(path); err != nil || running || pid != 0 {
		t.Fatalf("missing Check() = %d, %t, %v", pid, running, err)
	}
	if err := os.WriteFile(path, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, running, err := Check(path); err != nil || running {
		t.Fatalf("invalid PID should be treated as stale: running=%t err=%v", running, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid PID file was not removed: %v", err)
	}
	if err := os.WriteFile(path, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, running, err := Check(path); err != nil || running {
		t.Fatalf("invalid PID should be treated as stale: running=%t err=%v", running, err)
	}
}

func TestPIDOperationsOutsideContainer(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested", "server.pid")
	if pid, running, err := checkPIDFile(path); err != nil || running || pid != 0 {
		t.Fatalf("missing PID check = %d, %t, %v", pid, running, err)
	}
	if err := writePIDFile(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != fmt.Sprintf("%d\n", os.Getpid()) {
		t.Fatalf("written PID file = %q, %v", data, err)
	}
	if pid, running, err := checkPIDFile(path); err != nil || pid != os.Getpid() || running {
		t.Fatalf("test process is not a mail server: PID check = %d, %t, %v", pid, running, err)
	}
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, running, err := checkPIDFile(path); err != nil || running {
		t.Fatalf("invalid PID check = running %t, err %v", running, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid PID file was not removed: %v", err)
	}
	if _, _, err := checkPIDFile(root); err == nil {
		t.Fatal("reading a directory as a PID file did not return an error")
	}
	if err := writePIDFile(filepath.Join(root, "blocked", "mail.pid")); err != nil {
		t.Fatalf("writePIDFile could not create nested directory: %v", err)
	}
}
