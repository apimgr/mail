package server

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/paths"
)

func TestMailLogsReadSanitizedCurrentAndRotatedFiles(t *testing.T) {
	logDir := t.TempDir()
	current := filepath.Join(logDir, "server.log")
	if err := os.WriteFile(current, []byte("{\"time\":\"2026-11-27T12:00:00Z\",\"level\":\"INFO\",\"msg\":\"password=secret from 10.1.2.3 /srv/mail/private\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rotated, err := os.Create(filepath.Join(logDir, "server.log.2026-11-26.gz"))
	if err != nil {
		t.Fatal(err)
	}
	writer := gzip.NewWriter(rotated)
	if _, err := writer.Write([]byte("time=2026-11-26T12:00:00Z level=WARN msg=older token=adm_secret\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rotated.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	cfg.Server.Token = "adm_operator-test"
	srv := New(cfg, nil)
	srv.appPaths = &paths.Paths{Log: logDir}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/logs/server?limit=1", nil)
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var result mailLogListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Pagination.Total != 2 || result.Pagination.Page != 1 || result.Pagination.Limit != 1 {
		t.Fatalf("pagination = %+v", result.Pagination)
	}
	if len(result.Data) != 1 || result.Data[0].Level != "info" || result.Data[0].Timestamp != "2026-11-27T12:00:00Z" {
		t.Fatalf("first entry = %+v", result.Data)
	}
	for _, forbidden := range []string{"secret", "10.1.2.3", "/srv/mail/private"} {
		if strings.Contains(result.Data[0].Message, forbidden) {
			t.Errorf("returned message contains sensitive value %q: %s", forbidden, result.Data[0].Message)
		}
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/logs/server?level=WARN&q=older", nil)
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	response = httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "[redacted]") {
		t.Fatalf("filter response = %d %s", response.Code, response.Body.String())
	}
}

func TestMailLogsRequireOperatorAndValidateComponentAndPagination(t *testing.T) {
	logDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(logDir, "server.log"), []byte("entry\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Server.Token = "adm_operator-test"
	srv := New(cfg, nil)
	srv.appPaths = &paths.Paths{Log: logDir}
	for _, test := range []struct {
		path string
		code int
	}{
		{"/api/v1/logs/server", http.StatusUnauthorized},
		{"/api/v1/logs/invalid", http.StatusBadRequest},
		{"/api/v1/logs/server?limit=101", http.StatusBadRequest},
		{"/api/v1/logs/server?page=0", http.StatusBadRequest},
		{"/api/v1/logs/server?page=1&page=2", http.StatusBadRequest},
	} {
		t.Run(fmt.Sprintf("%s_%d", strings.ReplaceAll(test.path, "/", "_"), test.code), func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			if test.code != http.StatusUnauthorized {
				request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
			}
			response := httptest.NewRecorder()
			srv.Handler().ServeHTTP(response, request)
			if response.Code != test.code {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.code, response.Body.String())
			}
		})
	}
}

func TestMailLogSanitizerRedactsCredentialsForConfiguredTextFormats(t *testing.T) {
	for _, line := range []string{
		`time=2026-11-27T12:00:00Z level=INFO msg="request" api_key="secret value"`,
		`{"time":"2026-11-27T12:00:00Z","level":"ERROR","authorization":"Bearer secret"}`,
		`GET /health?access_token=abc123 HTTP/1.1`,
		`{"dsn":"postgres://dbadmin:db-password@db.internal/mail","database_url":"postgres://user:pass@host/db"}`,
	} {
		sanitized := sanitizeMailLogLine(line)
		for _, secret := range []string{"secret value", "Bearer secret", "abc123", "dbadmin", "db-password", "db.internal", "user:pass", "host/db"} {
			if strings.Contains(sanitized, secret) {
				t.Errorf("sanitizeMailLogLine(%q) leaked %q: %s", line, secret, sanitized)
			}
		}
	}
	if got := truncateMailLogMessage(strings.Repeat("a", maxMailLogMessage+20)); len([]rune(got)) > maxMailLogMessage {
		t.Errorf("truncated message has %d runes", len([]rune(got)))
	}
}

func TestMailLogScanLimitRejectsRatherThanReturningPartialResults(t *testing.T) {
	logDir := t.TempDir()
	file, err := os.Create(filepath.Join(logDir, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxMailLogScanBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readMailLogEntries(logDir, "server", "", ""); !errors.Is(err, errMailLogLimit) {
		t.Fatalf("readMailLogEntries error = %v, want explicit scan limit", err)
	}

	cfg := &config.Config{}
	cfg.Server.Token = "adm_operator-test"
	srv := New(cfg, nil)
	srv.appPaths = &paths.Paths{Log: logDir}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/logs/server", nil)
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("handler status = %d, want %d: %s", response.Code, http.StatusRequestEntityTooLarge, response.Body.String())
	}
}
