package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
	"github.com/google/uuid"
)

func TestOperatorMailAliasLifecycleRoutes(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_test-token"
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
	if _, err := db.CreateMailDomain(t.Context(), database.MailDomainInput{Domain: "example.test", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	handler := New(cfg, db).Handler()
	request := func(method, target, body string, authorized bool) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if authorized {
			req.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		handler.ServeHTTP(response, req)
		return response
	}
	if response := request(http.MethodGet, "/api/v1/aliases", "", false); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized alias list status = %d", response.Code)
	}
	if response := request(http.MethodPost, "/api/v1/aliases", `{"source":"info@example.test","destination":"ops@example.test","unexpected":true}`, true); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d: %s", response.Code, response.Body.String())
	}
	if response := request(http.MethodPost, "/api/v1/aliases", `{"source":"invalid","destination":"ops@example.test"}`, true); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid address status = %d", response.Code)
	}
	longLocalPart := `{"source":"` + strings.Repeat("a", 65) + `@example.test","destination":"ops@example.test"}`
	if response := request(http.MethodPost, "/api/v1/aliases", longLocalPart, true); response.Code != http.StatusBadRequest {
		t.Fatalf("overlong local-part status = %d", response.Code)
	}
	if response := request(http.MethodPost, "/api/v1/aliases", `{"source":"info@missing.test","destination":"ops@example.test"}`, true); response.Code != http.StatusBadRequest {
		t.Fatalf("missing source domain status = %d", response.Code)
	}
	created := request(http.MethodPost, "/api/v1/aliases", `{"source":"info@Exämple.test","destination":"ops@example.test"}`, true)
	if created.Code != http.StatusBadRequest {
		t.Fatalf("missing IDN domain status = %d: %s", created.Code, created.Body.String())
	}
	if _, err := db.CreateMailDomain(t.Context(), database.MailDomainInput{Domain: "xn--exmple-cua.test", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	created = request(http.MethodPost, "/api/v1/aliases", `{"source":"info@Exämple.test","destination":"ops@example.test"}`, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create alias status = %d: %s", created.Code, created.Body.String())
	}
	var payload struct {
		Data mailAliasResponse `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	parsedID, err := uuid.Parse(payload.Data.ID)
	if err != nil || parsedID.Version() != 7 || payload.Data.Source != "info@xn--exmple-cua.test" || payload.Data.Domain != "xn--exmple-cua.test" {
		t.Fatalf("created alias = %#v, ID error=%v", payload.Data, err)
	}
	var auditCategory, auditAction, auditTargetType string
	if err := db.ServerReadDB.QueryRowContext(t.Context(), `SELECT category, action, target_type FROM audit_log WHERE target_id = ?`, payload.Data.ID).Scan(&auditCategory, &auditAction, &auditTargetType); err != nil || auditCategory != "mail" || auditAction != "alias.created" || auditTargetType != "mail_alias" {
		t.Fatalf("alias create audit = %q/%q/%q, err=%v", auditCategory, auditAction, auditTargetType, err)
	}
	listed := request(http.MethodGet, "/api/v1/aliases?page=1&limit=1", "", true)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"pagination":{"limit":1,"page":1,"pages":1,"total":1}`) || strings.Contains(listed.Body.String(), `"id":1`) {
		t.Fatalf("alias list status/body = %d: %s", listed.Code, listed.Body.String())
	}
	if response := request(http.MethodGet, "/api/v1/aliases?page=0", "", true); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid pagination status = %d", response.Code)
	}
	if response := request(http.MethodGet, "/api/v1/aliases?page=9223372036854775807", "", true); response.Code != http.StatusBadRequest {
		t.Fatalf("overflow pagination status = %d", response.Code)
	}
	if response := request(http.MethodDelete, "/api/v1/aliases/not-found", "", true); response.Code != http.StatusNotFound {
		t.Fatalf("missing alias delete status = %d", response.Code)
	}
	deleted := request(http.MethodDelete, "/api/v1/aliases/"+payload.Data.ID, "", true)
	if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body.String(), `"id":"`+payload.Data.ID+`"`) {
		t.Fatalf("delete alias status/body = %d: %s", deleted.Code, deleted.Body.String())
	}
	if err := db.ServerReadDB.QueryRowContext(t.Context(), `SELECT action FROM audit_log WHERE target_id = ? ORDER BY id DESC LIMIT 1`, payload.Data.ID).Scan(&auditAction); err != nil || auditAction != "alias.deleted" {
		t.Fatalf("alias delete audit action = %q, err=%v", auditAction, err)
	}
}
