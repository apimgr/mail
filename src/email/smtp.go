package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Mailer checks SMTP readiness and sends notifications without queuing them.
type Mailer struct {
	mu            sync.RWMutex
	cfg           Config
	enabled       bool
	lastError     string
	warned        bool
	warn          WarningFunc
	info          InfoFunc
	privateLogged bool
	templates     *Templates
}

// NewMailer creates a disabled mailer until Check verifies the SMTP relay.
func NewMailer(cfg Config, warn WarningFunc, info ...InfoFunc) *Mailer {
	cfg = NormalizeConfig(cfg, warn)
	mailer := &Mailer{cfg: cfg, warn: warn, templates: NewTemplates(cfg.ConfigDir, warn)}
	if len(info) > 0 {
		mailer.info = info[0]
	}
	return mailer
}

// Status reports whether email is configured and currently usable.
func (m *Mailer) Status() SMTPStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	configured := m.cfg.Host != "" && m.cfg.FQDN != ""
	return SMTPStatus{Configured: configured, Enabled: configured && m.enabled, Error: m.lastError}
}

// Config returns a copy of the current non-secret configuration.
func (m *Mailer) Config() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// Reload replaces settings and performs the startup/config-change handshake.
func (m *Mailer) Reload(ctx context.Context, cfg Config) error {
	cfg = NormalizeConfig(cfg, m.warn)
	m.mu.Lock()
	m.cfg = cfg
	m.enabled = false
	m.lastError = ""
	m.warned = false
	m.privateLogged = false
	m.templates = NewTemplates(cfg.ConfigDir, m.warn)
	m.mu.Unlock()
	return m.Check(ctx)
}

// Check performs the same SMTP handshake and TLS policy used before each send.
func (m *Mailer) Check(ctx context.Context) error {
	m.mu.RLock()
	cfg := m.cfg
	m.mu.RUnlock()
	if cfg.Host == "" {
		m.setDisabled(ErrNotConfigured)
		return ErrNotConfigured
	}
	client, secure, err := openSMTP(ctx, cfg)
	if err == nil && cfg.Username != "" && !secure {
		err = ErrTLSRequired
	}
	if client != nil {
		_ = client.Quit()
	}
	if err == nil {
		err = validateSenderConfig(cfg)
	}
	if err != nil {
		m.setDisabled(err)
		return err
	}
	if secure && privateSMTPIP(cfg.Host) {
		m.mu.Lock()
		info := m.info
		shouldLog := !m.privateLogged
		m.privateLogged = true
		m.mu.Unlock()
		if shouldLog && info != nil {
			info("SMTP host " + cfg.Host + " is a private address: TLS certificate verification skipped")
		}
	}
	m.mu.Lock()
	m.enabled = true
	m.lastError = ""
	m.warned = false
	m.mu.Unlock()
	return nil
}

func validateSenderConfig(cfg Config) error {
	if !validMessageDomain(cfg.FQDN) {
		return errors.New("email requires a configured FQDN")
	}
	fromEmail := cfg.FromEmail
	if fromEmail == "" {
		fromEmail = "no-reply@" + strings.TrimSuffix(cfg.FQDN, ".")
	}
	if _, err := ParseAddress(fromEmail); err != nil {
		return errors.New("configured sender email address is invalid")
	}
	if containsHeaderBreak(cfg.FromName) || containsHeaderBreak(cfg.ReplyTo) {
		return errors.New("configured sender headers are invalid")
	}
	if strings.TrimSpace(cfg.FromName) == "" && strings.TrimSpace(cfg.AppName) == "" {
		return errors.New("email requires an application sender name")
	}
	if cfg.ReplyTo != "" {
		if _, err := ParseAddress(cfg.ReplyTo); err != nil {
			return errors.New("configured Reply-To address is invalid")
		}
	}
	return nil
}

