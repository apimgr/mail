package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	commonsecurity "github.com/apimgr/mail/src/common/security"
	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
)

func TestHealthAboutVersionRoutesAndSecurityHeaders(t *testing.T) {
	handler := New(&config.Config{}, nil).router
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/healthz", http.StatusServiceUnavailable},
		{"/api/healthz.txt", http.StatusServiceUnavailable},
		{"/api/v1/server/healthz", http.StatusServiceUnavailable},
		{"/api/v1/server/healthz.txt", http.StatusServiceUnavailable},
		{"/api/v1/server/about", http.StatusOK},
		{"/api/v1/server/terms", http.StatusOK},
		{"/api/v1/server/version", http.StatusOK},
		{"/api/v1/server/privacy", http.StatusOK},
		{"/api/v1/server/dpo", http.StatusOK},
		{"/api/v1/server/security/policy", http.StatusOK},
		{"/api/v1/server/help", http.StatusOK},
	} {
		t.Run(tc.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if !strings.HasSuffix(tc.path, ".txt") {
				request.Header.Set("Accept", "application/json")
			}
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
			if recorder.Header().Get("X-Content-Type-Options") != "nosniff" || recorder.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("security headers are missing")
			}
			if got := recorder.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
				t.Errorf("X-Frame-Options = %q, want SAMEORIGIN", got)
			}
			if got := recorder.Header().Get("X-XSS-Protection"); got != "1; mode=block" {
				t.Errorf("X-XSS-Protection = %q, want legacy protection enabled", got)
			}
			if got := recorder.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
				t.Errorf("Referrer-Policy = %q, want strict-origin-when-cross-origin", got)
			}
			if recorder.Header().Get("Permissions-Policy") == "" || recorder.Header().Get("X-Permitted-Cross-Domain-Policies") != "none" || recorder.Header().Get("Origin-Agent-Cluster") != "?1" {
				t.Fatal("required privacy and isolation headers are missing")
			}
			if strings.HasSuffix(tc.path, ".txt") {
				if recorder.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
					t.Fatalf("text health content type = %q", recorder.Header().Get("Content-Type"))
				}
				return
			}
			var body map[string]interface{}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("response is not JSON: %v", err)
			}
			if (tc.path == "/api/healthz" || tc.path == "/api/v1/server/healthz") && body["status"] != "unhealthy" {
				t.Fatalf("health status = %v", body["status"])
			}
		})
	}
}

func TestHealthTextExtensionOverridesJSONAccept(t *testing.T) {
	for _, route := range []string{"/api/healthz.txt", "/api/v1/server/healthz.txt"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, route, nil)
		request.Header.Set("Accept", "application/json")
		New(&config.Config{}, nil).router.ServeHTTP(recorder, request)
		if got := recorder.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Errorf("%s content type = %q, want text/plain", route, got)
		}
		if !strings.HasPrefix(recorder.Body.String(), "project.name: mail\n") {
			t.Errorf("%s body is not health text: %q", route, recorder.Body.String())
		}
	}
}

func TestServerRenderedPagesAndHealthNegotiation(t *testing.T) {
	handler := New(&config.Config{}, nil).router
	for _, target := range []string{"/", "/server/about", "/server/privacy", "/server/contact", "/server/help", "/server/help/mcp", "/server/terms", "/server/dpo", "/server/security", "/server/security/policy", "/server/security/thanks", "/server/preferences", "/server/docs/swagger", "/server/docs/graphql"} {
		t.Run(target, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, target, nil)
			request.Header.Set("Accept", "text/html")
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK && recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d", recorder.Code)
			}
			if contentType := recorder.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
				t.Fatalf("content type = %q", contentType)
			}
			if !strings.Contains(recorder.Body.String(), "<main id=\"main-content\">") {
				t.Fatal("response is missing the page shell")
			}
		})
	}
	for _, tc := range []struct {
		accept      string
		contentType string
	}{
		{"text/html", "text/html; charset=utf-8"},
		{"text/html, text/plain", "text/html; charset=utf-8"},
		{"application/json", "application/json; charset=utf-8"},
		{"text/plain", "text/plain; charset=utf-8"},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/server/healthz", nil)
		request.Header.Set("Accept", tc.accept)
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Content-Type") != tc.contentType {
			t.Errorf("Accept %q: status=%d content-type=%q", tc.accept, recorder.Code, recorder.Header().Get("Content-Type"))
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("disabled root health alias status = %d, want 404", response.Code)
	}
	rootHealthConfig := &config.Config{}
	rootHealthConfig.Server.Healthz.Root.Enabled = true
	response = httptest.NewRecorder()
	rootHealthRequest := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rootHealthRequest.Header.Set("Accept", "text/html")
	New(rootHealthConfig, nil).router.ServeHTTP(response, rootHealthRequest)
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("enabled root health alias status/content-type = %d/%q", response.Code, response.Header().Get("Content-Type"))
	}
}

func TestContactDisableAndSecurityOverviewPage(t *testing.T) {
	disabled := false
	cfg := &config.Config{}
	cfg.Server.Pages.Contact.Enabled = &disabled
	cfg.Server.FQDN = "mail.example"
	cfg.Server.Contact.Admin.Email = "private-admin@example.test"
	cfg.Server.Contact.Security.Email = "security@mail.example"
	cfg.Web.Security.ReportURL = "https://github.com/apimgr/mail/security/advisories/new"
	handler := New(cfg, nil).router

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, "/server/contact", strings.NewReader("type=privacy")))
		if response.Code != http.StatusNotFound {
			t.Errorf("contact %s status = %d, want 404", method, response.Code)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/server/security", nil)
	request.Header.Set("Accept", "text/html")
	request.Header.Set("Accept-Language", "en")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("security page status = %d", response.Code)
	}
	body := response.Body.String()
	for _, expected := range []string{"security@mail.example", "Security information expires", "https://github.com/apimgr/mail/security/advisories/new", "Other reports", "Machine-readable security.txt", "/server/security/policy", "/server/security/thanks", "/.well-known/security.txt"} {
		if !strings.Contains(body, expected) {
			t.Errorf("security page omitted %q", expected)
		}
	}
	for _, forbidden := range []string{"private-admin@example.test", "security_id=", "/.well-known/pgp-key.asc"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("security page unexpectedly contains %q", forbidden)
		}
	}
}

