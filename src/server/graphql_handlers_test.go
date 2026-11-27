package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/mail/src/config"
)

func TestGraphQLEndpointRequiresOperatorAndSupportsGetAndPostQueries(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_test-operator-token"
	handler := New(cfg, nil).router

	for _, test := range []struct {
		name   string
		method string
		target string
		body   string
	}{
		{name: "post", method: http.MethodPost, target: "/api/v1/server/graphql", body: `{"query":"{ __typename }"}`},
		{name: "get", method: http.MethodGet, target: "/api/v1/server/graphql?query=%7B%20__typename%20%7D"},
		{name: "post-alias", method: http.MethodPost, target: "/api/graphql", body: `{"query":"{ __typename }"}`},
		{name: "get-alias", method: http.MethodGet, target: "/api/graphql?query=%7B%20__typename%20%7D"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.target, strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
				t.Fatalf("Content-Type = %q", got)
			}
			if !strings.Contains(recorder.Body.String(), `"data":{"__typename":"Query"}`) {
				t.Fatalf("unexpected GraphQL response: %s", recorder.Body.String())
			}
		})
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/server/graphql", strings.NewReader(`{"query":"{ __typename }"}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestGraphQLGetWithoutQueryProvidesUsage(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_test-operator-token"
	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/graphql", nil)
	request.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
	recorder := httptest.NewRecorder()
	New(cfg, nil).router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Provide a GraphQL query") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
