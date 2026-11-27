package email

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

// Message is one individually addressed notification email.
type Message struct {
	FromName string
	From     string
	To       string
	ReplyTo  string
	Subject  string
	Text     string
	HTML     string
	FQDN     string
	Date     time.Time
}

// BuildMessage serializes a standards-compliant, single-recipient MIME message.
func BuildMessage(message Message) ([]byte, string, error) {
	if containsHeaderBreak(message.FromName) || containsHeaderBreak(message.From) || containsHeaderBreak(message.To) || containsHeaderBreak(message.ReplyTo) || containsHeaderBreak(message.Subject) {
		return nil, "", ErrInvalidMessage
	}
	if strings.TrimSpace(message.Subject) == "" || strings.TrimSpace(message.Text) == "" {
		return nil, "", ErrInvalidMessage
	}
	from, err := parseMailbox(message.From, true)
	if err != nil {
		return nil, "", fmt.Errorf("invalid From address: %w", err)
	}
	to, err := parseMailbox(message.To, true)
	if err != nil {
		return nil, "", fmt.Errorf("invalid To address: %w", err)
	}
	if message.ReplyTo != "" {
		if _, err := parseMailbox(message.ReplyTo, true); err != nil {
			return nil, "", fmt.Errorf("invalid Reply-To address: %w", err)
		}
	}
	if !validMessageDomain(message.FQDN) {
		return nil, "", errors.New("email requires a valid server FQDN")
	}
	if message.Date.IsZero() {
		message.Date = time.Now()
	}
	if message.HTML == "" {
		message.HTML = "<pre>" + htmlEscape(message.Text) + "</pre>"
	}
	boundary, err := randomHex(18)
	if err != nil {
		return nil, "", fmt.Errorf("generate MIME boundary: %w", err)
	}
	messageID, err := randomHex(16)
	if err != nil {
		return nil, "", fmt.Errorf("generate Message-ID: %w", err)
	}
	fromName := message.FromName
	if fromName == "" {
		fromName = from.Name
	}
	fromValue := encodeWords(fromName) + " <" + from.Address + ">"
	if fromName == "" {
		fromValue = from.Address
	}
	subject := encodeHeaderValue(message.Subject)
	var output bytes.Buffer
	headers := []struct{ name, value string }{
		{"From", fromValue},
		{"To", to.Address},
		{"Subject", subject},
		{"Date", message.Date.Format("Mon, 02 Jan 2006 15:04:05 -0700")},
		{"Message-ID", "<" + messageID + "@" + strings.TrimSuffix(message.FQDN, ".") + ">"},
		{"MIME-Version", "1.0"},
		{"Auto-Submitted", "auto-generated"},
		{"Content-Type", "multipart/alternative; boundary=\"" + boundary + "\""},
	}
	if message.ReplyTo != "" {
		replyTo, _ := parseMailbox(message.ReplyTo, true)
		headers = append(headers, struct{ name, value string }{"Reply-To", replyTo.String()})
	}
	for _, header := range headers {
		if err := writeHeader(&output, header.name, header.value); err != nil {
			return nil, "", err
		}
	}
	output.WriteString("\r\n")
	if err := writeMIMEPart(&output, boundary, "text/plain", message.Text); err != nil {
		return nil, "", err
	}
	if err := writeMIMEPart(&output, boundary, "text/html", message.HTML); err != nil {
		return nil, "", err
	}
	output.WriteString("--" + boundary + "--\r\n")
	if err := checkLineLengths(output.Bytes()); err != nil {
		return nil, "", err
	}
	return output.Bytes(), from.Address, nil
}

func parseMailbox(value string, requireAddrSpec bool) (*mail.Address, error) {
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil || address.Address == "" {
		return nil, errors.New("invalid mailbox")
	}
	if requireAddrSpec && address.Address != strings.TrimSpace(value) {
		return nil, errors.New("recipient must be one addr-spec")
	}
	if strings.ContainsAny(address.Address, "\r\n") {
		return nil, ErrInvalidMessage
	}
	return address, nil
}

func validMessageDomain(domain string) bool {
	domain = strings.TrimSuffix(strings.TrimSpace(domain), ".")
	if domain == "" || !strings.Contains(domain, ".") || strings.EqualFold(domain, "localhost") || strings.ContainsAny(domain, "\r\n @") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return len(domain) <= 253
}

func encodeHeaderValue(value string) string {
	if isASCII(value) {
		return value
	}
	return encodeWords(value)
}

func encodeWords(value string) string {
	words := make([]string, 0)
	var chunk strings.Builder
	for _, r := range value {
		candidate := chunk.String() + string(r)
		encoded := mime.QEncoding.Encode("UTF-8", candidate)
		if chunk.Len() > 0 && len(encoded) > 75 {
			words = append(words, mime.QEncoding.Encode("UTF-8", chunk.String()))
			chunk.Reset()
			candidate = string(r)
			encoded = mime.QEncoding.Encode("UTF-8", candidate)
		}
		if len(encoded) > 75 {
			continue
		}
		chunk.WriteRune(r)
	}
	if chunk.Len() > 0 {
		words = append(words, mime.QEncoding.Encode("UTF-8", chunk.String()))
	}
	return strings.Join(words, " ")
}

func writeHeader(output *bytes.Buffer, name, value string) error {
	if containsHeaderBreak(name) || containsHeaderBreak(value) {
		return ErrInvalidMessage
	}
	prefix := name + ": "
	line := prefix
	for _, word := range strings.Fields(value) {
		if len(line)+len(word) > 78 && line != prefix {
			output.WriteString(strings.TrimRight(line, " ") + "\r\n")
			line = " " + word
		} else {
			if line != prefix {
				line += " "
			}
			line += word
		}
	}
	if len(line) > 998 {
		return ErrInvalidMessage
	}
	output.WriteString(strings.TrimRight(line, " ") + "\r\n")
	return nil
}

func writeMIMEPart(output *bytes.Buffer, boundary, contentType, body string) error {
	output.WriteString("--" + boundary + "\r\n")
	output.WriteString("Content-Type: " + contentType + "; charset=UTF-8\r\n")
	output.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	writer := quotedprintable.NewWriter(output)
	_, err := writer.Write([]byte(normalizeCRLF(body)))
	if err != nil {
		return fmt.Errorf("encode %s body: %w", contentType, err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish %s body: %w", contentType, err)
	}
	if !bytes.HasSuffix(output.Bytes(), []byte("\r\n")) {
		output.WriteString("\r\n")
	}
	return nil
}

func normalizeCRLF(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}

func containsHeaderBreak(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}

func randomHex(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func isASCII(value string) bool {
	for _, r := range value {
		if r > 127 {
			return false
		}
	}
	return true
}

func htmlEscape(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&#34;", "'", "&#39;").Replace(value)
}

func checkLineLengths(message []byte) error {
	for _, line := range bytes.Split(message, []byte("\r\n")) {
		if len(line) > 998 {
			return fmt.Errorf("email header or body line exceeds 998 octets")
		}
		if !utf8.Valid(line) {
			return ErrInvalidMessage
		}
	}
	return nil
}