func TestPreferencesLanguageSelectorShowsAutomaticDetectionAndNativeNames(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/server/preferences", nil)
	request.Header.Set("Accept", "text/html")
	request.Header.Set("Accept-Language", "fr-CA,fr;q=0.9,en;q=0.8")
	recorder := httptest.NewRecorder()
	New(&config.Config{}, nil).router.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if !strings.Contains(body, "Automatique (Français)") || !strings.Contains(body, `value="fr" lang="fr"`) || !strings.Contains(body, `value="ar" lang="ar"`) {
		t.Fatalf("language selector does not expose automatic detection and native language metadata: %s", body)
	}
	if !strings.Contains(body, `name="theme" value="light"`) || !strings.Contains(body, `aria-label="Clair"`) || !strings.Contains(body, `<span>Clair</span>`) || !strings.Contains(body, `class="theme-icon"`) {
		t.Fatalf("theme toggle does not name its server-computed target mode: %s", body)
	}
	if !strings.Contains(body, `class="preferences-link active" href="/server/preferences" aria-current="page"`) {
		t.Fatalf("current Preferences navigation link has invalid or incomplete attributes: %s", body)
	}
}

func TestPublicPageContentNegotiation(t *testing.T) {
	handler := New(&config.Config{}, nil).router
	for _, test := range []struct {
		name        string
		accept      string
		userAgent   string
		contentType string
	}{
		{name: "HTML outranks text", accept: "text/html, text/plain", contentType: "text/html; charset=utf-8"},
		{name: "browser default", userAgent: "Mozilla/5.0", contentType: "text/html; charset=utf-8"},
		{name: "curl default", userAgent: "curl/8.0", contentType: "text/plain; charset=utf-8"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/server/about", nil)
			request.Header.Set("Accept", test.accept)
			request.Header.Set("User-Agent", test.userAgent)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if got := recorder.Header().Get("Content-Type"); got != test.contentType {
				t.Fatalf("content type = %q, want %q", got, test.contentType)
			}
		})
	}
}

func TestPrivacyPageUsesSelectedLocale(t *testing.T) {
	tests := []struct {
		language   string
		label      string
		collection string
	}{
		{language: "en", label: "Summary", collection: "We collect only"},
		{language: "es", label: "Resumen", collection: "Solo recopilamos"},
		{language: "fr", label: "Résumé", collection: "Nous recueillons"},
		{language: "de", label: "Zusammenfassung", collection: "Wir erheben"},
		{language: "it", label: "Riepilogo", collection: "Raccogliamo"},
		{language: "ar", label: "ملخص", collection: "نجمع فقط"},
	}
	server := New(&config.Config{}, nil)
	for _, test := range tests {
		t.Run(test.language, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/server/privacy?lang="+test.language, nil)
			request.Header.Set("Accept", "text/html")
			recorder := httptest.NewRecorder()
			server.router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("privacy page status = %d", recorder.Code)
			}
			if !strings.Contains(recorder.Body.String(), test.label) {
				t.Errorf("privacy page does not contain localized summary label %q", test.label)
			}
			if test.language != "en" && (strings.Contains(recorder.Body.String(), "Cookie Policy") || strings.Contains(recorder.Body.String(), `aria-label="Cookie consent"`)) {
				t.Error("privacy page contains an untranslated English privacy label")
			}
			if !strings.Contains(recorder.Body.String(), test.collection) {
				t.Errorf("privacy page does not use the selected locale for built-in collection text %q", test.collection)
			}
			if test.language != "en" && strings.Contains(recorder.Body.String(), "We collect only") {
				t.Error("privacy page contains an untranslated English built-in collection paragraph")
			}
		})
	}
}

func TestContactPageIsNotIndexed(t *testing.T) {
	handler := New(&config.Config{}, nil).router
	request := httptest.NewRequest(http.MethodGet, "/server/contact", nil)
	request.Header.Set("Accept", "text/html")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("contact page status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("X-Robots-Tag"); got != "noindex, nofollow" {
		t.Errorf("X-Robots-Tag = %q, want noindex, nofollow", got)
	}
	if !strings.Contains(recorder.Body.String(), `<meta name="robots" content="noindex,nofollow">`) {
		t.Error("contact page HTML is missing its noindex,nofollow directive")
	}
}

func TestAboutPageDescribesProjectAndFeatures(t *testing.T) {
	handler := New(&config.Config{}, nil).router
	request := httptest.NewRequest(http.MethodGet, "/server/about?lang=es", nil)
	request.Header.Set("Accept", "text/html")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("about page status = %d, want %d", recorder.Code, http.StatusOK)
	}
	for _, expected := range []string{"Administración autohospedada", "Acerca de Mail", "Funciones", "Postfix", "Dovecot", "Enlaces del proyecto"} {
		if !strings.Contains(recorder.Body.String(), expected) {
			t.Errorf("about page is missing %q", expected)
		}
	}
	if strings.Contains(recorder.Body.String(), "still being implemented") || strings.Contains(recorder.Body.String(), "Project links") {
		t.Error("about page contains an incomplete-build placeholder or untranslated English text")
	}
}

