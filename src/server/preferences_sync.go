package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/apimgr/mail/src/common/i18n"
	"golang.org/x/text/language"
)

type preferenceState struct {
	Theme           string            `json:"theme"`
	Lang            string            `json:"lang"`
	Prefs           map[string]string `json:"prefs"`
	URL             string            `json:"url,omitempty"`
	Code            string            `json:"code,omitempty"`
	Applied         []string          `json:"applied"`
	Ignored         []string          `json:"ignored"`
	ConsentRequired bool              `json:"consent_required,omitempty"`
}

func (s *Server) preferenceSnapshot(r *http.Request) preferenceState {
	theme, lang := "dark", s.pages.catalog.MatchAcceptLanguage(r.Header.Get("Accept-Language"))
	if c, e := r.Cookie("theme"); e == nil && hasPreferencesConsent(r) && (c.Value == "dark" || c.Value == "light" || c.Value == "auto") {
		theme = c.Value
	}
	if c, e := r.Cookie("lang"); e == nil && hasPreferencesConsent(r) && s.pages.catalog.IsSupported(c.Value) {
		lang = c.Value
	}
	return preferenceState{Theme: theme, Lang: lang, Prefs: map[string]string{}}
}

func (s *Server) handlePreferencesSyncPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	state := s.preferenceSnapshot(r)
	query := url.Values{"theme": {state.Theme}, "lang": {state.Lang}}.Encode()
	code := base64.RawURLEncoding.EncodeToString([]byte(query))
	state.URL = s.buildPublicURL(r, "/server/preferences/sync/import?"+query)
	state.Code = code
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": state})
		return
	}
	if wantsText(r) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("URL: " + state.URL + "\nCode: " + state.Code + "\n"))
		return
	}
	s.renderPage(w, r, http.StatusOK, pageContent{TitleKey: "preferences.sync.title", DescriptionKey: "preferences.sync.lead", Preferences: true, Sync: true, SyncURL: state.URL, SyncCode: state.Code, BreadcrumbKey: "nav.preferences", BreadcrumbPath: "/server/preferences"})
}

func (s *Server) handlePreferencesSyncPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	values := cloneValues(r.URL.Query())
	for k, v := range r.PostForm {
		if k != "code" {
			values[k] = append([]string(nil), v...)
		}
	}
	code := strings.TrimSpace(r.PostForm.Get("code"))
	if code == "" {
		code = values.Get("code")
	}
	delete(values, "code")
	if code != "" && len(code) <= 2048 {
		decoded := decodePreferenceCode(code)
		for k, v := range decoded {
			if _, exists := values[k]; !exists {
				values[k] = v
			}
		}
	}
	result := s.applyPreferenceValues(w, r, values)
	target := safePreferencesRedirect(r, "/server/preferences/sync")
	if result.ConsentRequired {
		target = addQueryFlag(target, "consent_required", "1")
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) handlePreferencesSyncImport(w http.ResponseWriter, r *http.Request) {
	values := cloneValues(r.URL.Query())
	if code := values.Get("code"); code != "" {
		for k, v := range decodePreferenceCode(code) {
			if _, ok := values[k]; !ok {
				values[k] = v
			}
		}
	}
	result := s.applyPreferenceValues(w, r, values)
	_ = result
	target := safePreferencesRedirect(r, "/")
	if result.ConsentRequired {
		target = addQueryFlag(target, "consent_required", "1")
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) applyPreferenceValues(w http.ResponseWriter, r *http.Request, values url.Values) preferenceState {
	state := s.preferenceSnapshot(r)
	state.Applied = []string{}
	state.Ignored = []string{}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	allowed := false
	if c, e := r.Cookie("cookie_consent"); e == nil {
		allowed = consentAllowsPreferences(c.Value)
	}
	validCount, blocked := 0, 0
	for _, key := range keys {
		if isPreferenceControl(key) {
			continue
		}
		if len(keys) > 32 && len(state.Applied)+len(state.Ignored) >= 32 {
			break
		}
		vals := values[key]
		if len(vals) == 0 || len(vals[0]) > 64 {
			state.Ignored = append(state.Ignored, key)
			continue
		}
		value := vals[0]
		valid := false
		switch key {
		case "theme":
			valid = value == "dark" || value == "light" || value == "auto"
		case "lang":
			value, valid = canonicalPreferenceLanguage(value, s.pages.catalog)
		}
		if !valid {
			state.Ignored = append(state.Ignored, key)
			continue
		}
		validCount++
		if !allowed {
			state.Ignored = append(state.Ignored, key)
			blocked++
			continue
		}
		s.setPreferenceCookie(w, r, key, value)
		state.Applied = append(state.Applied, key)
		if key == "theme" {
			state.Theme = value
		}
		if key == "lang" {
			state.Lang = value
		}
	}
	state.ConsentRequired = validCount > 0 && blocked == validCount
	return state
}

func cloneValues(values url.Values) url.Values {
	out := url.Values{}
	for k, v := range values {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func decodePreferenceCode(raw string) url.Values {
	raw = strings.TrimSpace(raw)
	if len(raw) > 2048 {
		return url.Values{}
	}
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		raw = raw[i+1:]
	}
	if strings.Contains(strings.TrimRight(raw, "="), "=") {
		values, err := url.ParseQuery(raw)
		if err != nil {
			return url.Values{}
		}
		return values
	}
	trimmed := strings.TrimRight(raw, "=")
	decoded, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(raw)
	}
	if err != nil {
		return url.Values{}
	}
	values, err := url.ParseQuery(string(decoded))
	if err != nil {
		return url.Values{}
	}
	return values
}

func canonicalPreferenceLanguage(raw string, catalog *i18n.Catalog) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if catalog.IsSupported(value) {
		return value, true
	}
	tag, err := language.Parse(value)
	if err != nil {
		return "", false
	}
	base, _ := tag.Base()
	value = strings.ToLower(base.String())
	return value, catalog.IsSupported(value)
}

func isPreferenceControl(key string) bool {
	switch key {
	case "csrf_token", "code", "redirect", "action", "consent_form", "consent_preferences", "consent_analytics", "ccpa_opt_out":
		return true
	}
	return false
}

func (s *Server) handlePreferencesAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": s.preferenceSnapshot(r)})
}

func (s *Server) handlePreferencesSyncAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	state := s.preferenceSnapshot(r)
	query := url.Values{"theme": {state.Theme}, "lang": {state.Lang}}.Encode()
	state.URL = s.buildPublicURL(r, "/server/preferences/sync/import?"+query)
	state.Code = base64.RawURLEncoding.EncodeToString([]byte(query))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": state})
}

func (s *Server) handlePreferencesSyncImportAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	values := cloneValues(r.URL.Query())
	if code := values.Get("code"); code != "" {
		for k, v := range decodePreferenceCode(code) {
			if _, ok := values[k]; !ok {
				values[k] = v
			}
		}
	}
	result := s.applyPreferenceValues(w, r, values)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": result})
}

func (s *Server) handlePreferencesAPIPost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	values := url.Values{}
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		var body map[string]any
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body) == nil {
			for k, v := range body {
				if str, ok := v.(string); ok {
					values.Set(k, str)
				}
			}
		}
	} else {
		_ = r.ParseForm()
		values = cloneValues(r.PostForm)
	}
	result := s.applyPreferenceValues(w, r, values)
	if len(result.Applied) == 0 && !result.ConsentRequired {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "No valid preference values were supplied"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": result})
}
