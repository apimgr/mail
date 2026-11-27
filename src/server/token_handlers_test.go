package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
)

func TestIssuePrimaryTokenAPIAndBearerConflict(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_" + strings.Repeat("a", 32)
	cfg.Server.Security.EncryptionKey = base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	cfg.Server.RateLimit.Strict = config.RateLimitWindowConfig{Requests: 10, Window: 900}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "server.db")
	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	handler := New(cfg, db).Handler()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/server/preferences/tokens/primary", nil)
	request.RemoteAddr = "192.0.2.1:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("issue token status = %d, body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		OK   bool `json:"ok"`
		Data struct {
			Token string `json:"owner_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || !payload.OK || !validOwnerToken(payload.Data.Token) {
		t.Fatalf("issue token response = %#v, err=%v", payload, err)
	}
	var storedHash, prefix, last4, encrypted string
	if err := db.ServerDB.QueryRow("SELECT token_hash, token_prefix, token_last4, token_enc FROM api_tokens").Scan(&storedHash, &prefix, &last4, &encrypted); err != nil {
		t.Fatal(err)
	}
	if storedHash != hashOwnerToken(payload.Data.Token) || prefix != payload.Data.Token[:12] || last4 != payload.Data.Token[len(payload.Data.Token)-4:] {
		t.Fatal("persisted token metadata does not match the issued token")
	}
	decrypted, err := decryptOwnerToken(encrypted, cfg.Server.Security.EncryptionKey)
	if err != nil || decrypted != payload.Data.Token {
		t.Fatalf("stored token ciphertext did not round-trip: err=%v", err)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/server/preferences/tokens/primary", nil)
	request.RemoteAddr = "192.0.2.2:1234"
	request.Header.Set("Authorization", "Bearer "+payload.Data.Token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("valid owner bearer issue status = %d, want 409", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/server/preferences/tokens", nil)
	request.Header.Set("Authorization", "Bearer "+payload.Data.Token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), payload.Data.Token) || !strings.Contains(response.Body.String(), "token_masked") {
		t.Fatalf("token list status/body did not return masked metadata: %d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/server/preferences/tokens", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("token list without bearer status = %d, want 401", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/server/preferences/tokens/primary/reveal", nil)
	request.Header.Set("Authorization", "Bearer "+payload.Data.Token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), payload.Data.Token) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("primary reveal status/header/body = %d/%q/%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/server/preferences/tokens/primary", nil)
	request.RemoteAddr = "192.0.2.3:1234"
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("valid operator bearer issue status = %d, want 409", response.Code)
	}
}

func TestIssuePrimaryTokenRateLimitAndCookieIsolation(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_" + strings.Repeat("b", 32)
	cfg.Server.Security.EncryptionKey = base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	cfg.Server.RateLimit.Strict = config.RateLimitWindowConfig{Requests: 3, Window: 900}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "server.db")
	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	handler := New(cfg, db).Handler()
	for i := 0; i < 3; i++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/server/preferences/tokens/primary", nil)
		request.RemoteAddr = "192.0.2.9:1234"
		request.AddCookie(&http.Cookie{Name: "owner_token", Value: "ignored"})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("issue request %d status = %d", i+1, response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/server/preferences/tokens/primary", nil)
	request.RemoteAddr = "192.0.2.9:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("rate limited issue status/header = %d/%q", response.Code, response.Header().Get("Retry-After"))
	}
	var count int
	if err := db.ServerDB.QueryRow("SELECT count(*) FROM owners WHERE id != 'system'").Scan(&count); err != nil || count != 3 {
		t.Fatalf("cookie-authenticated API request changed owner count: %d (err=%v)", count, err)
	}
}
