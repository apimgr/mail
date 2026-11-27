package config

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/apimgr/mail/src/common/i18n"
	"github.com/apimgr/mail/src/common/security"
)

// Config represents the complete server configuration
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	SSL      SSLConfig      `yaml:"ssl"`
	Email    EmailConfig    `yaml:"email"`
	Web      WebConfig      `yaml:"web"`
}

type PrivacyServiceConfig struct {
	Name      string `yaml:"name"`
	Purpose   string `yaml:"purpose"`
	DataSent  string `yaml:"data_sent"`
	PolicyURL string `yaml:"policy_url"`
}

type HelpPageConfig struct {
	Content string `yaml:"content"`
	MCP     string `yaml:"mcp"`
}

type NotificationConfig struct {
	WebUI WebUINotificationConfig `yaml:"webui"`
	Email EmailNotificationConfig `yaml:"email"`
}

type WebUINotificationConfig struct {
	Position        string `yaml:"position"`
	Duration        int    `yaml:"duration"`
	WarningDuration int    `yaml:"warning_duration"`
}

type EmailNotificationConfig struct {
	SMTP    SMTPConfig              `yaml:"smtp"`
	From    EmailFromConfig         `yaml:"from"`
	ReplyTo string                  `yaml:"reply_to"`
	Events  EmailNotificationEvents `yaml:"events"`
}

type SMTPConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	TLS      string `yaml:"tls"`
}

type EmailFromConfig struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
}

type EmailNotificationEvents struct {
	Startup          bool `yaml:"startup"`
	Shutdown         bool `yaml:"shutdown"`
	BackupComplete   bool `yaml:"backup_complete"`
	BackupFailed     bool `yaml:"backup_failed"`
	SSLExpiring      bool `yaml:"ssl_expiring"`
	SSLRenewed       bool `yaml:"ssl_renewed"`
	SSLRenewalFailed bool `yaml:"ssl_renewal_failed"`
	SecurityAlert    bool `yaml:"security_alert"`
	SchedulerError   bool `yaml:"scheduler_error"`
	UpdateAvailable  bool `yaml:"update_available"`
	UpdateInstalled  bool `yaml:"update_installed"`
	UpdateFailed     bool `yaml:"update_failed"`
	UpdateStaged     bool `yaml:"update_staged"`
	DiskLow          bool `yaml:"disk_low"`
	DatabaseError    bool `yaml:"database_error"`
}

// LogsConfig defines global and per-kind log behavior.
type LogsConfig struct {
	Level    string            `yaml:"level"`
	Defaults LogDefaultsConfig `yaml:"defaults"`
	Access   LogKindConfig     `yaml:"access"`
	Server   LogKindConfig     `yaml:"server"`
	Error    LogKindConfig     `yaml:"error"`
	Audit    LogKindConfig     `yaml:"audit"`
	Security LogKindConfig     `yaml:"security"`
	Debug    LogKindConfig     `yaml:"debug"`
}

// LogDefaultsConfig contains inherited values for every log kind.
type LogDefaultsConfig struct {
	Custom string `yaml:"custom"`
	Rotate string `yaml:"rotate"`
	Keep   string `yaml:"keep"`
}

// LogKindConfig contains one log's output and retention policy.
type LogKindConfig struct {
	Enabled  *bool  `yaml:"enabled"`
	Format   string `yaml:"format"`
	Custom   string `yaml:"custom"`
	Rotate   string `yaml:"rotate"`
	Keep     string `yaml:"keep"`
	Compress bool   `yaml:"compress"`
}

type BrandingPageContentConfig struct {
	Format string           `yaml:"format"`
	Body   LocalizedContent `yaml:"body"`
}

type LocalizedContent map[string]string

func (c *LocalizedContent) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		*c = LocalizedContent{"default": node.Value}
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("content body must be a string or language map")
	}
	values := make(LocalizedContent, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Kind != yaml.ScalarNode || value.Kind != yaml.ScalarNode {
			return fmt.Errorf("content body language keys and values must be strings")
		}
		values[strings.ToLower(strings.TrimSpace(key.Value))] = value.Value
	}
	*c = values
	return nil
}

func (c LocalizedContent) Resolve(language string) string {
	if content, ok := c[strings.ToLower(strings.TrimSpace(language))]; ok {
		return content
	}
	return c["default"]
}

type WebConfig struct {
	Security struct {
		ReportURL  string   `yaml:"report_url"`
		Policy     string   `yaml:"policy"`
		Keyservers []string `yaml:"keyservers"`
	} `yaml:"security"`
	Robots struct {
		Allow []string `yaml:"allow"`
		Deny  []string `yaml:"deny"`
	} `yaml:"robots"`
}

