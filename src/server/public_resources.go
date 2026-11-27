package server

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/apimgr/mail/src/common/api"
	"github.com/apimgr/mail/src/common/security"
)

var publicHostname = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)

func (s *Server) buildPublicURL(r *http.Request, path string) string {
	host := r.Host
	if s.isTrustedProxy(r) {
		if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); validPublicHost(forwarded) {
			host = forwarded
		}
	}
	if !validPublicHost(host) && validPublicHost(s.cfg.Server.FQDN) {
		host = s.cfg.Server.FQDN
	}
	if !validPublicHost(host) {
		host = "localhost"
	}
	proto := "http"
	if s.requestIsHTTPS(r) {
		proto = "https"
	}
	return proto + "://" + host + path
}

func validPublicHost(host string) bool {
	if host == "" || strings.ContainsAny(host, "\r\n/@\\") {
		return false
	}
	u, err := url.Parse("//" + host)
	if err != nil || u.Host != host || u.User != nil || u.Hostname() == "" {
		return false
	}
	name := u.Hostname()
	return net.ParseIP(name) != nil || publicHostname.MatchString(name)
}

func (s *Server) handleWellKnown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body string
	contentType := "text/plain; charset=utf-8"
	switch r.URL.Path {
	case "/.well-known/security.txt":
		if reportURL := s.securityReportURL(); reportURL != "" {
			body += "Contact: " + reportURL + "\n"
		}
		if s.contactFormEnabled() {
			if securityID, err := s.securityID(r.Context(), time.Now().UTC()); err == nil {
				body += "Contact: " + s.buildPublicURL(r, "/server/contact?security_id="+securityID) + "\n"
			}
		}
		if email := s.publicSecurityEmail(r); email != "" {
			body += "Contact: mailto:" + email + "\n"
		}
		if _, ok := s.pgpFingerprint(r.Context()); ok {
			body += "Encryption: " + s.buildPublicURL(r, "/.well-known/pgp-key.asc") + "\n"
		}
		body += "Expires: " + time.Now().UTC().AddDate(1, 0, 0).Format(time.RFC3339) + "\nPolicy: " + s.buildPublicURL(r, "/server/security/policy") + "\nPreferred-Languages: en, es, fr, de, it, ar\n"
	case "/.well-known/pgp-key.asc":
		_, ok := s.pgpFingerprint(r.Context())
		if !ok {
			s.handleNotFound(w, r)
			return
		}
		key, ok := s.readPGPPublicKey()
		if !ok {
			s.handleNotFound(w, r)
			return
		}
		writePublicResource(w, r, "application/pgp-keys; charset=utf-8", string(key))
		return
	case "/.well-known/llms.txt":
		s.writeLLMS(w, r)
		return
	default:
		s.handleNotFound(w, r)
		return
	}
	writePublicResource(w, r, contentType, body)
}

func (s *Server) handleRobots(w http.ResponseWriter, r *http.Request) {
	var body strings.Builder
	body.WriteString("User-agent: *\n")
	for _, rule := range s.robotAllowRules() {
		fmt.Fprintf(&body, "Allow: %s\n", rule)
	}
	for _, rule := range s.cfg.Web.Robots.Deny {
		if validRobotPath(rule) {
			fmt.Fprintf(&body, "Disallow: %s\n", rule)
		}
	}
	fmt.Fprintf(&body, "Sitemap: %s\n", s.buildPublicURL(r, "/sitemap.xml"))
	writePublicResource(w, r, "text/plain; charset=utf-8", body.String())
}

func (s *Server) readPGPPublicKey() ([]byte, bool) {
	if s.appPaths == nil || strings.TrimSpace(s.appPaths.Config) == "" {
		return nil, false
	}
	key, err := os.ReadFile(filepath.Join(s.appPaths.Config, "security", "pgp.pub.asc"))
	return key, err == nil && len(bytes.TrimSpace(key)) > 0
}

func (s *Server) pgpFingerprint(ctx context.Context) (string, bool) {
	key, ok := s.readPGPPublicKey()
	if !ok {
		return "", false
	}
	fingerprint, err := security.PGPFingerprint(key)
	if err != nil || s.db == nil {
		return "", false
	}
	metadata, err := s.db.GetPGPKeyMetadata(ctx)
	if err != nil || metadata.Revoked || metadata.Fingerprint != fingerprint || !metadata.ExpiresAt.After(time.Now().UTC()) {
		return "", false
	}
	return fingerprint, true
}

func (s *Server) securityReportURL() string {
	return strings.TrimSpace(s.cfg.Web.Security.ReportURL)
}

func (s *Server) contactFormEnabled() bool {
	return s.cfg.Server.Pages.Contact.Enabled == nil || *s.cfg.Server.Pages.Contact.Enabled
}

