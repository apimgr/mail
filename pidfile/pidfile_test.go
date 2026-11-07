package pidfile

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestWriteCheckAndRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "server.pid")
	if err := Write(path); err != nil {
		t.Fatal(err)
	}
	pid, running, err := Check(path)
	if err != nil || !running || pid != os.Getpid() {
		t.Fatalf("Check() = %d, %t, %v", pid, running, err)
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
	path := filepath.Join(t.TempDir(), "server.pid")
	if pid, running, err := Check(path); err != nil || running || pid != 0 {
		t.Fatalf("missing Check() = %d, %t, %v", pid, running, err)
	}
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Check(path); err == nil {
		t.Fatal("Check accepted an invalid PID")
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	if _, running, err := Check(path); err != nil || !running {
		t.Fatalf("current PID was not detected: running=%t err=%v", running, err)
	}
}
