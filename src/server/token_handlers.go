package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/apimgr/mail/src/database"
)

func (s *Server) handleIssuePrimaryToken(w http.ResponseWriter, r *http.Request) {
	if !s.enforceStrictRateLimit(w, r) {
		return
	}
	valid, err := s.hasValidBearerToken(r)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Token service is unavailable"})
		return
	}
	if valid {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "CONFLICT", "message": "A valid bearer token is already present"})
		return
	}
	if s.db == nil || s.db.ServerDB == nil || s.cfg == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Token service is unavailable"})
		return
	}
	token, err := generateOwnerToken()
	if err != nil {
		log.Printf("[ERROR] Failed to generate owner token")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be created"})
		return
	}
	tokenEnc, err := encryptOwnerToken(token, s.cfg.Server.Security.EncryptionKey)
	if err != nil {
		log.Printf("[ERROR] Failed to encrypt owner token")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be created"})
		return
	}
	digest := hashOwnerToken(token)
	ownerID, err := s.db.CreateOwnerPrimaryToken(r.Context(), digest, token[:12], token[len(token)-4:], tokenEnc)
	if err != nil {
		log.Printf("[ERROR] Failed to persist owner token: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be created"})
		return
	}
	s.auditRequest(r, "token.created", "owner", map[string]interface{}{"owner_id": ownerID, "token_prefix": token[:12], "source": "issue"})
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "data": map[string]string{"owner_token": token}})
}

func (s *Server) handleListOwnerTokens(w http.ResponseWriter, r *http.Request) {
	if s.authFailureLocked(w, r) {
		return
	}
	identity, ok, err := s.ownerBearerIdentity(r)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Token service is unavailable"})
		return
	}
	if !ok {
		s.recordAuthFailure(r)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "UNAUTHORIZED", "message": "Invalid credentials"})
		return
	}
	if identity.Kind != "primary" {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "FORBIDDEN", "message": "Token management requires the primary token"})
		return
	}
	records, err := s.db.ListOwnerTokens(r.Context(), identity.OwnerID)
	if err != nil {
		log.Printf("[ERROR] Failed to list owner tokens")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token list could not be loaded"})
		return
	}
	var primary map[string]any
	linked := make([]map[string]any, 0)
	now := time.Now().UTC().Unix()
	for _, record := range records {
		status := "active"
		if record.RevokedAt.Valid {
			status = "revoked"
		} else if record.ExpiresAt.Valid && record.ExpiresAt.Int64 <= now {
			status = "expired"
		}
		masked := "own_" + strings.Repeat("•", 24) + record.TokenLast4
		var name any
		if record.Name.Valid {
			name = record.Name.String
		}
		item := map[string]any{
			"id": record.ID, "kind": record.Kind, "name": name,
			"token_masked": masked, "created_at": time.Unix(record.CreatedAt, 0).UTC().Format(time.RFC3339),
			"last_used_at": nullableTimestamp(record.LastUsedAt), "expires_at": nullableTimestamp(record.ExpiresAt), "status": status,
		}
		if record.Kind == "primary" {
			primary = item
		} else {
			linked = append(linked, item)
		}
	}
	if primary == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "UNAUTHORIZED", "message": "Invalid credentials"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	maxLinked := s.cfg.Server.Tokens.MaxLinked
	if maxLinked < 1 {
		maxLinked = 25
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]any{"primary": primary, "linked": linked, "max_linked": maxLinked}})
}

func (s *Server) handleRevealPrimaryToken(w http.ResponseWriter, r *http.Request) {
	if s.authFailureLocked(w, r) {
		return
	}
	if !s.enforceStrictRateLimit(w, r) {
		return
	}
	identity, ok, err := s.ownerBearerIdentity(r)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Token service is unavailable"})
		return
	}
	if !ok {
		s.recordAuthFailure(r)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "UNAUTHORIZED", "message": "Invalid credentials"})
		return
	}
	if identity.Kind != "primary" {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "FORBIDDEN", "message": "Token management requires the primary token"})
		return
	}
	ciphertext, err := s.db.OwnerPrimaryTokenCiphertext(r.Context(), identity.OwnerID)
	if err != nil {
		log.Printf("[ERROR] Failed to load owner primary token")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be revealed"})
		return
	}
	token, err := decryptOwnerToken(ciphertext, s.cfg.Server.Security.EncryptionKey)
	if err != nil {
		log.Printf("[ERROR] Failed to decrypt owner primary token")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "INTERNAL_ERROR", "message": "The token could not be revealed"})
		return
	}
	s.auditRequest(r, "token.revealed", "owner", map[string]interface{}{"owner_id": identity.OwnerID, "token_prefix": token[:12], "source": "api"})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]string{"owner_token": token}})
}

func nullableTimestamp(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return time.Unix(value.Int64, 0).UTC().Format(time.RFC3339)
}

func (s *Server) ownerBearerIdentity(r *http.Request) (database.OwnerTokenIdentity, bool, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return database.OwnerTokenIdentity{}, false, nil
	}
	fields := strings.Fields(values[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || !validOwnerToken(fields[1]) {
		return database.OwnerTokenIdentity{}, false, nil
	}
	digest := sha256.Sum256([]byte(fields[1]))
	if s.db == nil || s.db.ServerDB == nil {
		return database.OwnerTokenIdentity{}, false, errors.New("owner token store is unavailable")
	}
	identity, valid, err := s.db.ResolveOwnerToken(r.Context(), fields[1][:12], hex.EncodeToString(digest[:]))
	return identity, valid, err
}

func (s *Server) hasValidBearerToken(r *http.Request) (bool, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return false, nil
	}
	fields := strings.Fields(values[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return false, nil
	}
	token := fields[1]
	digest := sha256.Sum256([]byte(token))
	if strings.HasPrefix(token, "adm_") && subtle.ConstantTimeCompare(digest[:], s.operatorTokenHash[:]) == 1 {
		return true, nil
	}
	if !validOwnerToken(token) {
		return false, nil
	}
	if s.db == nil || s.db.ServerDB == nil {
		return false, errors.New("owner token store is unavailable")
	}
	_, valid, err := s.db.ResolveOwnerToken(r.Context(), token[:12], hex.EncodeToString(digest[:]))
	return valid, err
}
