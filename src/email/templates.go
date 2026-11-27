package email

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	emailtemplates "github.com/apimgr/mail/src/server/template/email"
)

var variablePattern = regexp.MustCompile(`\{([a-z][a-z0-9_]*)\}`)

var templateDefaults = loadEmbeddedTemplates()

func loadEmbeddedTemplates() map[string]string {
	entries, err := emailtemplates.FS.ReadDir(".")
	if err != nil {
		panic("read embedded email templates: " + err.Error())
	}
	templates := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".tmpl" {
			continue
		}
		data, err := emailtemplates.FS.ReadFile(entry.Name())
		if err != nil {
			panic("read embedded email template: " + err.Error())
		}
		name := strings.TrimSuffix(entry.Name(), ".tmpl")
		templates[name] = string(data)
	}
	return templates
}

var globalVariables = map[string]struct{}{
	"app_name": {}, "app_url": {}, "fqdn": {}, "onion_url": {}, "onion_address": {},
	"i2p_url": {}, "i2p_address": {}, "notification_reply_to": {}, "timestamp": {}, "year": {},
}

var templateVariables = map[string][]string{
	"security_alert":           {"event", "ip", "details"},
	"security_report_received": {"tracking_id", "status_url"},
	"backup_complete":          {"filename", "size"}, "backup_failed": {"filename", "error"},
	"ssl_expiring": {"expires_in", "expiry_date"}, "ssl_renewed": {"valid_until"},
	"ssl_renewal_failed": {"error", "expires_in", "expiry_date", "next_retry"},
	"scheduler_error":    {"task_name", "error", "next_run"},
	"startup":            {"hostname", "version", "mode"}, "shutdown": {"hostname", "version", "mode", "reason"},
	"disk_low":         {"path", "used_percent", "free", "threshold_percent"},
	"database_error":   {"error", "since"},
	"update_available": {"current_version", "new_version", "channel"},
	"update_installed": {"previous_version", "new_version"},
	"update_failed":    {"version", "error"}, "update_staged": {"version"},
}

// RenderedTemplate is the validated subject and body of one notification.
type RenderedTemplate struct {
	Name    string
	Subject string
	Text    string
	HTML    string
}

// TemplateIssue describes a blocking error or non-blocking warning.
type TemplateIssue struct {
	Name    string
	Message string
	Warning bool
}

// Templates reads overrides on every render so edits and deletions take effect immediately.
type Templates struct {
	configDir string
	warn      WarningFunc
	seen      sync.Map
}

// NewTemplates creates a live-loading template manager.
func NewTemplates(configDir string, warn WarningFunc) *Templates {
	return &Templates{configDir: configDir, warn: warn}
}

