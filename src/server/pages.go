package server

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	texttemplate "text/template"
	"time"

	"github.com/apimgr/mail/src/common/api"
	commonformat "github.com/apimgr/mail/src/common/format"
	"github.com/apimgr/mail/src/common/i18n"
	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
	"github.com/apimgr/mail/src/logger"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
)

//go:embed web/templates/*.tmpl web/static/* web/manifest.json web/sw.js.tmpl
var webAssets embed.FS

type pageSection struct {
	ID         string
	HeadingKey string
	BodyKey    string
	Links      []pageLink
}

type pageLink struct {
	Path     string
	LabelKey string
}

type pageContent struct {
	TitleKey               string
	DescriptionKey         string
	GraphQLEndpoint        string
	SwaggerEndpoint        string
	BreadcrumbKey          string
	BreadcrumbPath         string
	Sections               []pageSection
	Status                 bool
	Healthy                bool
	Health                 *healthResponse
	Version                string
	Platform               string
	Lang                   string
	LanguageName           string
	AutomaticLanguageName  string
	Languages              []i18n.Language
	Dir                    string
	Theme                  string
	Canonical              string
	NoIndex                bool
	OfflineCachePublic     bool
	Preferences            bool
	AutomaticLang          bool
	ErrorKey               string
	ThemeNext              string
	CSRFToken              string
	HasConsentCookie       bool
	ConsentMessage         string
	ConsentPolicyURL       string
	ConsentPolicyText      string
	ConsentPreferencesText string
	ConsentShowPreferences bool
	ConsentPosition        string
	ConsentDecline         string
	ConsentAccept          string
	ConsentPreferences     bool
	ConsentAnalytics       bool
	CCPAOptOut             bool
	DataSold               bool
	Sync                   bool
	SyncURL                string
	SyncCode               string
	ConsentRequired        bool
	CancelTarget           string
	CurrentPath            string
	UptimeHuman            string
	Privacy                *privacyPageView
	Tokens                 *tokenPageView
	DPO                    *dpoPageView
	SecurityPolicy         string
	SecurityPolicyHTML     template.HTML
	SecurityExpires        string
	SecurityEncryptionURL  string
	SecurityFingerprint    string
	SecurityReport         *securityStatusPage
	SecurityThanks         []database.SecurityThanks
	ContactForm            *securityContactFormPage
	SecurityReview         *securityReviewPage
	ContactNotice          string
	ContactTrackingID      string
	Help                   *helpPageView
	MCPHelp                *mcpHelpView
	About                  *brandingPageView
	Terms                  *brandingPageView
	HeaderHTML             template.HTML
	FooterBrandingHTML     template.HTML
	FooterHubs             []registeredHelpPage
	MorePreferences        []registeredHelpPage
}

type mcpHelpTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type mcpHelpView struct {
	Endpoint      string
	APIEndpoint   string
	RootEndpoint  string
	ClaudeCommand string
	JSONConfig    string
	CurlCommand   string
	Tools         []mcpHelpTool
	Errors        map[string]string
	Project       string
	ProjectHTML   template.HTML
}

type brandingPageView struct {
	Content string
	Format  string
	HTML    template.HTML
}

type helpPageView struct {
	Content string
	HTML    template.HTML
	Pages   []registeredHelpPage
}

type registeredHelpPage struct {
	Title          string `json:"title"`
	TitleKey       string `json:"-"`
	Description    string `json:"description,omitempty"`
	DescriptionKey string `json:"-"`
	Path           string `json:"path"`
}

type pageRoute struct {
	Path           string
	HandlerKey     string
	TitleKey       string
	DescriptionKey string
	Hub            string
	HubTitleKey    string
	Order          int
	Rendered       bool
	Sitemap        bool
	Changefreq     string
	Priority       string
}

var pageRegistry = []pageRoute{
	{Path: "/", HandlerKey: "home", TitleKey: "page.home.title", DescriptionKey: "page.home.lead", Rendered: true, Sitemap: true, Changefreq: "daily", Priority: "1.0"},
	{Path: "/server", HandlerKey: "server_redirect"},
	{Path: "/server/about", HandlerKey: "about", TitleKey: "page.about.title", DescriptionKey: "page.about.lead", Rendered: true, Sitemap: true, Changefreq: "weekly", Priority: "0.8"},
	{Path: "/server/privacy", HandlerKey: "privacy", TitleKey: "page.privacy.title", DescriptionKey: "page.privacy.lead", Rendered: true, Sitemap: true, Changefreq: "weekly", Priority: "0.8"},
	{Path: "/server/contact", HandlerKey: "contact", TitleKey: "page.contact.title", DescriptionKey: "page.contact.lead", Rendered: true},
	{Path: "/server/help", HandlerKey: "help", TitleKey: "page.help.title", DescriptionKey: "page.help.lead", Rendered: true, Sitemap: true, Changefreq: "weekly", Priority: "0.8"},
	{Path: "/server/help/mcp", HandlerKey: "help_mcp", TitleKey: "page.mcp.title", DescriptionKey: "page.mcp.lead", Hub: "/server/help", HubTitleKey: "footer.help", Rendered: true, Sitemap: true, Changefreq: "weekly", Priority: "0.8"},
	{Path: "/server/terms", HandlerKey: "terms", TitleKey: "page.terms.title", DescriptionKey: "page.terms.lead", Rendered: true, Sitemap: true, Changefreq: "weekly", Priority: "0.8"},
	{Path: "/server/dpo", HandlerKey: "dpo", TitleKey: "page.dpo.title", DescriptionKey: "page.dpo.lead", Rendered: true, Sitemap: true, Changefreq: "weekly", Priority: "0.8"},
	{Path: "/server/security", HandlerKey: "security", TitleKey: "page.security.title", DescriptionKey: "page.security.lead", Rendered: true, Sitemap: true, Changefreq: "weekly", Priority: "0.8"},
	{Path: "/server/security/policy", HandlerKey: "security_policy", TitleKey: "security.policy.title", DescriptionKey: "page.security.lead", Rendered: true, Sitemap: true, Changefreq: "weekly", Priority: "0.8"},
	{Path: "/server/security/thanks", HandlerKey: "security_thanks", TitleKey: "security.thanks.title", DescriptionKey: "page.security.lead", Rendered: true},
	{Path: "/server/security/report/{tracking_id}", HandlerKey: "security_report", TitleKey: "security.report.status.title", DescriptionKey: "page.security.lead", Rendered: true},
	{Path: "/server/preferences", HandlerKey: "preferences", TitleKey: "nav.preferences", DescriptionKey: "preferences.lead", Rendered: true},
	{Path: "/server/preferences/sync", HandlerKey: "preferences_sync", TitleKey: "preferences.sync.title", DescriptionKey: "preferences.sync.lead", Hub: "/server/preferences", HubTitleKey: "nav.preferences", Rendered: true},
	{Path: "/server/preferences/tokens", HandlerKey: "preferences_tokens", TitleKey: "tokens.title", DescriptionKey: "tokens.lead", Hub: "/server/preferences", HubTitleKey: "nav.preferences", Rendered: true},
	{Path: "/server/docs/swagger", HandlerKey: "swagger", TitleKey: "page.swagger.title", DescriptionKey: "page.swagger.lead", Rendered: true, Sitemap: true, Changefreq: "weekly", Priority: "0.7"},
	{Path: "/server/docs/graphql", HandlerKey: "graphql", TitleKey: "page.graphql.title", DescriptionKey: "page.graphql.lead", Rendered: true, Sitemap: true, Changefreq: "weekly", Priority: "0.7"},
	{Path: "/server/healthz", HandlerKey: "health", TitleKey: "health.status", DescriptionKey: "health.status", Rendered: true, Sitemap: true, Changefreq: "weekly", Priority: "0.8"},
}

