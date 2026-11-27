package server

import (
	"context"
	"encoding/base64"
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

func TestOperatorMailDomainReadRoutesOmitOwnershipAndKeyMaterial(t *testing.T) {
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
	publicID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ServerDB.ExecContext(context.Background(), `INSERT INTO mail_domains
		(public_id, domain, description, owner_type, owner_id) VALUES (?, ?, ?, ?, ?)`, publicID.String(), "example.test", "Configured metadata", "system", "system"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ServerDB.ExecContext(context.Background(), `INSERT INTO mail_dkim_keys
		(domain_id, selector, private_key, public_key) VALUES (1, 'default', 'private-secret', 'public-record')`); err != nil {
		t.Fatal(err)
	}
	handler := New(cfg, db).Handler()
	for _, target := range []string{"/api/v1/domains", "/api/v1/domains/example.test"} {
		t.Run(target, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, target, nil)
			request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
			}
			if !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
				t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
			}
			var payload any
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			body := response.Body.String()
			if target == "/api/v1/domains" && !strings.Contains(body, `"pagination"`) {
				t.Errorf("domain list omits pagination metadata: %s", body)
			}
			for _, forbidden := range []string{"owner_id", "owner_type", "private-secret", "private_key"} {
				if strings.Contains(body, forbidden) {
					t.Errorf("response exposes %q: %s", forbidden, body)
				}
			}
			if !strings.Contains(body, "example.test") || !strings.Contains(body, "Configured metadata") {
				t.Errorf("response is missing domain metadata: %s", body)
			}
		})
	}
	invalidPage := httptest.NewRecorder()
	invalidPageRequest := httptest.NewRequest(http.MethodGet, "/api/v1/domains?page=0", nil)
	invalidPageRequest.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	handler.ServeHTTP(invalidPage, invalidPageRequest)
	if invalidPage.Code != http.StatusBadRequest {
		t.Fatalf("invalid domain page status = %d, want %d", invalidPage.Code, http.StatusBadRequest)
	}
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/domains", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated domain read status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}
	ownerToken := httptest.NewRecorder()
	ownerRequest := httptest.NewRequest(http.MethodGet, "/api/v1/domains", nil)
	ownerRequest.Header.Set("Authorization", "Bearer own_"+strings.Repeat("a", 32))
	handler.ServeHTTP(ownerToken, ownerRequest)
	if ownerToken.Code != http.StatusUnauthorized {
		t.Fatalf("owner-token domain read status = %d, want %d", ownerToken.Code, http.StatusUnauthorized)
	}
}

