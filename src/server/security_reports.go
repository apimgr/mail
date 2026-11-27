package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	commonsecurity "github.com/apimgr/mail/src/common/security"
	"github.com/apimgr/mail/src/database"
	"github.com/apimgr/mail/src/logger"
	"github.com/go-chi/chi/v5"
)

const securityIDWindow = int64(48 * time.Hour / time.Second)

type securityReportRequest struct {
	Step             string `json:"step"`
	SecurityID       string `json:"security_id"`
	Type             string `json:"type"`
	FormToken        string `json:"form_token"`
	ReviewToken      string `json:"review_token"`
	CaptchaID        string `json:"captcha_id"`
	CaptchaAnswer    string `json:"captcha_answer"`
	Website          string `json:"website"`
	Name             string `json:"name"`
	Email            string `json:"email"`
	ResearcherGPG    string `json:"researcher_gpg"`
	Component        string `json:"component"`
	ReportedURL      string `json:"reported_url"`
	Severity         string `json:"severity"`
	Summary          string `json:"summary"`
	Steps            string `json:"steps"`
	Impact           string `json:"impact"`
	SuggestedFix     string `json:"suggested_fix"`
	CVERequested     bool   `json:"cve_requested"`
	DisclosureDays   int    `json:"disclosure_days"`
	CreditPreference string `json:"credit_preference"`
	CreditHandle     string `json:"credit_handle"`
	Agreement        bool   `json:"agreement"`
	Confirmed        bool   `json:"confirmed"`
}

type securityReportPayload struct {
	Name           string `json:"name"`
	Email          string `json:"email"`
	ResearcherGPG  string `json:"researcher_gpg,omitempty"`
	Component      string `json:"component"`
	ReportedURL    string `json:"reported_url,omitempty"`
	Severity       string `json:"severity"`
	Summary        string `json:"summary"`
	Steps          string `json:"steps"`
	Impact         string `json:"impact"`
	SuggestedFix   string `json:"suggested_fix,omitempty"`
	CVERequested   bool   `json:"cve_requested"`
	DisclosureDays int    `json:"disclosure_days"`
	CreditPref     string `json:"credit_preference"`
	CreditName     string `json:"credit_name,omitempty"`
	UserAgent      string `json:"user_agent,omitempty"`
	AppVersion     string `json:"app_version"`
	CommitHash     string `json:"commit_hash"`
	Timestamp      string `json:"timestamp"`
}

type securityReportReview struct {
	Nonce     string                `json:"nonce"`
	ExpiresAt int64                 `json:"expires_at"`
	Window    int64                 `json:"window"`
	Report    securityReportPayload `json:"report"`
}

type signedSecurityToken struct {
	Purpose   string                `json:"purpose"`
	Nonce     string                `json:"nonce"`
	ExpiresAt int64                 `json:"expires_at"`
	Review    *securityReportReview `json:"review,omitempty"`
}

type securityStatusPage struct {
	TrackingID        string
	Status            string
	Severity          string
	ResearcherComment string
	DisclosureDate    string
}

type securityContactFormPage struct {
	SecurityID      string
	FormToken       string
	CaptchaID       string
	CaptchaQuestion string
	Error           string
}

type securityReviewPage struct {
	SecurityID  string
	ReviewToken string
	Summary     string
	Component   string
	Severity    string
	ReportedURL string
}

func securityIDFor(secret []byte, now time.Time) string {
	window := now.UTC().Unix() / securityIDWindow
	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "%d", window)
	return hex.EncodeToString(mac.Sum(nil))[:16]
}

func validSecurityID(secret []byte, supplied string, now time.Time) (int64, bool) {
	if len(supplied) != 16 {
		return 0, false
	}
	if _, err := hex.DecodeString(supplied); err != nil {
		return 0, false
	}
	window := now.UTC().Unix() / securityIDWindow
	for _, candidateWindow := range []int64{window, window - 1} {
		mac := hmac.New(sha256.New, secret)
		_, _ = fmt.Fprintf(mac, "%d", candidateWindow)
		expected := []byte(hex.EncodeToString(mac.Sum(nil))[:16])
		if subtle.ConstantTimeCompare(expected, []byte(strings.ToLower(supplied))) == 1 {
			return candidateWindow, true
		}
	}
	return 0, false
}

