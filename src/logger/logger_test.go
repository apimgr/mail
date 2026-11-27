package logger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	l, err := New(Config{Level: "debug", LogDir: logDir, Format: "json", Disabled: map[string]bool{"debug": false}})
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

func TestClosedLoggerDropsSubsequentWritesSafely(t *testing.T) {
	logger, err := New(Config{LogDir: t.TempDir(), Format: "json"})
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if logger.serverLog != nil || logger.errorLog != nil || logger.accessLog != nil || logger.auditLog != nil || logger.securityLog != nil || logger.debugLog != nil {
		t.Fatal("closed log file handles were retained")
	}
	logger.writeLog(ErrorLevel, "late shutdown event", nil)
}

func TestTextLogUsesSlogHandlerAndEscapesControlCharacters(t *testing.T) {
	logDir := t.TempDir()
	l, err := New(Config{Level: "info", LogDir: logDir, Format: "text"})
	if err != nil {
		t.Fatal(err)
	}
	l.writeLog(InfoLevel, "message", map[string]interface{}{"detail": "line one\nline two"})
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(logDir, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	line := string(data)
	if !strings.Contains(line, "level=INFO") || !strings.Contains(line, "msg=message") || !strings.Contains(line, `detail="line one\nline two"`) {
		t.Fatalf("text log is not structured and escaped: %q", line)
	}
	if strings.ContainsRune(strings.TrimSuffix(line, "\n"), '\n') || strings.ContainsRune(line, '\x1b') {
		t.Fatalf("text log contains a raw control character: %q", line)
	}
}

func TestNewRejectsLogPathThatIsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{LogDir: path}); err == nil {
		t.Fatal("New accepted a regular file as a log directory")
	}
}

func TestRotationAndRetention(t *testing.T) {
	logDir := t.TempDir()
	l, err := New(Config{Level: "info", LogDir: logDir, Policies: map[string]LogPolicy{
		"server": {Rotate: "daily", Keep: "1"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(logDir, "server.log"), old, old); err != nil {
		t.Fatal(err)
	}
	l.writeLog(InfoLevel, "after rotation", nil)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(filepath.Join(logDir, "server.log"))
	if err != nil || !strings.Contains(string(current), "after rotation") {
		t.Fatalf("new log does not contain entry after rotation: %q, %v", current, err)
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatal(err)
	}
	rotated := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "server.log.") {
			rotated++
		}
	}
	if rotated != 1 {
		t.Fatalf("rotated log count = %d, want 1 (entries: %v)", rotated, entries)
	}
}

func TestReopenSwitchesToCurrentPath(t *testing.T) {
	logDir := t.TempDir()
	l, err := New(Config{Level: "info", LogDir: logDir, Disabled: map[string]bool{"debug": false}})
	if err != nil {
		t.Fatal(err)
	}
	l.writeLog(InfoLevel, "before rename", nil)
	if err := os.Rename(filepath.Join(logDir, "server.log"), filepath.Join(logDir, "server.log.old")); err != nil {
		t.Fatal(err)
	}
	if err := l.Reopen(); err != nil {
		t.Fatal(err)
	}
	l.writeLog(InfoLevel, "after reopen", nil)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(filepath.Join(logDir, "server.log.old"))
	if err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(filepath.Join(logDir, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(old), "before rename") || strings.Contains(string(old), "after reopen") || !strings.Contains(string(current), "after reopen") {
		t.Fatalf("reopen did not switch handles (old=%q current=%q)", old, current)
	}
}

func TestCustomAccessFormatUsesConfiguredFields(t *testing.T) {
	logDir := t.TempDir()
	l, err := New(Config{
		Level: "info", LogDir: logDir,
		Formats: map[string]string{"access": "custom"},
		Custom:  map[string]string{"access": "{remote_ip} {method} {path} {status} {latency_ms} {request_id} {tls_version}"},
	})
	if err != nil {
		t.Fatal(err)
	}
	previous := global
	global = l
	AccessRequest(AccessRecord{
		IP: "192.0.2.1", Method: "GET", Path: "/server/about", Status: 200, Bytes: 17,
		Protocol: "HTTP/2.0", RequestID: "req-1", TLSVersion: "TLS 1.3", Latency: 25 * time.Millisecond,
		Time: time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC),
	})
	global = previous
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(logDir, "access.log"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(data)), "192.0.2.1 GET /server/about 200 25.000 req-1 TLS 1.3"; got != want {
		t.Fatalf("custom access log = %q, want %q", got, want)
	}
}

func TestSetLevelOpensAndClosesDebugLog(t *testing.T) {
	logDir := t.TempDir()
	l, err := New(Config{Level: "info", LogDir: logDir, Disabled: map[string]bool{"debug": false}})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.SetLevel(" DEBUG "); err != nil {
		t.Fatal(err)
	}
	l.writeLog(DebugLevel, "debug enabled", nil)
	if err := l.SetLevel("error"); err != nil {
		t.Fatal(err)
	}
	l.writeLog(DebugLevel, "debug disabled", nil)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(logDir, "debug.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "debug enabled") || strings.Contains(string(data), "debug disabled") {
		t.Fatalf("debug log has unexpected content: %s", data)
	}
	if err := l.SetLevel("verbose"); err == nil {
		t.Fatal("invalid level was accepted")
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
