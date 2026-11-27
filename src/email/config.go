package email

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var warnedConfigKeys sync.Map

// NormalizeConfig applies the non-fatal defaults required for SMTP settings.
func NormalizeConfig(cfg Config, warn WarningFunc) Config {
	cfg.Host = strings.TrimSpace(cfg.Host)
	if cfg.Port < 1 || cfg.Port > 65535 {
		warnOnce(warn, "server.notifications.email.smtp.port", "invalid value; using 587")
		cfg.Port = 587
	}
	cfg.TLSMode = strings.ToLower(strings.TrimSpace(cfg.TLSMode))
	switch cfg.TLSMode {
	case "", "auto":
		cfg.TLSMode = "auto"
	case "starttls", "tls", "none":
	default:
		warnOnce(warn, "server.notifications.email.smtp.tls", "invalid value; using auto")
		cfg.TLSMode = "auto"
	}
	if (cfg.Username == "") != (cfg.Password == "") {
		warnOnce(warn, "server.notifications.email.smtp.username", "username and password must both be set; disabling SMTP authentication")
		cfg.Username = ""
		cfg.Password = ""
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	cfg.FromName = strings.TrimSpace(cfg.FromName)
	cfg.FromEmail = strings.TrimSpace(cfg.FromEmail)
	cfg.ReplyTo = strings.TrimSpace(cfg.ReplyTo)
	cfg.AppName = strings.TrimSpace(cfg.AppName)
	cfg.AppURL = strings.TrimSpace(cfg.AppURL)
	cfg.FQDN = strings.TrimSpace(cfg.FQDN)
	cfg.ConfigDir = strings.TrimSpace(cfg.ConfigDir)
	return cfg
}

// ApplyEnvironment applies SMTP_* variables over file settings.
func ApplyEnvironment(cfg Config, lookup func(string) (string, bool), warn WarningFunc) Config {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	if value, ok := lookup("SMTP_HOST"); ok {
		cfg.Host = value
	}
	if value, ok := lookup("SMTP_PORT"); ok {
		port, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || port < 1 || port > 65535 {
			warnOnce(warn, "server.notifications.email.smtp.port", "invalid value; using 587")
			cfg.Port = 587
		} else {
			cfg.Port = port
		}
	}
	if value, ok := lookup("SMTP_USERNAME"); ok {
		cfg.Username = value
	}
	if value, ok := lookup("SMTP_PASSWORD"); ok {
		cfg.Password = value
	}
	if value, ok := lookup("SMTP_TLS"); ok {
		cfg.TLSMode = value
	}
	if value, ok := lookup("SMTP_FROM_NAME"); ok {
		cfg.FromName = value
	}
	if value, ok := lookup("SMTP_FROM_EMAIL"); ok {
		cfg.FromEmail = value
	}
	return NormalizeConfig(cfg, warn)
}

func warnOnce(warn WarningFunc, key, message string) {
	if warn != nil {
		if _, loaded := warnedConfigKeys.LoadOrStore(key, struct{}{}); loaded {
			return
		}
		warn(key, message)
	}
}
