package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	commonformat "github.com/apimgr/mail/src/common/format"
	"github.com/apimgr/mail/src/common/i18n"
)

var processStartedAt = time.Now()

type embeddedBuildMetadata struct {
	version string
	commit  string
	date    string
}

var buildMetadata struct {
	sync.RWMutex
	embeddedBuildMetadata
}

// SetBuildMetadata supplies the values embedded in the server executable.
func SetBuildMetadata(version, commit, date string) {
	buildMetadata.Lock()
	buildMetadata.embeddedBuildMetadata = embeddedBuildMetadata{
		version: strings.TrimSpace(version),
		commit:  strings.TrimSpace(commit),
		date:    strings.TrimSpace(date),
	}
	buildMetadata.Unlock()
}

func getBuildMetadata() embeddedBuildMetadata {
	buildMetadata.RLock()
	defer buildMetadata.RUnlock()
	return buildMetadata.embeddedBuildMetadata
}

type healthProject struct {
	Name        string `json:"name"`
	Tagline     string `json:"tagline"`
	Description string `json:"description"`
}

type healthBuild struct {
	Commit string `json:"commit"`
	Date   string `json:"date"`
}

type healthTor struct {
	Enabled  bool   `json:"enabled"`
	Running  bool   `json:"running"`
	State    string `json:"state"`
	Status   string `json:"status"`
	Hostname string `json:"hostname"`
}

type healthI2P struct {
	Enabled  bool   `json:"enabled"`
	Running  bool   `json:"running"`
	State    string `json:"state"`
	Status   string `json:"status"`
	Hostname string `json:"hostname"`
	Provider string `json:"provider"`
}

type healthFeatures struct {
	Tor   healthTor `json:"tor"`
	I2P   healthI2P `json:"i2p"`
	GeoIP bool      `json:"geoip"`
}

type healthChecks struct {
	Database  string `json:"database"`
	Cache     string `json:"cache"`
	Disk      string `json:"disk"`
	Scheduler string `json:"scheduler"`
}

type healthStats struct {
	RequestsTotal int64 `json:"requests_total"`
	Requests24h   int64 `json:"requests_24h"`
	ActiveConns   int64 `json:"active_connections"`
}

