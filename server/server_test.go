package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/apimgr/mail/config"
)

func TestHealthAboutVersionRoutesAndSecurityHeaders(t *testing.T) {
	handler := New(&config.Config{}, nil).router
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/healthz", http.StatusServiceUnavailable},
		{"/server/healthz", http.StatusServiceUnavailable},
		{"/api/v1/server/healthz", http.StatusServiceUnavailable},
		{"/api/v1/server/about", http.StatusOK},
		{"/api/v1/server/version", http.StatusOK},
	} {
		t.Run(tc.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
			if recorder.Header().Get("X-Content-Type-Options") != "nosniff" || recorder.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("security headers are missing")
			}
			var body map[string]interface{}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("response is not JSON: %v", err)
			}
			if tc.path == "/healthz" && body["status"] != "unhealthy" {
				t.Fatalf("health status = %v", body["status"])
			}
		})
	}
}

func TestPathSecurityMiddlewareRejectsTraversalAndCleansPaths(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := pathSecurityMiddleware(next)
	for _, target := range []string{"/../secret", "/%2e%2e/secret"} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL = &url.URL{Path: target, RawPath: target}
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("path %q status = %d", target, recorder.Code)
		}
	}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/safe/../healthz", nil)
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("untrusted traversal path status = %d", recorder.Code)
	}
}

func TestServerLifecycleAndListener(t *testing.T) {
	s := New(&config.Config{Server: config.ServerConfig{Address: "127.0.0.1", Port: 0}}, nil)
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown before start: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	listener, err := CreateListener("127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := listener.Addr().(*net.TCPAddr); !ok {
		t.Fatalf("listener address has type %T", listener.Addr())
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
}
