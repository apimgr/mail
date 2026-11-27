package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apimgr/mail/src/common/security"
	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
	"github.com/google/uuid"
)

func TestOperatorMailUserReadRoutes(t *testing.T) {
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
	publicID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(t.Context(), `INSERT INTO mail_mailboxes
		(public_id, domain_id, local_part, password, quota_bytes, enabled)
		VALUES (?, (SELECT id FROM mail_domains WHERE domain = 'example.test'), 'info', 'sensitive-hash', 4096, 1)`, publicID.String()); err != nil {
		t.Fatal(err)
	}
	handler := New(cfg, db).Handler()
	request := func(target string, authorized bool) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if authorized {
			req.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
		}
		handler.ServeHTTP(response, req)
		return response
	}
	if response := request("/api/v1/users", false); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized list status = %d", response.Code)
	}
	listed := request("/api/v1/users?page=1&limit=1", true)
	if listed.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", listed.Code, listed.Body.String())
	}
	if strings.Contains(listed.Body.String(), "sensitive-hash") || !strings.Contains(listed.Body.String(), `"email":"info@example.test"`) || !strings.Contains(listed.Body.String(), `"quota_bytes":4096`) || !strings.Contains(listed.Body.String(), publicID.String()) {
		t.Fatalf("mail user list leaks a password or omits public data: %s", listed.Body.String())
	}
	if response := request("/api/v1/users?page=0", true); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid pagination status = %d", response.Code)
	}
	got := request("/api/v1/users/"+publicID.String(), true)
	if got.Code != http.StatusOK {
		t.Fatalf("get status = %d: %s", got.Code, got.Body.String())
	}
	var payload struct {
		Data mailUserResponse `json:"data"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.ID != publicID.String() || payload.Data.Email != "info@example.test" || payload.Data.QuotaBytes != 4096 || !payload.Data.Enabled {
		t.Fatalf("mail user detail = %#v", payload.Data)
	}
	if response := request("/api/v1/users/not-found", true); response.Code != http.StatusNotFound {
		t.Fatalf("missing user status = %d", response.Code)
	}
}

func TestOperatorVirtualMailUserMutationRoutes(t *testing.T) {
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
	if response := request(http.MethodPost, "/api/v1/users", `{"email":"info@example.test","password":"correct horse battery staple"}`, false); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized create status = %d", response.Code)
	}
	if response := request(http.MethodPost, "/api/v1/users", `{"email":"info@example.test","password":"short"}`, true); response.Code != http.StatusBadRequest {
		t.Fatalf("short-password status = %d", response.Code)
	}
	if response := request(http.MethodPost, "/api/v1/users", `{"email":"info@example.test","password":"correct horse battery staple","other":true}`, true); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown-field status = %d", response.Code)
	}
	created := request(http.MethodPost, "/api/v1/users", `{"email":"Info@EXAMPLE.test","password":"correct horse battery staple","quota_bytes":4096}`, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), "correct horse battery staple") || strings.Contains(created.Body.String(), "$argon2id$") {
		t.Fatalf("create response disclosed password material: %s", created.Body.String())
	}
	var createdPayload struct {
		Data mailUserResponse `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdPayload); err != nil {
		t.Fatal(err)
	}
	user := createdPayload.Data
	if user.Kind != "virtual" || user.Email != "Info@example.test" || user.QuotaBytes != 4096 || !user.Enabled {
		t.Fatalf("created mail user = %#v", user)
	}
	var passwordHash string
	if err := db.UsersReadDB.QueryRowContext(t.Context(), `SELECT password FROM mail_mailboxes WHERE public_id = ?`, user.ID).Scan(&passwordHash); err != nil {
		t.Fatal(err)
	}
	if !security.VerifyPassword(passwordHash, "correct horse battery staple") {
		t.Fatal("stored mailbox password did not verify")
	}
	if response := request(http.MethodPost, "/api/v1/users", `{"email":"Info@example.test","password":"correct horse battery staple"}`, true); response.Code != http.StatusConflict {
		t.Fatalf("duplicate user status = %d", response.Code)
	}
	updated := request(http.MethodPut, "/api/v1/users/"+user.ID, `{"enabled":false}`, true)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"enabled":false`) || !strings.Contains(updated.Body.String(), `"quota_bytes":4096`) {
		t.Fatalf("partial update status/body = %d: %s", updated.Code, updated.Body.String())
	}
	if response := request(http.MethodPut, "/api/v1/users/"+user.ID, `{}`, true); response.Code != http.StatusBadRequest {
		t.Fatalf("empty update status = %d", response.Code)
	}
	if response := request(http.MethodPut, "/api/v1/users/"+user.ID, `{"quota_bytes":-1}`, true); response.Code != http.StatusBadRequest {
		t.Fatalf("negative quota status = %d", response.Code)
	}
	changed := request(http.MethodPost, "/api/v1/users/"+user.ID+"/password", `{"password":"a brand new password"}`, true)
	if changed.Code != http.StatusOK || strings.Contains(changed.Body.String(), "$argon2id$") {
		t.Fatalf("password change response = %d: %s", changed.Code, changed.Body.String())
	}
	if err := db.UsersReadDB.QueryRowContext(t.Context(), `SELECT password FROM mail_mailboxes WHERE public_id = ?`, user.ID).Scan(&passwordHash); err != nil {
		t.Fatal(err)
	}
	if security.VerifyPassword(passwordHash, "correct horse battery staple") || !security.VerifyPassword(passwordHash, "a brand new password") {
		t.Fatal("password change did not replace the verifier")
	}
	var details string
	if err := db.ServerReadDB.QueryRowContext(t.Context(), `SELECT details FROM audit_log WHERE action = 'user.password_changed' ORDER BY id DESC LIMIT 1`).Scan(&details); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(details, "brand new password") || strings.Contains(details, passwordHash) {
		t.Fatalf("password audit record disclosed credential material: %s", details)
	}
	var internalID int64
	if err := db.UsersReadDB.QueryRowContext(t.Context(), `SELECT id FROM mail_mailboxes WHERE public_id = ?`, user.ID).Scan(&internalID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(t.Context(), `INSERT INTO mail_forwards (mailbox_id, forward_to) VALUES (?, ?)`, internalID, "elsewhere@example.test"); err != nil {
		t.Fatal(err)
	}
	if response := request(http.MethodDelete, "/api/v1/users/"+user.ID, "", true); response.Code != http.StatusConflict {
		t.Fatalf("dependent user delete status = %d", response.Code)
	}
	if _, err := db.UsersDB.ExecContext(t.Context(), `DELETE FROM mail_forwards WHERE mailbox_id = ?`, internalID); err != nil {
		t.Fatal(err)
	}
	if response := request(http.MethodDelete, "/api/v1/users/"+user.ID, "", true); response.Code != http.StatusOK {
		t.Fatalf("delete status = %d: %s", response.Code, response.Body.String())
	}
}

func TestOperatorMailUserCSVImportRoute(t *testing.T) {
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
	csvBody := "email,password,quota_bytes,enabled\nfirst@example.test,correct horse battery staple,4096,true\nsecond@example.test,short,0,true\nthird@example.test,another secure password,0,false\n"
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/import", strings.NewReader(csvBody))
	request.Header.Set("Content-Type", "text/csv")
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("import status = %d: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "correct horse battery staple") || strings.Contains(response.Body.String(), "another secure password") || !strings.Contains(response.Body.String(), `"created":2`) || !strings.Contains(response.Body.String(), `"failed":1`) {
		t.Fatalf("import results disclose credentials or have wrong counts: %s", response.Body.String())
	}
	var count int
	if err := db.UsersReadDB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM mail_mailboxes`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("imported mailbox count = %d, err=%v", count, err)
	}
	unauthorized := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/users/import", strings.NewReader(csvBody))
	request.Header.Set("Content-Type", "text/csv")
	handler.ServeHTTP(unauthorized, request)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized import status = %d", unauthorized.Code)
	}
	badHeader := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/users/import", strings.NewReader("email,password\n"))
	request.Header.Set("Content-Type", "text/csv")
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	handler.ServeHTTP(badHeader, request)
	if badHeader.Code != http.StatusBadRequest {
		t.Fatalf("invalid CSV header status = %d", badHeader.Code)
	}
	tooManyRows := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/users/import", strings.NewReader("email,password,quota_bytes,enabled\n"+strings.Repeat("new@example.test,secure password,0,true\n", 101)))
	request.Header.Set("Content-Type", "text/csv")
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	handler.ServeHTTP(tooManyRows, request)
	if tooManyRows.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too many CSV rows status = %d", tooManyRows.Code)
	}
	oversized := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/users/import", strings.NewReader("email,password,quota_bytes,enabled\n"+strings.Repeat("x", 1<<20)))
	request.Header.Set("Content-Type", "text/csv")
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	handler.ServeHTTP(oversized, request)
	if oversized.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized CSV status = %d", oversized.Code)
	}
	if err := db.UsersReadDB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM mail_mailboxes`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("rejected CSV batches created mailboxes: count=%d err=%v", count, err)
	}
}