type dpoPageView struct {
	Name        string
	Email       string
	PrivacyURL  string
	SecurityURL string
}

type privacyPageView struct {
	LastUpdated      string
	DataSold         bool
	StoredOnServer   bool
	Essential        privacyCookie
	Preferences      privacyCookie
	Analytics        privacyCookie
	TrackingTypeName string
	Sharing          []privacySharing
	Retention        string
	ExportAvailable  bool
	DeleteAvailable  bool
	Services         []privacyService
	CCPAOptOut       bool
	CSRFToken        string
	TLS              bool
	DataCollection   template.HTML
	DataUsage        template.HTML
	DataSecurity     template.HTML
	DPOName          string
	DPOEmail         string
	ExtensionHTML    template.HTML
}

type consentState struct {
	Essential   bool  `json:"essential"`
	Preferences bool  `json:"preferences"`
	Analytics   bool  `json:"analytics"`
	Timestamp   int64 `json:"timestamp"`
}

type pageRenderer struct {
	templates *template.Template
	catalog   *i18n.Catalog
	parseErr  error
}

func newPageRenderer() *pageRenderer {
	r := &pageRenderer{}
	funcs := template.FuncMap{
		"path": func(p string) string {
			if strings.HasPrefix(p, "/static/") {
				return p + "?v=" + url.QueryEscape(applicationBuildStamp())
			}
			return p
		},
		"t":              r.translate,
		"humanTimestamp": func(lang, value string) string { return humanTimestamp(lang, value, r.catalog) },
		"formatCount":    commonformat.Count,
		"checkClass":     healthCheckClass,
	}
	r.templates, r.parseErr = template.New("public").Funcs(funcs).ParseFS(webAssets, "web/templates/*.tmpl")
	var err error
	r.catalog, err = i18n.Load()
	if err != nil && r.parseErr == nil {
		r.parseErr = err
	}
	return r
}

func (p *pageRenderer) translate(lang, key string) string {
	return p.catalog.Translate(lang, key)
}

func (s *Server) registerPages() {
	pageHandlers := map[string]http.HandlerFunc{
		"home": s.handleHome,
		"server_redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/server/about", http.StatusMovedPermanently)
		},
		"about":              s.handleAboutPage,
		"privacy":            s.handlePrivacyPage,
		"contact":            s.handleContactPage,
		"help":               s.handleHelpPage,
		"help_mcp":           s.handleMCPHelpPage,
		"terms":              s.handleTermsPage,
		"dpo":                s.handleDPOPage,
		"security":           s.handleSecurityPage,
		"security_policy":    s.handleSecurityPolicyPage,
		"security_thanks":    s.handleSecurityThanksPage,
		"security_report":    s.handleSecurityReportStatus,
		"preferences":        s.handlePreferencesPage,
		"preferences_sync":   s.handlePreferencesSyncPage,
		"preferences_tokens": s.handleOwnerTokensPage,
		"swagger":            s.handleSwaggerPage,
		"graphql":            s.handleGraphQLPage,
		"health":             s.handleHealthPage,
	}
	for _, route := range pageRegistry {
		handler, ok := pageHandlers[route.HandlerKey]
		if !ok {
			panic("registered page has no handler: " + route.Path)
		}
		if route.HandlerKey == "contact" {
			pageHandler := handler
			handler = func(w http.ResponseWriter, r *http.Request) {
				if !s.contactFormEnabled() {
					s.handleNotFound(w, r)
					return
				}
				pageHandler(w, r)
			}
		}
		s.router.Get(route.Path, handler)
	}
	s.router.Post("/server/preferences", s.handlePreferencesUpdate)
	s.router.Post("/server/contact", func(w http.ResponseWriter, r *http.Request) {
		if !s.contactFormEnabled() {
			s.handleNotFound(w, r)
			return
		}
		s.handleContactSubmit(w, r)
	})
	s.router.Post("/server/preferences/sync", s.handlePreferencesSyncPost)
	s.router.Post("/server/preferences/token/create", s.handleWebIssuePrimaryToken)
	s.router.Post("/server/preferences/token/restore", s.handleWebRestorePrimaryToken)
	s.router.Post("/server/preferences/token/forget", s.handleWebForgetPrimaryToken)
	s.router.Post("/server/preferences/token/reveal", s.handleWebRevealPrimaryToken)
	s.router.Post("/server/preferences/token/rotate", s.handleWebRotatePrimaryToken)
	s.router.Post("/server/preferences/token/revoke", s.handleWebRevokePrimaryToken)
	s.router.Post("/server/preferences/tokens", s.handleWebCreateLinkedToken)
	s.router.Post("/server/preferences/tokens/{id}/reveal", s.handleWebRevealLinkedToken)
	s.router.Post("/server/preferences/tokens/{id}/rotate", s.handleWebRotateLinkedToken)
	s.router.Post("/server/preferences/tokens/{id}/update", s.handleWebUpdateLinkedToken)
	s.router.Post("/server/preferences/tokens/{id}/revoke", s.handleWebRevokeLinkedToken)
	s.router.Get("/server/preferences/sync/import", s.handlePreferencesSyncImport)
	s.router.Post("/server/consent", s.handleConsentUpdate)
	s.router.Post("/server/ccpa", s.handleCCPAUpdate)
	s.router.Get("/manifest.json", s.handleManifest)
	s.router.Get("/sw.js", s.handleServiceWorker)
	s.router.Get("/static/*", s.handleStaticAsset)
	s.router.NotFound(s.handleNotFound)
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, http.StatusOK, pageContent{
		TitleKey: "page.home.title", DescriptionKey: "page.home.lead",
		OfflineCachePublic: true,
		Sections: []pageSection{
			{ID: "project-scope", HeadingKey: "home.scope.title", BodyKey: "home.scope.body"},
			{ID: "current-build", HeadingKey: "home.status.title", BodyKey: "home.status.body"},
		},
	})
}

func (s *Server) handleAboutPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, http.StatusOK, pageContent{
		TitleKey: "page.about.title", DescriptionKey: "page.about.lead", About: s.aboutPage(r), OfflineCachePublic: true,
	})
}

func (s *Server) aboutPage(r *http.Request) *brandingPageView {
	content := s.builtinAboutContent(r)
	_, _, usable := s.operatorBody(r, "about")
	builtIn := renderSafeContent("markdown", content)
	if usable {
		builtIn += s.operatorHTML(r, "about")
	}
	return &brandingPageView{Content: content, Format: "markdown", HTML: builtIn}
}

