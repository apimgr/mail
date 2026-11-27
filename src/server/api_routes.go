package server

import (
	"net/http"
	"strings"

	"github.com/apimgr/mail/src/common/api"
	"github.com/apimgr/mail/src/mailservice"
	"github.com/go-chi/chi/v5"
)

// Route is the single API route contract used for registration and OpenAPI.
type Route struct {
	Method             string
	Path               string
	OperationID        string
	Summary            string
	Auth               string
	Listed             bool
	Request            any
	RequestContentType string
	Response           any
	Parameters         []RouteParameter
	Primary            string
	Message            string
	Binary             string
	SuccessStatus      int
	Aliases            []string
	Handler            http.HandlerFunc
}

// RouteParameter declares an OpenAPI path or query parameter.
type RouteParameter struct {
	Name        string
	In          string
	Description string
	Required    bool
	Type        string
	Default     any
}

type apiResponseContract struct {
	OK      bool   `json:"ok"`
	Data    any    `json:"data"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

type apiDataResponse[T any] struct {
	OK      bool   `json:"ok"`
	Data    T      `json:"data"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

type preferencesAPIRequest struct {
	Theme *string `json:"theme,omitempty"`
	Lang  *string `json:"lang,omitempty"`
}

type authCheckResponse struct {
	Kind string `json:"kind"`
}

type RouteRegistry struct {
	routes []Route
}

func (registry *RouteRegistry) Add(route Route) {
	if route.Method == "" || route.Path == "" || route.OperationID == "" || route.Summary == "" || route.Handler == nil {
		panic("API route is missing required metadata")
	}
	if route.Auth == "" {
		route.Auth = "none"
	}
	if route.Auth != "none" && route.Auth != "owner" && route.Auth != "operator" && route.Auth != "token" && route.Auth != "service" {
		panic("API route has an invalid auth tier: " + route.Auth)
	}
	if route.Auth == "operator" || route.Auth == "service" {
		route.Listed = false
	}
	if route.Response == nil {
		route.Response = apiResponseContract{}
	}
	for _, existing := range registry.routes {
		if existing.Method == route.Method && existing.Path == route.Path {
			panic("duplicate API route: " + route.Method + " " + route.Path)
		}
		if existing.OperationID == route.OperationID {
			panic("duplicate API operation ID: " + route.OperationID)
		}
	}
	registry.routes = append(registry.routes, route)
}

func (registry *RouteRegistry) Routes() []Route {
	return append([]Route(nil), registry.routes...)
}

func (s *Server) apiRouteRegistry() []Route {
	apiPath := func(path string) string { return "/api/{api_version}" + path }
	specs := []Route{
		{Method: http.MethodGet, Path: apiPath("/server/about"), OperationID: "getServerAbout", Summary: "Get public server information", Auth: "none", Listed: true, Handler: s.handleAbout},
		{Method: http.MethodGet, Path: apiPath("/server/version"), OperationID: "getServerVersion", Summary: "Get server version information", Auth: "none", Listed: true, Handler: s.handleVersion},
		{Method: http.MethodGet, Path: apiPath("/server/healthz"), OperationID: "getServerHealth", Summary: "Get server health", Auth: "none", Listed: true, Response: healthResponse{}, Aliases: []string{"/api/healthz"}, Handler: s.handleHealth},
		{Method: http.MethodGet, Path: apiPath("/server/healthz.txt"), OperationID: "getServerHealthText", Summary: "Get server health as plain text", Auth: "none", Listed: true, Response: healthResponse{}, Aliases: []string{"/api/healthz.txt"}, Handler: s.handleHealth},
		{Method: http.MethodGet, Path: apiPath("/server/preferences"), OperationID: "getPreferences", Summary: "Get current browser preferences", Auth: "none", Listed: true, Handler: s.handlePreferencesAPI},
		{Method: http.MethodGet, Path: apiPath("/server/privacy"), OperationID: "getPrivacyInformation", Summary: "Get the privacy notice", Auth: "none", Listed: true, Handler: s.handlePrivacyAPI},
		{Method: http.MethodGet, Path: apiPath("/server/dpo"), OperationID: "getDataProtectionContact", Summary: "Get data protection contact information", Auth: "none", Listed: true, Handler: s.handleDPOAPI},
		{Method: http.MethodGet, Path: apiPath("/server/security"), OperationID: "getSecurityInformation", Summary: "Get security reporting information", Auth: "none", Listed: true, Handler: s.handleSecurityAPI},
		{Method: http.MethodGet, Path: apiPath("/server/security/thanks"), OperationID: "getSecurityAcknowledgments", Summary: "List disclosed security researcher acknowledgments", Auth: "none", Listed: true, Handler: s.handleSecurityThanksAPI},
		{Method: http.MethodGet, Path: apiPath("/server/contact"), OperationID: "getContactForm", Summary: "Get contact report form metadata", Auth: "none", Listed: true, Handler: s.handleSecurityContactAPI},
		{Method: http.MethodPost, Path: apiPath("/server/contact"), OperationID: "submitContactReport", Summary: "Submit a contact or security report", Auth: "none", Request: securityReportRequest{}, Handler: s.handleSecurityContactAPI},
		{Method: http.MethodGet, Path: apiPath("/server/security/policy"), OperationID: "getSecurityPolicy", Summary: "Get the security policy", Auth: "none", Listed: true, Handler: s.handleSecurityPolicyAPI},
		{Method: http.MethodGet, Path: apiPath("/server/help"), OperationID: "getHelp", Summary: "Get help content and endpoint guidance", Auth: "none", Listed: true, Handler: s.handleHelpAPI},
		{Method: http.MethodGet, Path: apiPath("/server/help/mcp"), OperationID: "getMCPHelp", Summary: "Get MCP connection guidance and available tools", Auth: "none", Listed: true, Handler: s.handleMCPHelpAPI},
		{Method: http.MethodGet, Path: apiPath("/server/terms"), OperationID: "getTerms", Summary: "Get the terms of service", Auth: "none", Listed: true, Handler: s.handleTermsAPI},
		{Method: http.MethodGet, Path: apiPath("/server/preferences/sync"), OperationID: "getPreferenceSync", Summary: "Export browser preferences for synchronization", Auth: "none", Listed: true, Handler: s.handlePreferencesSyncAPI},
		{Method: http.MethodGet, Path: apiPath("/server/preferences/sync/import"), OperationID: "getPreferenceSyncImport", Summary: "Import browser preferences from a synchronization code", Auth: "none", Listed: true, Handler: s.handlePreferencesSyncImportAPI},
		{Method: http.MethodPost, Path: apiPath("/server/preferences"), OperationID: "updatePreferences", Summary: "Update browser preferences", Auth: "none", Listed: true, Request: preferencesAPIRequest{}, Handler: s.handlePreferencesAPIPost},
		{Method: http.MethodPost, Path: apiPath("/server/preferences/tokens/primary"), OperationID: "createPrimaryToken", Summary: "Issue an owner token", Auth: "none", Listed: true, SuccessStatus: http.StatusCreated, Handler: s.handleIssuePrimaryToken},
		{Method: http.MethodGet, Path: apiPath("/server/preferences/tokens"), OperationID: "listOwnerTokens", Summary: "List owner tokens", Auth: "owner", Listed: true, Handler: s.handleListOwnerTokens},
		{Method: http.MethodPost, Path: apiPath("/server/preferences/tokens/primary/reveal"), OperationID: "revealPrimaryToken", Summary: "Reveal the primary owner token", Auth: "owner", Listed: true, Handler: s.handleRevealPrimaryToken},
		{Method: http.MethodPost, Path: apiPath("/server/preferences/tokens"), OperationID: "createLinkedToken", Summary: "Create a linked owner token", Auth: "owner", Listed: true, SuccessStatus: http.StatusCreated, Request: linkedTokenRequest{}, Handler: s.handleCreateLinkedToken},
		{Method: http.MethodPost, Path: apiPath("/server/preferences/tokens/{id}/reveal"), OperationID: "revealLinkedToken", Summary: "Reveal a linked owner token", Auth: "owner", Listed: true, Handler: s.handleRevealLinkedToken},
		{Method: http.MethodPost, Path: apiPath("/server/preferences/tokens/{id}/rotate"), OperationID: "rotateLinkedToken", Summary: "Rotate a linked owner token", Auth: "owner", Listed: true, Handler: s.handleRotateLinkedToken},
		{Method: http.MethodPatch, Path: apiPath("/server/preferences/tokens/{id}"), OperationID: "updateLinkedToken", Summary: "Update linked owner token metadata", Auth: "owner", Listed: true, Request: map[string]any{}, Handler: s.handleUpdateLinkedToken},
		{Method: http.MethodDelete, Path: apiPath("/server/preferences/tokens/{id}"), OperationID: "revokeLinkedToken", Summary: "Revoke a linked owner token", Auth: "owner", Listed: true, Handler: s.handleRevokeLinkedToken},
		{Method: http.MethodGet, Path: apiPath("/users"), OperationID: "listMailUsers", Summary: "List virtual mailboxes", Auth: "operator", Response: mailUserListResponse{}, Parameters: mailPaginationParameters(), Handler: s.handleMailUsers},
		{Method: http.MethodGet, Path: apiPath("/users/{id}"), OperationID: "getMailUser", Summary: "Get a virtual mailbox", Auth: "operator", Response: apiDataResponse[mailUserResponse]{}, Handler: s.handleMailUser},
		{Method: http.MethodPost, Path: apiPath("/users"), OperationID: "createMailUser", Summary: "Create a virtual mailbox", Auth: "operator", Request: mailUserCreateRequest{}, Response: apiDataResponse[mailUserResponse]{}, SuccessStatus: http.StatusCreated, Handler: s.handleCreateMailUser},
		{Method: http.MethodPut, Path: apiPath("/users/{id}"), OperationID: "updateMailUser", Summary: "Update virtual mailbox settings", Auth: "operator", Request: mailUserUpdateRequest{}, Response: apiDataResponse[mailUserResponse]{}, Handler: s.handleUpdateMailUser},
		{Method: http.MethodDelete, Path: apiPath("/users/{id}"), OperationID: "deleteMailUser", Summary: "Delete a virtual mailbox", Auth: "operator", Handler: s.handleDeleteMailUser},
		{Method: http.MethodPost, Path: apiPath("/users/{id}/password"), OperationID: "changeMailUserPassword", Summary: "Change a virtual mailbox password", Auth: "operator", Request: mailUserPasswordRequest{}, Response: apiDataResponse[mailUserResponse]{}, Handler: s.handleChangeMailUserPassword},
		{Method: http.MethodPost, Path: apiPath("/users/import"), OperationID: "importMailUsers", Summary: "Import virtual mailboxes from CSV", Auth: "operator", Request: "CSV rows with email,password,quota_bytes,enabled columns", RequestContentType: "text/csv", Response: apiDataResponse[map[string]any]{}, Handler: s.handleImportMailUsers},
		{Method: http.MethodGet, Path: apiPath("/domains"), OperationID: "listMailDomains", Summary: "List virtual mail domains", Auth: "operator", Response: mailDomainListResponse{}, Parameters: mailPaginationParameters(), Handler: s.handleMailDomains},
		{Method: http.MethodGet, Path: apiPath("/domains/{domain}"), OperationID: "getMailDomain", Summary: "Get a virtual mail domain", Auth: "operator", Response: apiDataResponse[mailDomainResponse]{}, Handler: s.handleMailDomain},
		{Method: http.MethodPost, Path: apiPath("/domains"), OperationID: "createMailDomain", Summary: "Create a virtual mail domain", Auth: "operator", Request: mailDomainRequest{}, Response: apiDataResponse[mailDomainResponse]{}, SuccessStatus: http.StatusCreated, Handler: s.handleCreateMailDomain},
		{Method: http.MethodPut, Path: apiPath("/domains/{domain}"), OperationID: "updateMailDomain", Summary: "Update a virtual mail domain", Auth: "operator", Request: mailDomainRequest{}, Response: apiDataResponse[mailDomainResponse]{}, Handler: s.handleUpdateMailDomain},
		{Method: http.MethodDelete, Path: apiPath("/domains/{domain}"), OperationID: "deleteMailDomain", Summary: "Delete a virtual mail domain", Auth: "operator", Handler: s.handleDeleteMailDomain},
		{Method: http.MethodGet, Path: apiPath("/domains/{domain}/dns"), OperationID: "getMailDomainDNS", Summary: "Get DNS guidance for a virtual mail domain", Auth: "operator", Response: apiDataResponse[mailDNSResponse]{}, Handler: s.handleMailDomainDNS},
		{Method: http.MethodGet, Path: apiPath("/domains/{domain}/dkim"), OperationID: "getMailDomainDKIM", Summary: "Get public DKIM records for a virtual mail domain", Auth: "operator", Response: apiDataResponse[[]mailDKIMResponse]{}, Handler: s.handleMailDKIM},
		{Method: http.MethodPost, Path: apiPath("/domains/{domain}/dkim/generate"), OperationID: "generateMailDomainDKIM", Summary: "Generate a DKIM key for a virtual mail domain", Auth: "operator", Response: apiDataResponse[mailDKIMResponse]{}, SuccessStatus: http.StatusCreated, Handler: s.handleGenerateMailDKIM},
		{Method: http.MethodGet, Path: apiPath("/aliases"), OperationID: "listMailAliases", Summary: "List mail aliases", Auth: "operator", Response: mailAliasListResponse{}, Parameters: mailPaginationParameters(), Handler: s.handleMailAliases},
		{Method: http.MethodPost, Path: apiPath("/aliases"), OperationID: "createMailAlias", Summary: "Create a mail alias", Auth: "operator", Request: mailAliasRequest{}, Response: apiDataResponse[mailAliasResponse]{}, SuccessStatus: http.StatusCreated, Handler: s.handleCreateMailAlias},
		{Method: http.MethodDelete, Path: apiPath("/aliases/{id}"), OperationID: "deleteMailAlias", Summary: "Delete a mail alias", Auth: "operator", Response: apiDataResponse[map[string]string]{}, Handler: s.handleDeleteMailAlias},
		{Method: http.MethodGet, Path: apiPath("/forwards"), OperationID: "listMailForwards", Summary: "List mail forwarding rules", Auth: "operator", Response: mailForwardListResponse{}, Parameters: mailPaginationParameters(), Handler: s.handleMailForwards},
		{Method: http.MethodPost, Path: apiPath("/forwards"), OperationID: "createMailForward", Summary: "Create a mail forwarding rule", Auth: "operator", Request: mailForwardRequest{}, Response: apiDataResponse[mailForwardResponse]{}, SuccessStatus: http.StatusCreated, Handler: s.handleCreateMailForward},
		{Method: http.MethodDelete, Path: apiPath("/forwards/{id}"), OperationID: "deleteMailForward", Summary: "Delete a mail forwarding rule", Auth: "operator", Response: apiDataResponse[map[string]string]{}, Handler: s.handleDeleteMailForward},
		{Method: http.MethodGet, Path: apiPath("/queue"), OperationID: "listMailQueue", Summary: "List Postfix queue entries", Auth: "operator", Response: mailQueueListResponse{}, Parameters: mailPaginationParameters(), Handler: s.handleMailQueue},
		{Method: http.MethodGet, Path: apiPath("/queue/{id}"), OperationID: "getMailQueueEntry", Summary: "Get a Postfix queue entry", Auth: "operator", Response: apiDataResponse[mailservice.QueueEntry]{}, Handler: s.handleMailQueueEntry},
		{Method: http.MethodPost, Path: apiPath("/queue/{id}/flush"), OperationID: "flushMailQueueEntry", Summary: "Retry delivery of a queued message", Auth: "operator", Response: apiDataResponse[map[string]string]{}, Handler: s.handleMailQueueAction},
		{Method: http.MethodPost, Path: apiPath("/queue/{id}/hold"), OperationID: "holdMailQueueEntry", Summary: "Hold a queued message", Auth: "operator", Response: apiDataResponse[map[string]string]{}, Handler: s.handleMailQueueAction},
		{Method: http.MethodPost, Path: apiPath("/queue/{id}/release"), OperationID: "releaseMailQueueEntry", Summary: "Release a held message", Auth: "operator", Response: apiDataResponse[map[string]string]{}, Handler: s.handleMailQueueAction},
		{Method: http.MethodDelete, Path: apiPath("/queue/{id}"), OperationID: "deleteMailQueueEntry", Summary: "Delete a queued message", Auth: "operator", Response: apiDataResponse[map[string]string]{}, Handler: s.handleDeleteMailQueueEntry},
		{Method: http.MethodPost, Path: apiPath("/queue/flush-all"), OperationID: "flushAllMailQueue", Summary: "Request delivery for all queued messages", Auth: "operator", Response: apiDataResponse[map[string]string]{}, Handler: s.handleMailQueueAction},
		{Method: http.MethodGet, Path: apiPath("/stats/delivery"), OperationID: "getMailDeliveryStats", Summary: "Get mail delivery counts by status", Auth: "operator", Response: apiDataResponse[mailDeliveryStatsResponse]{}, Parameters: []RouteParameter{mailStatsPeriodParameter()}, Handler: s.handleMailDeliveryStats},
		{Method: http.MethodGet, Path: apiPath("/stats/spam"), OperationID: "getMailSpamStats", Summary: "Get spam and virus detection counts", Auth: "operator", Response: apiDataResponse[mailSpamStatsResponse]{}, Parameters: []RouteParameter{mailStatsPeriodParameter()}, Handler: s.handleMailSpamStats},
		{Method: http.MethodGet, Path: apiPath("/stats/queue"), OperationID: "getMailQueueStats", Summary: "Get queued message counts by arrival period", Auth: "operator", Response: apiDataResponse[mailQueueStatsResponse]{}, Parameters: []RouteParameter{mailStatsPeriodParameter()}, Handler: s.handleMailQueueStats},
		{Method: http.MethodGet, Path: apiPath("/server/auth/check"), OperationID: "checkBearerToken", Summary: "Check bearer token validity and kind", Auth: "token", Listed: true, Response: authCheckResponse{}, Handler: s.handleAuthCheck},
		{Method: http.MethodGet, Path: apiPath("/services"), OperationID: "listMailServices", Summary: "List detected mail services", Auth: "operator", Handler: s.handleServices},
		{Method: http.MethodGet, Path: apiPath("/services/{service}"), OperationID: "getMailService", Summary: "Get detected mail service status", Auth: "operator", Handler: s.handleService},
		{Method: http.MethodGet, Path: apiPath("/health"), OperationID: "getMailHealth", Summary: "Get mail system health", Auth: "operator", Handler: s.handleMailHealth},
	}
	registry := &RouteRegistry{}
	for _, route := range specs {
		registry.Add(route)
	}
	for _, action := range []string{"start", "stop", "restart", "reload"} {
		registry.Add(Route{
			Method: http.MethodPost, Path: apiPath("/services/{service}/" + action),
			OperationID: action + "MailService", Summary: strings.ToUpper(action[:1]) + action[1:] + " a mail service",
			Auth: "operator", Handler: s.handleServiceAction,
		})
	}
	registry.Add(Route{
		Method: http.MethodGet, Path: apiPath("/server/swagger"), OperationID: "getOpenAPISpec",
		Summary: "Get the OpenAPI specification", Auth: "none", Listed: true,
		Aliases: []string{"/api/swagger"}, Handler: s.handleOpenAPISpec,
	})
	registry.Add(Route{
		Method: http.MethodPost, Path: apiPath("/server/mcp"), OperationID: "mcpEndpoint",
		Summary: "Use the API through Model Context Protocol", Auth: "token", Listed: false,
		Handler: s.handleMCP,
		Aliases: []string{"/api/mcp", "/mcp"},
	})
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		operationID := "executeGraphQLQuery"
		if method == http.MethodGet {
			operationID = "executeGraphQLQueryGet"
		}
		route := Route{
			Method: method, Path: apiPath("/server/graphql"), OperationID: operationID,
			Summary: "Execute a read-only GraphQL query", Auth: "operator", Response: graphQLAPIResponse{},
			Aliases: []string{"/api/graphql"}, Handler: s.handleGraphQL,
		}
		if method == http.MethodPost {
			route.Request = graphQLRequest{}
		} else {
			route.Parameters = []RouteParameter{
				{Name: "query", In: "query", Description: "A GraphQL query document", Type: "string"},
				{Name: "variables", In: "query", Description: "JSON object of query variables", Type: "string"},
				{Name: "operationName", In: "query", Description: "Name of the operation to execute", Type: "string"},
			}
		}
		registry.Add(route)
	}
	return registry.Routes()
}

func routeMountPath(routePath string) string {
	return strings.Replace(routePath, "/api/{api_version}", api.APIBasePath(), 1)
}

func mailPaginationParameters() []RouteParameter {
	return []RouteParameter{
		{Name: "page", In: "query", Description: "Page number, starting at 1", Type: "integer", Default: 1},
		{Name: "limit", In: "query", Description: "Items per page, from 1 to 250", Type: "integer", Default: 250},
	}
}

func (s *Server) mountAPIRoute(router chi.Router, route Route, path string) {
	handler := http.Handler(route.Handler)
	if route.Auth == "operator" {
		handler = s.requireOperatorToken(handler)
	}
	if route.OperationID == "mcpEndpoint" {
		router.Handle(path, handler)
		return
	}
	router.Method(route.Method, path, handler)
}
