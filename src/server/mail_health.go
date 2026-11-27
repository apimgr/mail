package server

import (
	"net/http"

	"github.com/apimgr/mail/src/mailservice"
)

type mailHealthSummary struct {
	Installed int `json:"installed"`
	Running   int `json:"running"`
	Stopped   int `json:"stopped"`
	Unknown   int `json:"unknown"`
}

func (s *Server) handleMailHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	components := mailservice.Inspect()
	summary := mailHealthSummary{}
	for _, component := range components {
		if !component.Installed {
			continue
		}
		summary.Installed++
		if component.Running == nil {
			summary.Unknown++
		} else if *component.Running {
			summary.Running++
		} else {
			summary.Stopped++
		}
	}
	status := mailHealthStatus(summary)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]any{
		"status": status, "summary": summary, "services": components,
	}})
}

func mailHealthStatus(summary mailHealthSummary) string {
	switch {
	case summary.Installed == 0:
		return "unavailable"
	case summary.Stopped > 0:
		return "degraded"
	case summary.Unknown > 0:
		return "unknown"
	default:
		return "healthy"
	}
}
