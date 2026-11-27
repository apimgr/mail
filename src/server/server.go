package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/apimgr/mail/src/common/api"
	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
	"github.com/apimgr/mail/src/email"
	mailgraphql "github.com/apimgr/mail/src/graphql"
	"github.com/apimgr/mail/src/logger"
	"github.com/apimgr/mail/src/mailservice"
	"github.com/apimgr/mail/src/paths"
	"github.com/apimgr/mail/src/scheduler"
)

// Server represents the HTTP server
type Server struct {
	cfg               *config.Config
	db                *database.DB
	mailer            *email.Mailer
	buildDate         string
	router            *chi.Mux
	httpServer        *http.Server
	serveErr          chan error
	shutdownRequested chan struct{}
	shutdownOnce      sync.Once
	pages             *pageRenderer
	contentWarnings   sync.Map
	appPaths          *paths.Paths
	scheduler         *scheduler.Scheduler
	stats             *requestStats
	limiter           *rateLimiter
	operatorTokenHash [32]byte
	statsStop         chan struct{}
	statsDone         chan struct{}
	healthChecks      *healthCheckCache
	mailQueue         mailservice.QueueManager
	graphqlSchema     *mailgraphql.Schema
	startedAt         time.Time
}

var ipv6FallbackWarning sync.Once

func (s *Server) requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if !s.isTrustedProxy(r) {
		return false
	}
	proto := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]))
	if proto == "" && strings.EqualFold(r.Header.Get("X-Forwarded-Ssl"), "on") {
		proto = "https"
	}
	if proto == "" {
		proto = strings.ToLower(r.Header.Get("X-Url-Scheme"))
	}
	return proto == "https"
}

func (s *Server) isTrustedProxy(r *http.Request) bool {
	peer, err := netip.ParseAddr(strings.Split(r.RemoteAddr, ":")[0])
	if err != nil {
		if host, _, e := net.SplitHostPort(r.RemoteAddr); e == nil {
			peer, _ = netip.ParseAddr(host)
		}
	}
	trusted := false
	for _, entry := range s.cfg.Server.TrustedProxies {
		if prefix, e := netip.ParsePrefix(entry); e == nil && peer.IsValid() && prefix.Contains(peer) {
			trusted = true
			break
		}
		if addr, e := netip.ParseAddr(entry); e == nil && peer.IsValid() && addr == peer {
			trusted = true
			break
		}
	}
	return trusted
}

// New creates a new server instance
func New(cfg *config.Config, db *database.DB, buildDates ...string) *Server {
	buildDate := "unknown"
	if len(buildDates) > 0 && strings.TrimSpace(buildDates[0]) != "" {
		buildDate = strings.TrimSpace(buildDates[0])
	}
	s := &Server{
		cfg:               cfg,
		db:                db,
		buildDate:         buildDate,
		router:            chi.NewRouter(),
		pages:             newPageRenderer(),
		shutdownRequested: make(chan struct{}),
		appPaths:          paths.GetDefaultPaths(),
		stats:             newRequestStats(database.RequestStats{Hourly: make(map[int64]int64)}),
		healthChecks:      newHealthCheckCache(),
		mailQueue:         mailservice.PostfixQueue{},
		limiter:           newRateLimiter(nil),
	}
	if cfg != nil {
		s.operatorTokenHash = sha256.Sum256([]byte(cfg.Server.Token))
	}
	var graphqlErr error
	s.graphqlSchema, graphqlErr = mailgraphql.NewSchema(db, s.mailQueue)
	if graphqlErr != nil {
		log.Printf("GraphQL schema initialization failed: %v", graphqlErr)
	}
	if db != nil && db.ServerDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		initial, err := db.LoadRequestStats(ctx, time.Now())
		cancel()
		if err != nil {
			log.Printf("[WARN] Failed to load aggregate request stats: %v", err)
		} else {
			s.stats = newRequestStats(initial)
		}
		limits, err := db.LoadRateLimits(context.Background())
		if err != nil {
			log.Printf("[WARN] Failed to load rate-limit counters; using in-process counters: %v", err)
		} else {
			s.limiter = newRateLimiter(limits)
		}
	}
	s.refreshHealthChecks()

	// Setup middleware per AI.md PART 16
	s.setupMiddleware()

	// Setup routes
	s.setupRoutes()

	return s
}

