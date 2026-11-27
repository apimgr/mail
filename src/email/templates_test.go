package email

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplateOverrideValidationAndLiveReload(t *testing.T) {
	configDir := t.TempDir()
	templateDir := filepath.Join(configDir, "template", "email")
	if err := os.MkdirAll(templateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manager := NewTemplates(configDir, nil)
	values := map[string]string{"app_name": "Mail", "fqdn": "mail.local", "app_url": "https://mail.local", "timestamp": "now"}
	first, err := manager.Render("startup", values)
	if err != nil || !strings.Contains(first.Subject, "Server Started - Mail") {
		t.Fatalf("embedded template render = %+v, err=%v", first, err)
	}
	custom := "Subject: Custom {app_name}\n---\nCustom message for {fqdn}.\n"
	path := filepath.Join(templateDir, "startup.tmpl")
	if err := os.WriteFile(path, []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := manager.Render("startup", values)
	if err != nil || second.Subject != "Custom Mail" || !strings.Contains(second.Text, "mail.local") {
		t.Fatalf("custom template render = %+v, err=%v", second, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	third, err := manager.Render("startup", values)
	if err != nil || !strings.Contains(third.Subject, "Server Started - Mail") {
		t.Fatalf("deleted override did not restore embedded default: %+v, err=%v", third, err)
	}
}

func TestInvalidTemplateOverrideFallsBackAndDryRunReportsEveryIssue(t *testing.T) {
	configDir := t.TempDir()
	templateDir := filepath.Join(configDir, "template", "email")
	if err := os.MkdirAll(templateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	invalid := "Subject: {unknown}\n---\n{{t \"key\"}}\n"
	if err := os.WriteFile(filepath.Join(templateDir, "test.tmpl"), []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewTemplates(configDir, nil)
	values := map[string]string{"app_name": "Mail", "fqdn": "mail.local", "app_url": "https://mail.local", "timestamp": "now"}
	rendered, err := manager.Render("test", values)
	if err != nil || !strings.Contains(rendered.Subject, "Test Email - Mail") {
		t.Fatalf("invalid override did not fall back: %+v, err=%v", rendered, err)
	}
	issues := manager.ValidateOverrides()
	if len(issues) < 2 {
		t.Fatalf("dry-run reported %d issues, want unknown variable and invalid syntax", len(issues))
	}
	foundUnknown, foundSyntax := false, false
	for _, issue := range issues {
		foundUnknown = foundUnknown || strings.Contains(issue.Message, "Unknown variable: {unknown}")
		foundSyntax = foundSyntax || strings.Contains(issue.Message, "Invalid template syntax")
	}
	if !foundUnknown || !foundSyntax {
		t.Fatalf("dry-run issues = %#v", issues)
	}
}

func TestTemplateValidationRejectsEmptySubjectBodyAndUnknownVariables(t *testing.T) {
	tests := []struct {
		name, source, want string
	}{
		{"empty subject", "Subject: \n---\nBody", "Subject cannot be empty"},
		{"empty body", "Subject: Alert\n---\n", "Body cannot be empty"},
		{"unknown variable", "Subject: {unknown}\n---\nBody", "Unknown variable: {unknown}"},
		{"bad syntax", "Subject: Alert\n--\nBody", "invalid template syntax at line 2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateTemplate("test", test.source)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateTemplate error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestEmbeddedDefaultTemplatesValidate(t *testing.T) {
	manager := NewTemplates("", nil)
	if len(manager.Names()) != 17 {
		t.Fatalf("embedded template count = %d, want 17", len(manager.Names()))
	}
	for _, name := range manager.Names() {
		if _, err := ValidateTemplate(name, templateDefaults[name]); err != nil {
			t.Errorf("embedded template %q is invalid: %v", name, err)
		}
	}
}

func TestTemplateWarningsAreReportedOncePerSource(t *testing.T) {
	configDir := t.TempDir()
	templateDir := filepath.Join(configDir, "template", "email")
	if err := os.MkdirAll(templateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(templateDir, "security_alert.tmpl")
	if err := os.WriteFile(path, []byte("Subject: Alert\n---\nA security event occurred."), 0o600); err != nil {
		t.Fatal(err)
	}
	warnings := 0
	manager := NewTemplates(configDir, func(string, string) { warnings++ })
	for range 2 {
		if _, err := manager.Render("security_alert", map[string]string{"event": "test"}); err != nil {
			t.Fatal(err)
		}
	}
	if warnings != 1 {
		t.Fatalf("warning callback count = %d, want one warning for unchanged source", warnings)
	}
	if err := os.WriteFile(path, []byte("Subject: Alert\n---\n{{ invalid }}"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := manager.Render("security_alert", nil); err != nil {
			t.Fatal(err)
		}
	}
	if warnings != 2 {
		t.Fatalf("warning callback count after invalid override = %d, want one additional warning", warnings)
	}
}

func TestTemplateValidationWarningsAndRenderFailures(t *testing.T) {
	source := "Subject: " + strings.Repeat("x", 80) + "\n---\nA report arrived."
	issues, err := ValidateTemplate("security_alert", source)
	if err != nil || len(issues) != 2 || !issues[0].Warning || !issues[1].Warning {
		t.Fatalf("warning-only validation = %#v, %v", issues, err)
	}
	issues, err = ValidateTemplate("startup", "Subject: {app_nam}\n---\nBody")
	if err == nil || len(issues) == 0 || !strings.Contains(issues[0].Message, "Did you mean {app_name}?") {
		t.Fatalf("unknown variable suggestion = %#v, %v", issues, err)
	}
	manager := NewTemplates("", nil)
	if _, err := manager.Render("unknown", nil); err == nil {
		t.Fatal("Render accepted an unknown template")
	}

	configDir := t.TempDir()
	templateDir := filepath.Join(configDir, "template", "email")
	if err := os.MkdirAll(templateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templateDir, "startup.tmpl"), []byte("Subject: {app_name}\n---\n{app_name}"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager = NewTemplates(configDir, nil)
	if _, err := manager.Render("startup", map[string]string{"app_name": "line\nbreak"}); err == nil || !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("header injection error = %v", err)
	}
	if _, err := manager.Render("startup", map[string]string{"app_name": ""}); err == nil || !strings.Contains(err.Error(), "body cannot be empty") {
		t.Fatalf("empty rendered body error = %v", err)
	}
}