func (s *Server) builtinAboutContent(r *http.Request) string {
	language := s.preferredPageLanguage(r)
	return fmt.Sprintf("# Mail\n\n%s\n\n## %s\n\n%s\n\n## %s\n\n%s\n\n## %s\n\n- %s: %s\n- %s: %s\n\n- [%s](https://github.com/apimgr/mail)\n- [%s](https://apimgr-mail.readthedocs.io)",
		s.pages.translate(language, "about.tagline.body"),
		s.pages.translate(language, "about.description.title"), s.pages.translate(language, "about.project.body"),
		s.pages.translate(language, "about.features.title"), s.pages.translate(language, "about.features.body"),
		s.pages.translate(language, "about.links.title"), s.pages.translate(language, "health.version"), applicationVersion(),
		s.pages.translate(language, "health.build"), formatBuildDate(s.buildDate, language, s.pages.catalog),
		s.pages.translate(language, "link.repository"), s.pages.translate(language, "link.documentation"))
}

func (s *Server) handlePrivacyPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, http.StatusOK, pageContent{
		TitleKey: "page.privacy.title", DescriptionKey: "page.privacy.lead",
		Privacy: &privacyPageView{},
	})
}

func (s *Server) handleContactPage(w http.ResponseWriter, r *http.Request) {
	securityID := strings.TrimSpace(r.URL.Query().Get("security_id"))
	if securityID != "" {
		if _, ok := s.validSecurityID(r.Context(), securityID, time.Now().UTC()); ok {
			s.renderSecurityContactForm(w, r, securityID, "")
			return
		}
		s.logInvalidSecurityID(r, securityID)
	}
	s.renderPage(w, r, http.StatusOK, pageContent{
		TitleKey: "page.contact.title", DescriptionKey: "page.contact.lead",
		NoIndex: true, OfflineCachePublic: true,
		Sections: []pageSection{{ID: "contact-chooser", HeadingKey: "contact.chooser.title", BodyKey: "contact.purpose", Links: []pageLink{
			{Path: "/server/contact?type=abuse", LabelKey: "contact.type.abuse"},
			{Path: "/server/contact?type=dmca", LabelKey: "contact.type.dmca"},
			{Path: "/server/contact?type=privacy", LabelKey: "contact.type.privacy"},
		}}},
	})
}

func (s *Server) handleHelpPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, http.StatusOK, pageContent{TitleKey: "page.help.title", DescriptionKey: "page.help.lead", Help: s.helpPage(r), OfflineCachePublic: true})
}

func (s *Server) helpPage(r *http.Request) *helpPageView {
	content := strings.TrimSpace(s.cfg.Server.Pages.Help.Content)
	if content == "" {
		language := s.preferredPageLanguage(r)
		content = fmt.Sprintf("# %s\n\n%s\n\n## %s\n\n%s\n\n## %s\n\n%s",
			s.pages.translate(language, "page.help.title"),
			s.pages.translate(language, "page.help.lead"),
			s.pages.translate(language, "help.routes.title"),
			s.pages.translate(language, "help.routes.body"),
			s.pages.translate(language, "help.cli.title"),
			s.pages.translate(language, "help.cli.body"))
	}
	return &helpPageView{Content: content, HTML: renderSafeContent("markdown", content), Pages: registeredPagesForHub("/server/help")}
}

func registeredPreferencePages() []registeredHelpPage {
	return registeredPagesForHub("/server/preferences")
}

func registeredPagesForHub(hub string) []registeredHelpPage {
	var entries []pageRoute
	for _, entry := range pageRegistry {
		if entry.Hub == hub {
			entries = append(entries, entry)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Order < entries[j].Order })
	pages := make([]registeredHelpPage, 0, len(entries))
	for _, entry := range entries {
		pages = append(pages, registeredHelpPage{TitleKey: entry.TitleKey, DescriptionKey: entry.DescriptionKey, Path: entry.Path})
	}
	return pages
}

func registeredHubs() []registeredHelpPage {
	seen := make(map[string]bool)
	var hubs []registeredHelpPage
	for _, entry := range pageRegistry {
		if entry.Hub == "" || entry.HubTitleKey == "" || seen[entry.Hub] {
			continue
		}
		seen[entry.Hub] = true
		hubs = append(hubs, registeredHelpPage{TitleKey: entry.HubTitleKey, Path: entry.Hub})
	}
	return hubs
}

func (s *Server) handleMCPHelpPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, http.StatusOK, pageContent{
		TitleKey: "page.mcp.title", DescriptionKey: "page.mcp.lead",
		BreadcrumbKey: "footer.help", BreadcrumbPath: "/server/help",
		MCPHelp: s.mcpHelpPage(r), OfflineCachePublic: true,
	})
}

func (s *Server) mcpHelpPage(r *http.Request) *mcpHelpView {
	endpoint := s.buildPublicURL(r, api.APIBasePath()+"/server/mcp")
	apiEndpoint := s.buildPublicURL(r, "/api/mcp")
	rootEndpoint := s.buildPublicURL(r, "/mcp")
	tools := make([]mcpHelpTool, 0)
	for _, route := range s.apiRouteRegistry() {
		if mcpRouteAvailable(route, false) {
			tools = append(tools, mcpHelpTool{Name: route.OperationID, Description: route.Summary})
		}
	}
	project := strings.TrimSpace(s.cfg.Server.Pages.Help.MCP)
	if project == "" {
		project = s.pages.translate(s.preferredPageLanguage(r), "mcp.help.project.default")
	}
	return &mcpHelpView{
		Endpoint: endpoint, APIEndpoint: apiEndpoint, RootEndpoint: rootEndpoint,
		ClaudeCommand: `claude mcp add --transport http mail ` + endpoint + ` --header "Authorization: Bearer $MCP_TOKEN"`,
		JSONConfig:    `{"mcpServers":{"mail":{"type":"http","url":"` + endpoint + `","headers":{"Authorization":"Bearer $MCP_TOKEN"}}}}`,
		CurlCommand:   `curl -X POST ` + endpoint + ` -H "Authorization: Bearer $MCP_TOKEN" -H "Content-Type: application/json" -H "MCP-Protocol-Version: 2026-07-28" -H "Mcp-Method: tools/list" --data '{"jsonrpc":"2.0","id":1,"method":"tools/list","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}'`,
		Tools:         tools,
		Errors: map[string]string{
			"400": "mcp.help.troubleshooting.400",
			"401": "mcp.help.troubleshooting.401",
			"403": "mcp.help.troubleshooting.403",
			"405": "mcp.help.troubleshooting.405",
		},
		Project:     project,
		ProjectHTML: renderSafeContent("markdown", project),
	}
}

func (s *Server) handleTermsPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, http.StatusOK, pageContent{TitleKey: "page.terms.title", DescriptionKey: "page.terms.lead", Terms: s.termsPage(r), OfflineCachePublic: true})
}