func (s *Server) installationSecret(ctx context.Context) ([]byte, error) {
	if s.db == nil || s.db.ServerReadDB == nil {
		return nil, errors.New("installation secret is unavailable")
	}
	return s.db.GeneratedSecret(ctx, "installation_secret")
}

func (s *Server) securityID(ctx context.Context, now time.Time) (string, error) {
	secret, err := s.installationSecret(ctx)
	if err != nil {
		return "", err
	}
	return securityIDFor(secret, now), nil
}

func (s *Server) validSecurityID(ctx context.Context, supplied string, now time.Time) (int64, bool) {
	secret, err := s.installationSecret(ctx)
	if err != nil {
		return 0, false
	}
	return validSecurityID(secret, supplied, now)
}

func securityToken(secret []byte, token signedSecurityToken) (string, error) {
	payload, err := json.Marshal(token)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func parseSecurityToken(secret []byte, raw, purpose string, now time.Time) (signedSecurityToken, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 2 || len(raw) > 16384 {
		return signedSecurityToken{}, errors.New("invalid signed token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return signedSecurityToken{}, errors.New("invalid signed token")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return signedSecurityToken{}, errors.New("invalid signed token")
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return signedSecurityToken{}, errors.New("invalid signed token")
	}
	var token signedSecurityToken
	if err := json.Unmarshal(payload, &token); err != nil || token.Purpose != purpose || token.ExpiresAt < now.UTC().Unix() {
		return signedSecurityToken{}, errors.New("expired or invalid signed token")
	}
	return token, nil
}

func (s *Server) handleSecurityContactAPI(w http.ResponseWriter, r *http.Request) {
	if !s.contactFormEnabled() {
		http.NotFound(w, r)
		return
	}
	if r.Method == "GET" {
		s.handleSecurityContactDescriptor(w, r)
		return
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "METHOD_NOT_ALLOWED"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request securityReportRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "INVALID_REQUEST"})
		return
	}
	secret, err := s.installationSecret(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "UNAVAILABLE"})
		return
	}
	now := time.Now().UTC()
	if request.Step == "" || request.Step == "submit" {
		s.submitSecurityReport(w, r, request, secret, now)
		return
	}
	if request.Step == "confirm" {
		s.confirmSecurityReport(w, r, request, secret, now)
		return
	}
	writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "field": "step"})
}

func (s *Server) renderSecurityContactForm(w http.ResponseWriter, r *http.Request, securityID, errorKey string) {
	now := time.Now().UTC()
	captchaID, question, err := s.issueSecurityCaptcha(r.Context(), now)
	if err != nil {
		s.renderPage(w, r, http.StatusServiceUnavailable, pageContent{TitleKey: "page.contact.title", DescriptionKey: "page.contact.lead", NoIndex: true, ContactNotice: "contact.unavailable"})
		return
	}
	secret, err := s.installationSecret(r.Context())
	if err != nil {
		s.renderPage(w, r, http.StatusServiceUnavailable, pageContent{TitleKey: "page.contact.title", DescriptionKey: "page.contact.lead", NoIndex: true, ContactNotice: "contact.unavailable"})
		return
	}
	nonce, err := randomHex(16)
	if err != nil {
		s.renderPage(w, r, http.StatusInternalServerError, pageContent{TitleKey: "page.contact.title", DescriptionKey: "page.contact.lead", NoIndex: true, ContactNotice: "contact.unavailable"})
		return
	}
	token, err := securityToken(secret, signedSecurityToken{Purpose: "contact-form", Nonce: nonce, ExpiresAt: now.Add(30 * time.Minute).Unix()})
	if err != nil {
		s.renderPage(w, r, http.StatusInternalServerError, pageContent{TitleKey: "page.contact.title", DescriptionKey: "page.contact.lead", NoIndex: true, ContactNotice: "contact.unavailable"})
		return
	}
	s.renderPage(w, r, http.StatusOK, pageContent{TitleKey: "page.contact.title", DescriptionKey: "page.contact.lead", NoIndex: true, ContactForm: &securityContactFormPage{SecurityID: securityID, FormToken: token, CaptchaID: captchaID, CaptchaQuestion: question, Error: errorKey}})
}

