package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/apimgr/mail/src/database"
	"github.com/go-chi/chi/v5"
)

type mailForwardRequest struct {
	Source       string   `json:"source"`
	Destinations []string `json:"destinations"`
	KeepLocal    *bool    `json:"keep_local"`
	Enabled      *bool    `json:"enabled"`
}

type mailForwardResponse struct {
	ID           string   `json:"id"`
	Source       string   `json:"source"`
	Destinations []string `json:"destinations"`
	KeepLocal    bool     `json:"keep_local"`
	Enabled      bool     `json:"enabled"`
	CreatedAt    int64    `json:"created_at"`
}

type mailForwardListResponse struct {
	OK         bool                  `json:"ok"`
	Data       []mailForwardResponse `json:"data"`
	Pagination apiPagination         `json:"pagination"`
}

func mailForwardView(forward database.MailForward) mailForwardResponse {
	return mailForwardResponse{ID: forward.ID, Source: forward.Source, Destinations: forward.Destinations, KeepLocal: forward.KeepLocal, Enabled: forward.Enabled, CreatedAt: forward.CreatedAt}
}

func (s *Server) handleMailForwards(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.db == nil || s.db.UsersReadDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, mailUserError("SERVICE_UNAVAILABLE", "Mail forward storage is unavailable"))
		return
	}
	page, limit, valid := mailPagination(r)
	if !valid {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Invalid pagination parameters"))
		return
	}
	forwards, total, err := s.db.ListMailForwards(r.Context(), page, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, mailUserError("INTERNAL_ERROR", "Mail forwards could not be loaded"))
		return
	}
	data := make([]mailForwardResponse, 0, len(forwards))
	for _, forward := range forwards {
		data = append(data, mailForwardView(forward))
	}
	pages := 0
	if total > 0 {
		pages = int((total-1)/int64(limit) + 1)
	}
	writeJSON(w, http.StatusOK, mailForwardListResponse{OK: true, Data: data, Pagination: apiPagination{Page: page, Limit: limit, Total: int(total), Pages: pages}})
}

func (s *Server) handleCreateMailForward(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.db == nil || s.db.UsersDB == nil || s.db.UsersReadDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, mailUserError("SERVICE_UNAVAILABLE", "Mail forward storage is unavailable"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMailDomainBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var body mailForwardRequest
	if err := decoder.Decode(&body); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Invalid mail forward request"))
		return
	}
	source, _, valid := normalizeMailAddress(body.Source)
	if !valid || len(body.Destinations) == 0 || len(body.Destinations) > 20 {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Provide a valid mailbox source and 1 to 20 destinations"))
		return
	}
	destinations := make([]string, 0, len(body.Destinations))
	for _, destination := range body.Destinations {
		normalized, _, ok := normalizeMailAddress(destination)
		if !ok {
			writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Every forward destination must be a valid email address"))
			return
		}
		destinations = append(destinations, normalized)
	}
	user, err := s.db.GetMailUserByEmail(r.Context(), source)
	if database.IsMailDomainNotFound(err) {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Source must be an enabled virtual mailbox"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, mailUserError("INTERNAL_ERROR", "Mail forward source could not be checked"))
		return
	}
	keepLocal, enabled := true, true
	if body.KeepLocal != nil {
		keepLocal = *body.KeepLocal
	}
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	forward, err := s.db.CreateMailForward(r.Context(), user.ID, destinations, keepLocal, enabled)
	if database.IsMailDomainNotFound(err) {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Source must be an enabled virtual mailbox"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, mailUserError("INTERNAL_ERROR", "Mail forward could not be created"))
		return
	}
	s.auditRequest(r, "mail.forward.created", "operator", map[string]any{"id": forward.ID})
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "data": mailForwardView(forward)})
}

func (s *Server) handleDeleteMailForward(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.db == nil || s.db.UsersDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, mailUserError("SERVICE_UNAVAILABLE", "Mail forward storage is unavailable"))
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if err := s.db.DeleteMailForward(r.Context(), id); database.IsMailDomainNotFound(err) {
		writeJSON(w, http.StatusNotFound, mailUserError("NOT_FOUND", "Mail forward not found"))
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, mailUserError("INTERNAL_ERROR", "Mail forward could not be deleted"))
		return
	}
	s.auditRequest(r, "mail.forward.deleted", "operator", map[string]any{"id": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]string{"id": id, "message": "Mail forward deleted"}})
}