func (s *Server) termsPage(r *http.Request) *brandingPageView {
	lang := s.preferredPageLanguage(r)
	format, body, usable := s.operatorBody(r, "terms")
	if usable {
		return &brandingPageView{Content: body, Format: format, HTML: renderSafeContent(format, body)}
	}
	content := ""
	var sections []string
	for _, section := range []struct{ title, body string }{{"terms.accept.title", "terms.accept.body"}, {"terms.use.title", "terms.use.body"}, {"terms.liability.title", "terms.liability.body"}, {"terms.changes.title", "terms.changes.body"}, {"terms.law.title", "terms.law.body"}} {
		sections = append(sections, "## "+s.pages.translate(lang, section.title)+"\n\n"+s.pages.translate(lang, section.body))
	}
	content = strings.Join(sections, "\n\n")
	return &brandingPageView{Content: content, Format: "markdown", HTML: renderSafeContent("markdown", content)}
}

func normalizeContentFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "md", "markdown":
		return "markdown"
	case "html":
		return "html"
	case "text", "txt":
		return "text"
	default:
		return ""
	}
}

func replaceBrandingVariables(content, buildDate string) string {
	now := time.Now()
	formattedBuildDate := "Unknown"
	stamp, err := time.Parse(time.RFC3339, buildDate)
	if err != nil {
		stamp, err = time.Parse(time.RFC3339, currentBuildInfo().Date)
	}
	if err == nil {
		formattedBuildDate = stamp.Local().Format("January 02, 2006 at 15:04:05 MST")
	}
	return strings.NewReplacer(
		"{current_year}", fmt.Sprint(now.Year()),
		"{project_name}", "mail",
		"{project_org}", "apimgr",
		"{project_version}", applicationVersion(),
		"{build_datetime}", formattedBuildDate,
		"{onion_address}", "",
		"{i2p_address}", "",
		"{app_name}", "Mail",
	).Replace(content)
}

func (s *Server) operatorBody(r *http.Request, name string) (string, string, bool) {
	var content config.BrandingPageContentConfig
	switch name {
	case "about":
		content = s.cfg.Server.Branding.Content.About
	case "header":
		content = s.cfg.Server.Branding.Content.Header
	case "privacy":
		content = s.cfg.Server.Branding.Content.Privacy
	case "terms":
		content = s.cfg.Server.Branding.Content.Terms
	case "footer":
		content = s.cfg.Server.Branding.Content.Footer
	default:
		return "", "", false
	}
	format := normalizeContentFormat(content.Format)
	contentFormat := strings.TrimSpace(content.Format)
	if name == "header" || name == "footer" {
		if strings.EqualFold(contentFormat, "html") {
			format = "html"
		} else {
			format = ""
		}
	}
	body := strings.TrimSpace(content.Body.Resolve(s.preferredPageLanguage(r)))
	if body == "" {
		return "", "", false
	}
	if format == "" || len(body) > 256*1024 {
		if _, warned := s.contentWarnings.LoadOrStore(name, struct{}{}); !warned {
			logger.Warnf("invalid yaml.d/branding.yml server.branding.content.%s value; built-in content will be used", name)
		}
		return "", "", false
	}
	return format, replaceBrandingVariables(body, s.buildDate), true
}

func (s *Server) operatorHTML(r *http.Request, name string) template.HTML {
	format, body, usable := s.operatorBody(r, name)
	if !usable {
		return ""
	}
	return renderSafeContentFor(name, format, body)
}

func renderSafeContent(format, body string) template.HTML {
	return renderSafeContentFor("document", format, body)
}

func renderSafeContentFor(name, format, body string) template.HTML {
	var rendered string
	switch format {
	case "html":
		rendered = body
	case "text":
		paragraphs := strings.Split(stdhtml.EscapeString(body), "\n\n")
		for i := range paragraphs {
			paragraphs[i] = "<p>" + strings.ReplaceAll(paragraphs[i], "\n", "<br>") + "</p>"
		}
		rendered = strings.Join(paragraphs, "\n")
	default:
		var output bytes.Buffer
		if err := goldmark.Convert([]byte(demoteMarkdownHeadings(body)), &output); err != nil {
			return ""
		}
		rendered = output.String()
	}
	policy := bluemonday.NewPolicy()
	policy.AllowElements("p", "br", "span", "div", "strong", "b", "em", "i", "u", "s", "small", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "a", "img")
	if name != "header" && name != "footer" {
		policy.AllowElements("table", "thead", "tbody", "tr", "th", "td", "pre", "code", "blockquote", "hr", "dl", "dt", "dd", "sup", "sub")
	}
	policy.AllowAttrs("href", "title", "target", "rel").OnElements("a")
	policy.AllowAttrs("src", "alt", "title", "width", "height").OnElements("img")
	policy.AllowAttrs("class", "id").Globally()
	policy.AllowURLSchemes("https")
	policy.AllowRelativeURLs(true)
	policy.RequireNoReferrerOnLinks(true)
	safe := policy.Sanitize(rendered)
	safe = stripContactFormLinks(safe)
	return template.HTML(safe)
}

var (
	anchorTagPattern = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	hrefAttrPattern  = regexp.MustCompile(`(?is)\bhref\s*=\s*(?:"([^"]*)"|'([^']*)')`)
)

func stripContactFormLinks(content string) string {
	return anchorTagPattern.ReplaceAllStringFunc(content, func(anchor string) string {
		parts := anchorTagPattern.FindStringSubmatch(anchor)
		if len(parts) != 3 {
			return anchor
		}
		href := hrefAttrPattern.FindStringSubmatch(parts[1])
		if len(href) != 3 {
			return anchor
		}
		target := href[1]
		if target == "" {
			target = href[2]
		}
		parsed, err := url.Parse(stdhtml.UnescapeString(target))
		if err == nil && strings.TrimSuffix(parsed.Path, "/") == "/server/contact" {
			return parts[2]
		}
		return anchor
	})
}

func demoteMarkdownHeadings(markdown string) string {
	lines := strings.Split(markdown, "\n")
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		prefix := len(line) - len(strings.TrimLeft(line, "#"))
		if prefix > 0 && prefix <= 5 && len(line) > prefix && (line[prefix] == ' ' || line[prefix] == '\t') {
			lines[i] = "#" + line
		}
	}
	return strings.Join(lines, "\n")
}

func (s *Server) preferredPageLanguage(r *http.Request) string {
	if requested := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang"))); s.pages.catalog.IsSupported(requested) {
		return requested
	}
	if hasPreferencesConsent(r) {
		if cookie, err := r.Cookie("lang"); err == nil && s.pages.catalog.IsSupported(cookie.Value) {
			return cookie.Value
		}
	}
	return s.pages.catalog.MatchAcceptLanguage(r.Header.Get("Accept-Language"))
}

func (s *Server) handleDPOPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, http.StatusOK, pageContent{TitleKey: "page.dpo.title", DescriptionKey: "page.dpo.lead", DPO: &dpoPageView{}, OfflineCachePublic: true})
}