func TestOperatorMailDomainLifecycleAndDKIMRoutes(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_test-token"
	cfg.Server.Security.EncryptionKey = base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
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
	request := func(method, target, body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		handler.ServeHTTP(response, req)
		return response
	}

	created := request(http.MethodPost, "/api/v1/domains", `{"domain":"Exämple.test.","description":"Managed","max_mailboxes":10,"max_aliases":5,"max_quota_mb":1024}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", created.Code, created.Body.String())
	}
	var createdBody struct {
		Data mailDomainResponse `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	parsedPublicID, parseIDErr := uuid.Parse(createdBody.Data.ID)
	if createdBody.Data.Domain != "xn--exmple-cua.test" || parseIDErr != nil || parsedPublicID.Version() != 7 {
		t.Fatalf("created domain = %#v", createdBody.Data)
	}
	var ownerType, ownerID string
	if err := db.UsersReadDB.QueryRowContext(context.Background(), `SELECT owner_type, owner_id FROM mail_domains WHERE domain = ?`, createdBody.Data.Domain).Scan(&ownerType, &ownerID); err != nil {
		t.Fatal(err)
	}
	if ownerType != "system" || ownerID != "system" {
		t.Fatalf("created owner = %q/%q, want system/system", ownerType, ownerID)
	}
	uppercaseRead := request(http.MethodGet, "/api/v1/domains/XN--EXMPLE-CUA.TEST", "")
	if uppercaseRead.Code != http.StatusOK {
		t.Fatalf("uppercase domain read status = %d: %s", uppercaseRead.Code, uppercaseRead.Body.String())
	}

	duplicate := request(http.MethodPost, "/api/v1/domains", `{"domain":"xn--exmple-cua.test"}`)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate create status = %d: %s", duplicate.Code, duplicate.Body.String())
	}
	invalid := request(http.MethodPost, "/api/v1/domains", `{"domain":"-bad.example"}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status = %d: %s", invalid.Code, invalid.Body.String())
	}
	if _, err := db.UsersDB.ExecContext(context.Background(), `INSERT INTO srv_mail_servers (hostname, server_role, agent_token, primary_for_domains)
		VALUES ('mx.example.test', 'primary', 'test-agent-token', 'xn--exmple-cua.test')`); err != nil {
		t.Fatal(err)
	}

	updated := request(http.MethodPut, "/api/v1/domains/xn--exmple-cua.test", `{"domain":"example.test","description":"Updated","max_mailboxes":20,"max_aliases":10,"max_quota_mb":2048,"enabled":false}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d: %s", updated.Code, updated.Body.String())
	}
	var primaryDomains string
	if err := db.UsersReadDB.QueryRowContext(context.Background(), `SELECT primary_for_domains FROM srv_mail_servers WHERE hostname = 'mx.example.test'`).Scan(&primaryDomains); err != nil {
		t.Fatal(err)
	}
	if primaryDomains != "example.test" {
		t.Fatalf("renamed mail server mapping = %q, want example.test", primaryDomains)
	}
	invalidSelector := request(http.MethodPost, "/api/v1/domains/example.test/dkim/generate?selector=bad_name", "")
	if invalidSelector.Code != http.StatusBadRequest {
		t.Fatalf("invalid DKIM selector status = %d: %s", invalidSelector.Code, invalidSelector.Body.String())
	}

	dkim := request(http.MethodPost, "/api/v1/domains/example.test/dkim/generate", "")
	if dkim.Code != http.StatusCreated {
		t.Fatalf("DKIM generate status = %d: %s", dkim.Code, dkim.Body.String())
	}
	if strings.Contains(dkim.Body.String(), "PRIVATE KEY") || strings.Contains(dkim.Body.String(), "private_key") {
		t.Fatalf("DKIM generate exposed private key: %s", dkim.Body.String())
	}
	var keyID int64
	var privateKeyCiphertext string
	if err := db.UsersReadDB.QueryRowContext(context.Background(), `SELECT id, private_key FROM mail_dkim_keys WHERE selector = 'default'`).Scan(&keyID, &privateKeyCiphertext); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(privateKeyCiphertext, "PRIVATE KEY") {
		t.Fatalf("stored DKIM private key was not encrypted")
	}
	privateKey, err := decryptSensitiveData(privateKeyCiphertext, cfg.Server.Security.EncryptionKey)
	if err != nil || !strings.Contains(string(privateKey), "PRIVATE KEY") {
		t.Fatalf("stored DKIM private key could not be decrypted: %v", err)
	}
	dkimRead := request(http.MethodGet, "/api/v1/domains/example.test/dkim", "")
	if dkimRead.Code != http.StatusOK || strings.Contains(dkimRead.Body.String(), string(privateKey)) || strings.Contains(dkimRead.Body.String(), privateKeyCiphertext) {
		t.Fatalf("DKIM read status/body = %d: %s", dkimRead.Code, dkimRead.Body.String())
	}
	var domainID int64
	if err := db.UsersReadDB.QueryRowContext(context.Background(), `SELECT id FROM mail_domains WHERE domain = 'example.test'`).Scan(&domainID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(context.Background(), `INSERT INTO mail_spf_records (domain_id, policy) VALUES (?, 'v=spf1 mx ~all')`, domainID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UsersDB.ExecContext(context.Background(), `INSERT INTO mail_dmarc_policies (domain_id, policy, percentage) VALUES (?, 'quarantine', 100)`, domainID); err != nil {
		t.Fatal(err)
	}

	dns := request(http.MethodGet, "/api/v1/domains/example.test/dns", "")
	if dns.Code != http.StatusOK || !strings.Contains(dns.Body.String(), `"type":"MX"`) ||
		!strings.Contains(dns.Body.String(), `"preference":10`) ||
		!strings.Contains(dns.Body.String(), `"name":"_dmarc.example.test"`) || !strings.Contains(dns.Body.String(), `"name":"default._domainkey.example.test"`) ||
		!strings.Contains(dns.Body.String(), `"missing":[]`) || strings.Contains(dns.Body.String(), string(privateKey)) || strings.Contains(dns.Body.String(), privateKeyCiphertext) {
		t.Fatalf("DNS guidance status/body = %d: %s", dns.Code, dns.Body.String())
	}

	blocked := request(http.MethodDelete, "/api/v1/domains/example.test", "")
	if blocked.Code != http.StatusConflict {
		t.Fatalf("delete with DKIM key status = %d: %s", blocked.Code, blocked.Body.String())
	}
	if _, err := db.UsersDB.ExecContext(context.Background(), `DELETE FROM mail_dkim_keys WHERE id = ?`, keyID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"mail_spf_records", "mail_dmarc_policies"} {
		if _, err := db.UsersDB.ExecContext(context.Background(), `DELETE FROM `+table+` WHERE domain_id = ?`, domainID); err != nil {
			t.Fatal(err)
		}
	}
	mapped := request(http.MethodDelete, "/api/v1/domains/example.test", "")
	if mapped.Code != http.StatusConflict {
		t.Fatalf("delete with server mapping status = %d: %s", mapped.Code, mapped.Body.String())
	}
	if _, err := db.UsersDB.ExecContext(context.Background(), `UPDATE srv_mail_servers SET primary_for_domains = '' WHERE hostname = 'mx.example.test'`); err != nil {
		t.Fatal(err)
	}
	deleted := request(http.MethodDelete, "/api/v1/domains/example.test", "")
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status = %d: %s", deleted.Code, deleted.Body.String())
	}
	var deletedBody struct {
		OK   bool                     `json:"ok"`
		Data mailDomainDeleteResponse `json:"data"`
	}
	if err := json.Unmarshal(deleted.Body.Bytes(), &deletedBody); err != nil {
		t.Fatal(err)
	}
	if !deletedBody.OK || deletedBody.Data.ID != createdBody.Data.ID || deletedBody.Data.Message != "Mail domain deleted" {
		t.Fatalf("delete response = %#v, want deleted domain ID and result", deletedBody)
	}
	var auditEvents int
	if err := db.ServerReadDB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE category = 'mail'
		AND target_type = 'mail_domain' AND target_id = ?`, createdBody.Data.ID).Scan(&auditEvents); err != nil {
		t.Fatal(err)
	}
	if auditEvents != 4 {
		t.Fatalf("mail-domain audit event count = %d, want create/update/DKIM/delete events", auditEvents)
	}
}