func (s *Server) SetScheduler(value *scheduler.Scheduler) {
	s.scheduler = value
	s.refreshHealthChecks()
}

// SetMailer provides the configured mail transport to public contact flows.
func (s *Server) SetMailer(value *email.Mailer) { s.mailer = value }

func (s *Server) refreshHealthChecks() {
	schedRunning := s.scheduler != nil && s.scheduler.IsRunning()
	s.healthChecks.store(probeHealthChecks(s.db, s.appPaths, schedRunning))
}

// Handler returns the server router with outer request statistics enabled.
func (s *Server) Handler() http.Handler {
	return s.requestStatsMiddleware(s.router)
}

// setupMiddleware configures middleware stack
// URL normalization is first in the router middleware chain.
func (s *Server) setupMiddleware() {
	s.router.Use(s.urlNormalizeMiddleware)
	s.router.Use(middleware.RequestID)
	s.router.Use(pathSecurityMiddleware)
	s.router.Use(middleware.ClientIPFromRemoteAddr)
	s.router.Use(securityHeadersMiddleware)
	s.router.Use(s.corsMiddleware)
	s.router.Use(s.rateLimitMiddleware)
	s.router.Use(s.csrfMiddleware)
	s.router.Use(s.requestLoggingMiddleware)
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.Timeout(60 * time.Second))
}

func (s *Server) urlNormalizeMiddleware(next http.Handler) http.Handler {
	normalized := urlNormalizeMiddleware(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/debug" || strings.HasPrefix(r.URL.Path, "/debug/") {
			if s.cfg == nil || !s.cfg.Server.Debug {
				securityHeadersMiddleware(http.HandlerFunc(s.handleNotFound)).ServeHTTP(w, r)
				return
			}
			securityHeadersMiddleware(s.requireOperatorToken(normalized)).ServeHTTP(w, r)
			return
		}
		normalized.ServeHTTP(w, r)
	})
}

func urlNormalizeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath := r.URL.Path
		if requestPath == "/" || !strings.HasSuffix(requestPath, "/") {
			next.ServeHTTP(w, r)
			return
		}
		canonical := strings.TrimRight(requestPath, "/")
		lastSegment := canonical[strings.LastIndex(canonical, "/")+1:]
		if strings.Contains(lastSegment, ".") {
			next.ServeHTTP(w, r)
			return
		}
		if canonical == "" {
			canonical = "/"
		}
		if r.URL.RawQuery != "" {
			canonical += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, canonical, http.StatusMovedPermanently)
	})
}

func (s *Server) requestLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		response := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(response, r)
		status := response.Status()
		if status == 0 {
			status = http.StatusOK
		}
		if isSuccessfulHealthCheck(r.URL.Path, status) {
			return
		}
		remoteIP := r.RemoteAddr
		if host, _, err := net.SplitHostPort(remoteIP); err == nil {
			remoteIP = host
		}
		requestURL := redactedLogURL(r.URL).RequestURI()
		tlsVersion := ""
		if r.TLS != nil {
			tlsVersion = tlsVersionName(r.TLS.Version)
		}
		logger.AccessRequest(logger.AccessRecord{
			IP: remoteIP, Method: r.Method, Path: requestURL, Status: status, Bytes: response.BytesWritten(),
			UserAgent: r.UserAgent(), Referer: r.Referer(), Host: r.Host, Protocol: r.Proto,
			TLSVersion: tlsVersion, RequestID: middleware.GetReqID(r.Context()), Latency: time.Since(started), Time: started,
		})
		if status >= http.StatusInternalServerError || (isHealthCheckPath(r.URL.Path) && !isSuccessfulHealthCheck(r.URL.Path, status)) {
			logger.Errorf("HTTP request failed: %s %s status=%d bytes=%d duration=%s", r.Method, requestURL, status, response.BytesWritten(), time.Since(started).Round(time.Millisecond))
		}
	})
}

