package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLoadAppliesDefaultsAndExpandsEnvironment(t *testing.T) {
	t.Setenv("MAIL_TEST_DB", filepath.Join(t.TempDir(), "server.db"))
	path := filepath.Join(t.TempDir(), "server.yml")
	if err := os.WriteFile(path, []byte("database:\n  driver: sqlite\n  name: ${MAIL_TEST_DB}\nserver:\n  admin_path: /control/\n  logs:\n    level: WARN\n  healthz:\n    root:\n      enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.URL != os.Getenv("MAIL_TEST_DB") || cfg.Server.AdminPath != "control" {
		t.Fatalf("loaded config = %#v", cfg)
	}
	configData, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(configData), "server:\n    admin_path:") || !strings.Contains(string(configData), "    database:") {
		t.Fatalf("legacy database section was not migrated: %s (err=%v)", configData, err)
	}
	backupInfo, err := os.Stat(path + ".bak")
	if err != nil || backupInfo.Mode().Perm() != 0o600 {
		t.Fatalf("configuration migration backup permissions = %v (err=%v)", backupInfo, err)
	}
	if !cfg.Server.Healthz.Root.Enabled {
		t.Fatal("server.healthz.root.enabled was not loaded")
	}
	if cfg.Server.Logs.Level != "warn" {
		t.Fatalf("server.logs.level = %q, want warn", cfg.Server.Logs.Level)
	}
	if cfg.Server.I18n.DefaultLanguage != "en" || len(cfg.Server.I18n.AvailableLanguages) != 6 || !slices.Contains(cfg.Server.I18n.AvailableLanguages, "ar") {
		t.Fatalf("i18n defaults = %#v", cfg.Server.I18n)
	}
	if !cfg.Server.Privacy.Data.StoredOnServer || !cfg.Server.Privacy.Consent.ShowUntilAcknowledged || !cfg.Server.Privacy.Cookies.Essential.Enabled || !cfg.Server.Privacy.Cookies.Preferences.Enabled || !cfg.Server.Privacy.Cookies.Analytics.Enabled {
		t.Fatalf("privacy defaults = %#v", cfg.Server.Privacy)
	}
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 || cfg.Database.Pool.MaxOpen != 25 {
		t.Fatalf("defaults not applied: %#v", cfg)
	}
	if cfg.Server.Tokens.MaxLinked != 25 || cfg.Server.Tokens.CookieMaxAge != "1y" || cfg.Server.Tokens.IdlePurgeDays != 0 {
		t.Fatalf("token defaults = %#v", cfg.Server.Tokens)
	}
	if cfg.Server.Security.Reports.RetentionDays != 365 {
		t.Fatalf("security report retention default = %d, want 365", cfg.Server.Security.Reports.RetentionDays)
	}
	if cfg.Server.Update.Branch != "stable" || cfg.Server.Update.AutoInstall || cfg.Server.Update.DeferDays != 0 {
		t.Fatalf("update defaults = %#v", cfg.Server.Update)
	}
	emailDefaults := cfg.Server.Notifications.Email
	if emailDefaults.SMTP.Port != 587 || emailDefaults.SMTP.TLS != "auto" || emailDefaults.Events.SecurityAlert != true || emailDefaults.Events.Startup {
		t.Fatalf("email notification defaults = %#v", emailDefaults)
	}
	if !slices.Equal(cfg.Web.Robots.Allow, []string{"/", "/api"}) {
		t.Fatalf("robots defaults = %#v", cfg.Web.Robots.Allow)
	}
	if !slices.Equal(cfg.Server.CORS.AllowedOrigins, []string{"*"}) || !cfg.Server.CORS.AllowCredentials || cfg.Server.CORS.MaxAge != 86400 {
		t.Fatalf("CORS defaults = %#v", cfg.Server.CORS)
	}
	if cfg.Server.RateLimit.Read.Requests != 120 || cfg.Server.RateLimit.Read.Window != 60 || cfg.Server.RateLimit.Write.Requests != 10 || cfg.Server.RateLimit.Write.Window != 60 || cfg.Server.RateLimit.Health.Requests != 120 || cfg.Server.RateLimit.Strict.Requests != 10 || cfg.Server.RateLimit.Strict.Window != 900 || cfg.Server.RateLimit.AuthFailures.Requests != 5 || cfg.Server.RateLimit.GlobalBurst != 240 {
		t.Fatalf("rate limit defaults = %#v", cfg.Server.RateLimit)
	}
	if Get() != cfg || GetConfigPath() != path {
		t.Fatal("global config state was not updated")
	}
}