// healthResponse preserves the public field order required by AI.md PART 13.
type healthResponse struct {
	Project        healthProject  `json:"project"`
	Status         string         `json:"status"`
	PendingRestart bool           `json:"pending_restart,omitempty"`
	RestartReason  []string       `json:"restart_reason,omitempty"`
	Version        string         `json:"version"`
	GoVersion      string         `json:"go_version"`
	Build          healthBuild    `json:"build"`
	Uptime         string         `json:"uptime"`
	Mode           string         `json:"mode"`
	Timestamp      time.Time      `json:"timestamp"`
	Features       healthFeatures `json:"features"`
	Checks         healthChecks   `json:"checks"`
	Stats          healthStats    `json:"stats"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	result := s.buildHealthResponse(r)
	status := healthHTTPStatus(result.Status)
	if isAPIHealthRoute(r) && healthWantsText(r) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		writeHealthAPIText(w, result)
		return
	}
	writeJSON(w, status, result)
}

func (s *Server) buildHealthResponse(r *http.Request) healthResponse {
	checks := s.healthChecks.load()
	state := "healthy"
	if checks.Cache == "error" || checks.Scheduler == "error" {
		state = "degraded"
	}
	if checks.Database == "error" || checks.Disk == "error" {
		state = "unhealthy"
	}
	if s.cfg != nil && s.cfg.Server.Maintenance {
		state = "maintenance"
	}
	mode := "production"
	if s.cfg != nil {
		if configured := strings.ToLower(strings.TrimSpace(s.cfg.Server.Mode)); configured == "production" || configured == "development" || configured == "debug" {
			mode = configured
		} else if s.cfg.Server.Debug {
			mode = "debug"
		}
	}
	build := currentBuildInfo()
	if s.buildDate != "" && s.buildDate != "unknown" {
		if stamp, err := time.Parse(time.RFC3339, s.buildDate); err == nil {
			build.Date = stamp.UTC().Truncate(time.Second).Format(time.RFC3339)
		}
	}
	return healthResponse{
		Project: healthProject{
			Name:        "mail",
			Tagline:     "Self-hosted mail infrastructure management",
			Description: "A management service for mail infrastructure and its supporting services.",
		},
		Status:    state,
		Version:   applicationVersion(),
		GoVersion: runtime.Version(),
		Build:     build,
		Uptime:    compactUptime(time.Since(processStartedAt)),
		Mode:      mode,
		Timestamp: time.Now().UTC().Truncate(time.Second),
		Features: healthFeatures{
			Tor: healthTor{State: "disabled", Status: "disabled"},
			I2P: healthI2P{State: "disabled", Status: "disabled", Provider: "none"},
		},
		Checks: checks,
		Stats:  s.stats.snapshot(time.Now()),
	}
}

func accessibleDirectory(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func healthHTTPStatus(status string) int {
	switch status {
	case "unhealthy", "maintenance", "shutting_down":
		return http.StatusServiceUnavailable
	default:
		return http.StatusOK
	}
}

func healthCheckClass(status string) string {
	switch status {
	case "ok":
		return "status-ok"
	case "degraded":
		return "status-warning"
	default:
		return "status-error"
	}
}

func isAPIHealthRoute(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/")
}

func healthWantsText(r *http.Request) bool {
	if strings.HasSuffix(r.URL.Path, ".txt") {
		return true
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	if strings.Contains(accept, "application/json") {
		return false
	}
	if strings.Contains(accept, "text/plain") {
		return true
	}
	if strings.TrimSpace(accept) != "" {
		return false
	}
	userAgent := strings.ToLower(r.UserAgent())
	if userAgent == "" || strings.Contains(userAgent, "curl/") || strings.Contains(userAgent, "wget/") || strings.Contains(userAgent, "httpie/") || strings.Contains(userAgent, "go-http-client") || strings.Contains(userAgent, "python-requests") {
		return true
	}
	return strings.Contains(userAgent, "lynx") || strings.Contains(userAgent, "w3m") || strings.Contains(userAgent, "links/")
}

func writeHealthAPIText(w http.ResponseWriter, health healthResponse) {
	geoIP := "disabled"
	if health.Features.GeoIP {
		geoIP = "enabled"
	}
	_, _ = fmt.Fprintf(w, "project.name: %s\nproject.tagline: %s\nproject.description: %s\nstatus: %s\nversion: %s\ngo_version: %s\nbuild.commit: %s\nbuild.date: %s\nuptime: %s\nmode: %s\ntimestamp: %s\nfeatures.tor: %s\nfeatures.i2p: %s provider=%s state=%s\nfeatures.geoip: %s\nchecks.database: %s\nchecks.cache: %s\nchecks.disk: %s\nchecks.scheduler: %s\nstats.requests_total: %d\nstats.requests_24h: %d\nstats.active_connections: %d\n",
		health.Project.Name, health.Project.Tagline, health.Project.Description, health.Status, health.Version,
		health.GoVersion, health.Build.Commit, health.Build.Date, health.Uptime, health.Mode,
		health.Timestamp.Format(time.RFC3339), health.Features.Tor.Status, health.Features.I2P.Status,
		health.Features.I2P.Provider, health.Features.I2P.State, geoIP, health.Checks.Database, health.Checks.Cache, health.Checks.Disk,
		health.Checks.Scheduler, health.Stats.RequestsTotal, health.Stats.Requests24h, health.Stats.ActiveConns)
}

func writeHealthFrontendText(w http.ResponseWriter, health healthResponse, lang string, translate func(string) string, formatTime func(string) string, formatDuration func(time.Duration) string) {
	geoIP := translate("health.state.disabled")
	if health.Features.GeoIP {
		geoIP = translate("health.state.enabled")
	}
	_, _ = fmt.Fprintf(w, "%s - %s\n\n%s: %s\n%s: %s\n%s: %s (%s)\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n\n%s\n  %s: %s\n  %s: %s (%s: %s)\n  %s: %s\n\n%s\n  %s: %s\n  %s: %s\n  %s: %s\n  %s: %s\n\n%s\n  %s: %s\n  %s: %s\n  %s: %s\n",
		health.Project.Name, translate("health.page.title"),
		translate("health.status_label"), translate("health.state."+health.Status),
		translate("health.version"), health.Version,
		translate("health.build"), health.Build.Commit, formatTime(health.Build.Date),
		translate("health.go_version"), health.GoVersion,
		translate("health.uptime"), formatDuration(time.Since(processStartedAt)),
		translate("health.mode"), translate("health.mode."+health.Mode),
		translate("health.checked"), formatTime(health.Timestamp.Format(time.RFC3339)),
		translate("health.features"), translate("health.feature.tor"), translate("health.state."+health.Features.Tor.Status),
		translate("health.feature.i2p"), translate("health.state."+health.Features.I2P.Status), translate("health.provider"), health.Features.I2P.Provider,
		translate("health.feature.geoip"), geoIP,
		translate("health.components"), translate("health.database"), translate("health.check."+health.Checks.Database),
		translate("health.cache"), translate("health.check."+health.Checks.Cache), translate("health.disk"), translate("health.check."+health.Checks.Disk),
		translate("health.scheduler"), translate("health.check."+health.Checks.Scheduler),
		translate("health.statistics"), translate("health.requests_total"), commonformat.Count(lang, health.Stats.RequestsTotal),
		translate("health.requests_24h"), commonformat.Count(lang, health.Stats.Requests24h), translate("health.active_connections"), commonformat.Count(lang, health.Stats.ActiveConns))
}

func humanTimestamp(lang, value string, catalog *i18n.Catalog) string {
	stamp, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	return commonformat.DateTime(stamp.UTC(), lang, catalog)
}

func currentBuildInfo() healthBuild {
	embedded := getBuildMetadata()
	build := healthBuild{Commit: "unknown", Date: "unknown"}
	info, ok := debug.ReadBuildInfo()
	if ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision := setting.Value
				if len(revision) > 7 {
					revision = revision[:7]
				}
				build.Commit = revision
			case "vcs.time":
				if stamp, err := time.Parse(time.RFC3339, setting.Value); err == nil {
					build.Date = stamp.UTC().Truncate(time.Second).Format(time.RFC3339)
				}
			}
		}
	}
	if embedded.commit != "" {
		build.Commit = embedded.commit
		if len(build.Commit) > 7 {
			build.Commit = build.Commit[:7]
		}
	}
	if stamp, err := time.Parse(time.RFC3339, embedded.date); err == nil {
		build.Date = stamp.UTC().Truncate(time.Second).Format(time.RFC3339)
	}
	return build
}

func compactUptime(elapsed time.Duration) string {
	if elapsed < time.Minute {
		seconds := int(elapsed.Seconds())
		if seconds < 0 {
			seconds = 0
		}
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := int(elapsed / time.Minute)
	days := minutes / (24 * 60)
	hours := (minutes / 60) % 24
	minutes %= 60
	parts := make([]string, 0, 3)
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	return strings.Join(parts, " ")
}

func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	language := s.preferredPageLanguage(r)
	page := &brandingPageView{Content: s.builtinAboutContent(r), Format: "markdown"}
	if format, body, usable := s.operatorBody(r, "about"); usable {
		page.Content = body
		page.Format = format
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]string{
		"title": s.pages.translate(language, "page.about.title"), "content": page.Content, "format": page.Format,
	}})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	build := currentBuildInfo()
	writeJSON(w, http.StatusOK, map[string]string{
		"version":    applicationVersion(),
		"commit":     build.Commit,
		"build_date": build.Date,
		"go_version": runtime.Version(),
		"platform":   runtime.GOOS + "/" + runtime.GOARCH,
	})
}

func applicationVersion() string {
	if embedded := getBuildMetadata(); embedded.version != "" {
		return embedded.version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return strings.TrimPrefix(info.Main.Version, "v")
		}
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				revision := setting.Value
				if len(revision) > 7 {
					revision = revision[:7]
				}
				return revision
			}
		}
	}
	return "dev"
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