func (s *Server) publicSecurityEmail(r *http.Request) string {
	value := strings.TrimSpace(s.cfg.Server.Contact.Security.Email)
	if value == "" {
		return ""
	}
	host := strings.TrimSpace(s.cfg.Server.FQDN)
	if host == "" {
		candidate := r.Host
		if s.isTrustedProxy(r) {
			if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); validPublicHost(forwarded) {
				candidate = forwarded
			}
		}
		if validPublicHost(candidate) {
			if publicURL, err := url.Parse("//" + candidate); err == nil {
				host = publicURL.Hostname()
			}
		}
	}
	if host == "" || !validPublicHost(host) {
		return ""
	}
	if publicURL, err := url.Parse("//" + host); err == nil {
		host = publicURL.Hostname()
	}
	value = strings.ReplaceAll(value, "{fqdn}", host)
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value {
		return ""
	}
	return value
}

func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	pages := []struct {
		path       string
		changefreq string
		priority   string
	}{}
	for _, registered := range pageRegistry {
		if registered.Sitemap {
			pages = append(pages, struct {
				path       string
				changefreq string
				priority   string
			}{path: registered.Path, changefreq: registered.Changefreq, priority: registered.Priority})
		}
	}
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
	lastmod := time.Now().UTC().Format("2006-01-02")
	for _, page := range pages {
		if !s.robotAllowsPath(page.path) {
			continue
		}
		b.WriteString("  <url><loc>")
		_ = xml.EscapeText(&b, []byte(s.buildPublicURL(r, page.path)))
		fmt.Fprintf(&b, "</loc><lastmod>%s</lastmod><changefreq>%s</changefreq><priority>%s</priority></url>\n", lastmod, page.changefreq, page.priority)
	}
	b.WriteString("</urlset>\n")
	writePublicResource(w, r, "application/xml; charset=utf-8", b.String())
}

func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	base := s.buildPublicURL(r, "/")
	if r.URL.Path == "/rss.xml" {
		body := `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>Mail</title><link>` + xmlText(base) + `</link><description>Mail project updates</description></channel></rss>`
		writePublicResource(w, r, "application/rss+xml; charset=utf-8", body)
		return
	}
	body := `<?xml version="1.0" encoding="UTF-8"?><feed xmlns="http://www.w3.org/2005/Atom"><id>` + xmlText(base) + `</id><title>Mail</title><updated>` + time.Now().UTC().Format(time.RFC3339) + `</updated><link href="` + xmlAttr(base) + `" rel="alternate"/></feed>`
	writePublicResource(w, r, "application/atom+xml; charset=utf-8", body)
}

func (s *Server) handleLLMS(w http.ResponseWriter, r *http.Request) { s.writeLLMS(w, r) }
func (s *Server) writeLLMS(w http.ResponseWriter, r *http.Request) {
	base := s.buildPublicURL(r, "")
	apiBase := api.APIBasePath()
	var body strings.Builder
	fmt.Fprintf(&body, "# Mail\n> Self-hosted mail infrastructure management service.\n\n## API\nBase URL: %s%s\n\n## Endpoints\n", base, apiBase)
	for _, route := range s.apiRouteRegistry() {
		if !route.Listed {
			continue
		}
		endpoint := base + strings.Replace(route.Path, "{api_version}", api.Version, 1)
		fmt.Fprintf(&body, "- %s %s - %s\n", route.Method, endpoint, route.Summary)
		for _, alias := range route.Aliases {
			fmt.Fprintf(&body, "- %s %s%s - Alias for %s\n", route.Method, base, alias, route.Summary)
		}
	}
	fmt.Fprintf(&body, "\n## Documentation\n- %s/server/help\n- %s/server/security\n", base, base)
	writePublicResource(w, r, "text/plain; charset=utf-8", body.String())
}

func (s *Server) robotAllowRules() []string {
	if len(s.cfg.Web.Robots.Allow) == 0 {
		return []string{"/", "/api"}
	}
	var rules []string
	for _, rule := range s.cfg.Web.Robots.Allow {
		if validRobotPath(rule) {
			rules = append(rules, rule)
		}
	}
	return rules
}

func (s *Server) robotAllowsPath(path string) bool {
	allowed := false
	for _, rule := range s.robotAllowRules() {
		if validRobotPath(rule) && strings.HasPrefix(path, rule) {
			allowed = true
		}
	}
	for _, rule := range s.cfg.Web.Robots.Deny {
		if validRobotPath(rule) && strings.HasPrefix(path, rule) {
			return false
		}
	}
	return allowed
}

func validRobotPath(rule string) bool {
	return strings.HasPrefix(rule, "/") && !strings.HasPrefix(rule, "//") && !strings.ContainsAny(rule, "\r\n")
}

func writePublicResource(w http.ResponseWriter, r *http.Request, contentType, body string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(body))
	}
}

func xmlText(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}
func xmlAttr(value string) string { return strings.ReplaceAll(xmlText(value), "'", "&apos;") }