func TestLanguagePreferencesAndShareableOverride(t *testing.T) {
	handler := New(&config.Config{}, nil).router
	consentValue := url.PathEscape(`{"essential":true,"preferences":true,"analytics":false,"timestamp":1704067200}`)
	request := httptest.NewRequest(http.MethodGet, "/server/about?lang=es", nil)
	request.Header.Set("Accept", "text/html")
	request.AddCookie(&http.Cookie{Name: "cookie_consent", Value: consentValue})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "lang=\"es\"") {
		t.Fatalf("shareable language override status/body did not select Spanish: %d", recorder.Code)
	}
	var languageCookie *http.Cookie
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == "lang" {
			languageCookie = cookie
		}
	}
	if languageCookie == nil || languageCookie.Value != "es" || languageCookie.HttpOnly {
		t.Fatalf("language override cookie = %#v", languageCookie)
	}

	request = httptest.NewRequest(http.MethodGet, "/server/about", nil)
	request.Header.Set("Accept", "text/html")
	request.Header.Set("Accept-Language", "ar")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if !strings.Contains(recorder.Body.String(), "lang=\"ar\" dir=\"rtl\"") {
		t.Fatal("Accept-Language did not select Arabic RTL")
	}

	csrfToken := "csrf-test-token-012345678901234567890123"
	values := url.Values{"csrf_token": {csrfToken}, "lang": {"fr"}, "theme": {"dark"}}
	request = httptest.NewRequest(http.MethodPost, "/server/preferences", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	request.AddCookie(&http.Cookie{Name: "cookie_consent", Value: consentValue})
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/server/preferences" {
		t.Fatalf("preference save status/location = %d/%q", recorder.Code, recorder.Header().Get("Location"))
	}
	seen := map[string]bool{}
	for _, cookie := range recorder.Result().Cookies() {
		seen[cookie.Name+"="+cookie.Value] = !cookie.HttpOnly || cookie.Name == "csrf_token"
	}
	if !seen["lang=fr"] || !seen["theme=dark"] {
		t.Fatalf("preference save cookies = %#v", recorder.Result().Cookies())
	}

	values = url.Values{"csrf_token": {csrfToken}, "lang": {"auto"}, "theme": {"auto"}}
	request = httptest.NewRequest(http.MethodPost, "/server/preferences", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	request.AddCookie(&http.Cookie{Name: "cookie_consent", Value: consentValue})
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("automatic preference save status = %d", recorder.Code)
	}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == "lang" && !strings.Contains(cookie.Raw, "Max-Age=0") {
			t.Errorf("automatic language did not expire cookie: %#v", cookie)
		}
		if cookie.Name == "theme" && (cookie.Value != "auto" || cookie.MaxAge <= 0) {
			t.Errorf("automatic theme was not stored as auto: %#v", cookie)
		}
	}
}

func TestCSRFProtectionForFrontendForms(t *testing.T) {
	handler := New(&config.Config{}, nil).router
	page := httptest.NewRecorder()
	pageRequest := httptest.NewRequest(http.MethodGet, "/server/preferences", nil)
	pageRequest.Header.Set("Accept", "text/html")
	handler.ServeHTTP(page, pageRequest)
	var token *http.Cookie
	for _, cookie := range page.Result().Cookies() {
		if cookie.Name == csrfCookieName {
			token = cookie
		}
	}
	if token == nil || token.Value == "" || token.HttpOnly || token.SameSite != http.SameSiteStrictMode {
		t.Fatalf("CSRF cookie = %#v", token)
	}
	if !strings.Contains(page.Body.String(), `name="csrf_token" value="`+token.Value+`"`) {
		t.Fatal("rendered preference form does not carry its CSRF token")
	}

	values := url.Values{"csrf_token": {token.Value}, "lang": {"fr"}, "theme": {"dark"}}
	request := httptest.NewRequest(http.MethodPost, "/server/preferences", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, request)
	if denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), `"CSRF_FAILED"`) {
		t.Fatalf("missing CSRF cookie status/body = %d/%q", denied.Code, denied.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/server/preferences", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(token)
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, request)
	if accepted.Code != http.StatusSeeOther {
		t.Fatalf("valid CSRF token status = %d", accepted.Code)
	}
}

func TestBuildStampCookieAndVersionPurge(t *testing.T) {
	handler := New(&config.Config{}, nil).router
	first := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	firstRequest.Header.Set("Accept", "text/html")
	handler.ServeHTTP(first, firstRequest)
	var stamp *http.Cookie
	for _, cookie := range first.Result().Cookies() {
		if cookie.Name == "mail_build" {
			stamp = cookie
		}
	}
	if stamp == nil || stamp.Value != applicationBuildStamp() || stamp.HttpOnly || stamp.Path != "/" {
		t.Fatalf("build stamp cookie = %#v", stamp)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept", "text/html")
	request.AddCookie(&http.Cookie{Name: "mail_build", Value: "old-version"})
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, request)
	if second.Header().Get("Clear-Site-Data") != `"cache", "storage"` {
		t.Fatalf("version purge header = %q", second.Header().Get("Clear-Site-Data"))
	}
}

func TestConsentBannerAndConsentUpdate(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Privacy.Data.Sold = true
	cfg.Server.Privacy.Cookies.Preferences.Enabled = true
	cfg.Server.Privacy.Cookies.Analytics.Enabled = true
	handler := New(cfg, nil).router
	page := httptest.NewRecorder()
	pageRequest := httptest.NewRequest(http.MethodGet, "/server/privacy", nil)
	pageRequest.Header.Set("Accept", "text/html")
	handler.ServeHTTP(page, pageRequest)
	if !strings.Contains(page.Body.String(), `id="cookie-consent"`) || !strings.Contains(page.Body.String(), `action="/server/ccpa"`) {
		t.Fatal("consent banner or CCPA control is missing")
	}
	var csrf *http.Cookie
	for _, cookie := range page.Result().Cookies() {
		if cookie.Name == csrfCookieName {
			csrf = cookie
		}
	}
	if csrf == nil {
		t.Fatal("page did not set a CSRF cookie")
	}
	form := url.Values{"csrf_token": {csrf.Value}, "choice": {"accept"}}
	request := httptest.NewRequest(http.MethodPost, "/server/consent", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("consent post status = %d", response.Code)
	}
	var state consentState
	var consentValue string
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "cookie_consent" {
			consentValue = cookie.Value
			decoded, _ := url.PathUnescape(cookie.Value)
			if err := json.Unmarshal([]byte(decoded), &state); err != nil {
				t.Fatalf("consent cookie JSON: %v", err)
			}
		}
	}
	if !state.Essential || !state.Preferences || !state.Analytics || state.Timestamp == 0 {
		t.Fatalf("accepted consent = %#v", state)
	}
	if !strings.HasPrefix(consentValue, "%7B%22") || consentAllowsPreferences(`{"essential":true,"preferences":true,"analytics":false,"timestamp":1704067200}`) {
		t.Fatalf("consent cookie wire encoding is not percent-encoded JSON: %q", consentValue)
	}
}