func TestConfigurationInvalidValuesUseDefaults(t *testing.T) {
	cfg := defaultConfig()
	cfg.Server.Tokens.MaxLinked = -1
	cfg.Server.Tokens.CookieMaxAge = ""
	cfg.Server.Tokens.IdlePurgeDays = -1
	cfg.Server.Security.Reports.RetentionDays = 0
	cfg.Server.Notifications.Email.SMTP.Port = 0
	cfg.Server.Notifications.Email.SMTP.TLS = "invalid"
	cfg.Server.Logs.Level = "trace"
	cfg.Server.Update.Branch = "daily"
	cfg.Server.Update.DeferDays = 366
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Tokens.MaxLinked != 25 || cfg.Server.Tokens.CookieMaxAge != "1y" || cfg.Server.Tokens.IdlePurgeDays != 0 {
		t.Fatalf("invalid token config did not fall back: %#v", cfg.Server.Tokens)
	}
	if cfg.Server.Security.Reports.RetentionDays != 365 {
		t.Fatalf("invalid report retention did not fall back: %d", cfg.Server.Security.Reports.RetentionDays)
	}
	if cfg.Server.Notifications.Email.SMTP.Port != 587 || cfg.Server.Notifications.Email.SMTP.TLS != "auto" {
		t.Fatalf("invalid SMTP config did not fall back: %#v", cfg.Server.Notifications.Email.SMTP)
	}
	if cfg.Server.Logs.Level != "" {
		t.Fatalf("invalid log level did not fall back to mode default: %q", cfg.Server.Logs.Level)
	}
	if cfg.Server.Update.Branch != "devel" || cfg.Server.Update.DeferDays != 0 {
		t.Fatalf("update values were not normalized: %#v", cfg.Server.Update)
	}
}

func TestLoadUsesCanonicalServerDatabaseSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yml")
	if err := os.WriteFile(path, []byte("server:\n  database:\n    driver: sqlite\n    url: file:canonical.db\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Database.Driver != "sqlite" || cfg.Server.Database.URL != "file:canonical.db" || cfg.Database.URL != cfg.Server.Database.URL {
		t.Fatalf("canonical database configuration = %+v (compatibility view %+v)", cfg.Server.Database, cfg.Database)
	}
}

func TestLoadUsesCanonicalEmailNotificationSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yml")
	data := []byte("server:\n  fqdn: mail.example.test\n  notifications:\n    email:\n      smtp:\n        host: relay.example.test\n        port: 465\n        tls: auto\n      from:\n        name: Mail\n        email: no-reply@mail.example.test\n      reply_to: postmaster@mail.example.test\n      events:\n        startup: true\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	email := cfg.Server.Notifications.Email
	if email.SMTP.Host != "relay.example.test" || email.SMTP.Port != 465 || email.SMTP.TLS != "auto" || email.From.Name != "Mail" || email.From.Email != "no-reply@mail.example.test" || email.ReplyTo != "postmaster@mail.example.test" || !email.Events.Startup {
		t.Fatalf("loaded email notification config = %#v", email)
	}
}

func TestLoadCORSConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yml")
	data := []byte("server:\n  cors:\n    allowed_origins: [https://mail.example]\n    allow_credentials: false\n    max_age: 600\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Server.CORS.AllowedOrigins, []string{"https://mail.example"}) || cfg.Server.CORS.AllowCredentials || cfg.Server.CORS.MaxAge != 600 {
		t.Fatalf("CORS settings = %#v", cfg.Server.CORS)
	}
}

