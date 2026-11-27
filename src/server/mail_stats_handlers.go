package server

import (
	"net/http"
	"time"
)

type mailStatsPeriod struct {
	Name     string
	Duration time.Duration
}

type mailDeliveryStatsResponse struct {
	Period   string           `json:"period"`
	Since    string           `json:"since"`
	Total    int64            `json:"total"`
	ByStatus map[string]int64 `json:"by_status"`
}

type mailSpamStatsResponse struct {
	Period      string `json:"period"`
	Since       string `json:"since"`
	Granularity string `json:"granularity"`
	SpamCount   int64  `json:"spam_count"`
	VirusCount  int64  `json:"virus_count"`
}

var mailStatsPeriods = map[string]time.Duration{
	"24h":  24 * time.Hour,
	"7d":   7 * 24 * time.Hour,
	"30d":  30 * 24 * time.Hour,
	"90d":  90 * 24 * time.Hour,
	"365d": 365 * 24 * time.Hour,
}

func mailStatsPeriodParameter() RouteParameter {
	return RouteParameter{
		Name: "period", In: "query", Description: "Statistics period: 24h, 7d, 30d, 90d, or 365d",
		Type: "string", Default: "30d",
	}
}

func readMailStatsPeriod(w http.ResponseWriter, r *http.Request) (mailStatsPeriod, bool) {
	name := r.URL.Query().Get("period")
	if name == "" {
		name = "30d"
	}
	duration, ok := mailStatsPeriods[name]
	if !ok {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Period must be one of 24h, 7d, 30d, 90d, or 365d"))
		return mailStatsPeriod{}, false
	}
	return mailStatsPeriod{Name: name, Duration: duration}, true
}

func (s *Server) handleMailDeliveryStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	period, ok := readMailStatsPeriod(w, r)
	if !ok {
		return
	}
	if s.db == nil || s.db.UsersReadDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, mailUserError("SERVICE_UNAVAILABLE", "Mail statistics storage is unavailable"))
		return
	}
	since := time.Now().UTC().Add(-period.Duration).Truncate(time.Second)
	stats, err := s.db.GetMailDeliveryStats(r.Context(), since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, mailUserError("INTERNAL_ERROR", "Mail delivery statistics could not be loaded"))
		return
	}
	writeJSON(w, http.StatusOK, apiDataResponse[mailDeliveryStatsResponse]{OK: true, Data: mailDeliveryStatsResponse{
		Period: period.Name, Since: since.Format(time.RFC3339), Total: stats.Total, ByStatus: stats.ByStatus,
	}})
}

func (s *Server) handleMailSpamStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	period, ok := readMailStatsPeriod(w, r)
	if !ok {
		return
	}
	if s.db == nil || s.db.UsersReadDB == nil {
		writeJSON(w, http.StatusServiceUnavailable, mailUserError("SERVICE_UNAVAILABLE", "Mail statistics storage is unavailable"))
		return
	}
	since := time.Now().UTC().Add(-period.Duration).Truncate(time.Second)
	stats, err := s.db.GetMailSpamStats(r.Context(), since, period.Duration)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, mailUserError("INTERNAL_ERROR", "Mail spam statistics could not be loaded"))
		return
	}
	writeJSON(w, http.StatusOK, apiDataResponse[mailSpamStatsResponse]{OK: true, Data: mailSpamStatsResponse{
		Period: period.Name, Since: since.Format(time.RFC3339), Granularity: stats.Granularity,
		SpamCount: stats.Spam, VirusCount: stats.Virus,
	}})
}