// ServerConfig holds server-specific settings
type ServerConfig struct {
	Port      int            `yaml:"port"`
	Address   string         `yaml:"address"`
	PIDFile   bool           `yaml:"pidfile"`
	Daemonize bool           `yaml:"daemonize"`
	FQDN      string         `yaml:"fqdn"`
	AdminPath string         `yaml:"admin_path"`
	Database  DatabaseConfig `yaml:"database"`
	SSL       SSLConfig      `yaml:"ssl"`
	Token     string         `yaml:"token,omitempty"`
	Security  struct {
		EncryptionKey        string `yaml:"encryption_key,omitempty"`
		EncryptionKeyVersion int    `yaml:"encryption_key_version,omitempty"`
		Reports              struct {
			RetentionDays int `yaml:"retention_days"`
		} `yaml:"reports"`
	} `yaml:"security"`
	SetupToken     string          `yaml:"setup_token"`
	Maintenance    bool            `yaml:"maintenance"`
	Debug          bool            `yaml:"debug"`
	Mode           string          `yaml:"mode"`
	Logs           LogsConfig      `yaml:"logs"`
	Scheduler      SchedulerConfig `yaml:"scheduler"`
	Update         UpdateConfig    `yaml:"update"`
	TrustedProxies []string        `yaml:"trusted_proxies"`
	CORS           struct {
		AllowedOrigins   []string `yaml:"allowed_origins"`
		AllowCredentials bool     `yaml:"allow_credentials"`
		MaxAge           int      `yaml:"max_age"`
	} `yaml:"cors"`

	Limits struct {
		MaxBodySize  string `yaml:"max_body_size"`
		ReadTimeout  string `yaml:"read_timeout"`
		WriteTimeout string `yaml:"write_timeout"`
		IdleTimeout  string `yaml:"idle_timeout"`
	} `yaml:"limits"`

	Session struct {
		Admin struct {
			CookieName       string `yaml:"cookie_name"`
			MaxAge           string `yaml:"max_age"`
			IdleTimeout      string `yaml:"idle_timeout"`
			ExtendOnActivity bool   `yaml:"extend_on_activity"`
		} `yaml:"admin"`
		User struct {
			CookieName       string `yaml:"cookie_name"`
			MaxAge           string `yaml:"max_age"`
			IdleTimeout      string `yaml:"idle_timeout"`
			ExtendOnActivity bool   `yaml:"extend_on_activity"`
		} `yaml:"user"`
		Secure   string `yaml:"secure"`
		HTTPOnly bool   `yaml:"http_only"`
		SameSite string `yaml:"same_site"`
	} `yaml:"session"`

	RateLimit     RateLimitConfig    `yaml:"rate_limit"`
	Notifications NotificationConfig `yaml:"notifications"`
	Tokens        struct {
		MaxLinked     int    `yaml:"max_linked"`
		CookieMaxAge  string `yaml:"cookie_max_age"`
		IdlePurgeDays int    `yaml:"idle_purge_days"`
	} `yaml:"tokens"`

	I18n struct {
		DefaultLanguage    string   `yaml:"default_language"`
		AvailableLanguages []string `yaml:"available_languages"`
	} `yaml:"i18n"`

	Tracking struct {
		Type string `yaml:"type"`
		ID   string `yaml:"id"`
		URL  string `yaml:"url"`
	} `yaml:"tracking"`
	Contact struct {
		Admin struct {
			Email string `yaml:"email"`
		} `yaml:"admin"`
		DPO struct {
			Name  string `yaml:"name"`
			Email string `yaml:"email"`
		} `yaml:"dpo"`
		Security struct {
			Email string `yaml:"email"`
		} `yaml:"security"`
		General struct {
			Email string `yaml:"email"`
		} `yaml:"general"`
	} `yaml:"contact"`
	Branding struct {
		Content struct {
			Header  BrandingPageContentConfig `yaml:"header"`
			About   BrandingPageContentConfig `yaml:"about"`
			Privacy BrandingPageContentConfig `yaml:"privacy"`
			Terms   BrandingPageContentConfig `yaml:"terms"`
			Footer  BrandingPageContentConfig `yaml:"footer"`
		} `yaml:"content"`
	} `yaml:"branding"`
	Pages struct {
		Help    HelpPageConfig `yaml:"help"`
		Contact struct {
			Enabled *bool `yaml:"enabled"`
		} `yaml:"contact"`
	} `yaml:"pages"`

	Privacy struct {
		Data struct {
			Sold           bool `yaml:"sold"`
			StoredOnServer bool `yaml:"stored_on_server"`
			Sharing        []struct {
				Condition string `yaml:"condition"`
				When      string `yaml:"when"`
				Data      string `yaml:"data"`
			} `yaml:"sharing"`
		} `yaml:"data"`
		Consent struct {
			ShowUntilAcknowledged bool   `yaml:"show_until_acknowledged"`
			Message               string `yaml:"message"`
			MessageIfSold         string `yaml:"message_if_sold"`
			Policy                struct {
				Text    string `yaml:"text"`
				URL     string `yaml:"url"`
				Version string `yaml:"version"`
			} `yaml:"policy"`
			Buttons struct {
				Decline string `yaml:"decline"`
				Accept  string `yaml:"accept"`
			} `yaml:"buttons"`
			Position        string `yaml:"position"`
			ShowPreferences bool   `yaml:"show_preferences"`
			PreferencesText string `yaml:"preferences_text"`
		} `yaml:"consent"`
		Cookies struct {
			Essential struct {
				Enabled     bool   `yaml:"enabled"`
				Description string `yaml:"description"`
			} `yaml:"essential"`
			Preferences struct {
				Enabled     bool   `yaml:"enabled"`
				Description string `yaml:"description"`
			} `yaml:"preferences"`
			Analytics struct {
				Enabled                  bool   `yaml:"enabled"`
				Description              string `yaml:"description"`
				DescriptionSuffixNotSold string `yaml:"description_suffix_not_sold"`
				DescriptionSuffixSold    string `yaml:"description_suffix_sold"`
			} `yaml:"analytics"`
		} `yaml:"cookies"`
		Retention struct {
			Period            string `yaml:"period"`
			ExportAvailable   bool   `yaml:"export_available"`
			DeletionAvailable bool   `yaml:"deletion_available"`
		} `yaml:"retention"`
		ThirdParty struct {
			Services []PrivacyServiceConfig `yaml:"services"`
		} `yaml:"third_party"`
	} `yaml:"privacy"`

	Healthz struct {
		Root struct {
			Enabled bool `yaml:"enabled"`
		} `yaml:"root"`
	} `yaml:"healthz"`
}

