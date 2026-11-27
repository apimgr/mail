package server

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/apimgr/mail/src/common/security"
	"github.com/apimgr/mail/src/database"
	"github.com/go-chi/chi/v5"
)

type mailUserResponse struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Email      string `json:"email"`
	LocalPart  string `json:"local_part"`
	Domain     string `json:"domain"`
	QuotaBytes int64  `json:"quota_bytes"`
	Enabled    bool   `json:"enabled"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

type mailUserCreateRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password" secret:"true"`
	QuotaBytes int64  `json:"quota_bytes"`
	Enabled    *bool  `json:"enabled"`
}

type mailUserUpdateRequest struct {
	QuotaBytes *int64 `json:"quota_bytes"`
	Enabled    *bool  `json:"enabled"`
}

type mailUserPasswordRequest struct {
	Password string `json:"password" secret:"true"`
}

type mailUserListResponse struct {
	OK         bool               `json:"ok"`
	Data       []mailUserResponse `json:"data"`
	Pagination apiPagination      `json:"pagination"`
}

type mailUserImportRow struct {
	Row     int    `json:"row"`
	Email   string `json:"email"`
	ID      string `json:"id,omitempty"`
	Error   string `json:"error,omitempty"`
	Created bool   `json:"created"`
}

type mailUserCSVRecord struct {
	fields  []string
	invalid bool
}

const maxMailUserImportRows = 100

func (s *Server) handleMailUsers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.db == nil || s.db.UsersReadDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Mail user storage is unavailable"})
		return
	}
	page, limit, valid := mailPagination(r)
	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid pagination parameters"})
		return
	}
	users, total, err := s.db.ListMailUsers(r.Context(), page, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "Mail users could not be loaded"})
		return
	}
	data := make([]mailUserResponse, 0, len(users))
	for _, user := range users {
		data = append(data, mailUserView(user))
	}
	pages := 0
	if total > 0 {
		pages = int((total-1)/int64(limit) + 1)
	}
	writeJSON(w, http.StatusOK, mailUserListResponse{
		OK: true, Data: data,
		Pagination: apiPagination{Page: page, Limit: limit, Total: int(total), Pages: pages},
	})
}

func (s *Server) handleMailUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.db == nil || s.db.UsersReadDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Mail user storage is unavailable"})
		return
	}
	user, err := s.db.GetMailUser(r.Context(), chi.URLParam(r, "id"))
	if database.IsMailDomainNotFound(err) {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NOT_FOUND", "message": "Mail user not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "Mail user could not be loaded"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": mailUserView(user)})
}

func (s *Server) handleCreateMailUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.db == nil || s.db.UsersDB == nil || s.db.UsersReadDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, mailUserError("SERVICE_UNAVAILABLE", "Mail user storage is unavailable"))
		return
	}
	var request mailUserCreateRequest
	if err := decodeMailUserRequest(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Invalid mail user request"))
		return
	}
	emailAddress, domain, valid := normalizeMailAddress(request.Email)
	if !valid {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Email must be a valid mailbox address"))
		return
	}
	if request.QuotaBytes < 0 {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Quota must be zero or greater"))
		return
	}
	if err := security.ValidatePassword(request.Password); err != nil {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", err.Error()))
		return
	}
	passwordHash, err := security.HashPassword(request.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, mailUserError("INTERNAL_ERROR", "Mail user password could not be secured"))
		return
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	localPart := emailAddress[:strings.LastIndexByte(emailAddress, '@')]
	user, err := s.db.CreateMailUser(r.Context(), localPart, domain, passwordHash, request.QuotaBytes, enabled)
	if errors.Is(err, database.ErrMailUserConflict) {
		writeJSON(w, http.StatusConflict, mailUserError("CONFLICT", "A mail user with this address already exists"))
		return
	}
	if database.IsMailDomainNotFound(err) {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Email domain is not configured or is disabled"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, mailUserError("INTERNAL_ERROR", "Mail user could not be created"))
		return
	}
	s.auditRequest(r, "mail.user.created", "operator", map[string]any{"id": user.ID})
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "data": mailUserView(user)})
}

func (s *Server) handleUpdateMailUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.db == nil || s.db.UsersDB == nil || s.db.UsersReadDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, mailUserError("SERVICE_UNAVAILABLE", "Mail user storage is unavailable"))
		return
	}
	var request mailUserUpdateRequest
	if err := decodeMailUserRequest(w, r, &request); err != nil || request.QuotaBytes == nil && request.Enabled == nil {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Provide quota_bytes or enabled"))
		return
	}
	if request.QuotaBytes != nil && *request.QuotaBytes < 0 {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Quota must be zero or greater"))
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.db.UpdateMailUserFields(r.Context(), id, request.QuotaBytes, request.Enabled); database.IsMailDomainNotFound(err) {
		writeJSON(w, http.StatusNotFound, mailUserError("NOT_FOUND", "Mail user not found"))
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, mailUserError("INTERNAL_ERROR", "Mail user could not be updated"))
		return
	}
	user, err := s.db.GetMailUser(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, mailUserError("INTERNAL_ERROR", "Updated mail user could not be loaded"))
		return
	}
	s.auditRequest(r, "mail.user.updated", "operator", map[string]any{"id": user.ID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": mailUserView(user)})
}

func (s *Server) handleDeleteMailUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.db == nil || s.db.UsersDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, mailUserError("SERVICE_UNAVAILABLE", "Mail user storage is unavailable"))
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.db.DeleteMailUser(r.Context(), id); database.IsMailDomainNotFound(err) {
		writeJSON(w, http.StatusNotFound, mailUserError("NOT_FOUND", "Mail user not found"))
		return
	} else if errors.Is(err, database.ErrMailUserHasDependencies) {
		writeJSON(w, http.StatusConflict, mailUserError("CONFLICT", "Mail user has dependent mail records"))
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, mailUserError("INTERNAL_ERROR", "Mail user could not be deleted"))
		return
	}
	s.auditRequest(r, "mail.user.deleted", "operator", map[string]any{"id": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]string{"id": id, "message": "Mail user deleted"}})
}