func (s *Server) handleContactSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := r.ParseForm(); err != nil {
		s.renderSecurityContactForm(w, r, "", "contact.invalid")
		return
	}
	allowedFields := map[string]bool{"type": true, "security_id": true, "form_token": true, "review_token": true, "captcha_id": true, "captcha_answer": true, "website": true, "name": true, "email": true, "researcher_gpg": true, "component": true, "reported_url": true, "severity": true, "summary": true, "steps": true, "impact": true, "suggested_fix": true, "cve_requested": true, "disclosure_days": true, "credit_preference": true, "credit_handle": true, "agreement": true, "step": true, "confirmed": true}
	for field := range r.PostForm {
		if !allowedFields[field] {
			s.renderSecurityContactForm(w, r, r.FormValue("security_id"), "contact.invalid")
			return
		}
	}
	request := securityReportRequest{
		Step: r.FormValue("step"), Type: r.FormValue("type"), SecurityID: r.FormValue("security_id"),
		FormToken: r.FormValue("form_token"), ReviewToken: r.FormValue("review_token"),
		CaptchaID: r.FormValue("captcha_id"), CaptchaAnswer: r.FormValue("captcha_answer"),
		Website: r.FormValue("website"), Name: r.FormValue("name"), Email: r.FormValue("email"),
		ResearcherGPG: r.FormValue("researcher_gpg"), Component: r.FormValue("component"),
		ReportedURL: r.FormValue("reported_url"), Severity: r.FormValue("severity"),
		Summary: r.FormValue("summary"), Steps: r.FormValue("steps"), Impact: r.FormValue("impact"),
		SuggestedFix: r.FormValue("suggested_fix"), CVERequested: r.FormValue("cve_requested") == "on",
		CreditPreference: r.FormValue("credit_preference"), CreditHandle: r.FormValue("credit_handle"),
		Agreement: r.FormValue("agreement") == "on", Confirmed: r.FormValue("confirmed") == "on",
	}
	if value := strings.TrimSpace(r.FormValue("disclosure_days")); value != "" {
		if _, err := fmt.Sscan(value, &request.DisclosureDays); err != nil {
			s.renderSecurityContactForm(w, r, request.SecurityID, "contact.invalid")
			return
		}
	}
	secret, err := s.installationSecret(r.Context())
	if err != nil {
		s.renderSecurityContactForm(w, r, request.SecurityID, "contact.unavailable")
		return
	}
	if request.Website != "" {
		s.renderPage(w, r, http.StatusOK, pageContent{TitleKey: "page.contact.title", DescriptionKey: "page.contact.lead", NoIndex: true, ContactNotice: "contact.received"})
		return
	}
	if _, valid := validSecurityID(secret, request.SecurityID, time.Now().UTC()); !valid {
		if request.SecurityID != "" {
			s.logInvalidSecurityID(r, request.SecurityID)
		}
		s.handleContactPage(w, r)
		return
	}
	response := httptest.NewRecorder()
	if request.Step == "confirm" {
		s.confirmSecurityReport(response, r, request, secret, time.Now().UTC())
		var result struct {
			OK   bool `json:"ok"`
			Data struct {
				TrackingID string `json:"tracking_id"`
			} `json:"data"`
		}
		if json.NewDecoder(bytes.NewReader(response.Body.Bytes())).Decode(&result) == nil && result.OK && result.Data.TrackingID != "" {
			s.renderPage(w, r, http.StatusOK, pageContent{TitleKey: "page.contact.title", DescriptionKey: "page.contact.lead", NoIndex: true, ContactNotice: "contact.received", ContactTrackingID: result.Data.TrackingID})
			return
		}
		s.renderSecurityContactForm(w, r, request.SecurityID, "contact.invalid")
		return
	}
	s.submitSecurityReport(response, r, request, secret, time.Now().UTC())
	var result struct {
		OK   bool `json:"ok"`
		Data struct {
			ReviewToken string `json:"review_token"`
			Summary     string `json:"summary"`
			Component   string `json:"component"`
			Severity    string `json:"severity"`
			ReportedURL string `json:"reported_url"`
		} `json:"data"`
	}
	if json.NewDecoder(bytes.NewReader(response.Body.Bytes())).Decode(&result) == nil && result.OK && result.Data.ReviewToken != "" {
		s.renderPage(w, r, http.StatusOK, pageContent{TitleKey: "page.contact.title", DescriptionKey: "page.contact.lead", NoIndex: true, SecurityReview: &securityReviewPage{SecurityID: request.SecurityID, ReviewToken: result.Data.ReviewToken, Summary: result.Data.Summary, Component: result.Data.Component, Severity: result.Data.Severity, ReportedURL: result.Data.ReportedURL}})
		return
	}
	s.renderSecurityContactForm(w, r, request.SecurityID, "contact.invalid")
}