// SchedulerConfig controls the built-in scheduler without persisting runtime
// task state in the configuration file.
type SchedulerConfig struct {
	Timezone      string                         `yaml:"timezone"`
	CatchUpWindow string                         `yaml:"catch_up_window"`
	Tasks         map[string]SchedulerTaskConfig `yaml:"tasks"`
}

// UpdateConfig controls the release channel and scheduled update behavior.
type UpdateConfig struct {
	Branch        string `yaml:"branch"`
	AutoInstall   bool   `yaml:"auto_install"`
	DeferDays     int    `yaml:"defer_days"`
	CLIMinVersion string `yaml:"cli_min_version"`
}

// SchedulerTaskConfig contains the supported per-task settings.
type SchedulerTaskConfig struct {
	Enabled     *bool  `yaml:"enabled"`
	Schedule    string `yaml:"schedule"`
	RetryOnFail bool   `yaml:"retry_on_fail"`
	RetryDelay  string `yaml:"retry_delay"`
}

type RateLimitWindowConfig struct {
	Requests int `yaml:"requests"`
	Window   int `yaml:"window"`
}

type RateLimitConfig struct {
	Read         RateLimitWindowConfig `yaml:"read"`
	Write        RateLimitWindowConfig `yaml:"write"`
	Health       RateLimitWindowConfig `yaml:"health"`
	Strict       RateLimitWindowConfig `yaml:"strict"`
	AuthFailures RateLimitWindowConfig `yaml:"auth_failures"`
	GlobalBurst  int                   `yaml:"global_burst"`
}

// DatabaseConfig holds database connection settings
type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	Name   string `yaml:"-"`
	URL    string `yaml:"url"`
	Token  string `yaml:"token"`

	Pool struct {
		MaxOpen     int    `yaml:"max_open"`
		MaxIdle     int    `yaml:"max_idle"`
		MaxLifetime string `yaml:"max_lifetime"`
		MaxIdleTime string `yaml:"max_idle_time"`
	} `yaml:"pool"`
}

// SSLConfig holds SSL/TLS and Let's Encrypt settings
type SSLConfig struct {
	Enabled     bool   `yaml:"enabled"`
	CertFile    string `yaml:"cert_file"`
	KeyFile     string `yaml:"key_file"`
	LetsEncrypt bool   `yaml:"lets_encrypt"`
	Email       string `yaml:"email"`
	Staging     bool   `yaml:"staging"`
}

// EmailConfig holds SMTP settings for outgoing mail
type EmailConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	From     string `yaml:"from"`
	UseTLS   bool   `yaml:"use_tls"`
}

var (
	// Global config instance
	globalConfig *Config
	configPath   string
)

// Load loads configuration from YAML file with environment variable expansion
func Load(path string) (*Config, error) {
	configPath = path
	if err := migrateLegacyDatabaseSection(path); err != nil {
		slog.Warn("configuration migration skipped; using the merged source", "file", path)
	}
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("configuration file unavailable; using defaults", "file", path, "error", err)
	} else {
		candidate := defaultConfig()
		if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(data))), candidate); err != nil {
			slog.Warn("configuration file is invalid; using defaults", "file", path)
		} else {
			cfg = candidate
		}
	}
	if err := loadYAMLDropins(filepath.Join(filepath.Dir(path), "yaml.d"), cfg); err != nil {
		slog.Warn("configuration drop-ins unavailable", "error", err)
	}
	if err := loadSecrets(filepath.Join(filepath.Dir(path), "secrets.yml"), cfg); err != nil {
		slog.Warn("secrets file unavailable or invalid; retaining merged defaults")
	}

	// Validate and set defaults.
	if err := cfg.validate(); err != nil {
		slog.Warn("configuration validation failed; using safe defaults")
		cfg = defaultConfig()
		_ = cfg.validate()
	}

	globalConfig = cfg
	return cfg, nil
}