func TestPrivacyPageReflectsConfigurationAndCCPAChoice(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Privacy.Data.Sold = true
	cfg.Server.Privacy.Data.StoredOnServer = true
	cfg.Server.Privacy.Cookies.Essential.Description = "Required for security"
	cfg.Server.Privacy.Cookies.Preferences.Description = "Remember browser settings"
	cfg.Server.Privacy.Cookies.Analytics.Description = "Usage measurements"
	cfg.Server.Privacy.Retention.Period = "Keep until expiry"
	cfg.Server.Privacy.Retention.ExportAvailable = true
	cfg.Server.Privacy.Retention.DeletionAvailable = true
	cfg.Server.Tracking.Type = "matomo"
	cfg.Server.Branding.Content.Privacy = config.BrandingPageContentConfig{Format: "md", Body: config.LocalizedContent{"default": "Operator notice <script>alert(1)</script>"}}
	cfg.Server.Privacy.Data.Sharing = append(cfg.Server.Privacy.Data.Sharing, struct {
		Condition string `yaml:"condition"`
		When      string `yaml:"when"`
		Data      string `yaml:"data"`
	}{Condition: "email", When: "when sent", Data: "address"})
	handler := New(cfg, nil, "2025-12-04T13:05:13Z").router
	pageRequest := httptest.NewRequest(http.MethodGet, "/server/privacy", nil)
	pageRequest.Header.Set("Accept", "text/html")
	pageResponse := httptest.NewRecorder()
	handler.ServeHTTP(pageResponse, pageRequest)
	for _, want := range []string{"Privacy Policy", "12/04/2025", "id=\"summary\"", "id=\"data-collection\"", "Required for security", "Remember browser settings", "Matomo", "Keep until expiry", "Operator notice", "id=\"ccpa-opt-out\"", "Do Not Sell My Personal Information", "csrf_token"} {
		if !strings.Contains(pageResponse.Body.String(), want) {
			t.Errorf("privacy page is missing %q", want)
		}
	}
	if strings.Contains(pageResponse.Body.String(), "<script>") {
		t.Fatal("privacy operator content rendered active script content")
	}
	var csrf *http.Cookie
	for _, cookie := range pageResponse.Result().Cookies() {
		if cookie.Name == csrfCookieName {
			csrf = cookie
		}
	}
	if csrf == nil {
		t.Fatal("privacy page did not issue CSRF token")
	}
	form := url.Values{"csrf_token": {csrf.Value}, "choice": {"opt-out"}}
	post := httptest.NewRequest(http.MethodPost, "/server/ccpa", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, post)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("CCPA opt-out status = %d, want redirect", response.Code)
	}
	var optedOut bool
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "ccpa_opt_out" && cookie.Value == "true" {
			optedOut = true
		}
	}
	if !optedOut {
		t.Fatal("CCPA opt-out choice did not set the preference cookie")
	}
}

func TestOperatorContentIsRenderedAndSanitized(t *testing.T) {
	markdown := renderSafeContent("markdown", "# Operator heading\n\n<script>alert(1)</script>\n\n[Contact form](/server/contact?type=privacy)")
	if !strings.Contains(string(markdown), "<h2>Operator heading</h2>") || strings.Contains(string(markdown), "<script>") || strings.Contains(string(markdown), `href="/server/contact`) {
		t.Fatalf("unsafe or malformed Markdown output: %s", markdown)
	}
	html := renderSafeContent("html", `<h1>Second title</h1><p onclick="alert(1)"><strong>Safe</strong><script>alert(2)</script></p>`)
	if strings.Contains(string(html), "<h1>") || strings.Contains(string(html), "onclick") || strings.Contains(string(html), "<script>") || !strings.Contains(string(html), "<strong>Safe</strong>") {
		t.Fatalf("unsafe or incomplete HTML output: %s", html)
	}
	footer := renderSafeContentFor("footer", "html", `<table><tr><td>Removed</td></tr></table><p><a href="/safe" target="_blank">Link</a></p>`)
	if strings.Contains(string(footer), "<table>") || !strings.Contains(string(footer), `target="_blank"`) || !strings.Contains(string(footer), "Link") || !strings.Contains(string(footer), "rel=\"noreferrer noopener\"") {
		t.Fatalf("footer must use the strict HTML policy: %s", footer)
	}
	contact := renderSafeContent("html", `<a href="/server/contact/?source=privacy">Contact</a><a href="/server/contact-help">Help</a>`)
	if strings.Contains(string(contact), `href="/server/contact/`) || strings.Contains(string(contact), `href="/server/contact?`) || strings.Contains(string(contact), `href="/server/contact"`) || !strings.Contains(string(contact), `href="/server/contact-help"`) {
		t.Fatalf("operator content must not link to the contact form route: %s", contact)
	}
}

func TestOperatorHeaderAndFooterCustomization(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Branding.Content.Header = config.BrandingPageContentConfig{
		Format: "html",
		Body:   config.LocalizedContent{"default": `<p>{project_org} {project_name}</p><script>alert(1)</script>`},
	}
	cfg.Server.Branding.Content.Footer = config.BrandingPageContentConfig{Format: "html", Body: config.LocalizedContent{"default": "  "}}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept", "text/html")
	New(cfg, nil, "2025-12-04T13:05:13Z").router.ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "<p>apimgr mail</p>") || strings.Contains(body, "<script>") {
		t.Fatalf("header customization was not rendered safely: status=%d body=%s", response.Code, body)
	}
	if strings.Contains(body, `class="footer-branding`) {
		t.Fatal("whitespace-only footer content must disable the custom branding block")
	}
}

