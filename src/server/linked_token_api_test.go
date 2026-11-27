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

func TestLinkedTokenAPILifecycle(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_" + strings.Repeat("a", 32)
	cfg.Server.Security.EncryptionKey = base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	cfg.Server.RateLimit.Strict = config.RateLimitWindowConfig{Requests: 20, Window: 900}
	cfg.Server.Tokens.MaxLinked = 3
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
	primary := "own_1234567890abcdefghijklmnopqrstuv"
	if _, err := db.CreateOwnerPrimaryToken(t.Context(), hashOwnerToken(primary), primary[:12], primary[len(primary)-4:], "encrypted-primary"); err != nil {
		t.Fatal(err)
	}
	handler := New(cfg, db).Handler()
	var created map[string]any
	status, body := linkedTokenRequestCall(t, handler, http.MethodPost, "/api/v1/server/preferences/tokens", `{"name":"ci","expires":"30d"}`, primary)
	if status != http.StatusCreated {
		t.Fatalf("create linked token status/body = %d/%s", status, body)
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	data := created["data"].(map[string]any)
	linkedToken, ok := data["linked_token"].(string)
	if !ok || !validOwnerToken(linkedToken) {
		t.Fatalf("create response did not return a linked token: %s", body)
	}
	id, ok := data["id"].(string)
	if !ok || id == "" {
		t.Fatalf("create response did not return an id: %s", body)
	}
	status, body = linkedTokenRequestCall(t, handler, http.MethodGet, "/api/v1/server/preferences/tokens", "", primary)
	if status != http.StatusOK || strings.Contains(body, linkedToken) || !strings.Contains(body, "token_masked") {
		t.Fatalf("list status/body leaked or omitted masked token: %d/%s", status, body)
	}
	status, body = linkedTokenRequestCall(t, handler, http.MethodGet, "/api/v1/server/preferences/tokens", "", linkedToken)
	if status != http.StatusForbidden {
		t.Fatalf("linked token management status = %d, want 403 (%s)", status, body)
	}
	status, body = linkedTokenRequestCall(t, handler, http.MethodPost, "/api/v1/server/preferences/tokens/"+id+"/reveal", "", primary)
	if status != http.StatusOK || !strings.Contains(body, linkedToken) {
		t.Fatalf("reveal status/body = %d/%s", status, body)
	}
	status, body = linkedTokenRequestCall(t, handler, http.MethodPost, "/api/v1/server/preferences/tokens/"+id+"/rotate", "", primary)
	if status != http.StatusOK {
		t.Fatalf("rotate status/body = %d/%s", status, body)
	}
	var rotated map[string]any
	if err := json.Unmarshal([]byte(body), &rotated); err != nil {
		t.Fatal(err)
	}
	rotatedData := rotated["data"].(map[string]any)
	rotatedToken := rotatedData["linked_token"].(string)
	if rotatedToken == linkedToken || rotatedData["id"] != id {
		t.Fatalf("rotation did not replace the credential in place: %s", body)
	}
	status, body = linkedTokenRequestCall(t, handler, http.MethodPatch, "/api/v1/server/preferences/tokens/"+id, `{"name":"ci-renamed","expires_on":"2099-01-31"}`, primary)
	if status != http.StatusOK || !strings.Contains(body, "ci-renamed") || !strings.Contains(body, "2099-02-01T00:00:00Z") {
		t.Fatalf("update status/body = %d/%s", status, body)
	}
	status, body = linkedTokenRequestCall(t, handler, http.MethodDelete, "/api/v1/server/preferences/tokens/"+id, "", primary)
	if status != http.StatusOK || !strings.Contains(body, `"status":"revoked"`) {
		t.Fatalf("revoke status/body = %d/%s", status, body)
	}
	status, body = linkedTokenRequestCall(t, handler, http.MethodPost, "/api/v1/server/preferences/tokens/"+id+"/reveal", "", primary)
	if status != http.StatusNotFound {
		t.Fatalf("reveal revoked token status = %d, want 404 (%s)", status, body)
	}
}

func linkedTokenRequestCall(t *testing.T, handler http.Handler, method, path, body, token string) (int, string) {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.RemoteAddr = "192.0.2.90:1234"
	request.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response.Code, response.Body.String()
}
