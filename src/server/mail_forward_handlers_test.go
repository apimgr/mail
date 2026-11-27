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

func TestOperatorMailForwardLifecycleRoutes(t *testing.T) {
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
	user, err := db.CreateMailUser(t.Context(), "info", "example.test", "$argon2id$test", 0, true)
	if err != nil {
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
	if response := request(http.MethodGet, "/api/v1/forwards", "", false); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized forward list status = %d", response.Code)
	}
	if response := request(http.MethodPost, "/api/v1/forwards", `{"source":"info@example.test","destinations":[]}`, true); response.Code != http.StatusBadRequest {
		t.Fatalf("empty destinations status = %d", response.Code)
	}
	created := request(http.MethodPost, "/api/v1/forwards", `{"source":"info@example.test","destinations":["ops@elsewhere.test","\"alerts,ops\"@example.test"],"keep_local":false}`, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create forward status = %d: %s", created.Code, created.Body.String())
	}
	var payload struct {
		Data mailForwardResponse `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	forward := payload.Data
	parsedID, err := uuid.Parse(forward.ID)
	if err != nil || parsedID.Version() != 7 || forward.Source != user.Email || len(forward.Destinations) != 2 || forward.Destinations[1] != `"alerts,ops"@example.test` || forward.KeepLocal || !forward.Enabled {
		t.Fatalf("created forward = %#v, ID error=%v", forward, err)
	}
	var ownerType, ownerID string
	if err := db.UsersReadDB.QueryRowContext(t.Context(), `SELECT owner_type, owner_id FROM mail_forwards WHERE public_id = ?`, forward.ID).Scan(&ownerType, &ownerID); err != nil || ownerType != "system" || ownerID != "system" {
		t.Fatalf("forward owner = %q/%q, err=%v", ownerType, ownerID, err)
	}
	listed := request(http.MethodGet, "/api/v1/forwards?page=1&limit=1", "", true)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"pagination":{"page":1,"limit":1,"total":1,"pages":1}`) {
		t.Fatalf("list forwards status/body = %d: %s", listed.Code, listed.Body.String())
	}
	if response := request(http.MethodDelete, "/api/v1/forwards/not-found", "", true); response.Code != http.StatusNotFound {
		t.Fatalf("missing forward delete status = %d: %s", response.Code, response.Body.String())
	}
	deleted := request(http.MethodDelete, "/api/v1/forwards/"+forward.ID, "", true)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete forward status = %d: %s", deleted.Code, deleted.Body.String())
	}
	var auditTargetType, auditAction string
	if err := db.ServerReadDB.QueryRowContext(t.Context(), `SELECT target_type, action FROM audit_log WHERE target_id = ? ORDER BY id DESC LIMIT 1`, forward.ID).Scan(&auditTargetType, &auditAction); err != nil || auditTargetType != "mail_forward" || auditAction != "forward.deleted" {
		t.Fatalf("forward delete audit = %q/%q, err=%v", auditTargetType, auditAction, err)
	}
}
