package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apimgr/mail/src/config"
)

func TestOpenAPISpecUsesRegisteredRoutesAndAliases(t *testing.T) {
	cfg := &config.Config{}
	srv := New(cfg, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/swagger", nil)
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	var spec struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			SecuritySchemes map[string]map[string]any `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/users", "/api/v1/users/{id}", "/api/v1/users/{id}/password", "/api/v1/users/import", "/api/v1/domains", "/api/v1/aliases", "/api/v1/forwards", "/api/v1/queue", "/api/v1/queue/{id}", "/api/v1/queue/{id}/flush", "/api/v1/queue/{id}/hold", "/api/v1/queue/{id}/release", "/api/v1/queue/flush-all", "/api/v1/stats/delivery", "/api/v1/stats/spam", "/api/v1/stats/queue", "/api/v1/server/auth/check", "/api/swagger"} {
		if _, ok := spec.Paths[path]; !ok {
			t.Errorf("OpenAPI paths omit registered route %q", path)
		}
	}
	if _, ok := spec.Paths["/api/v1/services"]; !ok {
		t.Error("OpenAPI spec omits operator-only service route")
	}
	for _, test := range []struct{ path, method string }{
		{"/api/v1/queue", "get"}, {"/api/v1/queue/{id}", "get"}, {"/api/v1/queue/{id}/flush", "post"},
		{"/api/v1/queue/{id}/hold", "post"}, {"/api/v1/queue/{id}/release", "post"}, {"/api/v1/queue/{id}", "delete"},
		{"/api/v1/queue/flush-all", "post"}, {"/api/v1/stats/delivery", "get"}, {"/api/v1/stats/spam", "get"}, {"/api/v1/stats/queue", "get"},
	} {
		operation := spec.Paths[test.path][test.method]
		if len(operation) == 0 || !bytes.Contains(operation, []byte(`"security":[{"BearerAuth":[]}]`)) {
			t.Errorf("queue OpenAPI operation %s %s is missing or lacks operator Bearer security: %s", test.method, test.path, operation)
		}
	}
	userOperation := spec.Paths["/api/v1/users"]["get"]
	for _, field := range [][]byte{[]byte(`"email":{"type":"string"}`), []byte(`"quota_bytes":{"type":"integer"}`), []byte(`"pagination"`), []byte(`"name":"page"`), []byte(`"name":"limit"`), []byte(`"security":[{"BearerAuth":[]}]`)} {
		if !bytes.Contains(userOperation, field) {
			t.Errorf("mail user OpenAPI list operation omits %s: %s", field, userOperation)
		}
	}
	for path, method := range map[string]string{
		"/api/v1/users": "post", "/api/v1/users/{id}": "put", "/api/v1/users/{id}/password": "post",
	} {
		operation := spec.Paths[path][method]
		if !bytes.Contains(operation, []byte(`"security":[{"BearerAuth":[]}]`)) {
			t.Errorf("mail user %s %s operation omits operator Bearer security: %s", method, path, operation)
		}
	}
	for _, contract := range []struct {
		path   string
		fields []string
	}{
		{"/api/v1/stats/delivery", []string{`"name":"period"`, `"in":"query"`, `"default":"30d"`, `"total":{"type":"integer"}`, `"by_status":{"additionalProperties":true,"type":"object"}`}},
		{"/api/v1/stats/spam", []string{`"name":"period"`, `"in":"query"`, `"spam_count":{"type":"integer"}`, `"virus_count":{"type":"integer"}`, `"granularity":{"type":"string"}`}},
		{"/api/v1/stats/queue", []string{`"name":"period"`, `"in":"query"`, `"default":"30d"`, `"queued_messages":{"type":"integer"}`, `"queued_size_bytes":{"type":"integer"}`, `"by_state":{"additionalProperties":true,"type":"object"}`}},
	} {
		operation := spec.Paths[contract.path]["get"]
		for _, field := range contract.fields {
			if !bytes.Contains(operation, []byte(field)) {
				t.Errorf("stats OpenAPI operation %s omits %s: %s", contract.path, field, operation)
			}
		}
	}
	var authOperation struct {
		Security []map[string][]string `json:"security"`
	}
	if err := json.Unmarshal(spec.Paths["/api/v1/server/auth/check"]["get"], &authOperation); err != nil {
		t.Fatal(err)
	}
	if len(authOperation.Security) != 1 {
		t.Fatalf("auth-check security = %#v, want bearer authentication", authOperation.Security)
	}
	if _, ok := authOperation.Security[0]["BearerAuth"]; !ok {
		t.Errorf("auth-check security = %#v, want BearerAuth", authOperation.Security[0])
	}
	if _, ok := spec.Components.SecuritySchemes["BearerAuth"]; !ok {
		t.Error("OpenAPI components omit the bearer token scheme")
	}
	var operatorOperation struct {
		Security []map[string][]string `json:"security"`
	}
	if err := json.Unmarshal(spec.Paths["/api/v1/services"]["get"], &operatorOperation); err != nil {
		t.Fatal(err)
	}
	if len(operatorOperation.Security) != 1 || len(operatorOperation.Security[0]["BearerAuth"]) != 0 {
		t.Errorf("operator route security = %#v, want BearerAuth", operatorOperation.Security)
	}
	domainOperation := spec.Paths["/api/v1/domains"]["get"]
	for _, code := range [][]byte{[]byte(`"400"`), []byte(`"401"`), []byte(`"403"`), []byte(`"404"`), []byte(`"429"`), []byte(`"500"`), []byte(`"503"`)} {
		if !bytes.Contains(domainOperation, code) {
			t.Errorf("domain OpenAPI operation omits response status %s", code)
		}
	}
	for _, field := range [][]byte{[]byte(`"id":{"type":"string"}`), []byte(`"max_mailboxes":{"type":"integer"}`), []byte(`"domain":{"type":"string"}`)} {
		if !bytes.Contains(domainOperation, field) {
			t.Errorf("domain OpenAPI schema omits field schema %s", field)
		}
	}
	if !bytes.Contains(domainOperation, []byte(`"security":[{"BearerAuth":[]}]`)) {
		t.Errorf("domain operation omits operator Bearer security: %s", domainOperation)
	}
	aliasOperation := spec.Paths["/api/v1/aliases"]["get"]
	for _, field := range [][]byte{[]byte(`"pagination"`), []byte(`"limit":{"type":"integer"}`), []byte(`"total":{"type":"integer"}`)} {
		if !bytes.Contains(aliasOperation, field) {
			t.Errorf("alias OpenAPI schema omits %s: %s", field, aliasOperation)
		}
	}
	if !bytes.Contains(aliasOperation, []byte(`"security":[{"BearerAuth":[]}]`)) {
		t.Errorf("alias operation omits operator Bearer security: %s", aliasOperation)
	}
	importOperation := spec.Paths["/api/v1/users/import"]["post"]
	for _, field := range [][]byte{[]byte(`"text/csv"`), []byte(`"type":"string"`), []byte(`"security":[{"BearerAuth":[]}]`)} {
		if !bytes.Contains(importOperation, field) {
			t.Errorf("CSV import OpenAPI operation omits %s: %s", field, importOperation)
		}
	}
	for _, parameter := range [][]byte{[]byte(`"name":"page"`), []byte(`"name":"limit"`), []byte(`"in":"query"`), []byte(`"default":1`), []byte(`"default":250`)} {
		if !bytes.Contains(aliasOperation, parameter) {
			t.Errorf("alias operation omits pagination parameter %s: %s", parameter, aliasOperation)
		}
	}
}

func TestAuthCheckReportsOperatorAndRejectsMissingToken(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_test-token"
	handler := New(cfg, nil).Handler()
	t.Run("operator", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/server/auth/check", nil)
		request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}
		var result map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result["kind"] != "operator" {
			t.Fatalf("kind = %q, want operator", result["kind"])
		}
	})
	t.Run("missing token", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/server/auth/check", nil)
		request.RemoteAddr = "192.0.2.20:1234"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
		}
	})
}
