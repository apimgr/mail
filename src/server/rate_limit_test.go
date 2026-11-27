package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/mail/src/config"
)

func TestRateLimiterUsesTwoWindowCounter(t *testing.T) {
	limiter := newRateLimiter(nil)
	now := time.Unix(1_700_000_000, 0).UTC()
	for attempt := 1; attempt <= 3; attempt++ {
		if allowed, _ := limiter.allow("ip:192.0.2.1:strict", 3, 60, now); !allowed {
			t.Fatalf("request %d was unexpectedly limited", attempt)
		}
	}
	if allowed, retry := limiter.allow("ip:192.0.2.1:strict", 3, 60, now); allowed || retry != 90 {
		t.Fatalf("limit result = allowed %v, retry %d; want denied, 90", allowed, retry)
	}
	if allowed, _ := limiter.allow("ip:192.0.2.1:strict", 3, 60, now.Add(2*time.Minute)); !allowed {
		t.Fatal("counter did not recover after two windows")
	}
}

func TestRateLimitMiddlewareAppliesReadWriteHealthAndGlobalClasses(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.RateLimit.Read = config.RateLimitWindowConfig{Requests: 1, Window: 60}
	cfg.Server.RateLimit.Write = config.RateLimitWindowConfig{Requests: 1, Window: 60}
	cfg.Server.RateLimit.Health = config.RateLimitWindowConfig{Requests: 1, Window: 60}
	cfg.Server.RateLimit.GlobalBurst = 10
	handler := New(cfg, nil).Handler()
	request := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = "192.0.2.10:1234"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response
	}
	if response := request(http.MethodGet, "/api/v1/server/about"); response.Code != http.StatusOK {
		t.Fatalf("first read status = %d", response.Code)
	}
	if response := request(http.MethodGet, "/api/v1/server/about"); response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("second read status/header = %d/%q", response.Code, response.Header().Get("Retry-After"))
	}
	if response := request(http.MethodGet, "/api/v1/server/healthz"); response.Code == http.StatusTooManyRequests {
		t.Fatalf("health route shared the read bucket: %d", response.Code)
	}
	if response := request(http.MethodGet, "/api/v1/server/healthz"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("second health request status = %d", response.Code)
	}
	if response := request(http.MethodPost, "/api/v1/server/preferences"); response.Code == http.StatusTooManyRequests {
		t.Fatalf("first write shared the read/health bucket: %d", response.Code)
	}
	if response := request(http.MethodPost, "/api/v1/server/preferences"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("second write status = %d", response.Code)
	}
}

func TestRateLimitMiddlewareKeysValidTokensAndLocksOutFailedAuthentication(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_" + strings.Repeat("a", 32)
	cfg.Server.RateLimit.Read = config.RateLimitWindowConfig{Requests: 1, Window: 60}
	cfg.Server.RateLimit.GlobalBurst = 100
	handler := New(cfg, nil).Handler()
	request := func(ip, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/services", nil)
		r.RemoteAddr = ip + ":1234"
		r.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response
	}
	if response := request("192.0.2.11", cfg.Server.Token); response.Code != http.StatusOK {
		t.Fatalf("first valid-token request status = %d", response.Code)
	}
	if response := request("192.0.2.12", cfg.Server.Token); response.Code != http.StatusTooManyRequests {
		t.Fatalf("valid token did not retain its rate-limit identity across IPs: %d", response.Code)
	}

	cfg = &config.Config{}
	cfg.Server.Token = "adm_" + strings.Repeat("b", 32)
	cfg.Server.RateLimit.AuthFailures = config.RateLimitWindowConfig{Requests: 2, Window: 60}
	cfg.Server.RateLimit.GlobalBurst = 100
	handler = New(cfg, nil).Handler()
	for attempt := 1; attempt <= 2; attempt++ {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/services", nil)
		r.RemoteAddr = "192.0.2.20:1234"
		r.Header.Set("Authorization", "Bearer invalid")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("failed-auth request %d status = %d", attempt, response.Code)
		}
	}
	for _, token := range []string{"invalid", cfg.Server.Token} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/services", nil)
		r.RemoteAddr = "192.0.2.20:1234"
		r.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		if response.Code != http.StatusTooManyRequests {
			t.Fatalf("locked-out request with token %q status = %d", token, response.Code)
		}
	}
}
