package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/apimgr/mail/src/database"
	"github.com/go-chi/chi/v5"
)

type mailAliasRequest struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type mailAliasResponse struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Domain      string `json:"domain"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   int64  `json:"created_at"`
}

type mailAliasListResponse struct {
	OK         bool                `json:"ok"`
	Data       []mailAliasResponse `json:"data"`
	Pagination apiPagination       `json:"pagination"`
}

type apiPagination struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
	Total int `json:"total"`
	Pages int `json:"pages"`
}

func (s *Server) handleMailAliases(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.db == nil || s.db.UsersReadDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Mail alias storage is unavailable"})
		return
	}
	page, limit, valid := mailPagination(r)
	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid pagination parameters"})
		return
	}
	aliases, total, err := s.db.ListMailAliases(r.Context(), page, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "Mail aliases could not be loaded"})
		return
	}
	data := make([]mailAliasResponse, 0, len(aliases))
	for _, alias := range aliases {
		data = append(data, mailAliasView(alias))
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

func (s *Server) handleCreateMailAlias(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.db == nil || s.db.UsersDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Mail alias storage is unavailable"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMailDomainBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var body mailAliasRequest
	if err := decoder.Decode(&body); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid mail alias request"})
		return
	}
	source, domain, sourceOK := normalizeMailAddress(body.Source)
	destination, _, destinationOK := normalizeMailAddress(body.Destination)
	if !sourceOK || !destinationOK {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Source and destination must be valid email addresses"})
		return
	}
	if _, err := s.db.GetMailDomain(r.Context(), domain); database.IsMailDomainNotFound(err) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Source address must belong to a configured mail domain"})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "Mail alias domain could not be checked"})
		return
	}
	alias, err := s.db.CreateMailAlias(r.Context(), domain, source, destination)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "Mail alias could not be created"})
		return
	}
	s.auditRequest(r, "mail.alias.created", "operator", map[string]any{"id": alias.ID})
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "data": mailAliasView(alias)})
}

func (s *Server) handleDeleteMailAlias(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.db == nil || s.db.UsersDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Mail alias storage is unavailable"})
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.db.DeleteMailAlias(r.Context(), id); database.IsMailDomainNotFound(err) {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NOT_FOUND", "message": "Mail alias not found"})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "Mail alias could not be deleted"})
		return
	}
	s.auditRequest(r, "mail.alias.deleted", "operator", map[string]any{"id": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]string{"id": id, "message": "Mail alias deleted"}})
}

func mailAliasView(alias database.MailAlias) mailAliasResponse {
	return mailAliasResponse{ID: alias.ID, Source: alias.Source, Destination: alias.Destination, Domain: alias.Domain, Enabled: alias.Enabled, CreatedAt: alias.CreatedAt}
}

func mailPagination(r *http.Request) (int, int, bool) {
	page, limit := 1, 250
	if raw := r.URL.Query().Get("page"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > int(^uint(0)>>1)/limit {
			return 0, 0, false
		}
		page = value
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 250 {
			return 0, 0, false
		}
		limit = value
	}
	return page, limit, true
}

func normalizeMailAddress(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	parsed, err := mail.ParseAddress(value)
	if err != nil || strings.ContainsAny(value, "\r\n") {
		return "", "", false
	}
	separator := strings.LastIndexByte(value, '@')
	if separator < 1 || separator == len(value)-1 || separator > 64 {
		return "", "", false
	}
	if parsed.Address != value && (value[0] != '"' || value[separator-1] != '"') {
		return "", "", false
	}
	domain, valid := normalizeMailDomain(value[separator+1:])
	if !valid || separator+1+len(domain) > 254 {
		return "", "", false
	}
	return value[:separator] + "@" + domain, domain, true
}
