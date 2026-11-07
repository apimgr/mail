package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"path"
	"os"
	"os/signal"
	"syscall"
	"time"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/apimgr/mail/config"
	"github.com/apimgr/mail/database"
)

// Server represents the HTTP server
type Server struct {
	cfg    *config.Config
	db     *database.DB
	router     *chi.Mux
	httpServer *http.Server
	serveErr   chan error
}

// New creates a new server instance
func New(cfg *config.Config, db *database.DB) *Server {
	s := &Server{
		cfg:    cfg,
		db:     db,
		router: chi.NewRouter(),
	}

	// Setup middleware per AI.md PART 16
	s.setupMiddleware()

	// Setup routes
	s.setupRoutes()

	return s
}

// setupMiddleware configures middleware stack
// Per AI.md PART 16: PathSecurityMiddleware MUST be FIRST
func (s *Server) setupMiddleware() {
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.ClientIPFromRemoteAddr)
	s.router.Use(pathSecurityMiddleware)
	s.router.Use(securityHeadersMiddleware)
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.Timeout(60 * time.Second))
}

func pathSecurityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.RawPath
		if raw == "" { raw = r.URL.Path }
		if strings.Contains(r.URL.Path, "..") || strings.Contains(raw, "..") || strings.Contains(strings.ToLower(raw), "%2e") {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		cleaned := path.Clean("/" + strings.TrimLeft(r.URL.Path, "/"))
		if strings.HasSuffix(r.URL.Path, "/") && cleaned != "/" { cleaned += "/" }
		r.URL.Path = cleaned
		next.ServeHTTP(w, r)
	})
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// setupRoutes configures all HTTP routes
// Per AI.md PART 14: All API routes must be versioned
func (s *Server) setupRoutes() {
	// Health endpoint (not versioned per AI.md)
	s.router.Get("/server/healthz", s.handleHealth)
	s.router.Get("/healthz", s.handleHealth)
	
	// API v1 routes
	s.router.Route("/api/v1", func(r chi.Router) {
		// Server info routes (per AI.md PART 14)
		r.Get("/server/about", s.handleAbout)
		r.Get("/server/version", s.handleVersion)
		r.Get("/server/healthz", s.handleHealth)
		
		// TODO: Add remaining API routes per IDEA.md
	})
	
	// TODO: Admin panel routes (per AI.md PART 17)
	// TODO: Auth routes (per AI.md PART 14)
	// TODO: User routes (per AI.md PART 14)
}

// Start starts the HTTP server
// Per AI.md PART 8: Server Startup Sequence Phase 6, step 18
func (s *Server) Start() error {
	addr := net.JoinHostPort(strings.Trim(s.cfg.Server.Address, "[]"), fmt.Sprint(s.cfg.Server.Port))
	
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.router,
		
		// Timeouts per AI.md best practices
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
	}
	
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	s.serveErr = make(chan error, 1)
	go func() {
		log.Printf("[INFO] Listening on %s", addr)
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
	if s.httpServer == nil { return nil }
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("HTTP server shutdown failed: %w", err)
	}
	
	log.Println("[INFO] HTTP server stopped")
	return nil
}

// WaitForShutdown blocks until shutdown signal received
// Per AI.md PART 8: Register signal handlers (step 19)
func (s *Server) WaitForShutdown() {
	quit := make(chan os.Signal, 1)
	
	// Per AI.md PART 8: SIGTERM, SIGINT, SIGQUIT for graceful shutdown
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
	
	select {
	case sig := <-quit:
		log.Printf("[INFO] Received signal: %v", sig)
	case err := <-s.serveErr:
		log.Printf("[ERROR] HTTP server error: %v", err)
		return
	}
	
	// Graceful shutdown with 30 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	if err := s.Shutdown(ctx); err != nil {
		log.Printf("[ERROR] Server shutdown error: %v", err)
	}
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
	addr := fmt.Sprintf("%s:%d", address, port)
	return net.Listen("tcp", addr)
}