func (s *Server) handleSecurityPage(w http.ResponseWriter, r *http.Request) {
	links := make([]pageLink, 0, 4)
	if reportURL := s.securityReportURL(); reportURL != "" {
		links = append(links, pageLink{Path: reportURL, LabelKey: "contact.link.github"})
	}
	if s.contactFormEnabled() {
		if securityID, err := s.securityID(r.Context(), time.Now().UTC()); err == nil {
			links = append(links, pageLink{Path: "/server/contact?security_id=" + securityID, LabelKey: "security.reporting.form"})
		}
	}
	if email := s.publicSecurityEmail(r); email != "" {
		links = append(links, pageLink{Path: "mailto:" + email, LabelKey: "security.report.email"})
	}
	otherReportLinks := []pageLink{}
	if s.contactFormEnabled() {
		otherReportLinks = append(otherReportLinks, pageLink{Path: "/server/contact", LabelKey: "page.contact.title"})
	}
	view := pageContent{TitleKey: "page.security.title", DescriptionKey: "page.security.lead", Sections: []pageSection{
		{ID: "reporting", HeadingKey: "security.reporting.title", BodyKey: "security.reporting.body", Links: links},
		{ID: "other-reports", HeadingKey: "security.other.title", BodyKey: "security.other.body", Links: otherReportLinks},
		{ID: "policy", HeadingKey: "security.policy.title", BodyKey: "security.policy.body", Links: []pageLink{{Path: "/server/security/policy", LabelKey: "security.policy.title"}, {Path: "/server/security/thanks", LabelKey: "security.thanks.title"}, {Path: "/.well-known/security.txt", LabelKey: "security.machine_readable"}}},
	}}
	view.SecurityExpires = time.Now().UTC().AddDate(1, 0, 0).Format(time.RFC3339)
	if fingerprint, ok := s.pgpFingerprint(r.Context()); ok {
		view.SecurityEncryptionURL = s.buildPublicURL(r, "/.well-known/pgp-key.asc")
		view.SecurityFingerprint = fingerprint
	}
	s.renderPage(w, r, http.StatusOK, view)
}

func (s *Server) handleSecurityPolicyPage(w http.ResponseWriter, r *http.Request) {
	policy := s.securityPolicy(r)
	s.renderPage(w, r, http.StatusOK, pageContent{TitleKey: "security.policy.title", DescriptionKey: "page.security.lead", BreadcrumbKey: "page.security.title", BreadcrumbPath: "/server/security", SecurityPolicyHTML: renderSafeContent("markdown", policy), OfflineCachePublic: true})
}

func (s *Server) securityPolicy(r *http.Request) string {
	if policy := strings.TrimSpace(s.cfg.Web.Security.Policy); policy != "" {
		return policy
	}
	resolvedURL, _ := url.Parse(s.buildPublicURL(r, ""))
	host := resolvedURL.Host
	return fmt.Sprintf("# Scope\nThe application served at `%s`, including its API.\n\n# Out of scope\nDenial of service and volumetric testing, social engineering, physical attacks, findings in third-party services or dependencies without an exploitable path here, automated scanner output without demonstrated impact, and missing best-practice headers on non-sensitive pages.\n\n# Coordinated disclosure\nPlease report privately through the security page and allow 90 days from the report to publish. The window ends early once a fix is released and may be extended by agreement.\n\n# Safe harbor\nGood-faith research that follows this policy, avoids privacy violations and service disruption, accesses only what is needed to demonstrate the issue, and stops when it has done so is authorized. No legal action will be taken for such research.\n\n# Reporting\nPlease report vulnerabilities through the security page: %s", host, s.buildPublicURL(r, "/server/security"))
}

func (s *Server) handleSecurityThanksPage(w http.ResponseWriter, r *http.Request) {
	var items []database.SecurityThanks
	if s.db != nil {
		var err error
		items, err = s.db.ListSecurityThanks(r.Context())
		if err != nil {
			items = []database.SecurityThanks{}
		}
	}
	s.renderPage(w, r, http.StatusOK, pageContent{TitleKey: "security.thanks.title", DescriptionKey: "page.security.lead", BreadcrumbKey: "page.security.title", BreadcrumbPath: "/server/security", SecurityThanks: items})
}

func (s *Server) handlePreferencesPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, http.StatusOK, s.preferencesContent(r, ""))
}

func (s *Server) preferencesContent(r *http.Request, errorKey string) pageContent {
	automatic := true
	if cookie, err := r.Cookie("lang"); err == nil && hasPreferencesConsent(r) && s.pages.catalog.IsSupported(cookie.Value) {
		automatic = false
	}
	return pageContent{TitleKey: "nav.preferences", DescriptionKey: "preferences.lead", Preferences: true, AutomaticLang: automatic, ErrorKey: errorKey, CancelTarget: safePreferencesRedirect(r, "/"), MorePreferences: registeredPreferencePages()}
}

func (s *Server) handlePreferencesUpdate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	_ = r.ParseForm()
	if r.PostForm.Get("action") == "reset" {
		s.resetPreferences(w, r)
		return
	}
	lang := strings.ToLower(strings.TrimSpace(r.PostForm.Get("lang")))
	theme := strings.TrimSpace(r.PostForm.Get("theme"))
	validCount := 0
	if theme == "auto" || theme == "light" || theme == "dark" {
		validCount++
	}
	if lang == "auto" || s.pages.catalog.IsSupported(lang) {
		validCount++
	}
	allowed := false
	if consentCookie, err := r.Cookie("cookie_consent"); err == nil {
		allowed = consentAllowsPreferences(consentCookie.Value)
	}
	if r.PostForm.Get("consent_form") == "1" {
		allowed = r.PostForm.Has("consent_preferences")
	}
	if allowed {
		if theme == "auto" || theme == "light" || theme == "dark" {
			s.setPreferenceCookie(w, r, "theme", theme)
		}
		if lang == "auto" {
			s.expireCookie(w, r, "lang")
		} else if s.pages.catalog.IsSupported(lang) {
			s.setPreferenceCookie(w, r, "lang", lang)
		}
	}
	if r.PostForm.Get("consent_form") == "1" {
		state := consentState{Essential: true, Preferences: r.PostForm.Has("consent_preferences"), Analytics: r.PostForm.Has("consent_analytics"), Timestamp: time.Now().Unix()}
		s.writeConsentCookie(w, r, state)
		if !state.Preferences {
			s.clearPreferenceCookies(w, r)
		}
		if s.cfg.Server.Privacy.Data.Sold {
			if r.PostForm.Has("ccpa_opt_out") {
				s.setPreferenceCookie(w, r, "ccpa_opt_out", "true")
			} else {
				s.expireCookie(w, r, "ccpa_opt_out")
			}
		}
	}
	target := safePreferencesRedirect(r, "/server/preferences")
	if validCount > 0 && !allowed && r.PostForm.Get("consent_form") != "1" {
		target = addQueryFlag(target, "consent_required", "1")
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) handleConsentUpdate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	state := consentState{Essential: true, Timestamp: time.Now().Unix()}
	if r.PostForm.Get("choice") == "accept" {
		state.Preferences = s.cfg.Server.Privacy.Cookies.Preferences.Enabled
		state.Analytics = s.cfg.Server.Privacy.Cookies.Analytics.Enabled
	}
	s.writeConsentCookie(w, r, state)
	if !state.Preferences {
		s.clearPreferenceCookies(w, r)
	}
	http.Redirect(w, r, safePreferencesRedirect(r, "/"), http.StatusSeeOther)
}