func (s *Server) handleSecurityContactDescriptor(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	securityID := strings.TrimSpace(r.URL.Query().Get("security_id"))
	if _, ok := s.validSecurityID(r.Context(), securityID, now); !ok {
		if securityID != "" {
			s.logInvalidSecurityID(r, securityID)
		}
		securityID = ""
	}
	captchaID, question, err := s.issueSecurityCaptcha(r.Context(), now)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "UNAVAILABLE"})
		return
	}
	secret, err := s.installationSecret(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "UNAVAILABLE"})
		return
	}
	nonce, err := randomHex(16)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR"})
		return
	}
	formToken, err := securityToken(secret, signedSecurityToken{Purpose: "contact-form", Nonce: nonce, ExpiresAt: now.Add(30 * time.Minute).Unix()})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR"})
		return
	}
	fields := securityReportFieldDescriptors()
	data := map[string]any{"types": []string{"abuse", "dmca", "privacy"}, "fields": fields, "form_token": formToken, "captcha": map[string]string{"provider": "simple", "id": captchaID, "question": question}, "security_url": s.buildPublicURL(r, "/server/security")}
	if securityID != "" {
		data["types"] = []string{"security"}
		data["security_id"] = securityID
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": data})
}

func securityReportFieldDescriptors() []map[string]any {
	field := func(name, kind string, required bool, max any) map[string]any {
		return map[string]any{"name": name, "type": kind, "required": required, "max_length": max, "required_when": nil, "max_length_when": nil}
	}
	withOptions := func(name, kind string, required bool, max any, options []string) map[string]any {
		descriptor := field(name, kind, required, max)
		descriptor["options"] = options
		return descriptor
	}
	fields := []map[string]any{
		field("name", "text", true, 100), field("email", "email", true, 254),
		{"name": "researcher_gpg", "type": "text", "required": false, "max_length": 16384, "required_when": nil, "max_length_when": []map[string]any{{"prefix": "https://", "max_length": 2048}}},
		withOptions("component", "combobox", true, 255, []string{"API", "Web interface", "CLI", "Postfix", "Dovecot", "SpamAssassin", "Rspamd", "ClamAV", "OpenDKIM", "Other"}), field("reported_url", "url", false, 2048),
		withOptions("severity", "select", true, nil, []string{"critical", "high", "medium", "low", "informational"}), field("summary", "text", true, 200),
		field("steps", "textarea", true, 10000), field("impact", "textarea", true, 5000),
		field("suggested_fix", "textarea", false, 5000), field("cve_requested", "checkbox", false, nil),
		field("disclosure_days", "number", false, nil), withOptions("credit_preference", "select", true, nil, []string{"name", "handle", "no", "anonymous"}),
		{"name": "credit_handle", "type": "text", "required": false, "max_length": 100, "required_when": map[string]string{"field": "credit_preference", "equals": "handle"}, "max_length_when": nil},
		field("agreement", "checkbox", true, nil),
	}
	fields = append(fields, field("captcha", "captcha", true, nil))
	return fields
}