func defaultConfig() *Config {
	cfg := &Config{}
	cfg.Server.Contact.Security.Email = "security@{fqdn}"
	cfg.Server.Pages.Contact.Enabled = boolValue(true)
	cfg.Web.Security.ReportURL = "https://github.com/apimgr/mail/security/advisories/new"
	cfg.Web.Security.Keyservers = []string{"keys.openpgp.org"}
	cfg.Server.PIDFile = true
	cfg.Server.Logs.Defaults.Rotate = "weekly,50MB"
	cfg.Server.Logs.Defaults.Keep = "none"
	cfg.Server.Logs.Access = LogKindConfig{Enabled: boolValue(true), Format: "apache"}
	cfg.Server.Logs.Server = LogKindConfig{Enabled: boolValue(true), Format: "text"}
	cfg.Server.Logs.Error = LogKindConfig{Enabled: boolValue(true), Format: "text"}
	cfg.Server.Logs.Audit = LogKindConfig{Enabled: boolValue(true), Format: "json"}
	cfg.Server.Logs.Security = LogKindConfig{Enabled: boolValue(true), Format: "fail2ban"}
	cfg.Server.Logs.Debug = LogKindConfig{Enabled: boolValue(false), Format: "text"}
	cfg.Server.Scheduler.Timezone = "America/New_York"
	cfg.Server.Scheduler.CatchUpWindow = "1h"
	cfg.Server.Update.Branch = "stable"
	cfg.Server.Tokens.MaxLinked = 25
	cfg.Server.Tokens.CookieMaxAge = "1y"
	cfg.Server.Security.Reports.RetentionDays = 365
	cfg.Server.Notifications.WebUI.Position = "top-right"
	cfg.Server.Notifications.WebUI.Duration = 3
	cfg.Server.Notifications.WebUI.WarningDuration = 5
	cfg.Server.Notifications.Email.SMTP.Port = 587
	cfg.Server.Notifications.Email.SMTP.TLS = "auto"
	cfg.Server.Notifications.Email.Events.BackupFailed = true
	cfg.Server.Notifications.Email.Events.SSLExpiring = true
	cfg.Server.Notifications.Email.Events.SSLRenewalFailed = true
	cfg.Server.Notifications.Email.Events.SecurityAlert = true
	cfg.Server.Notifications.Email.Events.SchedulerError = true
	cfg.Server.Notifications.Email.Events.UpdateInstalled = true
	cfg.Server.Notifications.Email.Events.UpdateFailed = true
	cfg.Server.Notifications.Email.Events.UpdateStaged = true
	cfg.Server.Notifications.Email.Events.DiskLow = true
	cfg.Server.Notifications.Email.Events.DatabaseError = true
	cfg.Server.Privacy.Data.StoredOnServer = true
	cfg.Server.Privacy.Consent.ShowUntilAcknowledged = true
	cfg.Server.Privacy.Consent.ShowPreferences = true
	cfg.Server.Privacy.Cookies.Preferences.Enabled = true
	cfg.Server.Privacy.Cookies.Analytics.Enabled = true
	cfg.Server.Privacy.Cookies.Essential.Enabled = true
	cfg.Server.Privacy.Retention.ExportAvailable = true
	cfg.Server.Privacy.Retention.DeletionAvailable = true
	cfg.Server.CORS.AllowCredentials = true
	cfg.Server.CORS.MaxAge = 86400
	return cfg
}

func boolValue(value bool) *bool { return &value }

func validateLogConfig(config *LogsConfig) {
	config.Defaults.Custom = strings.TrimSpace(config.Defaults.Custom)
	if !validLogRotation(config.Defaults.Rotate) {
		slog.Warn("invalid configuration value; using default", "key", "server.logs.defaults.rotate")
		config.Defaults.Rotate = "weekly,50MB"
	}
	if !validLogRetention(config.Defaults.Keep) {
		slog.Warn("invalid configuration value; using default", "key", "server.logs.defaults.keep")
		config.Defaults.Keep = "none"
	}
	kinds := []struct {
		name          string
		kind          *LogKindConfig
		formats       []string
		defaultFormat string
		defaultRotate string
	}{
		{name: "access", kind: &config.Access, formats: []string{"apache", "nginx", "json", "custom"}, defaultFormat: "apache", defaultRotate: "monthly"},
		{name: "server", kind: &config.Server, formats: []string{"text", "json"}, defaultFormat: "text"},
		{name: "error", kind: &config.Error, formats: []string{"text", "json"}, defaultFormat: "text"},
		{name: "audit", kind: &config.Audit, formats: []string{"json"}, defaultFormat: "json", defaultRotate: "daily"},
		{name: "security", kind: &config.Security, formats: []string{"fail2ban", "syslog", "cef", "json", "text"}, defaultFormat: "fail2ban"},
		{name: "debug", kind: &config.Debug, formats: []string{"text", "json"}, defaultFormat: "text"},
	}
	for _, item := range kinds {
		kind := item.kind
		if kind.Enabled == nil {
			kind.Enabled = boolValue(item.name != "debug")
		}
		kind.Format = strings.ToLower(strings.TrimSpace(kind.Format))
		if kind.Format == "" {
			kind.Format = item.defaultFormat
		}
		if !containsString(item.formats, kind.Format) {
			slog.Warn("invalid configuration value; using default", "key", "server.logs."+item.name+".format")
			kind.Format = item.defaultFormat
		}
		if kind.Rotate == "" {
			if item.defaultRotate != "" && config.Defaults.Rotate == "weekly,50MB" {
				kind.Rotate = item.defaultRotate
			} else {
				kind.Rotate = config.Defaults.Rotate
			}
		}
		if !validLogRotation(kind.Rotate) {
			slog.Warn("invalid configuration value; using default", "key", "server.logs."+item.name+".rotate")
			kind.Rotate = config.Defaults.Rotate
		}
		if kind.Keep == "" {
			kind.Keep = config.Defaults.Keep
		}
		if !validLogRetention(kind.Keep) {
			slog.Warn("invalid configuration value; using default", "key", "server.logs."+item.name+".keep")
			kind.Keep = config.Defaults.Keep
		}
		if kind.Custom == "" {
			kind.Custom = config.Defaults.Custom
		}
		if kind.Format == "custom" && strings.TrimSpace(kind.Custom) == "" {
			slog.Warn("custom log format is empty; using default format", "key", "server.logs."+item.name+".custom")
			kind.Format = item.defaultFormat
		}
		if kind.Format == "custom" && !validCustomLogFormat(kind.Custom) {
			slog.Warn("custom log format has unknown or malformed variables; using default format", "key", "server.logs."+item.name+".custom")
			kind.Format = item.defaultFormat
		}
	}
}

