package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/apimgr/mail/src/database"
	"github.com/go-chi/chi/v5"
)

type linkedTokenRequest struct {
	Name      *string `json:"name"`
	Expires   *string `json:"expires"`
	ExpiresOn *string `json:"expires_on"`
}

func (s *Server) requirePrimaryTokenOwner(w http.ResponseWriter, r *http.Request) (database.OwnerTokenIdentity, bool) {
	if s.authFailureLocked(w, r) {
		return database.OwnerTokenIdentity{}, false
	}
	identity, ok, err := s.ownerBearerIdentity(r)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Token service is unavailable"})
		return database.OwnerTokenIdentity{}, false
	}
	if !ok {
		s.recordAuthFailure(r)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "UNAUTHORIZED", "message": "Invalid credentials"})
		return database.OwnerTokenIdentity{}, false
	}
	if identity.Kind != "primary" {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "FORBIDDEN", "message": "Token management requires the primary token"})
		return database.OwnerTokenIdentity{}, false
	}
	return identity, true
}

func decodeLinkedTokenRequest(w http.ResponseWriter, r *http.Request) (linkedTokenRequest, bool) {
	var body linkedTokenRequest
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid request body"})
		return linkedTokenRequest{}, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid request body"})
		return linkedTokenRequest{}, false
	}
	return body, true
}

func linkedTokenExpiry(expires, expiresOn *string, now time.Time) (sql.NullInt64, error) {
	if expires != nil && expiresOn != nil {
		return sql.NullInt64{}, errors.New("expires and expires_on are mutually exclusive")
	}
	if expiresOn != nil {
		value := strings.TrimSpace(*expiresOn)
		date, err := time.Parse("2006-01-02", value)
		if err != nil || date.Format("2006-01-02") != value {
			return sql.NullInt64{}, errors.New("expires_on must be a UTC date in YYYY-MM-DD format")
		}
		date = date.UTC()
		if date.Before(time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)) {
			return sql.NullInt64{}, errors.New("expires_on must be today or later")
		}
		return sql.NullInt64{Int64: date.AddDate(0, 0, 1).Unix(), Valid: true}, nil
	}
	value := "never"
	if expires != nil {
		value = strings.TrimSpace(*expires)
	}
	switch value {
	case "never":
		return sql.NullInt64{}, nil
	case "7d":
		return sql.NullInt64{Int64: now.UTC().Add(7 * 24 * time.Hour).Unix(), Valid: true}, nil
	case "30d":
		return sql.NullInt64{Int64: now.UTC().Add(30 * 24 * time.Hour).Unix(), Valid: true}, nil
	case "90d":
		return sql.NullInt64{Int64: now.UTC().Add(90 * 24 * time.Hour).Unix(), Valid: true}, nil
	case "1y":
		return sql.NullInt64{Int64: now.UTC().AddDate(1, 0, 0).Unix(), Valid: true}, nil
	default:
		return sql.NullInt64{}, errors.New("expires must be never, 7d, 30d, 90d, 1y, or expires_on")
	}
}

func validLinkedTokenName(value string) (string, bool) {
	value = strings.TrimSpace(value)
	length := utf8.RuneCountInString(value)
	return value, length >= 1 && length <= 64
}

func linkedTokenStatus(record database.OwnerTokenRecord, now time.Time) string {
	if record.RevokedAt.Valid {
		return "revoked"
	}
	if record.ExpiresAt.Valid && record.ExpiresAt.Int64 <= now.UTC().Unix() {
		return "expired"
	}
	return "active"
}

func linkedTokenResponse(record database.OwnerTokenRecord) map[string]any {
	var name any
	if record.Name.Valid {
		name = record.Name.String
	}
	return map[string]any{
		"id": record.ID, "kind": record.Kind, "name": name,
		"token_masked": "own_" + strings.Repeat("•", 24) + record.TokenLast4,
		"created_at":   time.Unix(record.CreatedAt, 0).UTC().Format(time.RFC3339),
		"last_used_at": nullableTimestamp(record.LastUsedAt), "expires_at": nullableTimestamp(record.ExpiresAt),
		"status": linkedTokenStatus(record, time.Now()),
	}
}

func (s *Server) handleCreateLinkedToken(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requirePrimaryTokenOwner(w, r)
	if !ok {
		return
	}
	body, ok := decodeLinkedTokenRequest(w, r)
	if !ok {
		return
	}
	name, validName := "", false
	if body.Name != nil {
		name, validName = validLinkedTokenName(*body.Name)
	}
	if !validName {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Name must contain 1 to 64 characters"})
		return
	}
	expiresAt, err := linkedTokenExpiry(body.Expires, body.ExpiresOn, time.Now())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": err.Error()})
		return
	}
	token, err := generateOwnerToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be created"})
		return
	}
	encrypted, err := encryptOwnerToken(token, s.cfg.Server.Security.EncryptionKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be created"})
		return
	}
	maxLinked := s.cfg.Server.Tokens.MaxLinked
	if maxLinked < 1 {
		maxLinked = 25
	}
	record, err := s.db.CreateLinkedToken(r.Context(), identity.OwnerID, name, hashOwnerToken(token), token[:12], token[len(token)-4:], encrypted, expiresAt, maxLinked)
	if errors.Is(err, database.ErrLinkedTokenConflict) {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "CONFLICT", "message": "The token name is already used or the linked token limit has been reached"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be created"})
		return
	}
	s.auditRequest(r, "token.created", "owner", map[string]interface{}{"owner_id": identity.OwnerID, "token_prefix": token[:12], "source": "linked"})
	data := linkedTokenResponse(record)
	data["linked_token"] = token
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "data": data})
}