func TestPreferenceSyncImportGateAndReset(t *testing.T) {
	handler := New(&config.Config{}, nil).router
	emptyImport := httptest.NewRecorder()
	handler.ServeHTTP(emptyImport, httptest.NewRequest(http.MethodGet, "/api/v1/server/preferences/sync/import", nil))
	var emptyPayload map[string]any
	if err := json.Unmarshal(emptyImport.Body.Bytes(), &emptyPayload); err != nil {
		t.Fatal(err)
	}
	emptyData := emptyPayload["data"].(map[string]any)
	if _, ok := emptyData["applied"].([]any); !ok {
		t.Fatalf("empty sync import must return applied array: %s", emptyImport.Body.String())
	}
	if _, ok := emptyData["ignored"].([]any); !ok {
		t.Fatalf("empty sync import must return ignored array: %s", emptyImport.Body.String())
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/preferences/sync/import?theme=light&lang=es", nil)
	blocked := httptest.NewRecorder()
	handler.ServeHTTP(blocked, request)
	if blocked.Code != http.StatusOK || !strings.Contains(blocked.Body.String(), `"consent_required":true`) || len(blocked.Result().Cookies()) != 0 {
		t.Fatalf("unconsented sync import response/cookies = %d %q %#v", blocked.Code, blocked.Body.String(), blocked.Result().Cookies())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/server/preferences/sync/import?theme=light&lang=es", nil)
	request.AddCookie(&http.Cookie{Name: "cookie_consent", Value: url.PathEscape(`{"essential":true,"preferences":true,"analytics":false,"timestamp":1704067200}`)})
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, request)
	if accepted.Code != http.StatusOK {
		t.Fatalf("accepted sync import status = %d", accepted.Code)
	}
	got := map[string]string{}
	for _, cookie := range accepted.Result().Cookies() {
		got[cookie.Name] = cookie.Value
	}
	if got["theme"] != "light" || got["lang"] != "es" {
		t.Fatalf("imported preference cookies = %#v", got)
	}

	csrf := "csrf-test-token-012345678901234567890123"
	values := url.Values{"csrf_token": {csrf}, "action": {"reset"}}
	request = httptest.NewRequest(http.MethodPost, "/server/preferences", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrf})
	reset := httptest.NewRecorder()
	handler.ServeHTTP(reset, request)
	if reset.Code != http.StatusSeeOther || reset.Header().Get("Clear-Site-Data") != `"storage"` {
		t.Fatalf("reset status/header = %d/%q", reset.Code, reset.Header().Get("Clear-Site-Data"))
	}
	for _, cookie := range reset.Result().Cookies() {
		if cookie.Name == csrfCookieName {
			t.Fatal("reset removed the CSRF cookie")
		}
	}
}