func (s *Server) handleCCPAUpdate(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Server.Privacy.Data.Sold {
		_ = r.ParseForm()
		if r.PostForm.Get("choice") == "opt-out" || r.PostForm.Has("ccpa_opt_out") {
			s.setPreferenceCookie(w, r, "ccpa_opt_out", "true")
		} else {
			s.expireCookie(w, r, "ccpa_opt_out")
		}
	}
	http.Redirect(w, r, safePreferencesRedirect(r, "/server/privacy"), http.StatusSeeOther)
}

func consentAllowsPreferences(raw string) bool {
	var state consentState
	return decodeConsentCookie(raw, &state) && state.Preferences
}

func (s *Server) writeConsentCookie(w http.ResponseWriter, r *http.Request, state consentState) {
	b, _ := json.Marshal(state)
	http.SetCookie(w, &http.Cookie{Name: "cookie_consent", Value: url.PathEscape(string(b)), Path: "/", MaxAge: 31536000, SameSite: http.SameSiteLaxMode, Secure: s.requestIsHTTPS(r)})
}

func (s *Server) setPreferenceCookie(w http.ResponseWriter, r *http.Request, name, value string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: 31536000, SameSite: http.SameSiteLaxMode, Secure: s.requestIsHTTPS(r)})
}

func (s *Server) expireCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, SameSite: http.SameSiteLaxMode, Secure: s.requestIsHTTPS(r)})
}

func safePreferencesRedirect(r *http.Request, fallback string) string {
	rawRedirect := r.PostForm.Get("redirect")
	if rawRedirect == "" {
		rawRedirect = r.URL.Query().Get("redirect")
	}
	if rawRedirect != "" {
		if target, ok := safeRelativeRedirect(rawRedirect); ok && !isPreferencesAction(target) {
			return target
		}
	}
	if raw := r.Referer(); raw != "" {
		if u, e := url.Parse(raw); e == nil && (u.Host == "" || strings.EqualFold(u.Host, r.Host)) {
			target := u.RequestURI()
			if t, ok := safeRelativeRedirect(target); ok && !isPreferencesAction(t) {
				return t
			}
		}
	}
	return fallback
}

func safeRelativeRedirect(raw string) (string, bool) {
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "/\\") || strings.ContainsAny(raw, "\r\n") {
		return "", false
	}
	u, e := url.ParseRequestURI(raw)
	if e != nil || u.IsAbs() || u.Host != "" {
		return "", false
	}
	return u.RequestURI(), true
}

func isPreferencesAction(target string) bool {
	path := strings.SplitN(target, "?", 2)[0]
	return path == "/server/preferences" || path == "/server/preferences/sync" || path == "/server/preferences/sync/import"
}

func addQueryFlag(target, key, value string) string {
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	return target + sep + key + "=" + value
}

func (s *Server) resetPreferences(w http.ResponseWriter, r *http.Request) {
	deleted := map[string]bool{}
	for _, name := range []string{"theme", "lang", "cookie_consent", "ccpa_opt_out", "mail_build"} {
		s.expireCookie(w, r, name)
		deleted[name] = true
	}
	for _, cookie := range r.Cookies() {
		if !deleted[cookie.Name] && !isCredentialCookie(cookie.Name) {
			s.expireCookie(w, r, cookie.Name)
		}
	}
	w.Header().Set("Clear-Site-Data", `"storage"`)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) clearPreferenceCookies(w http.ResponseWriter, r *http.Request) {
	s.expireCookie(w, r, "theme")
	s.expireCookie(w, r, "lang")
}

func hasPreferencesConsent(r *http.Request) bool {
	c, e := r.Cookie("cookie_consent")
	return e == nil && consentAllowsPreferences(c.Value)
}

func isCredentialCookie(name string) bool {
	name = strings.ToLower(name)
	return name == csrfCookieName || strings.Contains(name, "session") || strings.Contains(name, "token") || strings.HasSuffix(name, "_auth")
}

func (s *Server) handleSwaggerPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, http.StatusOK, pageContent{TitleKey: "page.swagger.title", DescriptionKey: "page.swagger.lead", SwaggerEndpoint: api.APIBasePath() + "/server/swagger", OfflineCachePublic: true})
}

func (s *Server) handleGraphQLPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, http.StatusOK, pageContent{
		TitleKey: "page.graphql.title", DescriptionKey: "page.graphql.lead",
		GraphQLEndpoint: api.APIBasePath() + "/server/graphql", OfflineCachePublic: true,
	})
}

func (s *Server) handleHealthPage(w http.ResponseWriter, r *http.Request) {
	health := s.buildHealthResponse(r)
	status := healthHTTPStatus(health.Status)
	language := s.preferredPageLanguage(r)
	formatDuration := func(value time.Duration) string { return commonformat.Duration(value, language, s.pages.catalog) }
	accept := strings.ToLower(r.Header.Get("Accept"))
	if strings.Contains(accept, "application/json") {
		writeJSON(w, status, health)
		return
	}
	if prefersHTML(r) {
		s.renderPage(w, r, status, pageContent{TitleKey: "page.health.title", DescriptionKey: "page.home.lead", Status: true, Healthy: health.Status == "healthy", Health: &health, Version: applicationVersion(), Platform: "server", UptimeHuman: formatDuration(time.Since(processStartedAt)), OfflineCachePublic: true})
		return
	}
	if wantsText(r) || healthFrontendWantsText(r) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		translate := func(key string) string { return s.pages.catalog.Translate(language, key) }
		formatTime := func(value string) string { return humanTimestamp(language, value, s.pages.catalog) }
		writeHealthFrontendText(w, health, language, translate, formatTime, formatDuration)
		return
	}
	s.renderPage(w, r, status, pageContent{TitleKey: "page.health.title", DescriptionKey: "page.home.lead", Status: true, Healthy: health.Status == "healthy", Health: &health, Version: applicationVersion(), Platform: "server", UptimeHuman: formatDuration(time.Since(processStartedAt)), OfflineCachePublic: true})
}

func healthFrontendWantsText(r *http.Request) bool {
	return isNonInteractiveClient(r)
}