func (s *Server) issueSecurityCaptcha(ctx context.Context, now time.Time) (string, string, error) {
	if s.db == nil || s.db.ServerDB == nil {
		return "", "", errors.New("contact challenge storage is unavailable")
	}
	left, err := rand.Int(rand.Reader, big.NewInt(20))
	if err != nil {
		return "", "", err
	}
	right, err := rand.Int(rand.Reader, big.NewInt(20))
	if err != nil {
		return "", "", err
	}
	first, second := int(left.Int64()+1), int(right.Int64()+1)
	if first < second {
		first, second = second, first
	}
	operation, err := rand.Int(rand.Reader, big.NewInt(2))
	if err != nil {
		return "", "", err
	}
	operator := "+"
	answerValue := first + second
	if operation.Int64() == 1 {
		operator = "−"
		answerValue = first - second
	}
	answer := fmt.Sprint(answerValue)
	question := fmt.Sprintf("%d %s %d", first, operator, second)
	id, err := randomHex(16)
	if err != nil {
		return "", "", err
	}
	answerHash := sha256.Sum256([]byte(answer))
	err = s.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO captcha_challenges (id, answer_hash, expires_at) VALUES (?, ?, ?)`, id, hex.EncodeToString(answerHash[:]), now.Add(10*time.Minute).Format(time.RFC3339Nano))
		return err
	})
	return id, question, err
}

func randomHex(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func (s *Server) submitSecurityReport(w http.ResponseWriter, r *http.Request, request securityReportRequest, secret []byte, now time.Time) {
	if request.Type != "security" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "field": "type"})
		return
	}
	if request.Website != "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]string{"step": "review"}})
		return
	}
	window, valid := validSecurityID(secret, request.SecurityID, now)
	if !valid {
		s.logInvalidSecurityID(r, request.SecurityID)
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "field": "security_id"})
		return
	}
	if _, err := parseSecurityToken(secret, request.FormToken, "contact-form", now); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "field": "form_token"})
		return
	}
	if err := s.consumeSecurityCaptcha(r.Context(), request.CaptchaID, request.CaptchaAnswer, now); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "field": "captcha"})
		return
	}
	report, err := validateSecurityReport(request, r, now)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "field": err.Error()})
		return
	}
	nonce, err := randomHex(16)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR"})
		return
	}
	review := securityReportReview{Nonce: nonce, ExpiresAt: now.Add(15 * time.Minute).Unix(), Window: window, Report: report}
	token, err := securityToken(secret, signedSecurityToken{Purpose: "contact-review", Nonce: nonce, ExpiresAt: review.ExpiresAt, Review: &review})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR"})
		return
	}
	if err := s.storeReviewNonce(r.Context(), nonce, now.Add(15*time.Minute)); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "UNAVAILABLE"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]any{"step": "review", "review_token": token, "summary": report.Summary, "component": report.Component, "severity": report.Severity, "reported_url": report.ReportedURL}})
}

func validateSecurityReport(request securityReportRequest, r *http.Request, now time.Time) (securityReportPayload, error) {
	trim := func(value string) string { return strings.TrimSpace(value) }
	fields := map[string]string{"name": trim(request.Name), "email": trim(request.Email), "component": trim(request.Component), "severity": trim(request.Severity), "summary": trim(request.Summary), "steps": trim(request.Steps), "impact": trim(request.Impact), "suggested_fix": trim(request.SuggestedFix), "reported_url": trim(request.ReportedURL), "researcher_gpg": trim(request.ResearcherGPG), "credit_handle": trim(request.CreditHandle), "credit_preference": trim(request.CreditPreference)}
	limits := map[string]int{"name": 100, "email": 254, "researcher_gpg": 16384, "component": 255, "reported_url": 2048, "summary": 200, "steps": 10000, "impact": 5000, "suggested_fix": 5000, "credit_handle": 100}
	for field, max := range limits {
		if utf8.RuneCountInString(fields[field]) > max {
			return securityReportPayload{}, fmt.Errorf("%s", field)
		}
	}
	for _, field := range []string{"name", "email", "component", "severity", "summary", "steps", "impact"} {
		if fields[field] == "" {
			return securityReportPayload{}, fmt.Errorf("%s", field)
		}
	}
	parsedEmail, err := mail.ParseAddress(fields["email"])
	if err != nil || parsedEmail.Address != fields["email"] || strings.ContainsAny(fields["email"], "\r\n") {
		return securityReportPayload{}, errors.New("email")
	}
	severity := strings.ToLower(fields["severity"])
	if !oneOf(severity, "critical", "high", "medium", "low", "informational") {
		return securityReportPayload{}, errors.New("severity")
	}
	credit := strings.ToLower(fields["credit_preference"])
	if !oneOf(credit, "name", "handle", "no", "anonymous") {
		return securityReportPayload{}, errors.New("credit_preference")
	}
	if credit == "handle" && fields["credit_handle"] == "" {
		return securityReportPayload{}, errors.New("credit_handle")
	}
	if !request.Agreement {
		return securityReportPayload{}, errors.New("agreement")
	}
	if fields["reported_url"] != "" {
		value := fields["reported_url"]
		parsed, parseErr := url.ParseRequestURI(value)
		if parseErr != nil || strings.ContainsAny(value, "\r\n") || (parsed.IsAbs() && ((parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "")) || (!parsed.IsAbs() && !strings.HasPrefix(value, "/")) {
			return securityReportPayload{}, errors.New("reported_url")
		}
	}
	if gpg := fields["researcher_gpg"]; gpg != "" {
		if strings.HasPrefix(gpg, "https://") {
			parsed, parseErr := url.ParseRequestURI(gpg)
			if parseErr != nil || parsed.Host == "" || utf8.RuneCountInString(gpg) > 2048 {
				return securityReportPayload{}, errors.New("researcher_gpg")
			}
		} else if !strings.Contains(gpg, "-----BEGIN PGP PUBLIC KEY BLOCK-----") || !strings.Contains(gpg, "-----END PGP PUBLIC KEY BLOCK-----") {
			return securityReportPayload{}, errors.New("researcher_gpg")
		}
	}
	days := request.DisclosureDays
	if days == 0 {
		days = 90
	}
	if days < 1 || days > 365 {
		return securityReportPayload{}, errors.New("disclosure_days")
	}
	creditName := ""
	if credit == "name" {
		creditName = fields["name"]
	}
	if credit == "handle" {
		creditName = fields["credit_handle"]
	}
	return securityReportPayload{
		Name: fields["name"], Email: fields["email"], ResearcherGPG: fields["researcher_gpg"],
		Component: fields["component"], ReportedURL: fields["reported_url"], Severity: severity,
		Summary: fields["summary"], Steps: fields["steps"], Impact: fields["impact"],
		SuggestedFix: fields["suggested_fix"], CVERequested: request.CVERequested,
		DisclosureDays: days, CreditPref: credit, CreditName: creditName,
		UserAgent: cleanLogValue(r.UserAgent(), 512), AppVersion: applicationVersion(), CommitHash: currentBuildInfo().Commit,
		Timestamp: now.UTC().Format(time.RFC3339),
	}, nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func cleanLogValue(value string, max int) string {
	value = strings.Map(func(char rune) rune {
		if char < 0x20 || char == 0x7f {
			return ' '
		}
		return char
	}, value)
	if utf8.RuneCountInString(value) > max {
		value = string([]rune(value)[:max])
	}
	return value
}

func (s *Server) consumeSecurityCaptcha(ctx context.Context, id, answer string, now time.Time) error {
	if s.db == nil || id == "" || len(id) > 32 || len(answer) > 8 {
		return errors.New("captcha")
	}
	answer = strings.TrimSpace(answer)
	digest := sha256.Sum256([]byte(answer))
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		var expected, expiry string
		if err := tx.QueryRowContext(ctx, `SELECT answer_hash, expires_at FROM captcha_challenges WHERE id = ?`, id).Scan(&expected, &expiry); err != nil {
			return err
		}
		parsed, err := time.Parse(time.RFC3339Nano, expiry)
		_, deleteErr := tx.ExecContext(ctx, `DELETE FROM captcha_challenges WHERE id = ?`, id)
		if deleteErr != nil {
			return deleteErr
		}
		if err != nil || !parsed.After(now) || subtle.ConstantTimeCompare([]byte(expected), []byte(hex.EncodeToString(digest[:]))) != 1 {
			return errors.New("captcha")
		}
		return nil
	})
}

func (s *Server) storeReviewNonce(ctx context.Context, nonce string, expires time.Time) error {
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO contact_review_tokens (nonce_hash, expires_at) VALUES (?, ?)`, hashOwnerToken(nonce), expires.UTC().Format(time.RFC3339Nano))
		return err
	})
}

