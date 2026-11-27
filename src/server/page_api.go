package server

import (
	"net/http"
	"strings"
)

type privacySharing struct {
	Condition string `json:"condition"`
	When      string `json:"when"`
	Data      string `json:"data"`
}

type privacyService struct {
	Name      string `json:"name"`
	Purpose   string `json:"purpose"`
	DataSent  string `json:"data_sent"`
	PolicyURL string `json:"policy_url"`
}

type privacyAPIResponse struct {
	Summary struct {
		DataStoredOnServer bool `json:"data_stored_on_server"`
		DataSold           bool `json:"data_sold"`
		UserControl        bool `json:"user_control"`
	} `json:"summary"`
	Cookies struct {
		Essential   privacyCookie `json:"essential"`
		Preferences privacyCookie `json:"preferences"`
		Analytics   privacyCookie `json:"analytics"`
	} `json:"cookies"`
	Data struct {
		Sold           bool             `json:"sold"`
		StoredOnServer bool             `json:"stored_on_server"`
		Sharing        []privacySharing `json:"sharing"`
	} `json:"data"`
	Tracking struct {
		Enabled  bool   `json:"enabled"`
		Type     string `json:"type"`
		TypeName string `json:"type_name"`
	} `json:"tracking"`
	Retention struct {
		Period            string `json:"period"`
		ExportAvailable   bool   `json:"export_available"`
		DeletionAvailable bool   `json:"deletion_available"`
	} `json:"retention"`
	ThirdParty struct {
		Services []privacyService `json:"services"`
	} `json:"third_party"`
	CCPA struct {
		Applicable   bool   `json:"applicable"`
		OptOutURL    string `json:"opt_out_url"`
		UserOptedOut bool   `json:"user_opted_out"`
	} `json:"ccpa"`
	Content struct {
		ConsentMessage string `json:"consent_message"`
		DataUsage      string `json:"data_usage"`
	} `json:"content"`
}

type privacyCookie struct {
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
}