func tlsVersionName(version uint16) string {
	switch version {
	case 0x0301:
		return "TLS 1.0"
	case 0x0302:
		return "TLS 1.1"
	case 0x0303:
		return "TLS 1.2"
	case 0x0304:
		return "TLS 1.3"
	default:
		return ""
	}
}

func isSuccessfulHealthCheck(path string, status int) bool {
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return false
	}
	return isHealthCheckPath(path)
}

func isHealthCheckPath(path string) bool {
	return path == "/healthz" || path == "/api/healthz" || strings.HasSuffix(path, "/server/healthz") || strings.HasSuffix(path, "/server/healthz.txt")
}

func redactedLogURL(source *url.URL) *url.URL {
	redacted := *source
	values, err := url.ParseQuery(source.RawQuery)
	if err != nil {
		redacted.RawQuery = ""
		return &redacted
	}
	for key := range values {
		switch strings.ToLower(key) {
		case "token", "session", "code", "key", "password", "secret", "auth", "pwd", "api_key", "apikey", "access_token", "refresh_token":
			values.Del(key)
		}
	}
	redacted.RawQuery = values.Encode()
	return &redacted
}

func pathSecurityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.RawPath
		if raw == "" {
			raw = r.URL.Path
		}
		if strings.Contains(r.URL.Path, "..") || strings.Contains(raw, "..") || strings.Contains(strings.ToLower(raw), "%2e") {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		cleaned := path.Clean("/" + strings.TrimLeft(r.URL.Path, "/"))
		if cleaned != "/" && r.URL.Path != cleaned {
			if r.URL.RawQuery != "" {
				cleaned += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, cleaned, http.StatusMovedPermanently)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
		w.Header().Set("Origin-Agent-Cluster", "?1")
		w.Header().Set("Permissions-Policy", "accelerometer=(), attribution-reporting=(), browsing-topics=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), hid=(), idle-detection=(), magnetometer=(), microphone=(), midi=(), screen-wake-lock=(), serial=(), usb=(), xr-spatial-tracking=(), autoplay=(self), encrypted-media=(self), fullscreen=(self), payment=(self), picture-in-picture=(self), publickey-credentials-get=(self), storage-access=(self)")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; frame-ancestors 'self'")
		next.ServeHTTP(w, r)
	})
}

// setupRoutes configures all HTTP routes
// Per AI.md PART 14: All API routes must be versioned
func (s *Server) setupRoutes() {
	s.registerPages()
	s.router.Handle("/.well-known/*", http.HandlerFunc(s.handleWellKnown))
	s.router.Handle("/robots.txt", http.HandlerFunc(s.handleRobots))
	s.router.Handle("/sitemap.xml", http.HandlerFunc(s.handleSitemap))
	s.router.Handle("/feed.xml", http.HandlerFunc(s.handleFeed))
	s.router.Handle("/rss.xml", http.HandlerFunc(s.handleFeed))
	s.router.Handle("/llms.txt", http.HandlerFunc(s.handleLLMS))
	if s.cfg.Server.Healthz.Root.Enabled {
		s.router.Get("/healthz", s.handleHealthPage)
	}

	// API v1 routes
	apiRoutes := s.apiRouteRegistry()
	s.router.Route(api.APIBasePath(), func(r chi.Router) {
		for _, route := range apiRoutes {
			path := strings.TrimPrefix(routeMountPath(route.Path), api.APIBasePath())
			s.mountAPIRoute(r, route, path)
		}
	})
	for _, route := range apiRoutes {
		for _, alias := range route.Aliases {
			s.mountAPIRoute(s.router, route, alias)
		}
	}
	s.registerDebugRoutes(s.router)
}

func (s *Server) handleServices(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": mailservice.Inspect()})
}

func (s *Server) handleService(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	name := chi.URLParam(r, "service")
	component, err := mailservice.InspectOne(name)
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": component})
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NOT_FOUND", "message": "Service not found"})
}

