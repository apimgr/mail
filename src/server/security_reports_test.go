package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	commonsecurity "github.com/apimgr/mail/src/common/security"
	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
)

func TestEncryptSecurityReportBodyUsesActivePGPKeyAndAESFallback(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(root, "server.db")
	cfg.Server.Security.EncryptionKey = base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	server := New(cfg, db)
	server.appPaths.Config = filepath.Join(root, "config")
	body := []byte("security report body")

	encrypted, algorithm, fingerprint, err := server.encryptSecurityReportBody(body)
	if err != nil || algorithm != "aes-256-gcm" {
		t.Fatalf("AES fallback encryption = %q, %q, %v", encrypted, algorithm, err)
	}
	if fingerprint != "" {
		t.Fatalf("AES fallback returned a PGP fingerprint: %q", fingerprint)
	}
	decrypted, err := decryptSensitiveData(string(encrypted), cfg.Server.Security.EncryptionKey)
	if err != nil || !bytes.Equal(decrypted, body) {
		t.Fatalf("AES fallback decrypt = %q, %v", decrypted, err)
	}

	created := time.Now().UTC().Truncate(time.Second)
	publicArmor, privateArmor, keyFingerprint, expires, err := commonsecurity.GeneratePGPKey("Mail Security <security@example.test>", created)
	if err != nil {
		t.Fatal(err)
	}
	securityDir := filepath.Join(server.appPaths.Config, "security")
	if err := os.MkdirAll(securityDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(securityDir, "pgp.pub.asc"), publicArmor, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := db.SavePGPKeyMetadata(ctx, database.PGPKeyMetadata{Fingerprint: keyFingerprint, CreatedAt: created, ExpiresAt: expires, KeyserversPublished: []database.PGPKeyserverPublication{}}); err != nil {
		t.Fatal(err)
	}
	encrypted, algorithm, fingerprint, err = server.encryptSecurityReportBody(body)
	if err != nil || algorithm != "pgp" {
		t.Fatalf("PGP encryption = %q, %q, %v", encrypted, algorithm, err)
	}
	if fingerprint != keyFingerprint {
		t.Fatalf("PGP encryption fingerprint = %q, want %q", fingerprint, keyFingerprint)
	}
	decrypted, err = commonsecurity.DecryptPGPMessage(privateArmor, encrypted)
	if err != nil || !bytes.Equal(decrypted, body) {
		t.Fatalf("PGP report decrypt = %q, %v", decrypted, err)
	}

	if err := os.WriteFile(filepath.Join(securityDir, "pgp.pub.asc"), []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := server.encryptSecurityReportBody(body); err == nil {
		t.Fatal("PGP report encryption accepted a public key that mismatched active metadata")
	}
}

func TestSecurityContactAPIDescriptorAndRequestValidation(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "server.db")
	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	server := New(cfg, db)
	get := httptest.NewRecorder()
	server.handleSecurityContactAPI(get, httptest.NewRequest(http.MethodGet, "/api/v1/server/contact", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("contact descriptor status = %d: %s", get.Code, get.Body.String())
	}
	var descriptor struct {
		OK   bool `json:"ok"`
		Data struct {
			FormToken string `json:"form_token"`
			Captcha   struct {
				ID       string `json:"id"`
				Question string `json:"question"`
			} `json:"captcha"`
		} `json:"data"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &descriptor); err != nil {
		t.Fatal(err)
	}
	if !descriptor.OK || descriptor.Data.FormToken == "" || descriptor.Data.Captcha.ID == "" || descriptor.Data.Captcha.Question == "" {
		t.Fatalf("contact descriptor omitted required tokens or challenge: %+v", descriptor)
	}
	for _, test := range []struct {
		name string
		body string
		want int
	}{
		{"malformed JSON", "{", http.StatusBadRequest},
		{"unknown step", `{"step":"unknown"}`, http.StatusBadRequest},
		{"unsupported report type", `{"step":"submit","type":"abuse"}`, http.StatusBadRequest},
		{"invalid security ID", `{"step":"submit","type":"security","security_id":"invalid"}`, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/server/contact", strings.NewReader(test.body))
			server.handleSecurityContactAPI(response, request)
			if response.Code != test.want {
				t.Fatalf("response status = %d, want %d: %s", response.Code, test.want, response.Body.String())
			}
		})
	}
	method := httptest.NewRecorder()
	server.handleSecurityContactAPI(method, httptest.NewRequest(http.MethodDelete, "/api/v1/server/contact", nil))
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Allow") != "GET, POST" {
		t.Fatalf("unsupported contact method = %d Allow=%q", method.Code, method.Header().Get("Allow"))
	}
	if _, err := db.GeneratedSecret(context.Background(), "installation_secret"); err != nil {
		t.Fatal(err)
	}
	unavailable := httptest.NewRecorder()
	New(cfg, nil).handleSecurityContactAPI(unavailable, httptest.NewRequest(http.MethodGet, "/api/v1/server/contact", nil))
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("contact descriptor without storage = %d", unavailable.Code)
	}
}

func TestSecurityOverviewAPIContractAndContactDisable(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "server.db")
	cfg.Server.FQDN = "mail.example"
	cfg.Server.Contact.Admin.Email = "private-admin@example.test"
	cfg.Server.Contact.Security.Email = "security@mail.example"
	cfg.Web.Security.ReportURL = "https://github.com/apimgr/mail/security/advisories/new"
	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GeneratedSecret(context.Background(), "installation_secret"); err != nil {
		t.Fatal(err)
	}
	server := New(cfg, db)
	response := httptest.NewRecorder()
	server.handleSecurityAPI(response, httptest.NewRequest(http.MethodGet, "/api/v1/server/security", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("security API status = %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"contacts", "expires", "pgp", "policy_url", "thanks_url", "report_url"} {
		if _, ok := payload.Data[key]; !ok {
			t.Errorf("security API omitted %q: %#v", key, payload.Data)
		}
	}
	for _, forbidden := range []string{"github_url", "security_id", "expires_at"} {
		if _, ok := payload.Data[forbidden]; ok {
			t.Errorf("security API exposed forbidden %q", forbidden)
		}
	}
	pgp, ok := payload.Data["pgp"].(map[string]any)
	if !ok || pgp["available"] != false || pgp["url"] != nil || pgp["fingerprint"] != nil {
		t.Errorf("missing-key PGP descriptor = %#v", payload.Data["pgp"])
	}
	contacts, ok := payload.Data["contacts"].([]any)
	if !ok || len(contacts) != 3 {
		t.Fatalf("security contacts = %#v", payload.Data["contacts"])
	}
	if strings.Contains(response.Body.String(), "private-admin@example.test") {
		t.Fatal("security API exposed the private admin email")
	}
	disabled := false
	cfg.Server.Pages.Contact.Enabled = &disabled
	response = httptest.NewRecorder()
	server.handleSecurityAPI(response, httptest.NewRequest(http.MethodGet, "/api/v1/server/security", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("disabled contact security API status = %d", response.Code)
	}
	if strings.Contains(response.Body.String(), "report_url") || strings.Contains(response.Body.String(), "contact_form") {
		t.Fatalf("disabled contact leaked report route: %s", response.Body.String())
	}
	response = httptest.NewRecorder()
	server.handleSecurityContactAPI(response, httptest.NewRequest(http.MethodGet, "/api/v1/server/contact", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("disabled contact descriptor status = %d", response.Code)
	}
}

func TestSecurityIDAcceptsCurrentAndPreviousWindow(t *testing.T) {
	secret := []byte(strings.Repeat("s", 32))
	boundary := time.Unix(12*securityIDWindow, 0).UTC()
	current := securityIDFor(secret, boundary)
	previous := securityIDFor(secret, boundary.Add(-time.Second))

	if window, ok := validSecurityID(secret, current, boundary); !ok || window != 12 {
		t.Fatalf("current id rejected: window=%d valid=%t", window, ok)
	}
	if window, ok := validSecurityID(secret, previous, boundary); !ok || window != 11 {
		t.Fatalf("previous id rejected: window=%d valid=%t", window, ok)
	}
	if _, ok := validSecurityID(secret, strings.ToUpper(current), boundary); !ok {
		t.Fatal("uppercase hexadecimal representation should be accepted")
	}
	for _, invalid := range []string{"", "0123456789abcdef", current[:15], current + "0"} {
		if _, ok := validSecurityID(secret, invalid, boundary); ok {
			t.Errorf("invalid id accepted: %q", invalid)
		}
	}
}

func TestSecuritySignedTokenPurposeExpiryAndTampering(t *testing.T) {
	secret := []byte(strings.Repeat("k", 32))
	now := time.Unix(1_800_000_000, 0).UTC()
	token, err := securityToken(secret, signedSecurityToken{Purpose: "contact-form", Nonce: "nonce", ExpiresAt: now.Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseSecurityToken(secret, token, "contact-form", now); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	if _, err := parseSecurityToken(secret, token, "contact-review", now); err == nil {
		t.Fatal("token accepted for a different purpose")
	}
	if _, err := parseSecurityToken(secret, token, "contact-form", now.Add(2*time.Minute)); err == nil {
		t.Fatal("expired token accepted")
	}
	parts := strings.Split(token, ".")
	parts[1] = strings.Repeat("A", len(parts[1]))
	if _, err := parseSecurityToken(secret, strings.Join(parts, "."), "contact-form", now); err == nil {
		t.Fatal("tampered token accepted")
	}
}

func TestSecurityReportValidationEnforcesPublishedLimits(t *testing.T) {
	request := securityReportRequest{
		Name: "Researcher", Email: "researcher@example.test", Component: "API",
		ReportedURL: "/api/v1/server/health", Severity: "high", Summary: "Example issue",
		Steps: "1. Send request", Impact: "Information disclosure", CreditPreference: "handle",
		CreditHandle: "researcher", Agreement: true,
	}
	payload, err := validateSecurityReport(request, httptest.NewRequest("POST", "/", nil), time.Now())
	if err != nil {
		t.Fatalf("valid report rejected: %v", err)
	}
	if payload.DisclosureDays != 90 || payload.CreditName != "researcher" {
		t.Fatalf("defaults or credit mapping incorrect: %#v", payload)
	}
	request.Summary = strings.Repeat("x", 201)
	if _, err := validateSecurityReport(request, httptest.NewRequest("POST", "/", nil), time.Now()); err == nil || err.Error() != "summary" {
		t.Fatalf("overlong summary was not rejected by field: %v", err)
	}
}

func TestSecurityReportValidationRejectsInvalidFieldsAndAcceptsOptionalInputs(t *testing.T) {
	valid := securityReportRequest{
		Name: "Researcher", Email: "researcher@example.test", Component: "API",
		ReportedURL: "/api/v1/server/health", Severity: "high", Summary: "Example issue",
		Steps: "1. Send request", Impact: "Information disclosure", CreditPreference: "name", Agreement: true,
	}
	for _, test := range []struct {
		name   string
		field  string
		mutate func(*securityReportRequest)
	}{
		{"missing name", "name", func(r *securityReportRequest) { r.Name = " " }},
		{"missing email", "email", func(r *securityReportRequest) { r.Email = "" }},
		{"invalid email", "email", func(r *securityReportRequest) { r.Email = "Researcher <researcher@example.test>" }},
		{"missing component", "component", func(r *securityReportRequest) { r.Component = "" }},
		{"invalid severity", "severity", func(r *securityReportRequest) { r.Severity = "urgent" }},
		{"missing summary", "summary", func(r *securityReportRequest) { r.Summary = "" }},
		{"missing steps", "steps", func(r *securityReportRequest) { r.Steps = "" }},
		{"missing impact", "impact", func(r *securityReportRequest) { r.Impact = "" }},
		{"invalid credit", "credit_preference", func(r *securityReportRequest) { r.CreditPreference = "maybe" }},
		{"missing credit handle", "credit_handle", func(r *securityReportRequest) { r.CreditPreference = "handle" }},
		{"missing agreement", "agreement", func(r *securityReportRequest) { r.Agreement = false }},
		{"relative reported URL", "reported_url", func(r *securityReportRequest) { r.ReportedURL = "example.test/path" }},
		{"unsupported reported URL scheme", "reported_url", func(r *securityReportRequest) { r.ReportedURL = "javascript:alert(1)" }},
		{"invalid researcher key", "researcher_gpg", func(r *securityReportRequest) { r.ResearcherGPG = "-----BEGIN PGP PUBLIC KEY BLOCK-----" }},
		{"invalid disclosure days", "disclosure_days", func(r *securityReportRequest) { r.DisclosureDays = 366 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.mutate(&request)
			if _, err := validateSecurityReport(request, httptest.NewRequest(http.MethodPost, "/", nil), time.Now()); err == nil || err.Error() != test.field {
				t.Fatalf("validation error = %v, want field %q", err, test.field)
			}
		})
	}
	valid.ReportedURL = "https://mail.example.test/security"
	valid.ResearcherGPG = "https://keys.example.test/researcher.asc"
	valid.CreditPreference = "anonymous"
	valid.DisclosureDays = 30
	if payload, err := validateSecurityReport(valid, httptest.NewRequest(http.MethodPost, "/", nil), time.Now()); err != nil || payload.DisclosureDays != 30 || payload.CreditName != "" {
		t.Fatalf("optional valid report = %#v, %v", payload, err)
	}
}

func TestSecurityReportDescriptorsPublishEnumsAndConditionalFields(t *testing.T) {
	fields := securityReportFieldDescriptors()
	byName := make(map[string]map[string]any, len(fields))
	for _, field := range fields {
		byName[field["name"].(string)] = field
	}
	severity, ok := byName["severity"]
	if !ok || len(severity["options"].([]string)) != 5 || severity["max_length"] != nil {
		t.Fatalf("severity descriptor does not publish the enum: %#v", severity)
	}
	handle, ok := byName["credit_handle"]
	condition, conditionOK := handle["required_when"].(map[string]string)
	if !ok || !conditionOK || condition["field"] != "credit_preference" || condition["equals"] != "handle" {
		t.Fatalf("credit handle conditional is missing: %#v", handle)
	}
}

func TestSecurityReceiptIsSkippedWhenResearcherRequestsPGP(t *testing.T) {
	for _, test := range []struct {
		name   string
		report securityReportPayload
		want   bool
	}{
		{name: "email without PGP key", report: securityReportPayload{Email: "researcher@example.test"}, want: true},
		{name: "email with PGP key", report: securityReportPayload{Email: "researcher@example.test", ResearcherGPG: "key"}},
		{name: "missing email", report: securityReportPayload{ResearcherGPG: "key"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldSendSecurityReceipt(test.report); got != test.want {
				t.Fatalf("shouldSendSecurityReceipt() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestSecurityReportConfirmationMentionsEmailOnlyAfterSuccessfulSend(t *testing.T) {
	trackingID := "sec_0123456789abcdef"
	withoutEmail := securityReportConfirmationMessage(trackingID, false)
	if strings.Contains(withoutEmail, "email") {
		t.Fatalf("confirmation promised an email that was not sent: %q", withoutEmail)
	}
	withEmail := securityReportConfirmationMessage(trackingID, true)
	if !strings.Contains(withEmail, "A confirmation email is on its way") {
		t.Fatalf("confirmation omitted successful email notice: %q", withEmail)
	}
}