func (s *Server) handleStaticAsset(w http.ResponseWriter, r *http.Request) {
	assetPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/static/")
	if assetPath == "." || strings.HasPrefix(assetPath, "../") || !fs.ValidPath(assetPath) {
		s.handleNotFound(w, r)
		return
	}
	data, err := fs.ReadFile(webAssets, "web/static/"+assetPath)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	contentType := http.DetectContentType(data)
	if strings.HasSuffix(assetPath, ".css") {
		contentType = "text/css; charset=utf-8"
	} else if strings.HasSuffix(assetPath, ".js") {
		contentType = "text/javascript; charset=utf-8"
	} else if strings.HasSuffix(assetPath, ".svg") {
		contentType = "image/svg+xml"
	}
	w.Header().Set("Content-Type", contentType)
	stamp := applicationBuildStamp()
	w.Header().Set("ETag", fmt.Sprintf("%q", stamp))
	if r.URL.Query().Get("v") == stamp {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	s.serveEmbeddedFile(w, r, "web/manifest.json", "application/manifest+json; charset=utf-8", true)
}

func (s *Server) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(webAssets, "web/sw.js.tmpl")
	if err != nil {
		renderFallbackError(w, r, http.StatusInternalServerError)
		return
	}
	stamp := applicationBuildStamp()
	appNameJSON, _ := json.Marshal("mail")
	stampJSON, _ := json.Marshal(stamp)
	tmpl, err := texttemplate.New("worker").Parse(string(data))
	if err != nil {
		renderFallbackError(w, r, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", fmt.Sprintf("%q", stamp))
	if err := tmpl.Execute(w, map[string]string{
		"AppName": string(appNameJSON), "Stamp": string(stampJSON), "AssetStamp": url.QueryEscape(stamp),
	}); err != nil {
		renderFallbackError(w, r, http.StatusInternalServerError)
	}
}

func (s *Server) serveEmbeddedFile(w http.ResponseWriter, r *http.Request, name, contentType string, noCache bool) {
	data, err := fs.ReadFile(webAssets, name)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("ETag", fmt.Sprintf("%q", applicationBuildStamp()))
	if noCache {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func applicationBuildStamp() string {
	version := applicationVersion()
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				revision := setting.Value
				if len(revision) > 7 {
					revision = revision[:7]
				}
				return version + "-" + revision
			}
		}
	}
	return version
}

func formatBuildDate(value, lang string, catalog *i18n.Catalog) string {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return catalog.Translate(lang, "health.unavailable")
	}
	return commonformat.Date(parsed.UTC(), lang, catalog)
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, http.StatusNotFound, pageContent{TitleKey: "page.error.title", DescriptionKey: "page.error.lead", Sections: []pageSection{{ID: "not-found", HeadingKey: "error.home.title", BodyKey: "page.error.lead"}}})
}

func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, status int, page pageContent) {
	if s.pages == nil || s.pages.parseErr != nil {
		renderFallbackError(w, r, http.StatusInternalServerError)
		return
	}
	page.Lang = s.pageLanguage(w, r)
	if page.NoIndex {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	}
	language := s.pages.catalog.Language(page.Lang)
	page.LanguageName = language.Native
	page.AutomaticLanguageName = s.pages.catalog.Language(s.pages.catalog.MatchAcceptLanguage(r.Header.Get("Accept-Language"))).Native
	page.Languages = i18n.Languages()
	page.Dir = language.Direction
	page.Theme = "dark"
	if hasPreferencesConsent(r) {
		if cookie, err := r.Cookie("theme"); err == nil {
			switch cookie.Value {
			case "light":
				page.Theme = "light"
			case "auto":
				page.Theme = ""
			}
		}
	}
	if page.Theme == "dark" {
		page.Theme = "dark"
	}
	if requested := r.URL.Query().Get("theme"); requested == "auto" {
		page.Theme = ""
	} else if requested == "light" || requested == "dark" {
		page.Theme = requested
	}
	page.Version = applicationVersion()
	page.CSRFToken = csrfTokenFromContext(r.Context())
	page.CurrentPath = r.URL.Path
	page.ConsentRequired = r.URL.Query().Get("consent_required") == "1"
	page.DataSold = s.cfg.Server.Privacy.Data.Sold
	page.ConsentMessage = s.cfg.Server.Privacy.Consent.Message
	if page.DataSold {
		page.ConsentMessage = s.cfg.Server.Privacy.Consent.MessageIfSold
	}
	page.ConsentPolicyURL = s.cfg.Server.Privacy.Consent.Policy.URL
	page.ConsentPolicyText = s.cfg.Server.Privacy.Consent.Policy.Text
	page.ConsentPreferencesText = s.cfg.Server.Privacy.Consent.PreferencesText
	page.ConsentShowPreferences = s.cfg.Server.Privacy.Consent.ShowPreferences
	page.ConsentPosition = s.cfg.Server.Privacy.Consent.Position
	page.ConsentDecline = s.cfg.Server.Privacy.Consent.Buttons.Decline
	page.ConsentAccept = s.cfg.Server.Privacy.Consent.Buttons.Accept
	page.ConsentPreferences = true
	page.ConsentAnalytics = true
	if cookie, err := r.Cookie("cookie_consent"); err == nil {
		var state consentState
		if consentCookieValid(cookie.Value, &state) {
			page.HasConsentCookie = true
			page.ConsentPreferences = state.Preferences
			page.ConsentAnalytics = state.Analytics
		}
	}
	if cookie, err := r.Cookie("ccpa_opt_out"); err == nil && cookie.Value == "true" {
		page.CCPAOptOut = true
	}
	page.ThemeNext = "light"
	if page.Theme == "light" {
		page.ThemeNext = "dark"
	}
	page.Canonical = s.buildPublicURL(r, r.URL.Path)
	page.HeaderHTML = s.operatorHTML(r, "header")
	page.FooterHubs = registeredHubs()
	footerConfig := s.cfg.Server.Branding.Content.Footer
	footerFormat := normalizeContentFormat(footerConfig.Format)
	footerInvalid := strings.TrimSpace(footerConfig.Format) != "" && (footerFormat == "" || !strings.EqualFold(strings.TrimSpace(footerConfig.Format), "html"))
	if strings.TrimSpace(footerConfig.Format) == "" || footerInvalid {
		page.FooterBrandingHTML = renderSafeContentFor("footer", "html", `<p><strong>Mail</strong> · Self-hosted mail infrastructure management</p>`)
	} else {
		page.FooterBrandingHTML = s.operatorHTML(r, "footer")
	}
	if page.DPO != nil {
		page.DPO.Name = strings.TrimSpace(s.cfg.Server.Contact.DPO.Name)
		page.DPO.Email = strings.TrimSpace(s.cfg.Server.Contact.DPO.Email)
		if page.DPO.Email == "" {
			page.DPO.Email = strings.TrimSpace(s.cfg.Server.Contact.General.Email)
		}
		page.DPO.PrivacyURL = "/server/privacy"
		page.DPO.SecurityURL = "/server/security"
	}
	if page.Privacy != nil {
		view := page.Privacy
		privacy := s.cfg.Server.Privacy
		view.LastUpdated = formatBuildDate(s.buildDate, page.Lang, s.pages.catalog)
		view.DataSold = privacy.Data.Sold
		view.StoredOnServer = privacy.Data.StoredOnServer
		view.Essential = privacyCookie{Enabled: privacy.Cookies.Essential.Enabled, Description: privacy.Cookies.Essential.Description}
		view.Preferences = privacyCookie{Enabled: privacy.Cookies.Preferences.Enabled, Description: privacy.Cookies.Preferences.Description}
		analyticsDescription := privacy.Cookies.Analytics.Description
		if view.DataSold {
			analyticsDescription = strings.TrimSpace(analyticsDescription + " " + privacy.Cookies.Analytics.DescriptionSuffixSold)
		} else {
			analyticsDescription = strings.TrimSpace(analyticsDescription + " " + privacy.Cookies.Analytics.DescriptionSuffixNotSold)
		}
		view.Analytics = privacyCookie{Enabled: privacy.Cookies.Analytics.Enabled, Description: analyticsDescription}
		view.TrackingTypeName = trackingTypeName(strings.ToLower(strings.TrimSpace(s.cfg.Server.Tracking.Type)))
		view.Sharing = make([]privacySharing, 0, len(privacy.Data.Sharing))
		for _, item := range privacy.Data.Sharing {
			view.Sharing = append(view.Sharing, privacySharing{Condition: item.Condition, When: item.When, Data: item.Data})
		}
		view.Retention = privacy.Retention.Period
		view.ExportAvailable = privacy.Retention.ExportAvailable
		view.DeleteAvailable = privacy.Retention.DeletionAvailable
		view.Services = s.privacyServices()
		view.CCPAOptOut = page.CCPAOptOut
		view.CSRFToken = page.CSRFToken
		view.TLS = s.requestIsHTTPS(r)
		language := s.preferredPageLanguage(r)
		view.DataCollection = renderSafeContent("markdown", s.pages.translate(language, "privacy.data_collection"))
		view.DataUsage = renderSafeContent("markdown", s.pages.translate(language, "privacy.data_usage"))
		if view.DataSold {
			view.DataUsage = renderSafeContent("markdown", s.pages.translate(language, "privacy.data_usage.sold"))
		}
		view.DataSecurity = renderSafeContent("markdown", s.pages.translate(language, "privacy.data_security"))
		view.DPOName = strings.TrimSpace(s.cfg.Server.Contact.DPO.Name)
		view.ExtensionHTML = s.operatorHTML(r, "privacy")
		view.DPOEmail = strings.TrimSpace(s.cfg.Server.Contact.DPO.Email)
		if view.DPOEmail == "" {
			view.DPOEmail = strings.TrimSpace(s.cfg.Server.Contact.General.Email)
		}
		if !view.TLS {
			view.DataSecurity += renderSafeContent("markdown", "\n\n"+s.pages.translate(language, "privacy.data_security.no_tls"))
		}
	}
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": status < 400, "data": map[string]any{"title": s.pages.translate(page.Lang, page.TitleKey), "description": s.pages.translate(page.Lang, page.DescriptionKey)}})
		return
	}
	if wantsText(r) && !prefersHTML(r) || isNonInteractiveClient(r) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, "%s\n%s\n", s.pages.translate(page.Lang, page.TitleKey), s.pages.translate(page.Lang, page.DescriptionKey))
		return
	}
	if offlinePageEligible(r, page, status) {
		w.Header().Set("X-Offline-Cache", "public")
		w.Header().Add("Vary", "Accept-Language")
	}
	s.applyBuildStamp(w, r, status)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", fmt.Sprintf("%q", applicationVersion()))
	w.WriteHeader(status)
	if err := s.pages.templates.ExecuteTemplate(w, "public", page); err != nil {
		renderFallbackError(w, r, http.StatusInternalServerError)
	}
}