func (s *Server) handleServiceAction(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	name := chi.URLParam(r, "service")
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request mailServiceOperationRequest
	if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Invalid service operation request"))
		return
	}
	action := strings.TrimSpace(request.Operation)
	switch action {
	case "start", "stop", "restart", "reload":
	default:
		writeJSON(w, http.StatusBadRequest, mailUserError("VALIDATION_FAILED", "Unsupported service operation"))
		return
	}
	component, err := mailservice.Operate(r.Context(), name, action)
	if err != nil {
		s.auditRequest(r, "mail.service.operation", "operator", map[string]any{"service": name, "action": action, "success": false})
		switch {
		case errors.Is(err, mailservice.ErrServiceNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "NOT_FOUND", "message": "Service not found"})
		case errors.Is(err, mailservice.ErrServiceMissing):
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "CONFLICT", "message": "Service is not installed"})
		case errors.Is(err, mailservice.ErrManagerMissing):
			writeJSON(w, http.StatusNotImplemented, map[string]any{"ok": false, "error": "NOT_IMPLEMENTED", "message": "Service control is unavailable on this host"})
		default:
			writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": "SERVICE_OPERATION_FAILED", "message": "The service operation failed"})
		}
		return
	}
	s.auditRequest(r, "mail.service.operation", "operator", map[string]any{"service": name, "action": action, "success": true})
	writeJSON(w, http.StatusOK, apiDataResponse[mailServiceOperationResponse]{
		OK: true, Data: mailServiceOperationResponse{Operation: action, Service: component},
	})
}

func (s *Server) requestIP(r *http.Request) string {
	peer := r.RemoteAddr
	if host, _, err := net.SplitHostPort(peer); err == nil {
		peer = host
	}
	if s.isTrustedProxy(r) {
		forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0])
		if ip := net.ParseIP(forwarded); ip != nil {
			return ip.String()
		}
	}
	if ip := net.ParseIP(peer); ip != nil {
		return ip.String()
	}
	return ""
}

// Start starts the HTTP server
// Per AI.md PART 8: Server Startup Sequence Phase 6, step 18
func (s *Server) Start() error {
	if s.startedAt.IsZero() {
		s.startedAt = time.Now()
	}
	addr := net.JoinHostPort(strings.Trim(s.cfg.Server.Address, "[]"), fmt.Sprint(s.cfg.Server.Port))
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.Handler(),

		// Timeouts per AI.md best practices
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
	}

	listener, err := CreateListener(s.cfg.Server.Address, s.cfg.Server.Port)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	return s.StartWithListener(listener)
}

// StartWithListener serves on a socket bound during privileged initialization.
func (s *Server) StartWithListener(listener net.Listener) error {
	if s.startedAt.IsZero() {
		s.startedAt = time.Now()
	}
	if listener == nil {
		return fmt.Errorf("listener is required")
	}
	if s.httpServer == nil {
		s.httpServer = &http.Server{
			Handler:           s.Handler(),
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
			ReadHeaderTimeout: 10 * time.Second,
		}
	}
	s.serveErr = make(chan error, 1)
	s.startRequestStatsFlusher()
	go func() {
		log.Printf("[INFO] Listening on %s", listener.Addr())
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.serveErr <- err
		}
	}()
	return nil
}

// Shutdown gracefully stops the server
// Per AI.md PART 8: Graceful shutdown on SIGTERM/SIGINT
func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("[INFO] Shutting down HTTP server...")
	if s.httpServer == nil {
		return nil
	}
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("HTTP server shutdown failed: %w", err)
	}
	s.stopRequestStatsFlusher(ctx)

	log.Println("[INFO] HTTP server stopped")
	return nil
}

// RequestShutdown asks WaitForShutdown to perform its normal graceful shutdown.
func (s *Server) RequestShutdown() {
	s.shutdownOnce.Do(func() { close(s.shutdownRequested) })
}

func (s *Server) startRequestStatsFlusher() {
	if s.db == nil || s.db.ServerDB == nil || s.statsStop != nil {
		return
	}
	s.statsStop = make(chan struct{})
	s.statsDone = make(chan struct{})
	go func() {
		defer close(s.statsDone)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.refreshHealthChecks()
				s.flushRequestStats(context.Background())
				s.flushRateLimits()
			case <-s.statsStop:
				s.flushRequestStats(context.Background())
				s.flushRateLimits()
				return
			}
		}
	}()
}

