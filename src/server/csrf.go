package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
)

type csrfContextKey struct{}

const csrfCookieName = "csrf_token"

func (s *Server) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/server/contact" && !s.contactFormEnabled() {
			s.handleNotFound(w, r)
			return
		}
		if isBearerMCPRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			if r.Method == http.MethodGet && isHTMLPageRequest(r) && isServerRenderedPage(r.URL.Path) {
				r = s.ensureCSRFToken(w, r)
			}
			next.ServeHTTP(w, r)
			return
		}

		if !isServerRenderedPage(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			writeCSRFError(w)
			return
		}
		cookie, err := r.Cookie(csrfCookieName)
		if err != nil {
			writeCSRFError(w)
			return
		}
		submitted := r.Header.Get("X-CSRF-Token")
		if submitted == "" {
			submitted = r.PostForm.Get("csrf_token")
		}
		if submitted == "" || len(cookie.Value) != len(submitted) || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(submitted)) != 1 {
			writeCSRFError(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isBearerMCPRequest(r *http.Request) bool {
	if r.Method != http.MethodPost || (r.URL.Path != "/mcp" && r.URL.Path != "/api/mcp") {
		return false
	}
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	return len(value) > len("Bearer ") && strings.EqualFold(value[:len("Bearer ")], "Bearer ")
}

func isServerRenderedPage(path string) bool {
	if path == "/" {
		return true
	}
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/.well-known/") {
		return false
	}
	switch path {
	case "/manifest.json", "/sw.js", "/robots.txt", "/sitemap.xml", "/feed.xml", "/rss.xml", "/llms.txt", "/server/preferences/sync/import":
		return false
	}
	return true
}

func isHTMLPageRequest(r *http.Request) bool {
	accept := strings.ToLower(r.Header.Get("Accept"))
	return accept == "" || strings.Contains(accept, "text/html")
}

func (s *Server) ensureCSRFToken(w http.ResponseWriter, r *http.Request) *http.Request {
	if cookie, err := r.Cookie(csrfCookieName); err == nil && len(cookie.Value) >= 32 {
		return r.WithContext(context.WithValue(r.Context(), csrfContextKey{}, cookie.Value))
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return r
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		SameSite: http.SameSiteStrictMode,
		Secure:   s.requestIsHTTPS(r),
	})
	return r.WithContext(context.WithValue(r.Context(), csrfContextKey{}, token))
}

func csrfTokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(csrfContextKey{}).(string)
	return token
}

func writeCSRFError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      false,
		"error":   "CSRF_FAILED",
		"message": "CSRF token validation failed",
	})
}