func (s *Server) applyBuildStamp(w http.ResponseWriter, r *http.Request, status int) {
	if r.Method != http.MethodGet || status < http.StatusOK || status >= http.StatusMultipleChoices {
		return
	}
	destination := r.Header.Get("Sec-Fetch-Dest")
	mode := r.Header.Get("Sec-Fetch-Mode")
	if destination != "" || mode != "" {
		if destination != "document" || mode != "navigate" {
			return
		}
	}
	stamp := applicationBuildStamp()
	cookie, err := r.Cookie("mail_build")
	if err == nil && cookie.Value != stamp {
		w.Header().Set("Clear-Site-Data", `"cache", "storage"`)
	}
	if err != nil || cookie.Value != stamp {
		http.SetCookie(w, &http.Cookie{Name: "mail_build", Value: stamp, Path: "/", MaxAge: 31536000, SameSite: http.SameSiteLaxMode, Secure: s.requestIsHTTPS(r)})
	}
}

func offlinePageEligible(r *http.Request, page pageContent, status int) bool {
	if !page.OfflineCachePublic || status < http.StatusOK || status >= http.StatusMultipleChoices ||
		r.Method != http.MethodGet || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" ||
		page.Lang != "en" || page.Theme != "dark" || page.HasConsentCookie || page.Privacy != nil ||
		page.Preferences || page.Tokens != nil || page.ContactForm != nil || page.SecurityReview != nil || page.Sync {
		return false
	}
	for _, cookie := range r.Cookies() {
		if cookie.Name != csrfCookieName && cookie.Name != "mail_build" {
			return false
		}
	}
	return true
}

func consentCookieValid(raw string, state *consentState) bool {
	return decodeConsentCookie(raw, state)
}

func decodeConsentCookie(raw string, state *consentState) bool {
	decoded, err := url.PathUnescape(raw)
	if err != nil || decoded == raw {
		return false
	}
	return json.Unmarshal([]byte(decoded), state) == nil && state.Essential && state.Timestamp > 0
}

func (s *Server) pageLanguage(w http.ResponseWriter, r *http.Request) string {
	if requested := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang"))); requested != "" {
		if s.pages.catalog.IsSupported(requested) {
			if cookie, err := r.Cookie("cookie_consent"); err == nil && consentAllowsPreferences(cookie.Value) {
				s.setPreferenceCookie(w, r, "lang", requested)
			}
			return requested
		}
	}
	if hasPreferencesConsent(r) {
		if cookie, err := r.Cookie("lang"); err == nil && s.pages.catalog.IsSupported(cookie.Value) {
			return cookie.Value
		}
	}
	return s.pages.catalog.MatchAcceptLanguage(r.Header.Get("Accept-Language"))
}

func prefersHTML(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html")
}

func isNonInteractiveClient(r *http.Request) bool {
	if strings.TrimSpace(r.Header.Get("Accept")) != "" {
		return false
	}
	userAgent := strings.ToLower(r.UserAgent())
	if userAgent == "" {
		return true
	}
	for _, agent := range []string{"curl/", "wget/", "httpie/", "go-http-client", "python-requests", "lynx", "w3m", "links/"} {
		if strings.Contains(userAgent, agent) {
			return true
		}
	}
	return false
}

func wantsJSON(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "application/json")
}

func wantsText(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/plain")
}

func renderFallbackError(w http.ResponseWriter, r *http.Request, status int) {
	w.Header().Set("Cache-Control", "no-store")
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `{"ok":false,"error":{"message":%q,"status":%d}}`, http.StatusText(status), status)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "<!doctype html><html lang=\"en\"><title>%d %s</title><h1>%d %s</h1></html>", status, http.StatusText(status), status, http.StatusText(status))
}
