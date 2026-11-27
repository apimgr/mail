package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/mail/src/config"
)

func TestSwaggerPageLoadsLocalInteractiveUIAndSpec(t *testing.T) {
	server := New(&config.Config{}, nil)
	if server.pages == nil {
		t.Fatal("page renderer was not initialized")
	}
	if server.pages.parseErr != nil {
		t.Fatalf("page renderer initialization error: %v", server.pages.parseErr)
	}
	handler := server.Handler()
	page := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/server/docs/swagger", nil)
	request.Header.Set("Accept", "text/html")
	request.Header.Set("User-Agent", "Mozilla/5.0")
	handler.ServeHTTP(page, request)
	if page.Code != http.StatusOK {
		t.Fatalf("Swagger page status = %d", page.Code)
	}
	for _, fragment := range []string{`id="swagger-ui"`, `data-endpoint="/api/v1/server/swagger"`, "swagger-ui-bundle-5.20.1.js", "docs-swagger.js", "noscript"} {
		if !strings.Contains(page.Body.String(), fragment) {
			t.Errorf("Swagger page omits %q in response: %s", fragment, page.Body.String())
		}
	}
	for _, asset := range []string{"/static/vendor/swagger-ui/swagger-ui-bundle-5.20.1.js", "/static/vendor/swagger-ui/swagger-ui-standalone-preset-5.20.1.js", "/static/vendor/swagger-ui/swagger-ui-5.20.1.css", "/static/js/docs-swagger.js"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, asset, nil))
		if response.Code != http.StatusOK || response.Body.Len() == 0 {
			t.Errorf("asset %s status=%d bytes=%d", asset, response.Code, response.Body.Len())
		}
	}
}