func (s *Server) stopRequestStatsFlusher(ctx context.Context) {
	if s.statsStop == nil {
		return
	}
	close(s.statsStop)
	select {
	case <-s.statsDone:
		s.statsStop = nil
		s.statsDone = nil
	case <-ctx.Done():
		log.Printf("[WARN] Timed out waiting for aggregate request stats flush")
	}
}

// WaitForShutdown blocks until shutdown signal received
// Per AI.md PART 8: Register signal handlers (step 19)
func (s *Server) WaitForShutdown() {
	quit := make(chan os.Signal, 8)
	defer signal.Stop(quit)

	// Shutdown signals and operational signals are handled in one process-owned
	// subscription so status requests and log rotation never terminate the server.
	signals := append(shutdownSignals(), operationalSignals()...)
	signal.Notify(quit, signals...)
	ignored := ignoredSignals()
	if len(ignored) > 0 {
		signal.Ignore(ignored...)
	}

	for {
		select {
		case sig := <-quit:
			switch operationalSignalAction(sig) {
			case "reopen":
				if err := logger.Reopen(); err != nil {
					logger.Warnf("SIGUSR1 log reopen failed: %v", err)
				} else {
					logger.Info("SIGUSR1 received; log files reopened")
				}
				continue
			case "status":
				s.dumpStatus()
				continue
			}
			log.Printf("[INFO] Received signal: %v", sig)
			goto shutdown
		case <-s.shutdownRequested:
			log.Printf("[INFO] Graceful shutdown requested by the application")
			goto shutdown
		case err := <-s.serveErr:
			log.Printf("[ERROR] HTTP server error: %v", err)
			goto shutdown
		}
	}

shutdown:
	// Graceful shutdown with 30 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.Shutdown(ctx); err != nil {
		log.Printf("[ERROR] Server shutdown error: %v", err)
	}
}

func (s *Server) dumpStatus() {
	now := time.Now()
	startedAt := s.startedAt
	if startedAt.IsZero() {
		startedAt = now
	}
	fields := map[string]interface{}{
		"uptime_seconds":  now.Sub(startedAt).Seconds(),
		"goroutines":      runtime.NumGoroutine(),
		"active_requests": int64(0),
		"requests_total":  int64(0),
	}
	if s.httpServer != nil {
		fields["listen_address"] = s.httpServer.Addr
	}
	if s.cfg != nil {
		fields["mode"] = s.cfg.Server.Mode
	}
	if s.stats != nil {
		stats := s.stats.snapshot(now)
		fields["active_requests"] = stats.ActiveConns
		fields["requests_total"] = stats.RequestsTotal
	}
	logger.InfoFields("Status snapshot", fields)
}

// Run starts the server and blocks until shutdown
// This is the main entry point for running the server
func (s *Server) Run() error {
	if err := s.Start(); err != nil {
		return err
	}

	s.WaitForShutdown()
	return nil
}

// Listener returns a TCP listener for the configured address
// Per AI.md PART 8: For privileged port binding before privilege drop
func CreateListener(address string, port int) (net.Listener, error) {
	address = strings.TrimSpace(strings.Trim(address, "[]"))
	parsed, err := netip.ParseAddr(address)
	if err != nil || !parsed.IsValid() {
		return nil, fmt.Errorf("listen address %q must be an IP address", address)
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("listen port %d is outside 1-65535", port)
	}
	addr := net.JoinHostPort(parsed.String(), fmt.Sprint(port))
	listener, err := net.Listen("tcp", addr)
	if err == nil {
		return listener, nil
	}
	if parsed.Is6() && parsed.IsUnspecified() {
		probe, probeErr := net.Listen("tcp6", "[::1]:0")
		if probeErr != nil {
			fallback, fallbackErr := net.Listen("tcp4", net.JoinHostPort("0.0.0.0", fmt.Sprint(port)))
			if fallbackErr == nil {
				ipv6FallbackWarning.Do(func() {
					log.Printf("[WARN] IPv6 is unavailable; listening on 0.0.0.0:%d instead", port)
				})
				return fallback, nil
			}
		}
		if probe != nil {
			_ = probe.Close()
		}
	}
	return nil, fmt.Errorf("listen on %s: %w", addr, err)
}
