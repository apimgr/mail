package server

import (
	"net/http"
	"strings"
)

func (s *Server) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	if !s.enforceStrictRateLimit(w, r) || s.authFailureLocked(w, r) {
		return
	}
	valid, err := s.hasValidBearerToken(r)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "Token validation is unavailable"})
		return
	}
	if !valid {
		s.recordAuthFailure(r)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "UNAUTHORIZED", "message": "Invalid credentials"})
		return
	}
	fields := strings.Fields(r.Header.Get("Authorization"))
	kind := "owner"
	if len(fields) == 2 && strings.HasPrefix(fields[1], "adm_") {
		kind = "operator"
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"kind": kind})
}