func TestLogConfigDefaultsAndInvalidValueFallback(t *testing.T) {
	cfg := defaultConfig()
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	logs := cfg.Server.Logs
	if logs.Defaults.Rotate != "weekly,50MB" || logs.Defaults.Keep != "none" || logs.Access.Format != "apache" || logs.Access.Rotate != "monthly" || logs.Audit.Format != "json" || logs.Audit.Rotate != "daily" || *logs.Debug.Enabled {
		t.Fatalf("log defaults = %+v", logs)
	}
	cfg.Server.Logs.Access.Format = "unknown"
	cfg.Server.Logs.Access.Rotate = "0MB"
	cfg.Server.Logs.Audit.Format = "text"
	cfg.Server.Logs.Debug.Enabled = boolValue(true)
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Logs.Access.Format != "apache" || cfg.Server.Logs.Access.Rotate != "weekly,50MB" || cfg.Server.Logs.Audit.Format != "json" || !*cfg.Server.Logs.Debug.Enabled {
		t.Fatalf("invalid log values did not fall back safely: %+v", cfg.Server.Logs)
	}

	customDefaults := defaultConfig()
	customDefaults.Server.Logs.Defaults.Rotate = "daily,100MB"
	if err := customDefaults.validate(); err != nil {
		t.Fatal(err)
	}
	if customDefaults.Server.Logs.Access.Rotate != "daily,100MB" || customDefaults.Server.Logs.Audit.Rotate != "daily,100MB" {
		t.Fatalf("per-kind rotation did not inherit configured defaults: %+v", customDefaults.Server.Logs)
	}
}

func TestLoadRobotsRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yml")
	data := []byte("web:\n  robots:\n    allow: [/docs, /]\n    deny: [/private]\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Web.Robots.Allow, []string{"/docs", "/"}) || !slices.Equal(cfg.Web.Robots.Deny, []string{"/private"}) {
		t.Fatalf("robots rules = %#v", cfg.Web.Robots)
	}
}

