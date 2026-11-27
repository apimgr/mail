package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/mail/src/config"
)

func TestServiceDiscoveryAPIRequiresOperatorAndListsSupportedComponents(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_" + strings.Repeat("a", 32)
	handler := New(cfg, nil).Handler()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/services", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated service list status = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/services", nil)
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"postfix"`) || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("service list response status/headers/body = %d/%v/%s", response.Code, response.Header(), response.Body.String())
	}
}

func TestServiceDiscoveryAPIReturnsKnownServiceAndNotFound(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_" + strings.Repeat("b", 32)
	handler := New(cfg, nil).Handler()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/services/postfix", nil)
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"postfix"`) {
		t.Fatalf("known service response status/body = %d/%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/services/not-a-mail-service", nil)
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"error":"NOT_FOUND"`) {
		t.Fatalf("unknown service response status/body = %d/%s", response.Code, response.Body.String())
	}
}

func TestServiceRoutesRejectUnsupportedMethods(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_" + strings.Repeat("c", 32)
	handler := New(cfg, nil).Handler()
	for _, test := range []struct {
		method string
		path   string
		want   int
	}{
		{method: http.MethodPost, path: "/api/v1/services", want: http.StatusMethodNotAllowed},
		{method: http.MethodDelete, path: "/api/v1/services/postfix/operations", want: http.StatusMethodNotAllowed},
		{method: http.MethodPost, path: "/api/v1/services/postfix/restart", want: http.StatusNotFound},
	} {
		request := httptest.NewRequest(test.method, test.path, nil)
		request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Errorf("%s %s status = %d, want %d", test.method, test.path, response.Code, test.want)
		}
	}
}

func TestServiceOperationValidatesNamedRequest(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_" + strings.Repeat("e", 32)
	handler := New(cfg, nil).Handler()
	unauthorized := httptest.NewRequest(http.MethodPost, "/api/v1/services/postfix/operations", strings.NewReader(`{"operation":"restart"}`))
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated service operation status = %d, want %d", unauthorizedResponse.Code, http.StatusUnauthorized)
	}
	for _, body := range []string{`{}`, `{"operation":"invalid"}`, `{"operation":"start","command":"/bin/true"}`, `{"operation":`} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/services/postfix/operations", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"error":"VALIDATION_FAILED"`) {
			t.Errorf("invalid service operation body %q status/body = %d/%s", body, response.Code, response.Body.String())
		}
	}
}

func TestMailHealthRequiresOperatorAndReturnsServiceSummary(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_" + strings.Repeat("d", 32)
	handler := New(cfg, nil).Handler()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated mail health status = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"summary"`) || !strings.Contains(response.Body.String(), `"services"`) || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("mail health response status/headers/body = %d/%v/%s", response.Code, response.Header(), response.Body.String())
	}
}

func TestMailHealthStatus(t *testing.T) {
	for _, test := range []struct {
		name    string
		summary mailHealthSummary
		want    string
	}{
		{name: "no installed components", want: "unavailable"},
		{name: "running", summary: mailHealthSummary{Installed: 1, Running: 1}, want: "healthy"},
		{name: "stopped", summary: mailHealthSummary{Installed: 1, Stopped: 1}, want: "degraded"},
		{name: "unknown", summary: mailHealthSummary{Installed: 1, Unknown: 1}, want: "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := mailHealthStatus(test.summary); got != test.want {
				t.Fatalf("mailHealthStatus(%+v) = %q, want %q", test.summary, got, test.want)
			}
		})
	}
}
