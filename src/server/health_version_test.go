package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonformat "github.com/apimgr/mail/src/common/format"
	"github.com/apimgr/mail/src/config"
)

func TestHealthResponseHasCanonicalFieldsAndPublicSafeValues(t *testing.T) {
	server := New(&config.Config{}, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/healthz", nil)
	request.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	server.router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	body := recorder.Body.String()
	keys := []string{`"project"`, `"status"`, `"version"`, `"go_version"`, `"build"`, `"uptime"`, `"mode"`, `"timestamp"`, `"features"`, `"checks"`, `"stats"`}
	last := -1
	for _, key := range keys {
		index := strings.Index(body, key)
		if index <= last {
			t.Fatalf("field %s missing or out of canonical order in %s", key, body)
		}
		last = index
	}
	var payload healthResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if payload.Project.Name != "mail" || payload.Status != "unhealthy" || payload.Checks.Database != "error" {
		t.Fatalf("unexpected health values: %#v", payload)
	}
	if payload.Version == "" || payload.GoVersion == "" || payload.Timestamp.Location() != time.UTC {
		t.Fatalf("version/runtime/timestamp fields are incomplete: %#v", payload)
	}
	for _, forbidden := range []string{"password", "token", "smtp", "/root/", "database/sql"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("health response contains forbidden detail %q", forbidden)
		}
	}
}

func TestHealthAndVersionUseEmbeddedBuildMetadata(t *testing.T) {
	previous := getBuildMetadata()
	t.Cleanup(func() {
		SetBuildMetadata(previous.version, previous.commit, previous.date)
	})
	SetBuildMetadata("1.2.3", "0123456789abcdef", "2026-11-27T15:04:05Z")

	server := New(&config.Config{}, nil)
	health := server.buildHealthResponse(httptest.NewRequest(http.MethodGet, "/api/v1/server/healthz", nil))
	if health.Version != "1.2.3" || health.Build.Commit != "0123456" || health.Build.Date != "2026-11-27T15:04:05Z" {
		t.Fatalf("health build metadata = version %q, commit %q, date %q", health.Version, health.Build.Commit, health.Build.Date)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/version", nil)
	recorder := httptest.NewRecorder()
	server.handleVersion(recorder, request)
	var payload struct {
		Version   string `json:"version"`
		Commit    string `json:"commit"`
		BuildDate string `json:"build_date"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode version response: %v", err)
	}
	if payload.Version != health.Version || payload.Commit != health.Build.Commit || payload.BuildDate != health.Build.Date {
		t.Fatalf("version response = %#v; health metadata = %#v", payload, health)
	}
}

func TestHealthAPIContentNegotiationAndTextOrder(t *testing.T) {
	server := New(&config.Config{}, nil)
	for _, test := range []struct {
		name        string
		accept      string
		userAgent   string
		contentType string
	}{
		{name: "explicit json", accept: "application/json", userAgent: "curl/8.0", contentType: "application/json; charset=utf-8"},
		{name: "explicit text", accept: "text/plain", userAgent: "Mozilla/5.0", contentType: "text/plain; charset=utf-8"},
		{name: "unsupported explicit accept beats curl detection", accept: "text/html", userAgent: "curl/8.0", contentType: "application/json; charset=utf-8"},
		{name: "curl defaults to text", userAgent: "curl/8.0", contentType: "text/plain; charset=utf-8"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/server/healthz", nil)
			request.Header.Set("Accept", test.accept)
			request.Header.Set("User-Agent", test.userAgent)
			recorder := httptest.NewRecorder()
			server.router.ServeHTTP(recorder, request)
			if contentType := recorder.Header().Get("Content-Type"); contentType != test.contentType {
				t.Fatalf("content type = %q, want %q", contentType, test.contentType)
			}
			if test.contentType == "text/plain; charset=utf-8" {
				body := recorder.Body.String()
				if !strings.HasPrefix(body, "project.name: mail\n") || strings.Index(body, "checks.database:") < strings.Index(body, "timestamp:") {
					t.Fatalf("API text fields are missing or out of order: %s", body)
				}
			}
		})
	}
}

func TestHealthFrontendDefaultsToHTMLForBrowsersAndUnknownAccept(t *testing.T) {
	server := New(&config.Config{}, nil)
	for _, test := range []struct {
		name      string
		accept    string
		userAgent string
	}{
		{name: "browser default", userAgent: "Mozilla/5.0"},
		{name: "unsupported accept", accept: "application/xml", userAgent: "curl/8.0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/server/healthz", nil)
			request.Header.Set("Accept", test.accept)
			request.Header.Set("User-Agent", test.userAgent)
			recorder := httptest.NewRecorder()
			server.router.ServeHTTP(recorder, request)
			if recorder.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatalf("content type = %q, want HTML", recorder.Header().Get("Content-Type"))
			}
		})
	}
}

func TestHealthFormatsAndUptime(t *testing.T) {
	if got := compactUptime(45 * time.Second); got != "45s" {
		t.Fatalf("compactUptime(45s) = %q", got)
	}
	if got := compactUptime(2*time.Hour + 16*time.Minute); got != "2h 16m" {
		t.Fatalf("compactUptime(2h16m) = %q", got)
	}
	if got := compactUptime(24*time.Hour + time.Hour + 4*time.Minute); got != "1d 1h 4m" {
		t.Fatalf("compactUptime(1d1h4m) = %q", got)
	}
	if got := commonformat.Count("en", 1234567); got != "1,234,567" {
		t.Fatalf("Count(en) = %q", got)
	}
	if got := commonformat.Count("de", 1234567); got != "1.234.567" {
		t.Fatalf("Count(de) = %q", got)
	}
}

func TestHealthPageUsesRequiredLayoutAndFeatureFields(t *testing.T) {
	server := New(&config.Config{}, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/server/healthz", nil)
	request.Header.Set("Accept", "text/html")
	server.router.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	if !strings.Contains(body, "<title>Mail - Health Status</title>") || strings.Count(body, "<h1") != 1 {
		t.Fatalf("health page title/heading contract failed")
	}
	for _, label := range []string{"Version and build", "Component status", "Server statistics", "Tor", "I2P", "GeoIP", "health-checks"} {
		if !strings.Contains(body, label) {
			t.Errorf("health page is missing %q", label)
		}
	}
	if strings.Contains(body, ">0s</dd>") {
		t.Fatal("health page rendered machine-format uptime")
	}
}

func TestHealthFrontendTextUsesRequestedLocale(t *testing.T) {
	server := New(&config.Config{}, nil)
	request := httptest.NewRequest(http.MethodGet, "/server/healthz", nil)
	request.Header.Set("Accept", "text/plain")
	request.Header.Set("Accept-Language", "es")
	recorder := httptest.NewRecorder()
	server.router.ServeHTTP(recorder, request)
	if !strings.Contains(recorder.Body.String(), "Estado:") || !strings.Contains(recorder.Body.String(), "Base de datos:") {
		t.Fatalf("health text did not use Spanish labels: %s", recorder.Body.String())
	}
}

func TestVersionEndpointUsesSanitizedBuildMetadata(t *testing.T) {
	server := New(&config.Config{}, nil)
	recorder := httptest.NewRecorder()
	server.router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/server/version", nil))
	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode version response: %v", err)
	}
	if payload["version"] == "" || payload["go_version"] == "" || payload["platform"] == "" || payload["commit"] == "" || payload["build_date"] == "" {
		t.Fatalf("version response is missing required metadata: %#v", payload)
	}
	if strings.HasPrefix(payload["version"], "v") {
		t.Fatalf("version includes forbidden v prefix: %q", payload["version"])
	}
}