func validCustomLogFormat(format string) bool {
	allowed := map[string]struct{}{
		"time": {}, "date": {}, "datetime": {}, "remote_ip": {}, "method": {}, "path": {}, "query": {},
		"status": {}, "bytes": {}, "latency": {}, "latency_ms": {}, "user_agent": {}, "referer": {},
		"request_id": {}, "fqdn": {}, "protocol": {}, "tls_version": {}, "country": {}, "asn": {},
	}
	for len(format) > 0 {
		start := strings.IndexByte(format, '{')
		endWithoutStart := strings.IndexByte(format, '}')
		if endWithoutStart >= 0 && (start < 0 || endWithoutStart < start) {
			return false
		}
		if start < 0 {
			return true
		}
		end := strings.IndexByte(format[start+1:], '}')
		if end < 0 {
			return false
		}
		key := format[start+1 : start+1+end]
		if _, exists := allowed[key]; !exists {
			return false
		}
		format = format[start+2+end:]
	}
	return true
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validLogRotation(value string) bool {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(value)), ",")
	if len(parts) == 0 || len(parts) > 2 {
		return false
	}
	validTime := func(part string) bool {
		switch part {
		case "never", "daily", "weekly", "monthly", "yearly":
			return true
		}
		return false
	}
	validSize := func(part string) bool {
		for _, suffix := range []string{"mb", "gb"} {
			if strings.HasSuffix(part, suffix) {
				amount := strings.TrimSuffix(part, suffix)
				n, err := strconv.ParseUint(amount, 10, 32)
				return err == nil && n > 0
			}
		}
		return false
	}
	if len(parts) == 1 {
		return validTime(parts[0]) || validSize(parts[0])
	}
	return (validTime(parts[0]) && validSize(parts[1])) || (validSize(parts[0]) && validTime(parts[1]))
}

func validLogRetention(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "none" || value == "forever" {
		return true
	}
	for _, suffix := range []string{"d", "w", "m"} {
		if strings.HasSuffix(value, suffix) {
			n, err := strconv.ParseUint(strings.TrimSuffix(value, suffix), 10, 32)
			return err == nil && n > 0
		}
	}
	count, err := strconv.ParseUint(value, 10, 32)
	return err == nil && count > 0
}

func loadYAMLDropins(dir string, cfg *Config) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read config drop-in directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".yml") && !strings.HasSuffix(entry.Name(), ".yaml")) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			slog.Warn("cannot read configuration drop-in; retaining last good values", "file", entry.Name(), "error", err)
			continue
		}
		candidate, err := cloneConfig(cfg)
		if err != nil {
			return err
		}
		if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(data))), candidate); err != nil {
			slog.Warn("invalid configuration drop-in; retaining last good values", "file", entry.Name())
			continue
		}
		*cfg = *candidate
	}
	return nil
}

func cloneConfig(cfg *Config) (*Config, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	copy := &Config{}
	if err := yaml.Unmarshal(data, copy); err != nil {
		return nil, err
	}
	return copy, nil
}