func TestPreferenceSyncDirectValuesAndProxyCookieAttributes(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.TrustedProxies = []string{"127.0.0.1/32"}
	handler := New(cfg, nil).router
	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/preferences/sync/import?theme=light&code=", nil)
	request.URL.RawQuery = "theme=light&code=" + url.QueryEscape(base64.RawURLEncoding.EncodeToString([]byte("theme=dark&lang=fr")))
	request.RemoteAddr = "127.0.0.1:9000"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.AddCookie(&http.Cookie{Name: "cookie_consent", Value: url.PathEscape(`{"essential":true,"preferences":true,"analytics":false,"timestamp":1704067200}`)})
	request.AddCookie(&http.Cookie{Name: "theme", Value: "dark"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("sync import status = %d", response.Code)
	}
	var payload struct {
		Data preferenceState `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Theme != "light" || payload.Data.Lang != "fr" || len(payload.Data.Applied) != 2 {
		t.Fatalf("direct values did not override code: %#v", payload.Data)
	}
	seen := map[string]*http.Cookie{}
	for _, cookie := range response.Result().Cookies() {
		seen[cookie.Name] = cookie
	}
	for _, name := range []string{"theme", "lang"} {
		cookie := seen[name]
		if cookie == nil || !cookie.Secure || cookie.HttpOnly || cookie.Path != "/" || cookie.Domain != "" || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 31536000 {
			t.Errorf("%s cookie attributes = %#v", name, cookie)
		}
	}
}

func TestPreferencesWebSaveAlwaysRedirectsAndIgnoresCode(t *testing.T) {
	handler := New(&config.Config{}, nil).router
	csrf := "csrf-test-token-012345678901234567890123"
	request := httptest.NewRequest(http.MethodPost, "/server/preferences", nil)
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrf})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("empty preferences save status = %d", response.Code)
	}
	values := url.Values{"csrf_token": {csrf}, "code": {"theme=light&lang=fr"}}
	request = httptest.NewRequest(http.MethodPost, "/server/preferences", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrf})
	request.AddCookie(&http.Cookie{Name: "cookie_consent", Value: url.PathEscape(`{"essential":true,"preferences":true,"analytics":false,"timestamp":1704067200}`)})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("save with only code status = %d", response.Code)
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "theme" || cookie.Name == "lang" {
			t.Fatalf("save applied control code as preference: %#v", cookie)
		}
	}
}

func TestPreferenceSyncDecoderAndSafeRedirect(t *testing.T) {
	query := "theme=dark&lang=fr"
	encoded := base64.RawURLEncoding.EncodeToString([]byte(query))
	for _, value := range []string{query, encoded, base64.URLEncoding.EncodeToString([]byte(query)), "https://example.test/server/preferences/sync/import?" + query} {
		got := decodePreferenceCode(value)
		if got.Get("theme") != "dark" || got.Get("lang") != "fr" {
			t.Errorf("decode %q = %#v", value, got)
		}
	}
	if got := decodePreferenceCode(strings.Repeat("x", 2049)); len(got) != 0 {
		t.Fatalf("oversized code was decoded: %#v", got)
	}
	for _, value := range []string{"//evil.example", "/\\evil", "https://evil.example/", "/safe\r\nLocation: evil"} {
		if _, ok := safeRelativeRedirect(value); ok {
			t.Errorf("unsafe redirect accepted: %q", value)
		}
	}
	if got, ok := safeRelativeRedirect("/server/about?lang=fr"); !ok || got != "/server/about?lang=fr" {
		t.Fatalf("safe redirect = %q, %v", got, ok)
	}
}

func TestPublicDiscoveryResources(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.TrustedProxies = []string{"127.0.0.1/32"}
	handler := New(cfg, nil).router
	for _, tc := range []struct{ path, contentType, contains string }{
		{"/.well-known/security.txt", "text/plain; charset=utf-8", "https://mail.example/server/security/policy"},
		{"/.well-known/llms.txt", "text/plain; charset=utf-8", "GET https://mail.example/api/v1/server/about"},
		{"/llms.txt", "text/plain; charset=utf-8", "# Mail"},
		{"/robots.txt", "text/plain; charset=utf-8", "Sitemap: https://mail.example/sitemap.xml"},
		{"/sitemap.xml", "application/xml; charset=utf-8", "https://mail.example/server/about"},
		{"/feed.xml", "application/atom+xml; charset=utf-8", "https://mail.example/"},
		{"/rss.xml", "application/rss+xml; charset=utf-8", "<rss"},
	} {
		request := httptest.NewRequest(http.MethodGet, "http://example.com"+tc.path, nil)
		request.RemoteAddr = "127.0.0.1:1234"
		request.Header.Set("X-Forwarded-Proto", "https")
		request.Header.Set("X-Forwarded-Host", "mail.example")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != tc.contentType || !strings.Contains(response.Body.String(), tc.contains) {
			t.Errorf("%s: status/type/body = %d/%q/%q", tc.path, response.Code, response.Header().Get("Content-Type"), response.Body.String())
		}
		if tc.path == "/sitemap.xml" && (strings.Contains(response.Body.String(), "/server/contact") || strings.Contains(response.Body.String(), "/server/preferences")) {
			t.Errorf("sitemap contains contact or browser-management routes: %s", response.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodHead, "/robots.txt", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.Len() != 0 {
		t.Fatalf("HEAD robots response = %d %q", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/.well-known/security.txt", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("well-known POST status/Allow = %d/%q", response.Code, response.Header().Get("Allow"))
	}
	request = httptest.NewRequest(http.MethodGet, "/.well-known/unknown", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown well-known status = %d", response.Code)
	}
}

func TestWellKnownPGPKeyIsPublishedOnlyWhenPresent(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(root, "server.db")
	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	server := New(cfg, db)
	server.appPaths.Config = filepath.Join(root, "config")
	handler := server.router
	request := httptest.NewRequest(http.MethodGet, "/.well-known/pgp-key.asc", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing PGP key status = %d, want 404", response.Code)
	}
	securityText := httptest.NewRecorder()
	handler.ServeHTTP(securityText, httptest.NewRequest(http.MethodGet, "/.well-known/security.txt", nil))
	if strings.Contains(securityText.Body.String(), "Encryption:") {
		t.Fatalf("security.txt advertised a missing PGP key: %s", securityText.Body.String())
	}

	entity, err := openpgp.NewEntity("Test Security", "", "security@example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	var armored bytes.Buffer
	armoredWriter, err := armor.Encode(&armored, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := entity.Serialize(armoredWriter); err != nil {
		t.Fatal(err)
	}
	if err := armoredWriter.Close(); err != nil {
		t.Fatal(err)
	}
	publicKey := armored.String()
	fingerprint, err := commonsecurity.PGPFingerprint([]byte(publicKey))
	if err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Truncate(time.Second)
	if err := db.SavePGPKeyMetadata(context.Background(), database.PGPKeyMetadata{Fingerprint: fingerprint, CreatedAt: created, ExpiresAt: created.AddDate(2, 0, 0), KeyserversPublished: []database.PGPKeyserverPublication{}}); err != nil {
		t.Fatal(err)
	}
	keyDir := filepath.Join(server.appPaths.Config, "security")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keyDir, "pgp.pub.asc"), []byte(publicKey), 0o600); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/.well-known/pgp-key.asc", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/pgp-keys; charset=utf-8" || response.Body.String() != publicKey {
		t.Fatalf("published PGP key = %d %q %q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
	securityText = httptest.NewRecorder()
	handler.ServeHTTP(securityText, httptest.NewRequest(http.MethodGet, "/.well-known/security.txt", nil))
	if !strings.Contains(securityText.Body.String(), "Encryption: http://example.com/.well-known/pgp-key.asc") {
		t.Fatalf("security.txt omitted configured public-key URL: %s", securityText.Body.String())
	}
	metadata := database.PGPKeyMetadata{
		Fingerprint:         fingerprint,
		CreatedAt:           created.AddDate(-2, 0, 0),
		ExpiresAt:           created.Add(-time.Second),
		KeyserversPublished: []database.PGPKeyserverPublication{},
	}
	if err := db.SavePGPKeyMetadata(context.Background(), metadata); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/.well-known/pgp-key.asc", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("expired PGP key status = %d, want 404", response.Code)
	}
}

func TestRobotsDenyRulesFilterSitemap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Web.Robots.Allow = []string{"/", "/api"}
	cfg.Web.Robots.Deny = []string{"/server/preferences"}
	handler := New(cfg, nil).router
	robots := httptest.NewRecorder()
	handler.ServeHTTP(robots, httptest.NewRequest(http.MethodGet, "/robots.txt", nil))
	if robots.Code != http.StatusOK || !strings.Contains(robots.Body.String(), "Disallow: /server/preferences\n") {
		t.Fatalf("robots response = %d %q", robots.Code, robots.Body.String())
	}
	sitemap := httptest.NewRecorder()
	handler.ServeHTTP(sitemap, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if sitemap.Code != http.StatusOK || strings.Contains(sitemap.Body.String(), "/server/preferences") || !strings.Contains(sitemap.Body.String(), "<changefreq>daily</changefreq><priority>1.0</priority>") {
		t.Fatalf("sitemap did not respect robots rules: %d %q", sitemap.Code, sitemap.Body.String())
	}
}

func TestCORSAllowListCredentialsAndPreflight(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.CORS.AllowedOrigins = []string{"https://allowed.example"}
	cfg.Server.CORS.AllowCredentials = true
	cfg.Server.CORS.MaxAge = 600
	handler := New(cfg, nil).router

	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/about", nil)
	request.Header.Set("Origin", "https://allowed.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("Access-Control-Allow-Origin") != "https://allowed.example" || response.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("allowed origin CORS headers = %#v", response.Header())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/server/about", nil)
	request.Header.Set("Origin", "https://blocked.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("blocked origin received CORS permission: %#v", response.Header())
	}

	request = httptest.NewRequest(http.MethodOptions, "/api/v1/server/about", nil)
	request.Header.Set("Origin", "https://allowed.example")
	request.Header.Set("Access-Control-Request-Method", "POST")
	request.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Methods") != corsAllowedMethods || response.Header().Get("Access-Control-Max-Age") != "600" || !strings.Contains(response.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatalf("CORS preflight response = %d %#v", response.Code, response.Header())
	}
}

func TestCORSWildcardNeverAllowsCredentialsAndEmptyDisables(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.CORS.AllowedOrigins = []string{"*", "https://credential.example"}
	cfg.Server.CORS.AllowCredentials = true
	handler := New(cfg, nil).router
	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/about", nil)
	request.Header.Set("Origin", "https://any.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("Access-Control-Allow-Origin") != "*" || response.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("wildcard credentials headers = %#v", response.Header())
	}
	request.Header.Set("Origin", "https://credential.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("Access-Control-Allow-Origin") != "https://credential.example" || response.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("explicit origin mixed with wildcard headers = %#v", response.Header())
	}

	cfg.Server.CORS.AllowedOrigins = []string{""}
	handler = New(cfg, nil).router
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("disabled CORS emitted headers: %#v", response.Header())
	}
}

func TestCORSDomainAndTrustedProxyOrigins(t *testing.T) {
	t.Setenv("DOMAIN", "mail.example,admin.example")
	cfg := &config.Config{}
	cfg.Server.CORS.AllowedOrigins = nil
	cfg.Server.CORS.AllowCredentials = true
	cfg.Server.TrustedProxies = []string{"127.0.0.1/32"}
	handler := New(cfg, nil).router

	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/about", nil)
	request.Header.Set("Origin", "https://mail.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("Access-Control-Allow-Origin") != "https://mail.example" {
		t.Fatalf("DOMAIN origin response headers = %#v", response.Header())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/server/about", nil)
	request.RemoteAddr = "127.0.0.1:9000"
	request.Header.Set("Origin", "https://proxy.example")
	request.Header.Set("X-Forwarded-Host", "proxy.example")
	request.Header.Set("X-Forwarded-Proto", "https")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("Access-Control-Allow-Origin") != "https://proxy.example" {
		t.Fatalf("trusted proxy origin response headers = %#v", response.Header())
	}
}

func TestProgressiveWebAppResources(t *testing.T) {
	handler := New(&config.Config{}, nil).router
	stamp := applicationBuildStamp()
	for _, tc := range []struct {
		path        string
		contentType string
		contains    string
	}{
		{"/manifest.json", "application/manifest+json; charset=utf-8", "\"display\": \"standalone\""},
		{"/sw.js", "text/javascript; charset=utf-8", "offline-strings.json"},
		{"/static/app.js?v=dev", "text/javascript; charset=utf-8", "serviceWorker.register"},
		{"/static/icons/mail.svg?v=dev", "image/svg+xml", "<svg"},
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != tc.contentType || !strings.Contains(recorder.Body.String(), tc.contains) {
			t.Errorf("%s status/content-type/body = %d/%q/%q", tc.path, recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
		}
	}
	worker := httptest.NewRecorder()
	handler.ServeHTTP(worker, httptest.NewRequest(http.MethodGet, "/sw.js", nil))
	for _, expected := range []string{
		`const BUILD_STAMP = "` + stamp + `";`,
		`"/static/css/app.css?v=` + url.QueryEscape(stamp) + `"`,
		`"/static/app.js?v=` + url.QueryEscape(stamp) + `"`,
		`"/static/icons/mail.svg?v=` + url.QueryEscape(stamp) + `"`,
	} {
		if !strings.Contains(worker.Body.String(), expected) {
			t.Errorf("service worker does not contain build-matched value %q", expected)
		}
	}
	missingOfflinePage := httptest.NewRecorder()
	handler.ServeHTTP(missingOfflinePage, httptest.NewRequest(http.MethodGet, "/offline.html", nil))
	if missingOfflinePage.Code != http.StatusNotFound {
		t.Fatalf("standalone offline page status = %d, want 404", missingOfflinePage.Code)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/static/css/app.css?v="+applicationBuildStamp(), nil))
	if recorder.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("stamped asset cache control = %q", recorder.Header().Get("Cache-Control"))
	}
}

func TestOfflineCacheMarkerRequiresPublicDefaultPage(t *testing.T) {
	handler := New(&config.Config{}, nil).router
	tests := []struct {
		name           string
		path           string
		acceptLanguage string
		cookieName     string
		cookieValue    string
		sharedCookies  bool
		authorization  string
		wantPublic     bool
	}{
		{name: "public home", path: "/", wantPublic: true},
		{name: "shared CSRF and build cookies", path: "/server/about", sharedCookies: true, wantPublic: true},
		{name: "public contact chooser", path: "/server/contact", wantPublic: true},
		{name: "private privacy preferences", path: "/server/privacy"},
		{name: "private settings page", path: "/server/preferences"},
		{name: "query variant", path: "/?theme=light"},
		{name: "localized page", path: "/", acceptLanguage: "fr"},
		{name: "theme cookie", path: "/server/about", cookieName: "theme", cookieValue: "light"},
		{name: "consent cookie", path: "/server/about", cookieName: "cookie_consent", cookieValue: "private"},
		{name: "owner cookie", path: "/server/about", cookieName: "owner_token", cookieValue: "secret"},
		{name: "authorization header", path: "/server/about", authorization: "Bearer operator-secret"},
		{name: "unknown page", path: "/missing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.Header.Set("Accept", "text/html")
			if test.acceptLanguage != "" {
				request.Header.Set("Accept-Language", test.acceptLanguage)
			}
			if test.cookieName != "" {
				request.AddCookie(&http.Cookie{Name: test.cookieName, Value: test.cookieValue})
			}
			if test.sharedCookies {
				request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: strings.Repeat("a", 43)})
				request.AddCookie(&http.Cookie{Name: "mail_build", Value: applicationBuildStamp()})
			}
			if test.authorization != "" {
				request.Header.Set("Authorization", test.authorization)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			gotPublic := response.Header().Get("X-Offline-Cache") == "public"
			if gotPublic != test.wantPublic {
				t.Fatalf("X-Offline-Cache = %q, want public=%t", response.Header().Get("X-Offline-Cache"), test.wantPublic)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("HTML Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestBuildStampOnlyPurgesSuccessfulTopLevelNavigations(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		status      int
		destination string
		mode        string
		cookie      string
		wantPurge   bool
		wantCookie  bool
	}{
		{name: "legacy browser navigation", method: http.MethodGet, status: http.StatusOK, wantCookie: true},
		{name: "document navigation", method: http.MethodGet, status: http.StatusOK, destination: "document", mode: "navigate", wantCookie: true},
		{name: "stale document navigation", method: http.MethodGet, status: http.StatusOK, destination: "document", mode: "navigate", cookie: "old-build", wantPurge: true, wantCookie: true},
		{name: "subresource", method: http.MethodGet, status: http.StatusOK, destination: "script", mode: "no-cors"},
		{name: "fetch", method: http.MethodGet, status: http.StatusOK, destination: "empty", mode: "cors"},
		{name: "head", method: http.MethodHead, status: http.StatusOK},
		{name: "not found", method: http.MethodGet, status: http.StatusNotFound},
		{name: "unavailable", method: http.MethodGet, status: http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "/", nil)
			if test.destination != "" {
				request.Header.Set("Sec-Fetch-Dest", test.destination)
			}
			if test.mode != "" {
				request.Header.Set("Sec-Fetch-Mode", test.mode)
			}
			if test.cookie != "" {
				request.AddCookie(&http.Cookie{Name: "mail_build", Value: test.cookie})
			}
			response := httptest.NewRecorder()
			New(&config.Config{}, nil).applyBuildStamp(response, request, test.status)
			gotPurge := response.Header().Get("Clear-Site-Data") != ""
			if gotPurge != test.wantPurge {
				t.Fatalf("Clear-Site-Data present = %t, want %t", gotPurge, test.wantPurge)
			}
			if gotCookie := response.Header().Get("Set-Cookie") != ""; gotCookie != test.wantCookie {
				t.Errorf("build cookie present = %t, want %t", gotCookie, test.wantPurge)
			}
		})
	}
}

func TestErrorPagePreservesStaleBuildStampForNextNavigation(t *testing.T) {
	handler := New(&config.Config{}, nil).router
	staleBuild := &http.Cookie{Name: "mail_build", Value: "old-build"}

	errorRequest := httptest.NewRequest(http.MethodGet, "/missing", nil)
	errorRequest.Header.Set("Accept", "text/html")
	errorRequest.AddCookie(staleBuild)
	errorResponse := httptest.NewRecorder()
	handler.ServeHTTP(errorResponse, errorRequest)
	if errorResponse.Code != http.StatusNotFound || errorResponse.Header().Get("Clear-Site-Data") != "" {
		t.Fatalf("error response status/purge = %d/%q", errorResponse.Code, errorResponse.Header().Get("Clear-Site-Data"))
	}
	for _, cookie := range errorResponse.Result().Cookies() {
		if cookie.Name == "mail_build" {
			t.Fatalf("error response replaced stale build cookie with %q", cookie.Value)
		}
	}

	navigation := httptest.NewRequest(http.MethodGet, "/", nil)
	navigation.Header.Set("Accept", "text/html")
	navigation.AddCookie(staleBuild)
	navigationResponse := httptest.NewRecorder()
	handler.ServeHTTP(navigationResponse, navigation)
	if navigationResponse.Code != http.StatusOK || navigationResponse.Header().Get("Clear-Site-Data") != `"cache", "storage"` {
		t.Fatalf("next navigation status/purge = %d/%q", navigationResponse.Code, navigationResponse.Header().Get("Clear-Site-Data"))
	}
}

func TestPathSecurityMiddlewareRejectsTraversalAndCleansPaths(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := pathSecurityMiddleware(next)
	for _, target := range []string{"/../secret", "/%2e%2e/secret"} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL = &url.URL{Path: target, RawPath: target}
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("path %q status = %d", target, recorder.Code)
		}
	}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/safe/../healthz", nil)
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("untrusted traversal path status = %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/server/about/?lang=fr", nil))
	if recorder.Code != http.StatusMovedPermanently || recorder.Header().Get("Location") != "/server/about?lang=fr" {
		t.Fatalf("trailing slash normalization = %d, %q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestURLNormalizeMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := urlNormalizeMiddleware(next)
	request := httptest.NewRequest(http.MethodGet, "/server/about/?lang=fr", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMovedPermanently || response.Header().Get("Location") != "/server/about?lang=fr" {
		t.Fatalf("trailing slash normalization = %d, %q", response.Code, response.Header().Get("Location"))
	}

	request = httptest.NewRequest(http.MethodGet, "/dir/index.html/", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("file path was normalized: %d", response.Code)
	}
}

func TestServerLifecycleAndListener(t *testing.T) {
	s := New(&config.Config{Server: config.ServerConfig{Address: "127.0.0.1", Port: 64000}}, nil)
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown before start: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StartWithListener(listener); err != nil {
		t.Fatal(err)
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRequestShutdownBeforeWaitIsGraceful(t *testing.T) {
	s := New(&config.Config{}, nil)
	s.RequestShutdown()
	s.WaitForShutdown()
	select {
	case <-s.shutdownRequested:
	default:
		t.Fatal("shutdown request was not retained")
	}
}