func (s *Server) handlePrivacyAPI(w http.ResponseWriter, r *http.Request) {
	p := s.cfg.Server.Privacy
	result := privacyAPIResponse{}
	result.Summary.DataStoredOnServer = p.Data.StoredOnServer
	result.Summary.DataSold = p.Data.Sold
	result.Summary.UserControl = true
	result.Cookies.Essential = privacyCookie{Enabled: p.Cookies.Essential.Enabled, Description: p.Cookies.Essential.Description}
	result.Cookies.Preferences = privacyCookie{Enabled: p.Cookies.Preferences.Enabled, Description: p.Cookies.Preferences.Description}
	analyticsDescription := p.Cookies.Analytics.Description
	if p.Data.Sold {
		analyticsDescription = strings.TrimSpace(analyticsDescription + " " + p.Cookies.Analytics.DescriptionSuffixSold)
	} else {
		analyticsDescription = strings.TrimSpace(analyticsDescription + " " + p.Cookies.Analytics.DescriptionSuffixNotSold)
	}
	result.Cookies.Analytics = privacyCookie{Enabled: p.Cookies.Analytics.Enabled, Description: analyticsDescription}
	result.Data.Sold = p.Data.Sold
	result.Data.StoredOnServer = p.Data.StoredOnServer
	result.Data.Sharing = make([]privacySharing, 0, len(p.Data.Sharing))
	for _, sharing := range p.Data.Sharing {
		result.Data.Sharing = append(result.Data.Sharing, privacySharing{Condition: sharing.Condition, When: sharing.When, Data: sharing.Data})
	}
	result.Tracking.Type = strings.ToLower(strings.TrimSpace(s.cfg.Server.Tracking.Type))
	if result.Tracking.Type == "none" {
		result.Tracking.Type = ""
	}
	result.Tracking.TypeName = trackingTypeName(result.Tracking.Type)
	result.Tracking.Enabled = result.Tracking.Type != ""
	result.Retention.Period = p.Retention.Period
	result.Retention.ExportAvailable = p.Retention.ExportAvailable
	result.Retention.DeletionAvailable = p.Retention.DeletionAvailable
	result.ThirdParty.Services = s.privacyServices()
	result.CCPA.Applicable = p.Data.Sold
	result.CCPA.OptOutURL = "/server/privacy#ccpa-opt-out"
	if cookie, err := r.Cookie("ccpa_opt_out"); err == nil {
		result.CCPA.UserOptedOut = cookie.Value == "true"
	}
	result.Content.ConsentMessage = p.Consent.Message
	language := s.preferredPageLanguage(r)
	if p.Data.Sold {
		result.Content.ConsentMessage = p.Consent.MessageIfSold
		result.Content.DataUsage = s.pages.translate(language, "privacy.data_usage.sold")
	} else {
		result.Content.DataUsage = s.pages.translate(language, "privacy.data_usage")
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": result})
}

func (s *Server) privacyServices() []privacyService {
	services := make([]privacyService, 0, len(s.cfg.Server.Privacy.ThirdParty.Services)+1)
	trackingType := strings.ToLower(strings.TrimSpace(s.cfg.Server.Tracking.Type))
	if name := trackingTypeName(trackingType); name != "" {
		service := privacyService{Name: name, Purpose: "Usage analytics"}
		switch trackingType {
		case "matomo", "piwik":
			service.DataSent = "Page views, browser type, and country (anonymized IP)"
			service.PolicyURL = "https://matomo.org/privacy-policy/"
		case "google":
			service.DataSent = "Page views, browser and device information, and approximate location"
			service.PolicyURL = "https://policies.google.com/privacy"
		case "plausible":
			service.DataSent = "Aggregated page views and referral information"
			service.PolicyURL = "https://plausible.io/data-policy"
		case "fathom":
			service.DataSent = "Aggregated page views and referral information"
			service.PolicyURL = "https://usefathom.com/privacy"
		case "umami":
			service.DataSent = "Page views, referral information, and anonymized visitor data"
			service.PolicyURL = "https://umami.is/privacy"
		case "simple":
			service.DataSent = "Aggregated page views and referral information"
			service.PolicyURL = "https://simpleanalytics.com/privacy"
		case "cloudflare":
			service.DataSent = "Aggregated page views and performance information"
			service.PolicyURL = "https://www.cloudflare.com/privacypolicy/"
		default:
			service.DataSent = "Page views and anonymized usage information"
		}
		services = append(services, service)
	}
	for _, configured := range s.cfg.Server.Privacy.ThirdParty.Services {
		service := privacyService{Name: strings.TrimSpace(configured.Name), Purpose: strings.TrimSpace(configured.Purpose), DataSent: strings.TrimSpace(configured.DataSent), PolicyURL: strings.TrimSpace(configured.PolicyURL)}
		if service.Name == "" {
			continue
		}
		duplicate := false
		for _, existing := range services {
			if strings.EqualFold(existing.Name, service.Name) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			services = append(services, service)
		}
	}
	return services
}

func trackingTypeName(trackingType string) string {
	switch trackingType {
	case "google":
		return "Google Analytics"
	case "matomo":
		return "Matomo"
	case "piwik":
		return "Piwik"
	case "owa":
		return "Open Web Analytics"
	case "fathom":
		return "Fathom Analytics"
	case "plausible":
		return "Plausible Analytics"
	case "umami":
		return "Umami"
	case "simple":
		return "Simple Analytics"
	case "cloudflare":
		return "Cloudflare Web Analytics"
	default:
		return ""
	}
}

func (s *Server) handleDPOAPI(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(s.cfg.Server.Contact.DPO.Name)
	email := strings.TrimSpace(s.cfg.Server.Contact.DPO.Email)
	if email == "" {
		email = strings.TrimSpace(s.cfg.Server.Contact.General.Email)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]string{
		"name":        name,
		"email":       email,
		"privacy_url": s.buildPublicURL(r, "/server/privacy"),
	}})
}

func (s *Server) handleSecurityPolicyAPI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]string{"policy": s.securityPolicy(r)}})
}

func (s *Server) handleHelpAPI(w http.ResponseWriter, r *http.Request) {
	view := s.helpPage(r)
	language := s.preferredPageLanguage(r)
	pages := make([]registeredHelpPage, 0, len(view.Pages))
	for _, item := range view.Pages {
		pages = append(pages, registeredHelpPage{
			Title: s.pages.translate(language, item.TitleKey), Description: s.pages.translate(language, item.DescriptionKey), Path: item.Path,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]any{
		"title": s.pages.translate(language, "page.help.title"), "content": view.Content, "format": "markdown", "pages": pages,
	}})
}

func (s *Server) handleMCPHelpAPI(w http.ResponseWriter, r *http.Request) {
	view := s.mcpHelpPage(r)
	tools := make([]mcpHelpTool, len(view.Tools))
	copy(tools, view.Tools)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]any{
		"enabled": true,
		"url":     view.Endpoint,
		"aliases": []string{view.APIEndpoint, view.RootEndpoint},
		"auth":    map[string]string{"scheme": "Bearer"},
		"tools":   tools,
		"project": view.Project,
	}})
}

func (s *Server) handleTermsAPI(w http.ResponseWriter, r *http.Request) {
	language := s.preferredPageLanguage(r)
	page := s.termsPage(r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]string{
		"title": s.pages.translate(language, "page.terms.title"), "content": page.Content, "format": page.Format,
	}})
}