// validate validates and sets defaults for configuration
func (c *Config) validate() error {
	keyservers := make([]string, 0, len(c.Web.Security.Keyservers))
	for _, host := range c.Web.Security.Keyservers {
		host = strings.ToLower(strings.TrimSpace(host))
		if !validBareKeyserverHost(host) {
			slog.Warn("invalid keyserver host; ignoring entry", "key", "web.security.keyservers", "host", host)
			continue
		}
		keyservers = append(keyservers, host)
	}
	c.Web.Security.Keyservers = keyservers
	c.Server.Update.Branch = strings.ToLower(strings.TrimSpace(c.Server.Update.Branch))
	switch c.Server.Update.Branch {
	case "", "stable":
		c.Server.Update.Branch = "stable"
	case "beta":
	case "daily", "rolling", "dev", "development":
		c.Server.Update.Branch = "devel"
	case "devel":
	default:
		slog.Warn("invalid configuration value; using stable", "key", "server.update.branch")
		c.Server.Update.Branch = "stable"
	}
	if c.Server.Update.DeferDays < 0 || c.Server.Update.DeferDays > 365 {
		slog.Warn("invalid configuration value; using default", "key", "server.update.defer_days")
		c.Server.Update.DeferDays = 0
	}
	c.Server.Logs.Level = strings.ToLower(strings.TrimSpace(c.Server.Logs.Level))
	switch c.Server.Logs.Level {
	case "", "debug", "info", "warn", "warning", "error":
	default:
		slog.Warn("invalid configuration value; using mode log level", "key", "server.logs.level")
		c.Server.Logs.Level = ""
	}
	validateLogConfig(&c.Server.Logs)
	if c.Server.Tokens.MaxLinked < 1 {
		slog.Warn("invalid configuration value; using default", "key", "server.tokens.max_linked")
		c.Server.Tokens.MaxLinked = 25
	}
	if strings.TrimSpace(c.Server.Tokens.CookieMaxAge) == "" {
		c.Server.Tokens.CookieMaxAge = "1y"
	}
	if c.Server.Tokens.IdlePurgeDays < 0 {
		slog.Warn("invalid configuration value; using default", "key", "server.tokens.idle_purge_days")
		c.Server.Tokens.IdlePurgeDays = 0
	}
	if c.Server.Security.Reports.RetentionDays <= 0 {
		slog.Warn("invalid or missing security report retention; using default", "key", "server.security.reports.retention_days", "default", 365)
		c.Server.Security.Reports.RetentionDays = 365
	}
	if c.Server.Notifications.WebUI.Position == "" {
		c.Server.Notifications.WebUI.Position = "top-right"
	}
	switch c.Server.Notifications.WebUI.Position {
	case "top-right", "top-left", "bottom-right", "bottom-left":
	default:
		slog.Warn("invalid configuration value; using default", "key", "server.notifications.webui.position")
		c.Server.Notifications.WebUI.Position = "top-right"
	}
	if c.Server.Notifications.WebUI.Duration < 0 {
		slog.Warn("invalid configuration value; using default", "key", "server.notifications.webui.duration")
		c.Server.Notifications.WebUI.Duration = 3
	}
	if c.Server.Notifications.WebUI.WarningDuration < 0 {
		slog.Warn("invalid configuration value; using default", "key", "server.notifications.webui.warning_duration")
		c.Server.Notifications.WebUI.WarningDuration = 5
	}
	if c.Server.Notifications.Email.SMTP.Port < 1 || c.Server.Notifications.Email.SMTP.Port > 65535 {
		slog.Warn("invalid configuration value; using default", "key", "server.notifications.email.smtp.port")
		c.Server.Notifications.Email.SMTP.Port = 587
	}
	c.Server.Notifications.Email.SMTP.TLS = strings.ToLower(strings.TrimSpace(c.Server.Notifications.Email.SMTP.TLS))
	switch c.Server.Notifications.Email.SMTP.TLS {
	case "", "auto":
		c.Server.Notifications.Email.SMTP.TLS = "auto"
	case "starttls", "tls", "none":
	default:
		slog.Warn("invalid configuration value; using default", "key", "server.notifications.email.smtp.tls")
		c.Server.Notifications.Email.SMTP.TLS = "auto"
	}
	if c.Server.Notifications.Email.From.Email == "" && c.Server.FQDN != "" {
		c.Server.Notifications.Email.From.Email = "no-reply@" + strings.TrimSuffix(c.Server.FQDN, ".")
	}
	if databaseConfigSet(c.Server.Database) {
		c.Database = c.Server.Database
	} else {
		c.Server.Database = c.Database
	}
	setRateLimitDefaults(&c.Server.RateLimit)
	if len(c.Server.CORS.AllowedOrigins) == 0 {
		c.Server.CORS.AllowedOrigins = []string{"*"}
	}
	if c.Server.CORS.MaxAge < 0 {
		c.Server.CORS.MaxAge = 0
	}
	if len(c.Web.Robots.Allow) == 0 {
		c.Web.Robots.Allow = []string{"/", "/api"}
	}
	if c.Server.Privacy.Consent.Message == "" {
		c.Server.Privacy.Consent.Message = "We use essential cookies and, with your consent, preference and analytics cookies. Your data is stored on our servers and is never sold."
	}
	if c.Server.Privacy.Consent.MessageIfSold == "" {
		c.Server.Privacy.Consent.MessageIfSold = "We use essential cookies and, with your consent, preference and analytics cookies. Your data may be shared with or sold to third parties as described in our Privacy Policy."
	}
	if c.Server.Privacy.Consent.Policy.Text == "" {
		c.Server.Privacy.Consent.Policy.Text = "Privacy Policy"
	}
	if c.Server.Privacy.Consent.Policy.URL == "" {
		c.Server.Privacy.Consent.Policy.URL = "/server/privacy"
	}
	if c.Server.Privacy.Consent.Policy.Version == "" {
		c.Server.Privacy.Consent.Policy.Version = "1"
	}
	if c.Server.Privacy.Consent.Buttons.Decline == "" {
		c.Server.Privacy.Consent.Buttons.Decline = "Decline"
	}
	if c.Server.Privacy.Consent.Buttons.Accept == "" {
		c.Server.Privacy.Consent.Buttons.Accept = "I Agree"
	}
	if c.Server.Privacy.Consent.Position == "" {
		c.Server.Privacy.Consent.Position = "bottom"
	}
	if c.Server.Privacy.Consent.Position != "top" && c.Server.Privacy.Consent.Position != "bottom" {
		c.Server.Privacy.Consent.Position = "bottom"
	}
	if c.Server.Privacy.Consent.PreferencesText == "" {
		c.Server.Privacy.Consent.PreferencesText = "Manage Preferences"
	}
	if c.Server.Privacy.Cookies.Preferences.Description == "" {
		c.Server.Privacy.Cookies.Preferences.Description = "Remember your theme, language, and interface settings. Disabling this resets them to defaults on each visit."
	}
	if c.Server.Privacy.Cookies.Essential.Description == "" {
		c.Server.Privacy.Cookies.Essential.Enabled = true
		c.Server.Privacy.Cookies.Essential.Description = "Required for site operation, including security, consent, and automatic build-version cookies."
	}
	if c.Server.Privacy.Cookies.Analytics.Description == "" {
		c.Server.Privacy.Cookies.Analytics.Description = "Help us understand how visitors use this site."
	}
	if c.Server.Privacy.Cookies.Analytics.DescriptionSuffixNotSold == "" {
		c.Server.Privacy.Cookies.Analytics.DescriptionSuffixNotSold = "Analytics data is anonymized and never sold."
	}
	if c.Server.Privacy.Cookies.Analytics.DescriptionSuffixSold == "" {
		c.Server.Privacy.Cookies.Analytics.DescriptionSuffixSold = "Analytics data may be shared with third parties."
	}
	if c.Server.Privacy.Retention.Period == "" {
		c.Server.Privacy.Retention.Period = "Mail resources remain until the operator deletes them. Preferences are stored only in your browser. Operational logs are retained for up to 30 days. Anonymized analytics may be retained for up to 12 months."
	}
	// Server defaults
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		c.Server.Port = getRandomAvailablePort()
	}
	if c.Server.Address == "" {
		c.Server.Address = "[::]" // IPv6 any (includes IPv4)
	}
	if c.Server.AdminPath == "" {
		c.Server.AdminPath = "admin"
	}
	if c.Server.Mode == "" {
		c.Server.Mode = "production"
	}
	switch strings.ToLower(c.Server.Mode) {
	case "prod":
		c.Server.Mode = "production"
	case "dev", "devel":
		c.Server.Mode = "development"
	case "production", "development", "debug":
	default:
		slog.Warn("invalid server.mode; using default", "value", c.Server.Mode, "default", "production")
		c.Server.Mode = "production"
	}

	adminPath, err := security.SafePath(c.Server.AdminPath)
	if err != nil || adminPath == "" {
		slog.Warn("invalid server.admin_path; using default", "default", "admin")
		c.Server.AdminPath = "admin"
	} else {
		c.Server.AdminPath = adminPath
	}

	// Database defaults
	if c.Database.Driver == "" {
		c.Database.Driver = "sqlite"
	}

	// Normalize database driver
	c.Database.Driver = normalizeDriver(c.Database.Driver)
	c.Server.Database = c.Database

	// Pool defaults
	if c.Database.Pool.MaxOpen <= 0 {
		c.Database.Pool.MaxOpen = 25
	}
	if c.Database.Pool.MaxIdle <= 0 {
		c.Database.Pool.MaxIdle = 5
	}
	if c.Database.Pool.MaxLifetime == "" {
		c.Database.Pool.MaxLifetime = "5m"
	}
	if c.Database.Pool.MaxIdleTime == "" {
		c.Database.Pool.MaxIdleTime = "1m"
	}
	c.Server.Database = c.Database

	// Session defaults
	if c.Server.Session.Admin.CookieName == "" {
		c.Server.Session.Admin.CookieName = "admin_session"
	}
	if c.Server.Session.Admin.MaxAge == "" {
		c.Server.Session.Admin.MaxAge = "30d"
	}
	if c.Server.Session.Admin.IdleTimeout == "" {
		c.Server.Session.Admin.IdleTimeout = "24h"
	}
	if c.Server.Session.User.CookieName == "" {
		c.Server.Session.User.CookieName = "user_session"
	}
	if c.Server.Session.User.MaxAge == "" {
		c.Server.Session.User.MaxAge = "7d"
	}
	if c.Server.Session.User.IdleTimeout == "" {
		c.Server.Session.User.IdleTimeout = "24h"
	}

	// i18n defaults
	if c.Server.I18n.DefaultLanguage == "" {
		c.Server.I18n.DefaultLanguage = "en"
	}
	if !i18n.IsRegistered(c.Server.I18n.DefaultLanguage) {
		slog.Warn("invalid server.i18n.default_language; using default", "value", c.Server.I18n.DefaultLanguage, "default", "en")
		c.Server.I18n.DefaultLanguage = "en"
	}
	if len(c.Server.I18n.AvailableLanguages) == 0 {
		c.Server.I18n.AvailableLanguages = i18n.LanguageCodes()
	}
	for _, lang := range i18n.LanguageCodes() {
		if !containsLanguage(c.Server.I18n.AvailableLanguages, lang) {
			slog.Warn("required language missing from server.i18n.available_languages; restoring default language set", "language", lang)
			c.Server.I18n.AvailableLanguages = i18n.LanguageCodes()
			break
		}
	}
	for _, lang := range c.Server.I18n.AvailableLanguages {
		if !i18n.IsRegistered(lang) {
			slog.Warn("invalid server.i18n.available_languages; restoring default language set", "language", lang)
			c.Server.I18n.AvailableLanguages = i18n.LanguageCodes()
			break
		}
	}

	return nil
}

