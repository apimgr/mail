package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/mail/src/config"
)

func TestHandlePrivacyAPIReflectsConfiguredPrivacyState(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Privacy.Data.StoredOnServer = true
	cfg.Server.Privacy.Data.Sold = true
	cfg.Server.Privacy.Data.Sharing = append(cfg.Server.Privacy.Data.Sharing, struct {
		Condition string `yaml:"condition"`
		When      string `yaml:"when"`
		Data      string `yaml:"data"`
	}{Condition: "analytics", When: "with consent", Data: "page views"})
	cfg.Server.Privacy.Cookies.Essential.Enabled = true
	cfg.Server.Privacy.Cookies.Essential.Description = "Required"
	cfg.Server.Privacy.Cookies.Preferences.Enabled = true
	cfg.Server.Privacy.Cookies.Preferences.Description = "Preferences"
	cfg.Server.Privacy.Cookies.Analytics.Enabled = true
	cfg.Server.Privacy.Cookies.Analytics.Description = "Analytics"
	cfg.Server.Privacy.Cookies.Analytics.DescriptionSuffixSold = "may be shared"
	cfg.Server.Privacy.Consent.MessageIfSold = "Configured sold message"
	cfg.Server.Privacy.Retention.Period = "Configured retention"
	cfg.Server.Privacy.Retention.ExportAvailable = true
	cfg.Server.Privacy.Retention.DeletionAvailable = true
	cfg.Server.Tracking.Type = "matomo"
	cfg.Server.Tracking.ID = "configured-id"
	cfg.Server.Privacy.ThirdParty.Services = append(cfg.Server.Privacy.ThirdParty.Services, config.PrivacyServiceConfig{Name: "Mail provider", Purpose: "Delivery", DataSent: "Recipient address", PolicyURL: "https://example.test/privacy"})

	s := &Server{cfg: cfg, pages: newPageRenderer()}
	req := httptest.NewRequest("GET", "/api/v1/server/privacy", nil)
	req.AddCookie(&http.Cookie{Name: "ccpa_opt_out", Value: "true"})
	recorder := httptest.NewRecorder()
	s.handlePrivacyAPI(recorder, req)
	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		OK   bool `json:"ok"`
		Data struct {
			Summary struct {
				DataStoredOnServer bool `json:"data_stored_on_server"`
				DataSold           bool `json:"data_sold"`
				UserControl        bool `json:"user_control"`
			} `json:"summary"`
			Cookies struct {
				Analytics privacyCookie `json:"analytics"`
			} `json:"cookies"`
			Data struct {
				Sharing []privacySharing `json:"sharing"`
			} `json:"data"`
			Tracking struct {
				Enabled bool   `json:"enabled"`
				Type    string `json:"type"`
			} `json:"tracking"`
			ThirdParty struct {
				Services []privacyService `json:"services"`
			} `json:"third_party"`
			Retention struct {
				Period            string `json:"period"`
				ExportAvailable   bool   `json:"export_available"`
				DeletionAvailable bool   `json:"deletion_available"`
			} `json:"retention"`
			CCPA struct {
				Applicable   bool   `json:"applicable"`
				UserOptedOut bool   `json:"user_opted_out"`
				OptOutURL    string `json:"opt_out_url"`
			} `json:"ccpa"`
			Content struct {
				ConsentMessage string `json:"consent_message"`
				DataUsage      string `json:"data_usage"`
			} `json:"content"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.OK || !body.Data.Summary.DataStoredOnServer || !body.Data.Summary.DataSold || !body.Data.Summary.UserControl {
		t.Fatalf("summary = %+v", body.Data.Summary)
	}
	if body.Data.Cookies.Analytics.Description != "Analytics may be shared" {
		t.Errorf("analytics description = %q", body.Data.Cookies.Analytics.Description)
	}
	if len(body.Data.Data.Sharing) != 1 || body.Data.Data.Sharing[0].Condition != "analytics" {
		t.Errorf("sharing = %+v", body.Data.Data.Sharing)
	}
	if !body.Data.Tracking.Enabled || body.Data.Tracking.Type != "matomo" {
		t.Errorf("tracking = %+v", body.Data.Tracking)
	}
	if len(body.Data.ThirdParty.Services) != 2 || body.Data.ThirdParty.Services[0].Name != "Matomo" || body.Data.ThirdParty.Services[1].Name != "Mail provider" {
		t.Errorf("third-party services = %+v", body.Data.ThirdParty.Services)
	}
	if body.Data.Retention.Period != "Configured retention" || !body.Data.Retention.ExportAvailable || !body.Data.Retention.DeletionAvailable {
		t.Errorf("retention = %+v", body.Data.Retention)
	}
	if !body.Data.CCPA.Applicable || !body.Data.CCPA.UserOptedOut || body.Data.CCPA.OptOutURL != "/server/privacy#ccpa-opt-out" {
		t.Errorf("ccpa = %+v", body.Data.CCPA)
	}
	if body.Data.Content.ConsentMessage != "Configured sold message" || body.Data.Content.DataUsage != s.pages.translate("en", "privacy.data_usage.sold") {
		t.Errorf("content = %+v", body.Data.Content)
	}
}

func TestHandlePrivacyAPINotSoldUsesBuiltInUsageAndUnconfiguredTracking(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Privacy.Consent.Message = "Configured default message"
	s := &Server{cfg: cfg, pages: newPageRenderer()}
	recorder := httptest.NewRecorder()
	s.handlePrivacyAPI(recorder, httptest.NewRequest("GET", "/api/v1/server/privacy", nil))
	var body struct {
		Data struct {
			Tracking struct {
				Enabled bool `json:"enabled"`
			} `json:"tracking"`
			CCPA struct {
				Applicable   bool `json:"applicable"`
				UserOptedOut bool `json:"user_opted_out"`
			} `json:"ccpa"`
			Content struct {
				ConsentMessage string `json:"consent_message"`
				DataUsage      string `json:"data_usage"`
			} `json:"content"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Tracking.Enabled || body.Data.CCPA.Applicable || body.Data.CCPA.UserOptedOut {
		t.Errorf("default state unexpectedly enabled: tracking=%+v ccpa=%+v", body.Data.Tracking, body.Data.CCPA)
	}
	if body.Data.Content.ConsentMessage != "Configured default message" || body.Data.Content.DataUsage != s.pages.translate("en", "privacy.data_usage") {
		t.Errorf("content = %+v", body.Data.Content)
	}
}

func TestHandlePrivacyAPIUsesSelectedLocaleForBuiltInUsage(t *testing.T) {
	cfg := &config.Config{}
	s := &Server{cfg: cfg, pages: newPageRenderer()}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/privacy", nil)
	request.Header.Set("Accept-Language", "es")
	recorder := httptest.NewRecorder()
	s.handlePrivacyAPI(recorder, request)
	var body struct {
		Data struct {
			Content struct {
				DataUsage string `json:"data_usage"`
			} `json:"content"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(body.Data.Content.DataUsage, "Sus datos se utilizan únicamente") || strings.Contains(body.Data.Content.DataUsage, "Your data is used solely") {
		t.Errorf("API data usage does not match selected locale: %q", body.Data.Content.DataUsage)
	}
}

func TestHandleDPOAPIFallsBackWithoutExposingAdminAddress(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Contact.DPO.Name = "Privacy Officer"
	cfg.Server.Contact.General.Email = "privacy@example.test"
	s := &Server{cfg: cfg}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/dpo", nil)
	request.Host = "mail.example.test"
	recorder := httptest.NewRecorder()
	s.handleDPOAPI(recorder, request)
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Name       string `json:"name"`
			Email      string `json:"email"`
			PrivacyURL string `json:"privacy_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Data.Name != "Privacy Officer" || response.Data.Email != "privacy@example.test" || response.Data.PrivacyURL != "http://mail.example.test/server/privacy" {
		t.Fatalf("DPO response = %+v", response)
	}
	if strings.Contains(recorder.Body.String(), "admin") {
		t.Fatalf("DPO response exposed an admin address: %s", recorder.Body.String())
	}
}

func TestSecurityPolicyAPIAndPageUseConfiguredMarkdown(t *testing.T) {
	cfg := &config.Config{}
	cfg.Web.Security.Policy = "# Custom policy\nContact maintainers privately."
	handler := New(cfg, nil).router
	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/security/policy", nil)
	request.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Policy string `json:"policy"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Data.Policy != cfg.Web.Security.Policy {
		t.Fatalf("policy API response = %+v", response)
	}
	pageRequest := httptest.NewRequest(http.MethodGet, "/server/security/policy", nil)
	pageRequest.Header.Set("Accept", "text/html")
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, pageRequest)
	if !strings.Contains(page.Body.String(), "<h2>Custom policy</h2>") || !strings.Contains(page.Body.String(), "Contact maintainers privately.") {
		t.Fatalf("policy page omitted configured Markdown: %s", page.Body.String())
	}
}

func TestHelpPageAndAPIMirrorUseConfiguredContentAndRegisteredPages(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Pages.Help.Content = "# Mail operations\nUse the status route to inspect this instance."
	handler := New(cfg, nil).router
	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/help", nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Language", "en")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Title   string               `json:"title"`
			Content string               `json:"content"`
			Format  string               `json:"format"`
			Pages   []registeredHelpPage `json:"pages"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Data.Title == "" || response.Data.Content != cfg.Server.Pages.Help.Content || response.Data.Format != "markdown" || len(response.Data.Pages) != 1 || response.Data.Pages[0].Path != "/server/help/mcp" || response.Data.Pages[0].Description == "" {
		t.Fatalf("help API response = %+v", response)
	}
	pageRequest := httptest.NewRequest(http.MethodGet, "/server/help", nil)
	pageRequest.Header.Set("Accept", "text/html")
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, pageRequest)
	if !strings.Contains(page.Body.String(), "<h2>Mail operations</h2>") || !strings.Contains(page.Body.String(), "/server/help/mcp") {
		t.Fatalf("help page omitted configured content or links")
	}
}

func TestMCPHelpPageAndAPIMirrorDescribeLiveEndpointAndTools(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Pages.Help.MCP = "# Mail operations\n\nMail changes are operator-managed."
	handler := New(cfg, nil).router
	request := httptest.NewRequest(http.MethodGet, "https://mail.example.test/api/v1/server/help/mcp", nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Language", "en")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Enabled bool     `json:"enabled"`
			URL     string   `json:"url"`
			Aliases []string `json:"aliases"`
			Auth    struct {
				Scheme string `json:"scheme"`
			} `json:"auth"`
			Tools   []mcpHelpTool `json:"tools"`
			Project string        `json:"project"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || !response.Data.Enabled || response.Data.URL != "https://mail.example.test/api/v1/server/mcp" || response.Data.Auth.Scheme != "Bearer" || response.Data.Project != cfg.Server.Pages.Help.MCP {
		t.Fatalf("MCP help API response = %+v", response)
	}
	if len(response.Data.Aliases) != 2 || response.Data.Aliases[0] != "https://mail.example.test/api/mcp" || response.Data.Aliases[1] != "https://mail.example.test/mcp" {
		t.Fatalf("MCP aliases = %#v", response.Data.Aliases)
	}
	foundPublicTool, foundOperatorTool := false, false
	for _, tool := range response.Data.Tools {
		foundPublicTool = foundPublicTool || tool.Name == "getServerVersion"
		foundOperatorTool = foundOperatorTool || tool.Name == "listMailDomains"
	}
	if !foundPublicTool || foundOperatorTool {
		t.Fatalf("MCP tools do not match standard-token visibility: public=%t operator=%t", foundPublicTool, foundOperatorTool)
	}
	pageRequest := httptest.NewRequest(http.MethodGet, "https://mail.example.test/server/help/mcp", nil)
	pageRequest.Header.Set("Accept", "text/html")
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, pageRequest)
	for _, expected := range []string{`id="overview"`, `id="endpoint"`, `id="authentication"`, `id="connect"`, `id="tools"`, `id="limits"`, `id="troubleshooting"`, `id="project"`, "mail.example.test/api/v1/server/mcp", "MCP_TOKEN", "Mail operations"} {
		if !strings.Contains(page.Body.String(), expected) {
			t.Errorf("MCP help page omitted %q", expected)
		}
	}

	defaultHandler := New(&config.Config{}, nil).router
	defaultRequest := httptest.NewRequest(http.MethodGet, "/api/v1/server/help/mcp", nil)
	defaultRequest.Header.Set("Accept", "application/json")
	defaultRequest.Header.Set("Accept-Language", "en")
	defaultResponse := httptest.NewRecorder()
	defaultHandler.ServeHTTP(defaultResponse, defaultRequest)
	var defaultBody struct {
		Data struct {
			Project string `json:"project"`
		} `json:"data"`
	}
	if err := json.Unmarshal(defaultResponse.Body.Bytes(), &defaultBody); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"listMailDomains", "getMailDomainDNS", "listMailQueue", "Example requests", "no MCP resources or prompts"} {
		if !strings.Contains(defaultBody.Data.Project, expected) {
			t.Errorf("default MCP project guidance omitted %q", expected)
		}
	}
}

func TestAboutAndTermsPageMirrorsUseConfiguredFormatAndPlaceholders(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Branding.Content.About = config.BrandingPageContentConfig{Format: "text", Body: config.LocalizedContent{"default": "Project {project_name}"}}
	cfg.Server.Branding.Content.Terms = config.BrandingPageContentConfig{Format: "md", Body: config.LocalizedContent{"default": "Terms for {app_name}"}}
	handler := New(cfg, nil).router
	for _, tc := range []struct {
		path, wantContent, wantFormat string
	}{
		{"/api/v1/server/about", "Project mail", "text"},
		{"/api/v1/server/terms", "Terms for Mail", "markdown"},
	} {
		request := httptest.NewRequest(http.MethodGet, tc.path, nil)
		request.Host = "mail.example.test"
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Accept-Language", "es")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		var response struct {
			OK   bool `json:"ok"`
			Data struct {
				Title   string `json:"title"`
				Content string `json:"content"`
				Format  string `json:"format"`
			} `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if !response.OK || response.Data.Content != tc.wantContent || response.Data.Format != tc.wantFormat || response.Data.Title == "" {
			t.Errorf("%s response = %+v", tc.path, response)
		}
	}
}
