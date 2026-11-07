package logger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLevelAndParsing(t *testing.T) {
	for level, want := range map[Level]string{DebugLevel: "DEBUG", InfoLevel: "INFO", WarnLevel: "WARN", ErrorLevel: "ERROR", Level(99): "UNKNOWN"} {
		if got := level.String(); got != want {
			t.Errorf("Level(%d).String() = %q, want %q", level, got, want)
		}
	}
	for input, want := range map[string]Level{"debug": DebugLevel, "info": InfoLevel, "warn": WarnLevel, "warning": WarnLevel, "error": ErrorLevel, "unknown": InfoLevel} {
		if got := parseLevel(input); got != want {
			t.Errorf("parseLevel(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestNewWritesStructuredLogsAndCloses(t *testing.T) {
	logDir := t.TempDir()
	l, err := New(Config{Level: "debug", LogDir: logDir, Format: "json"})
	if err != nil {
		t.Fatal(err)
	}
	l.writeLog(InfoLevel, "message with \"quotes\"", map[string]interface{}{"request_id": "abc"})
	l.writeLog(DebugLevel, "debug entry", nil)
	l.writeLog(ErrorLevel, "failure", nil)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	for file, text := range map[string]string{"server.log": "message with", "debug.log": "debug entry", "error.log": "failure"} {
		data, err := os.ReadFile(filepath.Join(logDir, file))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), text) {
			t.Errorf("%s does not contain %q: %s", file, text, data)
		}
	}
	entry := l.formatJSON("now", InfoLevel, "message with \"quotes\"", map[string]interface{}{"key": "value"})
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(entry), &parsed); err != nil || parsed["msg"] != "message with \"quotes\"" || parsed["key"] != "value" {
		t.Fatalf("invalid formatted JSON %q: %v", entry, err)
	}
}

func TestNewRejectsLogPathThatIsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{LogDir: path}); err == nil {
		t.Fatal("New accepted a regular file as a log directory")
	}
}

func TestAuditJSONEscapesValuesAndProtectsCoreFields(t *testing.T) {
	l, err := New(Config{Level: "info", LogDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	previous := global
	global = l
	Audit("user\"created", "admin\nactor", map[string]interface{}{"level": "forged", "count": 2})
	global = previous
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(l.logDir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	var entry map[string]interface{}
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("audit log is not valid JSON: %v: %s", err, data)
	}
	if entry["event"] != "user\"created" || entry["actor"] != "admin\nactor" || entry["count"] != float64(2) {
		t.Fatalf("unexpected audit entry: %#v", entry)
	}
}