func TestLoadPrivacyServicesAndDPOContact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yml")
	data := []byte("server:\n  contact:\n    dpo:\n      name: Privacy Officer\n      email: dpo@example.test\n    general:\n      email: general@example.test\n  privacy:\n    third_party:\n      services:\n        - name: Mail provider\n          purpose: Delivery\n          data_sent: Recipient address\n          policy_url: https://provider.example/privacy\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	brandingDir := filepath.Join(dir, "yaml.d")
	if err := os.Mkdir(brandingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	branding := []byte("server:\n  branding:\n    content:\n      about:\n        format: text\n        body: About body\n      privacy:\n        format: md\n        body: Operator notice\n      terms:\n        format: html\n        body: Terms body\n")
	if err := os.WriteFile(filepath.Join(brandingDir, "branding.yml"), branding, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Contact.DPO.Name != "Privacy Officer" || cfg.Server.Contact.DPO.Email != "dpo@example.test" || cfg.Server.Contact.General.Email != "general@example.test" {
		t.Fatalf("DPO contact = %+v", cfg.Server.Contact)
	}
	if len(cfg.Server.Privacy.ThirdParty.Services) != 1 || cfg.Server.Privacy.ThirdParty.Services[0].Name != "Mail provider" || cfg.Server.Privacy.ThirdParty.Services[0].PolicyURL != "https://provider.example/privacy" {
		t.Fatalf("privacy services = %+v", cfg.Server.Privacy.ThirdParty.Services)
	}
	if cfg.Server.Branding.Content.Privacy.Body.Resolve("en") != "Operator notice" || cfg.Server.Branding.Content.Privacy.Format != "md" {
		t.Fatalf("privacy branding = %+v", cfg.Server.Branding.Content.Privacy)
	}
	if cfg.Server.Branding.Content.About.Body.Resolve("en") != "About body" || cfg.Server.Branding.Content.About.Format != "text" || cfg.Server.Branding.Content.Terms.Body.Resolve("en") != "Terms body" || cfg.Server.Branding.Content.Terms.Format != "html" {
		t.Fatalf("about/terms branding = %+v / %+v", cfg.Server.Branding.Content.About, cfg.Server.Branding.Content.Terms)
	}
}

func TestLoadSecurityContactsAndContactPageSwitch(t *testing.T) {
	dir := t.TempDir()
	defaultPath := filepath.Join(dir, "defaults.yml")
	if err := os.WriteFile(defaultPath, []byte("server: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	defaults, err := Load(defaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Server.Contact.Security.Email != "security@{fqdn}" || defaults.Server.Pages.Contact.Enabled == nil || !*defaults.Server.Pages.Contact.Enabled || defaults.Web.Security.ReportURL == "" || len(defaults.Web.Security.Keyservers) != 1 || defaults.Web.Security.Keyservers[0] != "keys.openpgp.org" {
		t.Fatalf("security contact defaults = email %q, enabled %v, report URL %q, keyservers %v", defaults.Server.Contact.Security.Email, defaults.Server.Pages.Contact.Enabled, defaults.Web.Security.ReportURL, defaults.Web.Security.Keyservers)
	}
	path := filepath.Join(dir, "override.yml")
	data := []byte("server:\n  contact:\n    security:\n      email: \"\"\n  pages:\n    contact:\n      enabled: false\nweb:\n  security:\n    report_url: \"\"\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	override, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if override.Server.Contact.Security.Email != "" || override.Server.Pages.Contact.Enabled == nil || *override.Server.Pages.Contact.Enabled || override.Web.Security.ReportURL != "" {
		t.Fatalf("security contact overrides = email %q, enabled %v, report URL %q", override.Server.Contact.Security.Email, override.Server.Pages.Contact.Enabled, override.Web.Security.ReportURL)
	}
	keyserverPath := filepath.Join(dir, "keyservers.yml")
	if err := os.WriteFile(keyserverPath, []byte("web:\n  security:\n    keyservers: [keys.openpgp.org, https://attacker.example/path, keyserver.ubuntu.com]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keyserverConfig, err := Load(keyserverPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(keyserverConfig.Web.Security.Keyservers, ","); got != "keys.openpgp.org,keyserver.ubuntu.com" {
		t.Fatalf("validated keyservers = %q", got)
	}
}

func TestValidBareKeyserverHost(t *testing.T) {
	for _, host := range []string{"keys.openpgp.org", "127.0.0.1"} {
		if !validBareKeyserverHost(host) {
			t.Errorf("validBareKeyserverHost(%q) = false", host)
		}
	}
	for _, host := range []string{"", "https://keys.example", "a..b", "-bad.example", "bad-.example", "bad/host", strings.Repeat("a", 64) + ".example", strings.Repeat("a", 254)} {
		if validBareKeyserverHost(host) {
			t.Errorf("validBareKeyserverHost(%q) = true", host)
		}
	}
}

func TestLoadRestoresIncompleteLocaleSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yml")
	data := []byte("server:\n  i18n:\n    default_language: en\n    available_languages: [en]\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil || len(cfg.Server.I18n.AvailableLanguages) != 6 {
		t.Fatalf("incomplete locale set fallback = %#v (err=%v)", cfg.Server.I18n.AvailableLanguages, err)
	}
}

func TestLoadFallsBackForMalformedAndMissingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.yml")
	if err := os.WriteFile(path, []byte("database: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, err := Load(path); err != nil || cfg.Server.Mode != "production" {
		t.Fatalf("Load malformed config = %#v, %v", cfg, err)
	}
	if cfg, err := Load(filepath.Join(t.TempDir(), "missing.yml")); err != nil || cfg.Server.Mode != "production" {
		t.Fatalf("Load missing config = %#v, %v", cfg, err)
	}
}

func TestDatabaseDSNAndDuration(t *testing.T) {
	for _, tc := range []struct {
		db   DatabaseConfig
		want string
	}{
		{DatabaseConfig{Driver: "sqlite", URL: "file:test.db"}, "file:test.db"},
		{DatabaseConfig{Driver: "sqlite", Name: "server.db"}, "server.db"},
		{DatabaseConfig{Driver: "libsql", URL: "libsql://db", Token: "secret"}, "libsql://db?authToken=secret"},
		{DatabaseConfig{Driver: "libsql", URL: "libsql://db?mode=rw", Token: "secret"}, "libsql://db?mode=rw&authToken=secret"},
		{DatabaseConfig{Driver: "postgres"}, ""},
		{DatabaseConfig{Driver: "mysql"}, ""},
	} {
		if got := tc.db.DSN(); got != tc.want {
			t.Errorf("DSN() = %q, want %q", got, tc.want)
		}
	}
	if got, err := ParseDuration("5m"); err != nil || got != 5*time.Minute {
		t.Fatalf("ParseDuration = %v, %v", got, err)
	}
	if _, err := ParseDuration("bad"); err == nil {
		t.Fatal("ParseDuration accepted an invalid duration")
	}
}