func (s *Server) handleRevealLinkedToken(w http.ResponseWriter, r *http.Request) {
	if !s.enforceStrictRateLimit(w, r) {
		return
	}
	identity, ok := s.requirePrimaryTokenOwner(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	record, err := s.db.GetLinkedToken(r.Context(), identity.OwnerID, id)
	if errors.Is(err, database.ErrLinkedTokenNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NOT_FOUND", "message": "Token not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be loaded"})
		return
	}
	if linkedTokenStatus(record, time.Now()) != "active" {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NOT_FOUND", "message": "Token not found"})
		return
	}
	ciphertext, err := s.db.LinkedTokenCiphertext(r.Context(), identity.OwnerID, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be revealed"})
		return
	}
	token, err := decryptOwnerToken(ciphertext, s.cfg.Server.Security.EncryptionKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be revealed"})
		return
	}
	s.auditRequest(r, "token.revealed", "owner", map[string]interface{}{"owner_id": identity.OwnerID, "token_prefix": token[:12], "source": "api"})
	data := linkedTokenResponse(record)
	data["linked_token"] = token
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": data})
}

func (s *Server) handleRotateLinkedToken(w http.ResponseWriter, r *http.Request) {
	if !s.enforceStrictRateLimit(w, r) {
		return
	}
	identity, ok := s.requirePrimaryTokenOwner(w, r)
	if !ok {
		return
	}
	token, err := generateOwnerToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be rotated"})
		return
	}
	encrypted, err := encryptOwnerToken(token, s.cfg.Server.Security.EncryptionKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be rotated"})
		return
	}
	record, err := s.db.RotateLinkedToken(r.Context(), identity.OwnerID, chi.URLParam(r, "id"), hashOwnerToken(token), token[:12], token[len(token)-4:], encrypted)
	if errors.Is(err, database.ErrLinkedTokenNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NOT_FOUND", "message": "Token not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be rotated"})
		return
	}
	s.auditRequest(r, "token.rotated", "owner", map[string]interface{}{"owner_id": identity.OwnerID, "token_prefix": token[:12], "source": "api"})
	data := linkedTokenResponse(record)
	data["linked_token"] = token
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": data})
}

func (s *Server) handleUpdateLinkedToken(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requirePrimaryTokenOwner(w, r)
	if !ok {
		return
	}
	body, ok := decodeLinkedTokenRequest(w, r)
	if !ok {
		return
	}
	if body.Name == nil && body.Expires == nil && body.ExpiresOn == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "At least one token field must be changed"})
		return
	}
	id := chi.URLParam(r, "id")
	existing, err := s.db.GetLinkedToken(r.Context(), identity.OwnerID, id)
	if errors.Is(err, database.ErrLinkedTokenNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NOT_FOUND", "message": "Token not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be loaded"})
		return
	}
	name := existing.Name.String
	if body.Name != nil {
		var validName bool
		name, validName = validLinkedTokenName(*body.Name)
		if !validName {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Name must contain 1 to 64 characters"})
			return
		}
	}
	expiresAt := existing.ExpiresAt
	if body.Expires != nil || body.ExpiresOn != nil {
		expiresAt, err = linkedTokenExpiry(body.Expires, body.ExpiresOn, time.Now())
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": err.Error()})
			return
		}
	}
	updated, err := s.db.UpdateLinkedToken(r.Context(), identity.OwnerID, id, name, expiresAt)
	if errors.Is(err, database.ErrLinkedTokenNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NOT_FOUND", "message": "Token not found"})
		return
	}
	if errors.Is(err, database.ErrLinkedTokenConflict) {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "CONFLICT", "message": "The token name is already used"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be updated"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": linkedTokenResponse(updated)})
}

func (s *Server) handleRevokeLinkedToken(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requirePrimaryTokenOwner(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	record, err := s.db.GetLinkedToken(r.Context(), identity.OwnerID, id)
	if errors.Is(err, database.ErrLinkedTokenNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NOT_FOUND", "message": "Token not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be loaded"})
		return
	}
	err = s.db.RevokeLinkedToken(r.Context(), identity.OwnerID, id)
	if errors.Is(err, database.ErrLinkedTokenNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NOT_FOUND", "message": "Token not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be revoked"})
		return
	}
	s.auditRequest(r, "token.revoked", "owner", map[string]interface{}{"owner_id": identity.OwnerID, "token_prefix": record.TokenPrefix})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]string{"id": id, "status": "revoked"}})
}
