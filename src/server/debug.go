package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"expvar"
	"net/http"
	"net/http/pprof"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/apimgr/mail/src/database"
)

func (s *Server) registerDebugRoutes(router chi.Router) {
	if s.cfg == nil || !s.cfg.Server.Debug {
		return
	}
	router.Route("/debug", func(r chi.Router) {
		r.Get("/pprof", s.handleDebugPprofIndex)
		r.Get("/pprof/", func(w http.ResponseWriter, request *http.Request) {
			http.Redirect(w, request, "/debug/pprof", http.StatusMovedPermanently)
		})
		r.Handle("/pprof/cmdline", http.HandlerFunc(s.handleDebugCommandLine))
		r.Handle("/pprof/profile", http.HandlerFunc(pprof.Profile))
		r.Handle("/pprof/symbol", http.HandlerFunc(pprof.Symbol))
		r.Handle("/pprof/trace", http.HandlerFunc(pprof.Trace))
		for _, profile := range []string{"heap", "goroutine", "allocs", "block", "mutex", "threadcreate"} {
			r.Handle("/pprof/"+profile, pprof.Handler(profile))
		}
		r.Get("/vars", s.handleDebugVars)
		r.Get("/config", s.handleDebugConfig)
		r.Get("/routes", s.handleDebugRoutes)
		r.Get("/cache", s.handleDebugCache)
		r.Get("/db", s.handleDebugDatabase)
		r.Get("/scheduler", s.handleDebugScheduler)
		r.Get("/memory", s.handleDebugMemory)
		r.Get("/goroutines", s.handleDebugGoroutines)
		r.Get("/error500", s.handleDebugError500)
	})
}

func (s *Server) handleDebugPprofIndex(w http.ResponseWriter, r *http.Request) {
	request := r.Clone(r.Context())
	request.URL = new(url.URL)
	*request.URL = *r.URL
	request.URL.Path += "/"
	pprof.Index(w, request)
}

func (s *Server) requireOperatorToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authFailureLocked(w, r) {
			return
		}
		values := r.Header.Values("Authorization")
		token := ""
		validFormat := false
		if len(values) == 1 {
			fields := strings.Fields(values[0])
			if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
				token = fields[1]
				validFormat = true
			}
		}
		providedHash := sha256.Sum256([]byte(token))
		validToken := subtle.ConstantTimeCompare(providedHash[:], s.operatorTokenHash[:]) == 1
		if !validFormat || !validToken {
			s.recordAuthFailure(r)
			challenge := `Bearer realm="mail"`
			if len(values) > 0 {
				challenge += `, error="invalid_token"`
			}
			w.Header().Set("WWW-Authenticate", challenge)
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"ok": false, "error": "UNAUTHORIZED", "message": "Invalid credentials",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleDebugCommandLine(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(filepath.Base(os.Args[0]) + "\x00"))
}

func (s *Server) handleDebugVars(w http.ResponseWriter, _ *http.Request) {
	variables := make(map[string]json.RawMessage)
	expvar.Do(func(value expvar.KeyValue) {
		if value.Key == "cmdline" {
			encoded, _ := json.Marshal([]string{filepath.Base(os.Args[0])})
			variables[value.Key] = encoded
			return
		}
		variables[value.Key] = json.RawMessage(value.Value.String())
	})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, variables)
}

func (s *Server) handleDebugConfig(w http.ResponseWriter, _ *http.Request) {
	result := map[string]any{"server": map[string]any{}, "database": map[string]any{}}
	if s.cfg != nil {
		result["server"] = map[string]any{
			"mode": s.cfg.Server.Mode, "debug": s.cfg.Server.Debug, "port": s.cfg.Server.Port,
		}
		result["database"] = map[string]any{"driver": s.cfg.Database.Driver}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleDebugRoutes(w http.ResponseWriter, _ *http.Request) {
	routes := make([]string, 0)
	var collect func(chi.Routes)
	collect = func(current chi.Routes) {
		for _, route := range current.Routes() {
			for method := range route.Handlers {
				routes = append(routes, method+" "+route.Pattern)
			}
			if route.SubRoutes != nil {
				collect(route.SubRoutes)
			}
		}
	}
	collect(s.router)
	sort.Strings(routes)
	writeJSON(w, http.StatusOK, map[string]any{"routes": routes, "count": len(routes)})
}

func (s *Server) handleDebugCache(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"backend": "none", "connected": false, "entries": 0})
}

func (s *Server) handleDebugDatabase(w http.ResponseWriter, _ *http.Request) {
	result := map[string]any{"connected": false}
	if s.db != nil && s.db.ServerDB != nil {
		stats := s.db.ServerDB.Stats()
		result = map[string]any{
			"connected": true, "open_connections": stats.OpenConnections,
			"in_use": stats.InUse, "idle": stats.Idle,
			"wait_count": stats.WaitCount, "wait_duration_ns": stats.WaitDuration.Nanoseconds(),
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleDebugScheduler(w http.ResponseWriter, _ *http.Request) {
	tasks := []database.SchedulerTaskState{}
	running := false
	if s.scheduler != nil {
		running = s.scheduler.IsRunning()
		tasks = s.scheduler.TaskStates()
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	writeJSON(w, http.StatusOK, map[string]any{"running": running, "tasks": tasks})
}

func (s *Server) handleDebugMemory(w http.ResponseWriter, _ *http.Request) {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	writeJSON(w, http.StatusOK, memory)
}

func (s *Server) handleDebugGoroutines(w http.ResponseWriter, _ *http.Request) {
	buffer := make([]byte, 1<<20)
	length := runtime.Stack(buffer, true)
	lines := strings.Split(string(buffer[:length]), "\n")
	functions := make(map[string]int)
	for i := 0; i+1 < len(lines); i++ {
		if strings.HasPrefix(lines[i], "goroutine ") {
			function := strings.TrimSpace(lines[i+1])
			if name, _, ok := strings.Cut(function, "("); ok {
				functions[name]++
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count": runtime.NumGoroutine(), "top_functions": functions, "stack_truncated": length == len(buffer),
	})
}

func (s *Server) handleDebugError500(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, http.StatusInternalServerError, pageContent{
		TitleKey: "page.error.title", DescriptionKey: "page.error.lead",
		Sections: []pageSection{{ID: "internal-error", HeadingKey: "page.error.title", BodyKey: "page.error.lead"}},
	})
}