func (s *Server) consumeReviewNonce(ctx context.Context, nonce string, now time.Time) error {
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM contact_review_tokens WHERE nonce_hash = ? AND expires_at > ?`, hashOwnerToken(nonce), now.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return errors.New("invalid review token")
		}
		return nil
	})
}

func (s *Server) confirmSecurityReport(w http.ResponseWriter, r *http.Request, request securityReportRequest, secret []byte, now time.Time) {
	token, err := parseSecurityToken(secret, request.ReviewToken, "contact-review", now)
	if err != nil || token.Review == nil || token.Nonce != token.Review.Nonce || !request.Confirmed {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "field": "review_token"})
		return
	}
	if !request.WebsiteEmptyForConfirm() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "field": "website"})
		return
	}
	window, valid := validSecurityID(secret, request.SecurityID, now)
	if !valid || window != token.Review.Window {
		s.logInvalidSecurityID(r, request.SecurityID)
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "field": "security_id"})
		return
	}
	if strings.TrimSpace(s.cfg.Server.Security.EncryptionKey) == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "ENCRYPTION_UNAVAILABLE"})
		return
	}
	report := token.Review.Report
	body, err := json.Marshal(report)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR"})
		return
	}
	unlockPGP := commonsecurity.WithPGPKeyReadLock()
	defer unlockPGP()
	bodyEncrypted, bodyAlgorithm, pgpFingerprint, err := s.encryptSecurityReportBody(body)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "ENCRYPTION_UNAVAILABLE"})
		return
	}
	contact, err := json.Marshal(map[string]string{"email": report.Email, "researcher_gpg": report.ResearcherGPG})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR"})
		return
	}
	contactEncrypted, err := encryptSensitiveData(contact, s.cfg.Server.Security.EncryptionKey)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "ENCRYPTION_UNAVAILABLE"})
		return
	}
	if err := s.consumeReviewNonce(r.Context(), token.Nonce, now); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "field": "review_token"})
		return
	}
	trackingSuffix, err := randomHex(8)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR"})
		return
	}
	trackingID := "sec_" + trackingSuffix
	statusToken, err := randomHex(32)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR"})
		return
	}
	creditPreference := report.CreditPref
	if creditPreference == "anonymous" {
		creditPreference = "anon"
	}
	creditName := sql.NullString{}
	if report.CreditName != "" {
		creditName = sql.NullString{String: report.CreditName, Valid: true}
	}
	created := now.UTC()
	err = s.db.CreateSecurityReport(r.Context(), database.SecurityReport{
		TrackingID: trackingID, IDWindow: token.Review.Window, Severity: report.Severity,
		Component: report.Component, BodyEncrypted: bodyEncrypted, BodyAlgorithm: bodyAlgorithm, PGPFingerprint: pgpFingerprint,
		ContactEncrypted: []byte(contactEncrypted), CreditPreference: creditPreference,
		CreditName: creditName, CVERequested: report.CVERequested, TokenHash: hashOwnerToken(statusToken),
		CreatedAt: created, UpdatedAt: created,
	})
	if err != nil {
		logger.Security(fmt.Sprintf("security.report_storage_failed tracking_id=%s", trackingID), s.requestIP(r))
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "STORAGE_UNAVAILABLE"})
		return
	}
	acknowledged := s.sendSecurityAcknowledgement(r.Context(), r, trackingID, statusToken, report)
	logger.Security(fmt.Sprintf("security.report_received tracking_id=%s notified=false severity=%s component=%q", trackingID, report.Severity, cleanLogValue(report.Component, 100)), s.requestIP(r))
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "data": map[string]string{"tracking_id": trackingID}, "message": securityReportConfirmationMessage(trackingID, acknowledged)})
}

func (s *Server) encryptSecurityReportBody(body []byte) ([]byte, string, string, error) {
	metadata, err := s.db.GetPGPKeyMetadata(context.Background())
	if err == nil && !metadata.Revoked && metadata.ExpiresAt.After(time.Now().UTC()) {
		publicPath := filepath.Join(s.appPaths.Config, "security", "pgp.pub.asc")
		publicArmor, readErr := os.ReadFile(publicPath)
		if readErr != nil {
			return nil, "", "", fmt.Errorf("read active PGP public key: %w", readErr)
		}
		fingerprint, fingerprintErr := commonsecurity.PGPFingerprint(publicArmor)
		if fingerprintErr != nil || fingerprint != metadata.Fingerprint {
			return nil, "", "", fmt.Errorf("active PGP public key does not match database metadata")
		}
		encrypted, encryptErr := commonsecurity.EncryptPGPMessage(publicArmor, body)
		if encryptErr != nil {
			return nil, "", "", fmt.Errorf("encrypt security report with PGP: %w", encryptErr)
		}
		return encrypted, "pgp", metadata.Fingerprint, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, "", "", fmt.Errorf("read active PGP key metadata: %w", err)
	}
	encrypted, err := encryptSensitiveData(body, s.cfg.Server.Security.EncryptionKey)
	if err != nil {
		return nil, "", "", fmt.Errorf("encrypt security report with AES-GCM: %w", err)
	}
	return []byte(encrypted), "aes-256-gcm", "", nil
}

// sendSecurityAcknowledgement sends only the researcher's one-shot status link.
// It never sends report content and does not indicate maintainer notification.
func (s *Server) sendSecurityAcknowledgement(ctx context.Context, r *http.Request, trackingID, statusToken string, report securityReportPayload) bool {
	if s.mailer == nil {
		return false
	}
	statusURL := s.buildPublicURL(r, "/server/security/report/"+trackingID+"?token="+url.QueryEscape(statusToken))
	if shouldSendSecurityReceipt(report) {
		if err := s.mailer.Send(ctx, "security_report_received", report.Email, map[string]string{
			"tracking_id": trackingID,
			"status_url":  statusURL,
		}); err == nil {
			return true
		}
	}
	return false
}

func securityReportConfirmationMessage(trackingID string, acknowledged bool) string {
	message := "We received your report. Your tracking ID is " + trackingID + "."
	if acknowledged {
		message += " A confirmation email is on its way."
	}
	return message
}

func shouldSendSecurityReceipt(report securityReportPayload) bool {
	return report.Email != "" && report.ResearcherGPG == ""
}

func (request securityReportRequest) WebsiteEmptyForConfirm() bool { return request.Website == "" }

func (s *Server) logInvalidSecurityID(r *http.Request, supplied string) {
	digest := sha256.Sum256([]byte(supplied))
	logger.Security(fmt.Sprintf("security.security_id_invalid user_agent=%q supplied_hash=%s", cleanLogValue(r.UserAgent(), 512), hex.EncodeToString(digest[:])[:8]), s.requestIP(r))
}

func (s *Server) handleSecurityReportStatus(w http.ResponseWriter, r *http.Request) {
	trackingID := chi.URLParam(r, "tracking_id")
	token := r.URL.Query().Get("token")
	if len(trackingID) != 20 || !strings.HasPrefix(trackingID, "sec_") || len(token) != 64 || s.db == nil {
		s.handleNotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	status, err := s.db.ConsumeSecurityReportStatus(ctx, trackingID, hashOwnerToken(token), time.Now().UTC().Format("2006-01-02"), time.Now().UTC())
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	view := &securityStatusPage{TrackingID: trackingID, Status: status.Status, Severity: status.Severity, ResearcherComment: status.ResearcherComment}
	if status.DisclosureDate.Valid {
		view.DisclosureDate = status.DisclosureDate.String
	}
	s.renderPage(w, r, http.StatusOK, pageContent{TitleKey: "security.report.status.title", DescriptionKey: "page.security.lead", BreadcrumbKey: "page.security.title", BreadcrumbPath: "/server/security", NoIndex: true, SecurityReport: view})
}

func (s *Server) handleSecurityThanksAPI(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": []database.SecurityThanks{}})
		return
	}
	items, err := s.db.ListSecurityThanks(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "UNAVAILABLE"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": items})
}

func (s *Server) handleSecurityAPI(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	contacts := make([]map[string]string, 0, 3)
	if reportURL := s.securityReportURL(); reportURL != "" {
		contacts = append(contacts, map[string]string{"type": "github", "value": reportURL})
	}
	if s.contactFormEnabled() {
		securityID, err := s.securityID(r.Context(), now)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "UNAVAILABLE"})
			return
		}
		contacts = append(contacts, map[string]string{"type": "contact_form", "value": s.buildPublicURL(r, "/server/contact?security_id="+securityID)})
	}
	if email := s.publicSecurityEmail(r); email != "" {
		contacts = append(contacts, map[string]string{"type": "email", "value": email})
	}
	fingerprint, available := s.pgpFingerprint(r.Context())
	var pgpURL, pgpFingerprint any
	if available {
		pgpURL = s.buildPublicURL(r, "/.well-known/pgp-key.asc")
		pgpFingerprint = fingerprint
	}
	data := map[string]any{
		"contacts":   contacts,
		"expires":    now.AddDate(1, 0, 0).Format(time.RFC3339),
		"pgp":        map[string]any{"available": available, "url": pgpURL, "fingerprint": pgpFingerprint},
		"policy_url": s.buildPublicURL(r, "/server/security/policy"),
		"thanks_url": s.buildPublicURL(r, "/server/security/thanks"),
	}
	if s.contactFormEnabled() {
		data["report_url"] = s.buildPublicURL(r, "/server/contact")
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": data})
}