// Names returns the built-in template names in stable order.
func (t *Templates) Names() []string {
	names := make([]string, 0, len(templateDefaults))
	for name := range templateDefaults {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidateTemplate checks a template and returns all blocking errors and warnings.
func ValidateTemplate(name, source string) ([]TemplateIssue, error) {
	issues := make([]TemplateIssue, 0)
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	if len(lines) < 3 || !strings.HasPrefix(lines[0], "Subject:") {
		return nil, errors.New("invalid template syntax at line 1")
	}
	subject := strings.TrimSpace(strings.TrimPrefix(lines[0], "Subject:"))
	if subject == "" {
		issues = append(issues, TemplateIssue{Name: name, Message: "Subject cannot be empty"})
	}
	if lines[1] != "---" {
		return nil, errors.New("invalid template syntax at line 2")
	}
	body := strings.TrimSpace(strings.Join(lines[2:], "\n"))
	if body == "" {
		issues = append(issues, TemplateIssue{Name: name, Message: "Body cannot be empty"})
	}
	allowed := make(map[string]struct{}, len(globalVariables)+len(templateVariables[name]))
	for variable := range globalVariables {
		allowed[variable] = struct{}{}
	}
	for _, variable := range templateVariables[name] {
		allowed[variable] = struct{}{}
	}
	for lineIndex, line := range lines {
		if strings.Contains(line, "{{") || strings.Contains(line, "}}") {
			issues = append(issues, TemplateIssue{Name: name, Message: fmt.Sprintf("Invalid template syntax at line %d", lineIndex+1)})
			continue
		}
		remaining := variablePattern.ReplaceAllString(line, "")
		if strings.ContainsAny(remaining, "{}") {
			issues = append(issues, TemplateIssue{Name: name, Message: fmt.Sprintf("Invalid template syntax at line %d", lineIndex+1)})
			continue
		}
		for _, match := range variablePattern.FindAllStringSubmatch(line, -1) {
			if _, exists := allowed[match[1]]; exists {
				continue
			}
			message := "Unknown variable: {" + match[1] + "}"
			if suggestion := closestVariable(match[1], allowed); suggestion != "" {
				message += ". Did you mean {" + suggestion + "}?"
			}
			issues = append(issues, TemplateIssue{Name: name, Message: message})
		}
	}
	if len(subject) > 78 {
		issues = append(issues, TemplateIssue{Name: name, Message: "Subject exceeds 78 characters", Warning: true})
	}
	if name == "security_alert" && !strings.Contains(strings.ToLower(body), "contact") {
		issues = append(issues, TemplateIssue{Name: name, Message: "Missing recommended contact information", Warning: true})
	}
	for _, line := range issues {
		if !line.Warning {
			return issues, errors.New(line.Message)
		}
	}
	return issues, nil
}

// Render applies a valid custom override or uses the embedded default.
func (t *Templates) Render(name string, values map[string]string) (RenderedTemplate, error) {
	defaultSource, ok := templateDefaults[name]
	if !ok {
		return RenderedTemplate{}, fmt.Errorf("unknown email template %q", name)
	}
	source := defaultSource
	if t.configDir != "" {
		customPath := filepath.Join(t.configDir, "template", "email", name+".tmpl")
		custom, err := os.ReadFile(customPath)
		if err == nil {
			if _, validationErr := ValidateTemplate(name, string(custom)); validationErr != nil {
				t.warnInvalid(name, custom, validationErr)
			} else {
				source = string(custom)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			t.warnInvalid(name, []byte(err.Error()), err)
		}
	}
	issues, err := ValidateTemplate(name, source)
	if err != nil {
		return RenderedTemplate{}, err
	}
	for _, issue := range issues {
		if issue.Warning {
			t.warnIssueOnce(name, []byte(source), issue.Message)
		}
	}
	parts := strings.SplitN(strings.ReplaceAll(source, "\r\n", "\n"), "\n---\n", 2)
	if len(parts) != 2 {
		return RenderedTemplate{}, errors.New("invalid template syntax at line 2")
	}
	subject := strings.TrimSpace(strings.TrimPrefix(parts[0], "Subject:"))
	body := strings.TrimSpace(parts[1])
	subject = substitute(subject, values)
	body = substitute(body, values)
	if strings.ContainsAny(subject, "\r\n") {
		return RenderedTemplate{}, ErrInvalidMessage
	}
	if strings.TrimSpace(body) == "" {
		return RenderedTemplate{}, errors.New("body cannot be empty")
	}
	return RenderedTemplate{Name: name, Subject: subject, Text: body, HTML: "<pre>" + html.EscapeString(body) + "</pre>"}, nil
}

// ValidateOverrides reports all override errors for CLI dry-run output.
func (t *Templates) ValidateOverrides() []TemplateIssue {
	issues := make([]TemplateIssue, 0)
	if t.configDir == "" {
		return issues
	}
	for _, name := range t.Names() {
		path := filepath.Join(t.configDir, "template", "email", name+".tmpl")
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			issues = append(issues, TemplateIssue{Name: name, Message: "Cannot read template override"})
			continue
		}
		found, validationErr := ValidateTemplate(name, string(data))
		if validationErr != nil {
			if len(found) == 0 {
				issues = append(issues, TemplateIssue{Name: name, Message: validationErr.Error()})
			} else {
				issues = append(issues, found...)
			}
			continue
		}
		issues = append(issues, found...)
	}
	return issues
}

func (t *Templates) warnInvalid(name string, source []byte, err error) {
	if t.warn == nil {
		return
	}
	digest := sha256.Sum256(source)
	key := fmt.Sprintf("%s:%x", name, digest)
	if _, loaded := t.seen.LoadOrStore(key, struct{}{}); !loaded {
		t.warn("server.notifications.email.templates."+name, err.Error())
	}
}

func (t *Templates) warnIssueOnce(name string, source []byte, message string) {
	if t.warn == nil {
		return
	}
	digest := sha256.Sum256(source)
	key := fmt.Sprintf("%s:%x:%s", name, digest, message)
	if _, loaded := t.seen.LoadOrStore(key, struct{}{}); !loaded {
		t.warn("server.notifications.email.templates."+name, message)
	}
}

func substitute(source string, values map[string]string) string {
	return variablePattern.ReplaceAllStringFunc(source, func(token string) string {
		return values[token[1:len(token)-1]]
	})
}

func closestVariable(input string, allowed map[string]struct{}) string {
	best := ""
	bestDistance := len(input) + 1
	for candidate := range allowed {
		distance := editDistance(input, candidate)
		if distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	if bestDistance > 3 {
		return ""
	}
	return best
}

func editDistance(left, right string) int {
	previous := make([]int, len(right)+1)
	for j := range previous {
		previous[j] = j
	}
	for i, a := range left {
		current := make([]int, len(right)+1)
		current[0] = i + 1
		for j, b := range right {
			cost := 0
			if a != b {
				cost = 1
			}
			current[j+1] = min(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = current
	}
	return previous[len(right)]
}
