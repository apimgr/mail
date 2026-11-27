package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/apimgr/mail/src/database"
	"github.com/go-chi/chi/v5"
	"golang.org/x/net/idna"
)

const maxMailDomainBody = 16 << 10

type mailDomainRequest struct {
	Domain       string `json:"domain"`
	Description  string `json:"description"`
	MaxMailboxes int64  `json:"max_mailboxes"`
	MaxAliases   int64  `json:"max_aliases"`
	MaxQuotaMB   int64  `json:"max_quota_mb"`
	Enabled      *bool  `json:"enabled"`
}

type mailDomainResponse struct {
	ID           string `json:"id"`
	Domain       string `json:"domain"`
	Description  string `json:"description,omitempty"`
	MaxMailboxes int64  `json:"max_mailboxes"`
	MaxAliases   int64  `json:"max_aliases"`
	MaxQuotaMB   int64  `json:"max_quota_mb"`
	Enabled      bool   `json:"enabled"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

type mailDomainListResponse struct {
	OK         bool                 `json:"ok"`
	Data       []mailDomainResponse `json:"data"`
	Pagination apiPagination        `json:"pagination"`
}

type mailDomainDeleteResponse struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

func mailDomainView(domain database.MailDomain) mailDomainResponse {
	return mailDomainResponse{
		ID: domain.ID, Domain: domain.Domain, Description: domain.Description,
		MaxMailboxes: domain.MaxMailboxes, MaxAliases: domain.MaxAliases,
		MaxQuotaMB: domain.MaxQuotaMB, Enabled: domain.Enabled,
		CreatedAt: domain.CreatedAt, UpdatedAt: domain.UpdatedAt,
	}
}

func (s *Server) handleMailDomains(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.db == nil || s.db.UsersReadDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Mail domain storage is unavailable"})
		return
	}
	page, limit, valid := mailPagination(r)
	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid pagination parameters"})
		return
	}
	domains, total, err := s.db.ListMailDomains(r.Context(), page, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "Mail domains could not be loaded"})
		return
	}
	data := make([]mailDomainResponse, 0, len(domains))
	for _, domain := range domains {
		data = append(data, mailDomainView(domain))
	}
	pages := 0
	if total > 0 {
		pages = int((total-1)/int64(limit) + 1)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "data": data,
		"pagination": map[string]int{"page": page, "limit": limit, "total": int(total), "pages": pages},
	})
}

func (s *Server) handleMailDomain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.db == nil || s.db.UsersReadDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Mail domain storage is unavailable"})
		return
	}
	name, valid := normalizeMailDomain(chi.URLParam(r, "domain"))
	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid mail domain"})
		return
	}
	domain, err := s.db.GetMailDomain(r.Context(), name)
	if database.IsMailDomainNotFound(err) {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NOT_FOUND", "message": "Mail domain not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "Mail domain could not be loaded"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": mailDomainView(domain)})
}

func decodeMailDomainRequest(w http.ResponseWriter, r *http.Request, requireEnabled bool) (database.MailDomainInput, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMailDomainBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var body mailDomainRequest
	if err := decoder.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid mail domain request"})
		return database.MailDomainInput{}, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid mail domain request"})
		return database.MailDomainInput{}, false
	}
	domain, ok := normalizeMailDomain(body.Domain)
	if !ok || utf8.RuneCountInString(body.Description) > 256 || body.MaxMailboxes < 0 || body.MaxAliases < 0 || body.MaxQuotaMB < 0 || (requireEnabled && body.Enabled == nil) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid mail domain request"})
		return database.MailDomainInput{}, false
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	return database.MailDomainInput{
		Domain: domain, Description: strings.TrimSpace(body.Description), MaxMailboxes: body.MaxMailboxes,
		MaxAliases: body.MaxAliases, MaxQuotaMB: body.MaxQuotaMB, Enabled: enabled,
	}, true
}

func normalizeMailDomain(value string) (string, bool) {
	value = strings.TrimSuffix(strings.TrimSpace(value), ".")
	if value == "" || len(value) > 253 || net.ParseIP(value) != nil {
		return "", false
	}
	ascii, err := idna.Lookup.ToASCII(value)
	if err != nil || len(ascii) > 253 {
		return "", false
	}
	ascii = strings.ToLower(ascii)
	for _, label := range strings.Split(ascii, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return "", false
			}
		}
	}
	return ascii, true
}

func (s *Server) handleCreateMailDomain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.db == nil || s.db.UsersDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Mail domain storage is unavailable"})
		return
	}
	input, ok := decodeMailDomainRequest(w, r, false)
	if !ok {
		return
	}
	domain, err := s.db.CreateMailDomain(r.Context(), input)
	if database.IsMailDomainConflict(err) {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "CONFLICT", "message": "Mail domain already exists"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "Mail domain could not be created"})
		return
	}
	s.auditRequest(r, "mail.domain.created", "operator", map[string]any{"id": domain.ID, "domain": domain.Domain})
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "data": mailDomainView(domain)})
}

func (s *Server) handleUpdateMailDomain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.db == nil || s.db.UsersDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Mail domain storage is unavailable"})
		return
	}
	input, ok := decodeMailDomainRequest(w, r, true)
	if !ok {
		return
	}
	name, valid := normalizeMailDomain(chi.URLParam(r, "domain"))
	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid mail domain"})
		return
	}
	if err := s.db.UpdateMailDomain(r.Context(), name, input); database.IsMailDomainNotFound(err) {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NOT_FOUND", "message": "Mail domain not found"})
	} else if database.IsMailDomainConflict(err) {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "CONFLICT", "message": "Mail domain already exists"})
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "Mail domain could not be updated"})
	} else {
		domain, err := s.db.GetMailDomain(r.Context(), input.Domain)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "Updated mail domain could not be loaded"})
			return
		}
		s.auditRequest(r, "mail.domain.updated", "operator", map[string]any{"id": domain.ID, "domain": domain.Domain})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": mailDomainView(domain)})
	}
}

func (s *Server) handleDeleteMailDomain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	name, valid := normalizeMailDomain(chi.URLParam(r, "domain"))
	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid mail domain"})
		return
	}
	if s.db == nil || s.db.UsersDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Mail domain storage is unavailable"})
		return
	}
	domain, lookupErr := s.db.GetMailDomain(r.Context(), name)
	if lookupErr != nil {
		if database.IsMailDomainNotFound(lookupErr) {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NOT_FOUND", "message": "Mail domain not found"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "Mail domain could not be loaded"})
		}
		return
	}
	err := s.db.DeleteMailDomain(r.Context(), name)
	switch {
	case database.IsMailDomainNotFound(err):
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NOT_FOUND", "message": "Mail domain not found"})
	case errors.Is(err, database.ErrMailDomainInUse):
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "CONFLICT", "message": "Mail domain still has dependent resources"})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "Mail domain could not be deleted"})
	default:
		s.auditRequest(r, "mail.domain.deleted", "operator", map[string]any{"id": domain.ID, "domain": domain.Domain})
		writeJSON(w, http.StatusOK, apiDataResponse[mailDomainDeleteResponse]{
			OK: true, Data: mailDomainDeleteResponse{ID: domain.ID, Message: "Mail domain deleted"},
		})
	}
}

type mailDNSResponse struct {
	Domain  string                   `json:"domain"`
	Records []database.MailDNSRecord `json:"records"`
	Missing []string                 `json:"missing"`
}

func (s *Server) handleMailDomainDNS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	domain, ok := s.mailDomainForDNS(w, r)
	if !ok {
		return
	}
	records, missing, err := s.db.GetMailDNSRecords(r.Context(), domain.Domain)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "DNS records could not be loaded"})
		return
	}
	data := mailDNSResponse{Domain: domain.Domain, Records: records, Missing: missing}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": data})
}

func (s *Server) mailDomainForDNS(w http.ResponseWriter, r *http.Request) (database.MailDomain, bool) {
	name, ok := normalizeMailDomain(chi.URLParam(r, "domain"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid mail domain"})
		return database.MailDomain{}, false
	}
	if s.db == nil || s.db.UsersReadDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Mail domain storage is unavailable"})
		return database.MailDomain{}, false
	}
	domain, err := s.db.GetMailDomain(r.Context(), name)
	if database.IsMailDomainNotFound(err) {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NOT_FOUND", "message": "Mail domain not found"})
		return database.MailDomain{}, false
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "Mail domain could not be loaded"})
		return database.MailDomain{}, false
	}
	return domain, true
}

type mailDKIMResponse struct {
	Domain   string `json:"domain"`
	Selector string `json:"selector"`
	Record   string `json:"record"`
}

func (s *Server) handleGenerateMailDKIM(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	domain, ok := s.mailDomainForDNS(w, r)
	if !ok {
		return
	}
	selector := "default"
	if value := strings.TrimSpace(r.URL.Query().Get("selector")); value != "" {
		selector = value
	}
	if len(selector) > 63 || !validMailDKIMSelector(selector) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid DKIM selector"})
		return
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "DKIM key could not be generated"})
		return
	}
	privateKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	publicKey := base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PublicKey(&key.PublicKey))
	privateKeyCiphertext, err := encryptSensitiveData([]byte(privateKey), s.cfg.Server.Security.EncryptionKey)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Sensitive-data encryption is unavailable"})
		return
	}
	if err := s.db.SaveMailDKIMKey(r.Context(), domain.Domain, selector, privateKeyCiphertext, publicKey); err != nil {
		if database.IsMailDomainConflict(err) {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "CONFLICT", "message": "A DKIM key already exists for this selector"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "DKIM key could not be saved"})
		return
	}
	s.auditRequest(r, "mail.dkim.generated", "operator", map[string]any{"id": domain.ID, "domain": domain.Domain, "selector": selector})
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "data": mailDKIMResponse{
		Domain: domain.Domain, Selector: selector,
		Record: fmt.Sprintf("v=DKIM1; k=rsa; p=%s", publicKey),
	}})
}

func (s *Server) handleMailDKIM(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	domain, ok := s.mailDomainForDNS(w, r)
	if !ok {
		return
	}
	keys, err := s.db.ListMailDKIMKeys(r.Context(), domain.Domain)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "DKIM records could not be loaded"})
		return
	}
	data := make([]mailDKIMResponse, 0, len(keys))
	for _, key := range keys {
		data = append(data, mailDKIMResponse{Domain: domain.Domain, Selector: key.Selector, Record: fmt.Sprintf("v=DKIM1; k=rsa; p=%s", key.PublicKey)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": data})
}

func validMailDKIMSelector(value string) bool {
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return value != "" && value[0] != '-' && value[len(value)-1] != '-'
}
