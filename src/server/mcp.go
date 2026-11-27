package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
)

const mcpProtocolVersion = "2026-07-28"

type mcpRequest struct {
	JSONRPC string                     `json:"jsonrpc"`
	ID      json.RawMessage            `json:"id"`
	Method  string                     `json:"method"`
	Params  json.RawMessage            `json:"params"`
	Meta    map[string]json.RawMessage `json:"_meta"`
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !s.mcpOriginAllowed(r, origin) {
		s.mcpFailure(w, http.StatusForbidden, nil, -32003, "Origin not allowed")
		return
	}
	valid, err := s.hasValidBearerToken(r)
	if err != nil {
		s.mcpFailure(w, http.StatusServiceUnavailable, nil, -32603, "Token service unavailable")
		return
	}
	if !valid {
		w.Header().Set("WWW-Authenticate", `Bearer realm="mail"`)
		s.mcpFailure(w, http.StatusUnauthorized, nil, -32001, "Invalid or missing bearer token")
		return
	}
	var request mcpRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(&request); err != nil {
		s.mcpFailure(w, http.StatusBadRequest, nil, -32700, "Invalid JSON")
		return
	}
	if request.JSONRPC != "2.0" {
		s.mcpFailure(w, http.StatusBadRequest, request.ID, -32600, "Invalid JSON-RPC request")
		return
	}
	if request.Method == "initialize" || r.Header.Get("MCP-Protocol-Version") != mcpProtocolVersion {
		s.mcpFailure(w, http.StatusBadRequest, request.ID, -32010, "Unsupported protocol version; supported version: "+mcpProtocolVersion)
		return
	}
	var bodyProtocolVersion string
	_ = json.Unmarshal(request.Meta["io.modelcontextprotocol/protocolVersion"], &bodyProtocolVersion)
	if bodyProtocolVersion != mcpProtocolVersion || r.Header.Get("Mcp-Method") != request.Method {
		s.mcpFailure(w, http.StatusBadRequest, request.ID, -32020, "MCP header mismatch")
		return
	}
	if request.Method != "tools/list" && request.Method != "tools/call" {
		s.mcpFailure(w, http.StatusNotFound, request.ID, -32601, "Method not found")
		return
	}
	isOperator := s.isOperatorBearer(r)
	if request.Method == "tools/list" {
		tools := make([]map[string]any, 0)
		for _, route := range s.apiRouteRegistry() {
			if mcpRouteAvailable(route, isOperator) {
				tools = append(tools, mcpTool(route))
			}
		}
		s.mcpSuccess(w, request.ID, map[string]any{"tools": tools})
		return
	}
	var args struct {
		Name      string                     `json:"name"`
		Arguments map[string]json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(request.Params, &args); err != nil || args.Name == "" {
		s.mcpFailure(w, http.StatusBadRequest, request.ID, -32602, "Invalid tool arguments")
		return
	}
	if r.Header.Get("Mcp-Name") != args.Name {
		s.mcpFailure(w, http.StatusBadRequest, request.ID, -32020, "MCP header mismatch")
		return
	}
	for _, route := range s.apiRouteRegistry() {
		if route.OperationID == args.Name && mcpRouteAvailable(route, isOperator) {
			status, headers, body := s.invokeMCPRoute(r, route, args.Arguments)
			var result any
			if json.Unmarshal(body, &result) != nil {
				result = string(body)
			}
			s.mcpSuccess(w, request.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": string(body)}}, "structuredContent": result, "isError": status >= 400, "_meta": map[string]any{"httpStatus": status, "contentType": headers.Get("Content-Type")}})
			return
		}
	}
	s.mcpFailure(w, http.StatusForbidden, request.ID, -32003, "Tool is unavailable for this token")
}

func mcpRouteAvailable(route Route, operator bool) bool {
	return route.OperationID != "mcpEndpoint" && route.Auth != "service" && route.Auth != "owner" &&
		!strings.HasPrefix(route.Path, "/api/{api_version}/server/preferences") &&
		(operator || route.Auth == "none" || route.Auth == "token")
}

func mcpTool(route Route) map[string]any {
	annotations := map[string]any{"readOnlyHint": route.Method == http.MethodGet}
	if route.Method == http.MethodDelete {
		annotations["destructiveHint"] = true
		annotations["idempotentHint"] = true
	}
	if route.Method == http.MethodPut {
		annotations["idempotentHint"] = true
	}
	return map[string]any{"name": route.OperationID, "description": route.Summary, "inputSchema": mcpInputSchema(route), "annotations": annotations}
}

func mcpInputSchema(route Route) map[string]any {
	properties := make(map[string]any)
	required := make([]string, 0)
	if route.Request != nil {
		if schema := openAPISchema(route.Request); schema["properties"] != nil {
			for key, value := range schema["properties"].(map[string]any) {
				properties[key] = value
			}
			if names, ok := schema["required"].([]string); ok {
				required = append(required, names...)
			}
		}
	}
	for _, parameter := range route.Parameters {
		properties[parameter.Name] = map[string]any{"type": parameter.Type, "description": parameter.Description}
		if parameter.Required {
			required = append(required, parameter.Name)
		}
	}
	for _, segment := range strings.Split(route.Path, "/") {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}")
			if name != "api_version" {
				properties[name] = map[string]any{"type": "string"}
				required = append(required, name)
			}
		}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func (s *Server) invokeMCPRoute(parent *http.Request, route Route, args map[string]json.RawMessage) (int, http.Header, []byte) {
	path := routeMountPath(route.Path)
	query := url.Values{}
	bodyArgs := make(map[string]json.RawMessage)
	for key, value := range args {
		if strings.Contains(path, "{"+key+"}") {
			var text string
			_ = json.Unmarshal(value, &text)
			path = strings.ReplaceAll(path, "{"+key+"}", url.PathEscape(text))
			continue
		}
		var text string
		if json.Unmarshal(value, &text) == nil {
			query.Set(key, text)
		} else {
			query.Set(key, string(value))
		}
		bodyArgs[key] = value
	}
	var body io.Reader
	if route.Request != nil {
		encoded, _ := json.Marshal(bodyArgs)
		body = bytes.NewReader(encoded)
	}
	ctx := context.WithValue(parent.Context(), chi.RouteCtxKey, nil)
	ctx = context.WithValue(ctx, auditSourceContextKey{}, "mcp")
	request := httptest.NewRequest(route.Method, path+queryString(query), body).WithContext(ctx)
	request.Header = parent.Header.Clone()
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = parent.RemoteAddr
	request.Host = parent.Host
	request.TLS = parent.TLS
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	return response.Code, response.Header(), response.Body.Bytes()
}

func queryString(query url.Values) string {
	if len(query) == 0 {
		return ""
	}
	return "?" + query.Encode()
}

func (s *Server) isOperatorBearer(r *http.Request) bool {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || s.cfg == nil {
		return false
	}
	fields := strings.Fields(values[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return false
	}
	digest := sha256SumString(fields[1])
	expected := sha256SumString(s.cfg.Server.Token)
	return strings.HasPrefix(fields[1], "adm_") && subtle.ConstantTimeCompare(digest, expected) == 1
}

func sha256SumString(value string) []byte { sum := sha256.Sum256([]byte(value)); return sum[:] }

func (s *Server) mcpOriginAllowed(r *http.Request, origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.Path != "" && parsed.Path != "/" {
		return false
	}
	requestOrigin := "http://" + r.Host
	if s.requestIsHTTPS(r) {
		requestOrigin = "https://" + r.Host
	}
	if strings.EqualFold(origin, requestOrigin) {
		return true
	}
	if s.cfg != nil && s.cfg.Server.FQDN != "" {
		publicOrigin := "http://" + strings.TrimSpace(s.cfg.Server.FQDN)
		if s.requestIsHTTPS(r) {
			publicOrigin = "https://" + strings.TrimSpace(s.cfg.Server.FQDN)
		}
		if strings.EqualFold(strings.TrimRight(origin, "/"), publicOrigin) {
			return true
		}
	}
	if s.cfg != nil {
		for _, allowed := range s.cfg.Server.CORS.AllowedOrigins {
			if allowed != "*" && strings.EqualFold(strings.TrimRight(allowed, "/"), strings.TrimRight(origin, "/")) {
				return true
			}
		}
	}
	return false
}

func (s *Server) mcpSuccess(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (s *Server) mcpFailure(w http.ResponseWriter, status int, id json.RawMessage, code int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}
