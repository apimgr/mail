package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/mail/src/config"
)

func debugServer(t *testing.T) (*Server, string) {
	t.Helper()
	cfg := &config.Config{}
	if _, err := config.EnsureOperatorToken(t.TempDir(), cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Server.Debug = true
	return New(cfg, nil), cfg.Server.Token
}

func TestDebugRoutesAreAbsentUnlessEnabled(t *testing.T) {
	handler := New(&config.Config{}, nil).Handler()
	for _, route := range []string{"/debug/routes", "/debug/pprof/"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, route, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("disabled debug route %s status = %d, want 404", route, recorder.Code)
		}
	}
}

func TestDebugRoutesRequireOperatorBearerToken(t *testing.T) {
	server, token := debugServer(t)
	for _, test := range []struct {
		name       string
		headerName string
		header     string
		query      string
		want       string
	}{
		{name: "missing", want: `Bearer realm="mail"`},
		{name: "alternate header ignored", headerName: "X-API-Key", header: token, want: `Bearer realm="mail"`},
		{name: "query ignored", query: "?token=" + token, want: `Bearer realm="mail"`},
		{name: "invalid bearer", headerName: "Authorization", header: "Bearer invalid", want: `Bearer realm="mail", error="invalid_token"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/debug/config"+test.query, nil)
			if test.header != "" {
				request.Header.Set(test.headerName, test.header)
			}
			server.Handler().ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", recorder.Code)
			}
			if got := recorder.Header().Get("WWW-Authenticate"); got != test.want {
				t.Fatalf("WWW-Authenticate = %q, want %q", got, test.want)
			}
			if strings.Contains(recorder.Body.String(), token) || !strings.Contains(recorder.Body.String(), `"error":"UNAUTHORIZED"`) {
				t.Fatal("authentication error exposed a credential or used a noncanonical error code")
			}
		})
	}
}

func TestDebugDiagnosticsRequireAuthAndRedactCredentials(t *testing.T) {
	server, token := debugServer(t)
	for _, route := range []string{
		"/debug/vars", "/debug/config", "/debug/routes", "/debug/cache", "/debug/db",
		"/debug/scheduler", "/debug/memory", "/debug/goroutines", "/debug/pprof",
		"/debug/pprof/cmdline", "/debug/pprof/heap",
	} {
		t.Run(route, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, route, nil)
			request.Header.Set("Authorization", "Bearer "+token)
			server.Handler().ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), token) {
				t.Fatal("debug diagnostics exposed the operator token")
			}
		})
	}
	unauthorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	server.Handler().ServeHTTP(unauthorized, request)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated trailing-slash debug path status = %d, want 401", unauthorized.Code)
	}
	if unauthorized.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("debug authentication error omitted the baseline security headers")
	}
	redirect := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	server.Handler().ServeHTTP(redirect, request)
	if redirect.Code != http.StatusMovedPermanently || redirect.Header().Get("Location") != "/debug/pprof" {
		t.Fatalf("non-canonical pprof index response = %d, %q", redirect.Code, redirect.Header().Get("Location"))
	}

	recorder := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/debug/error500", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "text/html")
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "<html") {
		t.Fatalf("debug error page status/body = %d/%q", recorder.Code, recorder.Body.String())
	}
}

func TestDebugVarsReplaceProcessCommandLine(t *testing.T) {
	server, token := debugServer(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/debug/vars", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	server.Handler().ServeHTTP(recorder, request)
	var variables map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &variables); err != nil {
		t.Fatal(err)
	}
	commandLine, ok := variables["cmdline"]
	if !ok {
		t.Fatal("debug vars response omitted the sanitized cmdline value")
	}
	var args []string
	if err := json.Unmarshal(commandLine, &args); err != nil {
		t.Fatal(err)
	}
	if len(args) != 1 || strings.Contains(strings.Join(args, " "), "debug_test.go") {
		t.Fatalf("debug cmdline = %q, want only the binary name", args)
	}
}

func TestRequestLoggingRedactsSensitiveQueryValuesButPreservesHandlerURL(t *testing.T) {
	secretValue := "private-test-value"
	request := httptest.NewRequest(http.MethodGet, "/private?token="+secretValue+"&keep=yes&ACCESS_TOKEN=other-private-value", nil)
	redactedURL := redactedLogURL(request.URL).RequestURI()
	if strings.Contains(redactedURL, secretValue) || strings.Contains(redactedURL, "other-private-value") {
		t.Fatal("request URL sanitizer exposed a sensitive query value")
	}
	if !strings.Contains(redactedURL, "keep=yes") {
		t.Fatal("request URL sanitizer removed a non-sensitive query value")
	}
	handler := New(&config.Config{}, nil).requestLoggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("token"); got != secretValue {
			t.Errorf("handler token query = %q, want original query value", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), request)
}