func validBareKeyserverHost(host string) bool {
	if host == "" || len(host) > 253 || strings.ContainsAny(host, ":/@?#\\") {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

func databaseConfigSet(database DatabaseConfig) bool {
	return database.Driver != "" || database.URL != "" || database.Name != "" || database.Token != "" ||
		database.Pool.MaxOpen != 0 || database.Pool.MaxIdle != 0 || database.Pool.MaxLifetime != "" || database.Pool.MaxIdleTime != ""
}

func setRateLimitDefaults(limit *RateLimitConfig) {
	defaults := RateLimitConfig{
		Read:         RateLimitWindowConfig{Requests: 120, Window: 60},
		Write:        RateLimitWindowConfig{Requests: 10, Window: 60},
		Health:       RateLimitWindowConfig{Requests: 120, Window: 60},
		Strict:       RateLimitWindowConfig{Requests: 10, Window: 900},
		AuthFailures: RateLimitWindowConfig{Requests: 5, Window: 900},
		GlobalBurst:  240,
	}
	if limit.Read.Requests <= 0 {
		limit.Read.Requests = defaults.Read.Requests
	}
	if limit.Read.Window <= 0 {
		limit.Read.Window = defaults.Read.Window
	}
	if limit.Write.Requests <= 0 {
		limit.Write.Requests = defaults.Write.Requests
	}
	if limit.Write.Window <= 0 {
		limit.Write.Window = defaults.Write.Window
	}
	if limit.Health.Requests <= 0 {
		limit.Health.Requests = defaults.Health.Requests
	}
	if limit.Health.Window <= 0 {
		limit.Health.Window = defaults.Health.Window
	}
	if limit.Strict.Requests <= 0 {
		limit.Strict.Requests = defaults.Strict.Requests
	}
	if limit.Strict.Window <= 0 {
		limit.Strict.Window = defaults.Strict.Window
	}
	if limit.AuthFailures.Requests <= 0 {
		limit.AuthFailures.Requests = defaults.AuthFailures.Requests
	}
	if limit.AuthFailures.Window <= 0 {
		limit.AuthFailures.Window = defaults.AuthFailures.Window
	}
	if limit.GlobalBurst <= 0 {
		limit.GlobalBurst = defaults.GlobalBurst
	}
}

func containsLanguage(languages []string, code string) bool {
	for _, language := range languages {
		if language == code {
			return true
		}
	}
	return false
}

// normalizeDriver normalizes database driver names per AI.md PART 3
func normalizeDriver(driver string) string {
	switch strings.ToLower(driver) {
	case "sqlite", "sqlite2", "sqlite3":
		return "sqlite"
	case "libsql", "turso":
		return "libsql"
	default:
		return driver
	}
}

// getRandomAvailablePort returns a random port in 64000-64999 range
func getRandomAvailablePort() int {
	for attempt := 0; attempt < 1000; attempt++ {
		n, err := rand.Int(rand.Reader, big.NewInt(1000))
		if err != nil {
			break
		}
		port := 64000 + int(n.Int64())
		listener, err := net.Listen("tcp", fmt.Sprintf("[::]:%d", port))
		if err == nil {
			_ = listener.Close()
			return port
		}
	}
	return 64000
}

// AvailablePort selects an unused port in the application's configured range.
func AvailablePort() int { return getRandomAvailablePort() }

// Get returns the global config instance
func Get() *Config {
	return globalConfig
}

// ParseDuration parses duration string (e.g., "30s", "5m", "24h")
func ParseDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}

// GetConfigPath returns the current config file path
func GetConfigPath() string {
	return configPath
}

// DSN generates a database connection string based on driver type
func (d *DatabaseConfig) DSN() string {
	switch d.Driver {
	case "sqlite":
		if d.URL != "" {
			return d.URL
		}
		if d.Name != "" {
			return d.Name
		}
		return filepath.Join("/var/lib/mail", "server.db")

	case "libsql":
		// libSQL requires URL
		if d.Token != "" && !strings.Contains(d.URL, "authToken=") {
			sep := "?"
			if strings.Contains(d.URL, "?") {
				sep = "&"
			}
			return d.URL + sep + "authToken=" + d.Token
		}
		return d.URL

	default:
		return ""
	}
}