// DetectSMTP tries local relay candidates in the specified priority order.
func (m *Mailer) DetectSMTP(ctx context.Context, facts SMTPFacts) (Config, error) {
	m.mu.RLock()
	base := m.cfg
	m.mu.RUnlock()
	if base.FQDN == "" {
		base.FQDN = facts.FQDN
		m.mu.Lock()
		m.cfg.FQDN = base.FQDN
		m.mu.Unlock()
	}
	if base.Host != "" {
		if err := m.Check(ctx); err != nil {
			return base, err
		}
		return base, nil
	}
	hosts := smtpHosts(facts)
	for _, host := range hosts {
		for _, port := range []int{25, 465, 587} {
			candidate := base
			candidate.Host = host
			candidate.Port = port
			candidate.TLSMode = "auto"
			client, secure, err := openSMTP(ctx, candidate)
			if client != nil {
				_ = client.Quit()
			}
			if err == nil && (candidate.Username == "" || secure) {
				candidate = NormalizeConfig(candidate, m.warn)
				senderErr := validateSenderConfig(candidate)
				m.mu.Lock()
				m.cfg = candidate
				m.enabled = senderErr == nil
				m.lastError = safeSMTPError(senderErr)
				m.warned = false
				info := m.info
				logPrivateTLS := secure && privateSMTPIP(candidate.Host)
				m.privateLogged = logPrivateTLS
				m.templates = NewTemplates(candidate.ConfigDir, m.warn)
				m.mu.Unlock()
				if logPrivateTLS && info != nil {
					info("SMTP host " + candidate.Host + " is a private address: TLS certificate verification skipped")
				}
				if senderErr != nil {
					m.setDisabled(senderErr)
				}
				return candidate, nil
			}
		}
	}
	m.setDisabled(ErrNoSMTPDetected)
	return base, ErrNoSMTPDetected
}

// Send renders one template and sends one message to one recipient.
func (m *Mailer) Send(ctx context.Context, templateName, recipient string, values map[string]string) error {
	m.mu.RLock()
	cfg := m.cfg
	enabled := m.enabled
	templates := m.templates
	m.mu.RUnlock()
	if !enabled || cfg.Host == "" {
		return ErrSMTPUnavailable
	}
	if !validMessageDomain(cfg.FQDN) {
		m.setDisabled(errors.New("email requires a configured FQDN"))
		return ErrSMTPUnavailable
	}
	values = withGlobalValues(cfg, values, time.Now())
	rendered, err := templates.Render(templateName, values)
	if err != nil {
		return err
	}
	fromName := cfg.FromName
	if fromName == "" {
		fromName = cfg.AppName
	}
	fromEmail := cfg.FromEmail
	if fromEmail == "" {
		fromEmail = "no-reply@" + strings.TrimSuffix(cfg.FQDN, ".")
	}
	if fromName == "" || cfg.FQDN == "" {
		m.setDisabled(errors.New("email sender identity is incomplete"))
		return ErrSMTPUnavailable
	}
	if templateName == "test" {
		rendered.Subject = "[TEST] " + rendered.Subject
	}
	message, envelopeFrom, err := BuildMessage(Message{
		FromName: fromName, From: fromEmail, To: recipient, ReplyTo: cfg.ReplyTo,
		Subject: rendered.Subject, Text: rendered.Text, HTML: rendered.HTML,
		FQDN: cfg.FQDN, Date: time.Now(),
	})
	if err != nil {
		return err
	}
	parsedRecipient, err := ParseAddress(recipient)
	if err != nil {
		return err
	}
	if err := sendSMTP(ctx, cfg, envelopeFrom, parsedRecipient, message); err != nil {
		m.setDisabled(err)
		return err
	}
	return nil
}

func withGlobalValues(cfg Config, values map[string]string, now time.Time) map[string]string {
	result := make(map[string]string, len(values)+len(globalVariables))
	for key, value := range values {
		result[key] = value
	}
	defaults := map[string]string{
		"app_name":              cfg.AppName,
		"app_url":               cfg.AppURL,
		"fqdn":                  cfg.FQDN,
		"notification_reply_to": cfg.ReplyTo,
		"timestamp":             now.UTC().Format(time.RFC3339),
		"year":                  strconv.Itoa(now.UTC().Year()),
		"onion_url":             "",
		"onion_address":         "",
		"i2p_url":               "",
		"i2p_address":           "",
	}
	for key, value := range defaults {
		if _, ok := result[key]; !ok {
			result[key] = value
		}
	}
	return result
}

func (m *Mailer) setDisabled(err error) {
	m.mu.Lock()
	m.enabled = false
	m.lastError = safeSMTPError(err, m.cfg.Password)
	warn := m.warn
	shouldWarn := !m.warned && err != nil && !errors.Is(err, ErrNotConfigured)
	if shouldWarn {
		m.warned = true
	}
	m.mu.Unlock()
	if shouldWarn && warn != nil {
		warn("server.notifications.email.smtp", "SMTP is unavailable or email configuration is invalid; email features are disabled")
	}
}

func safeSMTPError(err error, secrets ...string) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return message
}

