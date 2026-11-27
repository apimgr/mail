package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSuccessfulHealthCheckSuppression(t *testing.T) {
	tests := []struct {
		path   string
		status int
		want   bool
	}{
		{path: "/server/healthz", status: http.StatusOK, want: true},
		{path: "/api/v1/server/healthz", status: http.StatusNoContent, want: true},
		{path: "/server/healthz.txt", status: http.StatusOK, want: true},
		{path: "/healthz", status: http.StatusServiceUnavailable, want: false},
		{path: "/server/healthz", status: http.StatusUnauthorized, want: false},
		{path: "/server/about", status: http.StatusOK, want: false},
	}
	for _, test := range tests {
		t.Run(test.path+"/"+http.StatusText(test.status), func(t *testing.T) {
			if got := isSuccessfulHealthCheck(test.path, test.status); got != test.want {
				t.Fatalf("isSuccessfulHealthCheck(%q, %d) = %v, want %v", test.path, test.status, got, test.want)
			}
		})
	}
}

func TestRequestLoggingMiddlewarePreservesResponse(t *testing.T) {
	handler := (&Server{}).requestLoggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	}))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/items?token=secret", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || response.Body.String() != "created" {
		t.Fatalf("response = %d %q, want 201 created", response.Code, response.Body.String())
	}
}
