package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apimgr/mail/src/config"
	"github.com/apimgr/mail/src/database"
)

func mcpTestRequest(handler http.Handler, target, method, token, protocolMethod, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("MCP-Protocol-Version", mcpProtocolVersion)
	request.Header.Set("Mcp-Method", protocolMethod)
	if protocolMethod == "tools/call" {
		request.Header.Set("Mcp-Name", "getServerVersion")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestMCPRequestSourceIsRecordedInAudit(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Name = filepath.Join(t.TempDir(), "server.db")
	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	s := New(cfg, db)
	request := httptest.NewRequest(http.MethodPost, "/mcp", nil).WithContext(withRequestAuditSource(context.Background(), "mcp"))
	s.auditRequest(request, "mail.domain.created", "operator", map[string]any{"id": "domain-id"})
	var details string
	if err := db.ServerReadDB.QueryRowContext(context.Background(), `SELECT details FROM audit_log WHERE action = 'domain.created'`).Scan(&details); err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(details), &fields); err != nil {
		t.Fatal(err)
	}
	if fields["source"] != "mcp" {
		t.Fatalf("audit source = %#v, want mcp", fields["source"])
	}
}

func TestMCPRequiresBearerAndProtocolHeaders(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_test-operator-token"
	handler := New(cfg, nil).Handler()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}`
	if got := mcpTestRequest(handler, "/api/v1/server/mcp", http.MethodPost, "", "tools/list", body); got.Code != http.StatusUnauthorized || got.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("missing-token response = %d %s", got.Code, got.Body.String())
	}
	if got := mcpTestRequest(handler, "/api/v1/server/mcp", http.MethodPost, cfg.Server.Token, "tools/call", body); got.Code != http.StatusBadRequest || !strings.Contains(got.Body.String(), "-32020") {
		t.Fatalf("mismatched-method response = %d %s", got.Code, got.Body.String())
	}
	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/server/mcp", nil))
	if get.Code != http.StatusMethodNotAllowed || get.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("GET response = %d %q", get.Code, get.Header().Get("Content-Type"))
	}
}

func TestMCPListsRegisteredToolsAndCallsInProcess(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Token = "adm_test-operator-token"
	handler := New(cfg, nil).Handler()
	listBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}`
	response := mcpTestRequest(handler, "/api/v1/server/mcp", http.MethodPost, cfg.Server.Token, "tools/list", listBody)
	if response.Code != http.StatusOK {
		t.Fatalf("tools/list status = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Result struct {
			Tools []struct {
				Name        string          `json:"name"`
				Annotations map[string]bool `json:"annotations"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range envelope.Result.Tools {
		if tool.Name == "getServerVersion" {
			found = true
			if !tool.Annotations["readOnlyHint"] {
				t.Error("GET tool is not marked read-only")
			}
		}
	}
	if !found {
		t.Fatal("tools/list omitted registered getServerVersion operation")
	}
	for _, alias := range []string{"/api/mcp", "/mcp"} {
		aliased := mcpTestRequest(handler, alias, http.MethodPost, cfg.Server.Token, "tools/list", listBody)
		if aliased.Code != response.Code || aliased.Header().Get("Content-Type") != response.Header().Get("Content-Type") || aliased.Body.String() != response.Body.String() {
			t.Errorf("MCP alias %s differs from canonical response: status=%d body=%s", alias, aliased.Code, aliased.Body.String())
		}
	}
	callBody := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"getServerVersion","arguments":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}`
	called := mcpTestRequest(handler, "/mcp", http.MethodPost, cfg.Server.Token, "tools/call", callBody)
	if called.Code != http.StatusOK || !strings.Contains(called.Body.String(), `"isError":false`) {
		t.Fatalf("tools/call status = %d: %s", called.Code, called.Body.String())
	}
}