func smtpHosts(facts SMTPFacts) []string {
	ordered := []string{"127.0.0.1", "172.17.0.1", facts.GatewayIP, facts.FQDN, facts.GlobalIPv4}
	if facts.FQDN != "" {
		ordered = append(ordered, "mail."+facts.FQDN, "smtp."+facts.FQDN)
	}
	seen := make(map[string]struct{}, len(ordered))
	hosts := make([]string, 0, len(ordered))
	for _, host := range ordered {
		host = strings.Trim(strings.TrimSpace(strings.TrimSuffix(host, ".")), "[]")
		if !validSMTPHost(host) {
			continue
		}
		key := strings.ToLower(host)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		hosts = append(hosts, host)
	}
	return hosts
}

func validSMTPHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" || strings.ContainsAny(host, "\r\n") {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	return validMessageDomain(host)
}

func openSMTP(ctx context.Context, cfg Config) (*smtp.Client, bool, error) {
	dialer := net.Dialer{Timeout: cfg.Timeout}
	return openSMTPWithDial(ctx, cfg, dialer.DialContext)
}

type smtpDialFunc func(context.Context, string, string) (net.Conn, error)

func openSMTPWithDial(ctx context.Context, cfg Config, dial smtpDialFunc) (*smtp.Client, bool, error) {
	if !validSMTPHost(cfg.Host) || cfg.Port < 1 || cfg.Port > 65535 {
		return nil, false, ErrNotConfigured
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.TLSMode))
	if mode == "" {
		mode = "auto"
	}
	implicitTLS := mode == "tls" || (mode == "auto" && cfg.Port == 465)
	address := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	conn, err := dial(ctx, "tcp", address)
	if err != nil {
		return nil, false, fmt.Errorf("connect to SMTP relay: %w", err)
	}
	deadline := time.Now().Add(cfg.Timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = conn.SetDeadline(deadline)
	secure := false
	if implicitTLS {
		tlsConn := tls.Client(conn, smtpTLSConfig(cfg.Host))
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, false, fmt.Errorf("SMTP TLS handshake failed: %w", err)
		}
		conn = tlsConn
		secure = true
	}
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		_ = conn.Close()
		return nil, false, fmt.Errorf("SMTP greeting failed: %w", err)
	}
	localName := cfg.FQDN
	if !validMessageDomain(localName) {
		localName = "localhost"
	}
	if err := client.Hello(localName); err != nil {
		_ = client.Close()
		return nil, secure, fmt.Errorf("SMTP EHLO failed: %w", err)
	}
	if !implicitTLS && mode != "none" {
		available, _ := client.Extension("STARTTLS")
		if available {
			if err := client.StartTLS(smtpTLSConfig(cfg.Host)); err != nil {
				_ = client.Close()
				return nil, secure, fmt.Errorf("SMTP STARTTLS failed: %w", err)
			}
			secure = true
		} else if mode == "starttls" {
			_ = client.Close()
			return nil, false, errors.New("SMTP relay does not offer STARTTLS")
		}
	}
	if cfg.Username != "" && !secure {
		_ = client.Close()
		return nil, false, ErrTLSRequired
	}
	return client, secure, nil
}

func smtpTLSConfig(configuredHost string) *tls.Config {
	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: configuredHost}
	if privateSMTPIP(configuredHost) {
		// The policy deliberately relaxes certificate checks only for configured private IP literals.
		config.InsecureSkipVerify = true
	}
	return config
}

func privateSMTPIP(host string) bool {
	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return false
	}
	if address.Is4In6() {
		address = address.Unmap()
	}
	if address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return true
	}
	if address.Is4() {
		return netip.MustParsePrefix("100.64.0.0/10").Contains(address)
	}
	return false
}

func sendSMTP(ctx context.Context, cfg Config, from, recipient string, message []byte) error {
	client, secure, err := openSMTP(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	if cfg.Username != "" {
		if !secure {
			return ErrTLSRequired
		}
		auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
		if err := client.Auth(auth); err != nil {
			return errors.New("SMTP authentication failed")
		}
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("SMTP MAIL FROM failed: %w", err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("SMTP recipient rejected: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA command failed: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish SMTP message: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("SMTP QUIT failed: %w", err)
	}
	return nil
}

// IsPrivateSMTPIP reports whether the configured literal gets relaxed verification.
func IsPrivateSMTPIP(host string) bool {
	return privateSMTPIP(host)
}

// ParseAddress verifies that a value contains one SMTP mailbox.
func ParseAddress(value string) (string, error) {
	address, err := parseMailbox(value, true)
	if err != nil {
		return "", err
	}
	return address.Address, nil
}