func (s *Server) handleChangeMailUserPassword(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.db == nil || s.db.UsersDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, mailUserError("SERVICE_UNAVAILABLE", "Mail user storage is unavailable"))
		return
	}
	var request mailUserPasswordRequest
	if err := decodeMailUserRequest(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Invalid password request"))
		return
	}
	if err := security.ValidatePassword(request.Password); err != nil {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", err.Error()))
		return
	}
	passwordHash, err := security.HashPassword(request.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, mailUserError("INTERNAL_ERROR", "Mail user password could not be secured"))
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.db.ChangeMailUserPassword(r.Context(), id, passwordHash); database.IsMailDomainNotFound(err) {
		writeJSON(w, http.StatusNotFound, mailUserError("NOT_FOUND", "Mail user not found"))
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, mailUserError("INTERNAL_ERROR", "Mail user password could not be changed"))
		return
	}
	user, err := s.db.GetMailUser(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, mailUserError("INTERNAL_ERROR", "Updated mail user could not be loaded"))
		return
	}
	s.auditRequest(r, "mail.user.password_changed", "operator", map[string]any{"id": user.ID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": mailUserView(user)})
}

func (s *Server) handleImportMailUsers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.db == nil || s.db.UsersDB == nil || s.db.UsersReadDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, mailUserError("SERVICE_UNAVAILABLE", "Mail user storage is unavailable"))
		return
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])), "text/csv") {
		writeJSON(w, http.StatusUnsupportedMediaType, mailUserError("VALIDATION_FAILED", "Content-Type must be text/csv"))
		return
	}
	content, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, mailUserError("VALIDATION_FAILED", "CSV request exceeds the 1 MiB limit"))
		return
	}
	reader := csv.NewReader(strings.NewReader(string(content)))
	reader.FieldsPerRecord = 4
	reader.ReuseRecord = false
	header, err := reader.Read()
	if err != nil || len(header) != 4 || !strings.EqualFold(strings.TrimSpace(header[0]), "email") || !strings.EqualFold(strings.TrimSpace(header[1]), "password") || !strings.EqualFold(strings.TrimSpace(header[2]), "quota_bytes") || !strings.EqualFold(strings.TrimSpace(header[3]), "enabled") {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "CSV header must be email,password,quota_bytes,enabled"))
		return
	}
	inputRows := make([]mailUserCSVRecord, 0)
	for {
		row, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if errors.Is(readErr, csv.ErrFieldCount) {
			inputRows = append(inputRows, mailUserCSVRecord{invalid: true})
			continue
		}
		if readErr != nil {
			writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "CSV contains invalid syntax"))
			return
		}
		inputRows = append(inputRows, mailUserCSVRecord{fields: row})
	}
	if len(inputRows) > maxMailUserImportRows {
		writeJSON(w, http.StatusRequestEntityTooLarge, mailUserError("VALIDATION_FAILED", "CSV contains more than 100 data rows"))
		return
	}
	results := make([]mailUserImportRow, 0)
	for index, input := range inputRows {
		rowNumber := index + 2
		result := mailUserImportRow{Row: rowNumber}
		if input.invalid {
			result.Error = "Invalid CSV row"
			results = append(results, result)
			continue
		}
		row := input.fields
		result.Email = strings.TrimSpace(row[0])
		address, domain, valid := normalizeMailAddress(result.Email)
		quotaBytes, quotaErr := strconv.ParseInt(strings.TrimSpace(row[2]), 10, 64)
		enabled, enabledErr := strconv.ParseBool(strings.TrimSpace(row[3]))
		if !valid || quotaErr != nil || quotaBytes < 0 || enabledErr != nil || security.ValidatePassword(row[1]) != nil {
			result.Error = "Invalid email, password, quota_bytes, or enabled value"
			results = append(results, result)
			continue
		}
		passwordHash, hashErr := security.HashPassword(row[1])
		if hashErr != nil {
			result.Error = "Password could not be secured"
			results = append(results, result)
			continue
		}
		localPart := address[:strings.LastIndexByte(address, '@')]
		user, createErr := s.db.CreateMailUser(r.Context(), localPart, domain, passwordHash, quotaBytes, enabled)
		switch {
		case errors.Is(createErr, database.ErrMailUserConflict):
			result.Error = "Mail user already exists"
		case database.IsMailDomainNotFound(createErr):
			result.Error = "Email domain is not configured or is disabled"
		case createErr != nil:
			result.Error = "Mail user could not be created"
		default:
			result.ID = user.ID
			result.Created = true
			s.auditRequest(r, "mail.user.created", "operator", map[string]any{"id": user.ID, "import_row": rowNumber})
		}
		results = append(results, result)
	}
	created := 0
	for _, result := range results {
		if result.Created {
			created++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]any{"rows": results, "total": len(results), "created": created, "failed": len(results) - created}})
}

func decodeMailUserRequest(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxMailDomainBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request must contain one JSON value")
	}
	return nil
}

func mailUserError(code, message string) map[string]any {
	return map[string]any{"ok": false, "error": code, "message": message}
}

func mailUserView(user database.MailUser) mailUserResponse {
	return mailUserResponse{
		ID: user.ID, Kind: "virtual", Email: user.Email, LocalPart: user.LocalPart, Domain: user.Domain,
		QuotaBytes: user.QuotaBytes, Enabled: user.Enabled, CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}
}
