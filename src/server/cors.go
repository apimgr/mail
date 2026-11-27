package server

import (
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	corsAllowedHeaders = "Content-Type, Accept, X-Requested-With, Authorization, X-CSRF-Token, X-XSRF-Token, MCP-Protocol-Version, Mcp-Method, Mcp-Name"
	corsAllowedMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
)

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		origins, disabled := s.corsOrigins(r)
		if !disabled && origin != "" {
			if normalizedOrigin, ok := normalizeOrigin(origin); ok && containsOrigin(origins, normalizedOrigin) {
				w.Header().Set("Access-Control-Allow-Origin", normalizedOrigin)
				w.Header().Add("Vary", "Origin")
				if s.cfg.Server.CORS.AllowCredentials {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
			} else if containsOrigin(origins, "*") {
				w.Header().Set("Access-Control-Allow-Origin", "*")
				w.Header().Add("Vary", "Origin")
			}
		}

		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.Header().Add("Vary", "Access-Control-Request-Method")
			w.Header().Add("Vary", "Access-Control-Request-Headers")
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				w.Header().Set("Access-Control-Allow-Methods", corsAllowedMethods)
				w.Header().Set("Access-Control-Allow-Headers", corsAllowedHeaders)
				if s.cfg.Server.CORS.MaxAge > 0 {
					w.Header().Set("Access-Control-Max-Age", strconv.Itoa(s.cfg.Server.CORS.MaxAge))
				}
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) corsOrigins(r *http.Request) ([]string, bool) {
	configured := s.cfg.Server.CORS.AllowedOrigins
	if len(configured) == 1 && configured[0] == "" {
		return nil, true
	}
	origins := make([]string, 0, len(configured)+4)
	for _, origin := range configured {
		if strings.TrimSpace(origin) == "*" {
			origins = append(origins, "*")
			continue
		}
		if normalized, ok := normalizeOrigin(strings.TrimSpace(origin)); ok {
			origins = append(origins, normalized)
		}
	}
	for _, domain := range strings.FieldsFunc(os.Getenv("DOMAIN"), func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		if !strings.Contains(domain, "://") {
			domain = "https://" + domain
		}
		if normalized, ok := normalizeOrigin(domain); ok {
			origins = appendUnique(origins, normalized)
		}
	}
	if s.isTrustedProxy(r) {
		host := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0])
		if validPublicHost(host) {
			scheme := "http"
			if s.requestIsHTTPS(r) {
				scheme = "https"
			}
			if normalized, ok := normalizeOrigin(scheme + "://" + host); ok {
				origins = appendUnique(origins, normalized)
			}
		}
	}
	if len(origins) == 0 {
		return []string{"*"}, false
	}
	return origins, false
}

func normalizeOrigin(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	return scheme + "://" + strings.ToLower(u.Host), true
}

func containsOrigin(origins []string, want string) bool {
	for _, origin := range origins {
		if origin == want {
			return true
		}
	}
	return false
}

func appendUnique(origins []string, value string) []string {
	if !containsOrigin(origins, value) {
		return append(origins, value)
	}
	return origins
}
