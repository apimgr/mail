package email

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotConfigured   = errors.New("email requires SMTP configuration")
	ErrSMTPUnavailable = errors.New("email features require SMTP configuration")
	ErrTLSRequired     = errors.New("SMTP authentication requires an established TLS connection")
	ErrInvalidMessage  = errors.New("invalid email message")
	ErrNoSMTPDetected  = errors.New("no working SMTP server detected")
)

// Config contains the SMTP, sender and template settings needed by the mailer.
type Config struct {
	Host      string
	Port      int
	Username  string
	Password  string
	TLSMode   string
	FromName  string
	FromEmail string
	ReplyTo   string
	AppName   string
	AppURL    string
	FQDN      string
	ConfigDir string
	Timeout   time.Duration
}

// SMTPStatus reports readiness without exposing credentials or connection details.
type SMTPStatus struct {
	Configured bool
	Enabled    bool
	Error      string
}

// SMTPFacts supplies host candidates discovered by the platform layer.
type SMTPFacts struct {
	GatewayIP  string
	FQDN       string
	GlobalIPv4 string
}

// WarningFunc receives a configuration key and a safe, non-secret warning.
type WarningFunc func(key, message string)

// InfoFunc receives a non-secret operational information message.
type InfoFunc func(message string)

// Sender is the SMTP operation needed by the mailer.
type Sender interface {
	Send(context.Context, Config, string, []byte) error
}
