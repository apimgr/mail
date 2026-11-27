package email

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestBuildMessageStandardsAndEncodedHeaders(t *testing.T) {
	message, envelopeFrom, err := BuildMessage(Message{
		FromName: "Équipe Mail", From: "no-reply@mail.local", To: "ops@ops.local",
		Subject: "Alerte – café", Text: "Une notification.", HTML: "<p>Une notification.</p>",
		FQDN: "mail.local", Date: time.Date(2026, 11, 26, 12, 30, 0, 0, time.FixedZone("EST", -5*60*60)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if envelopeFrom != "no-reply@mail.local" {
		t.Fatalf("envelope sender = %q", envelopeFrom)
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(message))
	if err != nil {
		t.Fatal(err)
	}
	decoder := new(mime.WordDecoder)
	from, err := mail.ParseAddress(parsed.Header.Get("From"))
	if err != nil {
		t.Fatal(err)
	}
	fromName, err := decoder.DecodeHeader(from.Name)
	if err != nil || fromName != "Équipe Mail" {
		t.Fatalf("decoded From name = %q, err=%v", fromName, err)
	}
	subject, err := decoder.DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || subject != "Alerte – café" {
		t.Fatalf("decoded subject = %q, err=%v", subject, err)
	}
	if parsed.Header.Get("To") != "ops@ops.local" || parsed.Header.Get("Bcc") != "" || parsed.Header.Get("Auto-Submitted") != "auto-generated" || parsed.Header.Get("MIME-Version") != "1.0" {
		t.Fatalf("required message headers missing: %#v", parsed.Header)
	}
	if _, err := mail.ParseAddress(parsed.Header.Get("Message-ID")); err != nil {
		t.Fatalf("invalid Message-ID: %v", err)
	}
	if _, err := time.Parse(time.RFC1123Z, parsed.Header.Get("Date")); err != nil {
		t.Fatalf("invalid Date: %v", err)
	}
	contentType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil || contentType != "multipart/alternative" {
		t.Fatalf("Content-Type = %q, err=%v", contentType, err)
	}
	parts := multipart.NewReader(parsed.Body, params["boundary"])
	plain, err := parts.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	plainBody, err := io.ReadAll(quotedprintable.NewReader(plain))
	if err != nil || strings.TrimSuffix(string(plainBody), "\r\n") != "Une notification." {
		t.Fatalf("plain part = %q, err=%v", plainBody, err)
	}
	htmlPart, err := parts.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	htmlBody, err := io.ReadAll(quotedprintable.NewReader(htmlPart))
	if err != nil || strings.TrimSuffix(string(htmlBody), "\r\n") != "<p>Une notification.</p>" {
		t.Fatalf("HTML part = %q, err=%v", htmlBody, err)
	}
	for _, line := range bytes.Split(message, []byte("\r\n")) {
		if len(line) > 998 {
			t.Fatalf("message has a %d-octet line", len(line))
		}
	}
}

func TestBuildMessageRejectsHeaderInjectionAndEmptyText(t *testing.T) {
	base := Message{FromName: "Mail", From: "no-reply@mail.local", To: "ops@ops.local", Subject: "Alert", Text: "Body", FQDN: "mail.local"}
	for _, mutate := range []func(*Message){
		func(m *Message) { m.To = "ops@ops.local\r\nBcc: victim@ops.local" },
		func(m *Message) { m.Subject = "Alert\nBcc: victim@ops.local" },
		func(m *Message) { m.FromName = "Mail\r\nBcc: victim@ops.local" },
		func(m *Message) { m.Text = "" },
	} {
		message := base
		mutate(&message)
		if _, _, err := BuildMessage(message); err == nil {
			t.Fatalf("BuildMessage accepted unsafe message: %+v", message)
		}
	}
}

func TestPrivateSMTPIPUsesConfiguredLiteralOnly(t *testing.T) {
	for _, host := range []string{"10.2.3.4", "172.31.1.2", "192.168.1.1", "100.64.0.1", "127.0.0.1", "169.254.1.2", "::1", "fc00::1", "fe80::1"} {
		if !IsPrivateSMTPIP(host) {
			t.Errorf("IsPrivateSMTPIP(%q) = false", host)
		}
	}
	for _, host := range []string{"8.8.8.8", "2606:4700:4700::1111", "smtp.mail.local", "smtp.internal"} {
		if IsPrivateSMTPIP(host) {
			t.Errorf("IsPrivateSMTPIP(%q) = true", host)
		}
	}
	if !smtpTLSConfig("172.17.0.1").InsecureSkipVerify {
		t.Fatal("private SMTP IP did not receive the configured verification exception")
	}
	for _, host := range []string{"8.8.8.8", "smtp.internal"} {
		if smtpTLSConfig(host).InsecureSkipVerify {
			t.Errorf("SMTP host %q unexpectedly skipped certificate verification", host)
		}
	}
}

func TestSMTPHostCandidatePriority(t *testing.T) {
	hosts := smtpHosts(SMTPFacts{GatewayIP: "192.168.50.1", FQDN: "mail.local", GlobalIPv4: "8.8.4.4"})
	want := []string{"127.0.0.1", "172.17.0.1", "192.168.50.1", "mail.local", "8.8.4.4", "mail.mail.local", "smtp.mail.local"}
	if strings.Join(hosts, ",") != strings.Join(want, ",") {
		t.Fatalf("SMTP hosts = %#v, want %#v", hosts, want)
	}
}

func TestSMTPHostCandidatesRejectInjectedOrMalformedNames(t *testing.T) {
	hosts := smtpHosts(SMTPFacts{FQDN: "mail.example.test\r\nAUTH PLAIN attacker"})
	for _, host := range hosts {
		if strings.ContainsAny(host, "\r\n") {
			t.Fatalf("unsafe SMTP host candidate %q", host)
		}
	}
	for _, host := range []string{"mail.example.test", "127.0.0.1", "2001:db8::1"} {
		if !validSMTPHost(host) {
			t.Errorf("valid SMTP host rejected: %q", host)
		}
	}
	for _, host := range []string{"", "mail\r\nexample.test", "bad host"} {
		if validSMTPHost(host) {
			t.Errorf("invalid SMTP host accepted: %q", host)
		}
	}
}

func TestSMTPConfigInvalidValuesUseDefaults(t *testing.T) {
	warnings := make([]string, 0)
	warn := func(key, _ string) { warnings = append(warnings, key) }
	cfg := NormalizeConfig(Config{Port: 70000, TLSMode: "unknown", Username: "relay"}, warn)
	if cfg.Port != 587 || cfg.TLSMode != "auto" || cfg.Username != "" || cfg.Password != "" {
		t.Fatalf("normalized SMTP config = %+v", cfg)
	}
	if len(warnings) != 3 {
		t.Fatalf("warnings = %#v, want port, TLS and incomplete-auth warnings", warnings)
	}
	environment := map[string]string{"SMTP_PORT": "not-a-port", "SMTP_TLS": "bogus"}
	fromEnv := ApplyEnvironment(Config{Port: 465, TLSMode: "tls"}, func(key string) (string, bool) {
		value, ok := environment[key]
		return value, ok
	}, nil)
	if fromEnv.Port != 587 || fromEnv.TLSMode != "auto" {
		t.Fatalf("environment defaults = %+v", fromEnv)
	}
}

func TestSMTPAuthenticationRequiresEstablishedTLS(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close SMTP listener: %v", err)
		}
	})
	serverErr := make(chan error, 1)
	go serveSMTPHandshake(listener, false, false, serverErr)
	cfg := Config{Host: "127.0.0.1", Port: 587, Username: "relay-user", Password: "test-value", TLSMode: "auto", FQDN: "mail.local", Timeout: 5 * time.Second}
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
	}
	if _, _, err := openSMTPWithDial(context.Background(), cfg, dial); err != ErrTLSRequired {
		t.Fatalf("openSMTP error = %v, want %v", err, ErrTLSRequired)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticTLSModeByPortAndSTARTTLS(t *testing.T) {
	cases := []struct {
		name        string
		port        int
		advertise   bool
		implicitTLS bool
		wantSecure  bool
	}{
		{name: "port 25 plain without STARTTLS", port: 25},
		{name: "port 25 plain then STARTTLS", port: 25, advertise: true, wantSecure: true},
		{name: "port 587 plain without STARTTLS", port: 587},
		{name: "port 587 plain then STARTTLS", port: 587, advertise: true, wantSecure: true},
		{name: "port 465 implicit TLS", port: 465, implicitTLS: true, wantSecure: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := listener.Close(); err != nil {
					t.Errorf("close SMTP listener: %v", err)
				}
			})
			serverErr := make(chan error, 1)
			go serveSMTPHandshake(listener, test.advertise, test.implicitTLS, serverErr)
			cfg := Config{Host: "127.0.0.1", Port: test.port, TLSMode: "auto", FQDN: "mail.local", Timeout: 5 * time.Second}
			dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
			}
			client, secure, err := openSMTPWithDial(context.Background(), cfg, dial)
			if err != nil {
				t.Fatal(err)
			}
			if secure != test.wantSecure {
				t.Fatalf("secure = %v, want %v", secure, test.wantSecure)
			}
			_ = client.Close()
			if err := <-serverErr; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRequiredSTARTTLSDoesNotFallBack(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close SMTP listener: %v", err)
		}
	})
	serverErr := make(chan error, 1)
	go serveSMTPHandshake(listener, false, false, serverErr)
	cfg := Config{Host: "127.0.0.1", Port: 587, TLSMode: "starttls", FQDN: "mail.local", Timeout: 5 * time.Second}
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
	}
	if _, _, err := openSMTPWithDial(context.Background(), cfg, dial); err == nil {
		t.Fatal("starttls mode accepted a server without STARTTLS")
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func serveSMTPHandshake(listener net.Listener, advertiseSTARTTLS, implicitTLS bool, result chan<- error) {
	conn, err := listener.Accept()
	if err != nil {
		result <- err
		return
	}
	defer func() { _ = conn.Close() }()
	if implicitTLS {
		conn = tls.Server(conn, testTLSConfig())
		if err := conn.(*tls.Conn).Handshake(); err != nil {
			result <- err
			return
		}
	}
	if _, err := io.WriteString(conn, "220 smtp.test ESMTP ready\r\n"); err != nil {
		result <- err
		return
	}
	reader := bufio.NewReader(conn)
	if err := readEHLO(reader); err != nil {
		result <- err
		return
	}
	if advertiseSTARTTLS {
		if _, err := io.WriteString(conn, "250-smtp.test\r\n250-STARTTLS\r\n250 PIPELINING\r\n"); err != nil {
			result <- err
			return
		}
		if _, err := reader.ReadString('\n'); err != nil {
			result <- err
			return
		}
		if _, err := io.WriteString(conn, "220 begin TLS\r\n"); err != nil {
			result <- err
			return
		}
		tlsConn := tls.Server(conn, testTLSConfig())
		if err := tlsConn.Handshake(); err != nil {
			result <- err
			return
		}
		conn = tlsConn
		reader = bufio.NewReader(conn)
		if err := readEHLO(reader); err != nil {
			result <- err
			return
		}
	}
	if _, err := io.WriteString(conn, "250 smtp.test\r\n"); err != nil {
		result <- err
		return
	}
	result <- nil
}

func readEHLO(reader *bufio.Reader) error {
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(strings.ToUpper(line), "EHLO ") {
		return &smtpTestProtocolError{line: line}
	}
	return nil
}

type smtpTestProtocolError struct{ line string }

func (e *smtpTestProtocolError) Error() string {
	return "expected EHLO, received " + strings.TrimSpace(e.line)
}

func testTLSConfig() *tls.Config {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "smtp.test"},
		NotBefore:    now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
}
