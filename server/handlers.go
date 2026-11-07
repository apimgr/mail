package server

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"runtime/debug"
	"time"
)

// handleHealth handles /healthz endpoint
// Per AI.md PART 13: Health endpoint for monitoring
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := "healthy"
	code := http.StatusOK
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if s.db == nil || s.db.ServerDB == nil || s.db.UsersDB == nil || s.db.ServerDB.PingContext(ctx) != nil || (s.db.UsersDB != s.db.ServerDB && s.db.UsersDB.PingContext(ctx) != nil) {
		status = "unhealthy"
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]string{"status": status})
}

func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"name": "mail",
		"description": "Web-based control panel for the complete mail server stack.",
		"version": applicationVersion(),
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	version := map[string]string{
		"version": applicationVersion(),
		"commit": "unknown",
		"build_date": "unknown",
		"go_version": runtime.Version(),
		"platform": runtime.GOOS + "/" + runtime.GOARCH,
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision": version["commit"] = setting.Value
			case "vcs.time": version["build_date"] = setting.Value
			}
		}
	}
	writeJSON(w, http.StatusOK, version)
}

func applicationVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
