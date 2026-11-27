package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const maxGraphQLRequestBody = 1 << 20

type graphQLRequest struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
}

type graphQLAPIResponse struct {
	Data   map[string]any `json:"data"`
	Errors []any          `json:"errors,omitempty"`
}

func (s *Server) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if s.graphqlSchema == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "SERVICE_UNAVAILABLE", "message": "GraphQL is unavailable"})
		return
	}
	var request graphQLRequest
	switch r.Method {
	case http.MethodGet:
		request = graphQLRequest{Query: r.URL.Query().Get("query"), OperationName: r.URL.Query().Get("operationName")}
		if raw := r.URL.Query().Get("variables"); raw != "" {
			if len(raw) > maxGraphQLRequestBody || json.Unmarshal([]byte(raw), &request.Variables) != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid GraphQL variables"})
				return
			}
		}
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, maxGraphQLRequestBody)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "VALIDATION_FAILED", "message": "Invalid GraphQL request"})
			return
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "METHOD_NOT_ALLOWED", "message": "Method not allowed"})
		return
	}
	request.Query = strings.TrimSpace(request.Query)
	if request.Query == "" {
		writeJSON(w, http.StatusOK, map[string]any{"data": nil, "errors": []map[string]any{{"message": "Provide a GraphQL query in the query parameter or JSON request body", "extensions": map[string]string{"code": "QUERY_REQUIRED"}}}})
		return
	}
	result := s.graphqlSchema.Exec(r.Context(), request.Query, request.OperationName, request.Variables)
	if result == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": nil, "errors": []map[string]any{{"message": "GraphQL request could not be executed", "extensions": map[string]string{"code": "EXECUTION_FAILED"}}}})
		return
	}
	if result.Data == nil {
		result.Data = json.RawMessage("null")
	}
	for _, graphqlErr := range result.Errors {
		if graphqlErr.Extensions == nil {
			graphqlErr.Extensions = make(map[string]any)
		}
		code := "GRAPHQL_VALIDATION_FAILED"
		if graphqlErr.ResolverError != nil {
			code = "EXECUTION_ERROR"
		}
		graphqlErr.Extensions["code"] = code
	}
	_ = json.NewEncoder(w).Encode(result)
}
