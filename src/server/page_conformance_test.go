package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/mail/src/config"
	"golang.org/x/net/html"
)

func TestRegisteredPageHTMLConformance(t *testing.T) {
	var paths []string
	routes := make(map[string]bool, len(pageRegistry)+3)
	for _, route := range pageRegistry {
		routes[route.Path] = true
		if route.Rendered {
			paths = append(paths, route.Path)
		}
	}
	routes["/server/consent"] = true
	routes["/server/ccpa"] = true
	routes["/api/v1/server/preferences/sync/import"] = true
	handler := New(&config.Config{}, nil).router
	paths = append(paths, "/missing-page")
	for _, target := range paths {
		t.Run(target, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, target, nil)
			request.Header.Set("Accept", "text/html")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			root, err := html.Parse(strings.NewReader(response.Body.String()))
			if err != nil {
				t.Fatal(err)
			}
			ids := map[string]int{}
			headingIDs := map[string]bool{}
			var sectionLabels []string
			var pageHeaders int
			var titledH1 int
			labels := map[string]bool{}
			var controls []string
			var links []string
			var tableOutsideWrapper bool
			var firstAnchors []string
			var breadcrumbCount int
			var breadcrumbHref string
			var executableInlineScript bool
			var walk func(*html.Node, bool, bool, bool)
			walk = func(node *html.Node, inForm, inTableWrapper, inLabel bool) {
				if node.Type == html.ElementNode {
					attrs := node.Attr
					values := make(map[string]string, len(attrs))
					seenAttrs := make(map[string]bool, len(attrs))
					for _, attr := range attrs {
						key := strings.ToLower(attr.Key)
						if seenAttrs[key] {
							t.Errorf("%s has duplicate %s attribute", node.Data, key)
						}
						seenAttrs[key] = true
						values[key] = attr.Val
						if key == "id" {
							ids[attr.Val]++
						}
						if key == "for" {
							labels[attr.Val] = true
						}
						if key == "style" || strings.HasPrefix(key, "on") {
							t.Errorf("%s has forbidden %s attribute", node.Data, key)
						}
					}
					switch node.Data {
					case "style":
						t.Error("inline style element is forbidden")
					case "script":
						if values["src"] == "" && values["type"] != "application/ld+json" {
							for child := node.FirstChild; child != nil; child = child.NextSibling {
								if child.Type == html.TextNode && strings.TrimSpace(child.Data) != "" {
									executableInlineScript = true
								}
							}
						}
					case "main":
						ids["__main"]++
					case "h1":
						ids["__h1"]++
						if hasClass(values["class"], "page-title") {
							titledH1++
						}
					case "h2":
						if values["id"] != "" {
							headingIDs[values["id"]] = true
						}
					case "header":
						if hasClass(values["class"], "page-header") {
							pageHeaders++
						}
					case "a":
						if len(firstAnchors) < 2 {
							firstAnchors = append(firstAnchors, values["class"])
						}
						if href := values["href"]; strings.HasPrefix(href, "/server/") {
							links = append(links, strings.Split(strings.Split(href, "?")[0], "#")[0])
						}
						if values["href"] == "/server/preferences" && hasClass(values["class"], "preferences-link") && target == "/server/preferences" && (!hasClass(values["class"], "active") || values["aria-current"] != "page") {
							t.Error("current preferences link must have active class and aria-current=page")
						}
					case "nav":
						if hasClass(values["class"], "breadcrumb") {
							breadcrumbCount++
							if node.FirstChild != nil {
								var findHref func(*html.Node)
								findHref = func(child *html.Node) {
									if child.Type == html.ElementNode && child.Data == "a" {
										for _, attr := range child.Attr {
											if attr.Key == "href" {
												breadcrumbHref = attr.Val
											}
										}
									}
									for nested := child.FirstChild; nested != nil; nested = nested.NextSibling {
										findHref(nested)
									}
								}
								findHref(node)
							}
						}
					case "input", "select", "textarea":
						if node.Data == "input" && strings.EqualFold(values["type"], "hidden") {
							break
						}
						if values["aria-label"] == "" && values["aria-labelledby"] == "" && !inLabel {
							controls = append(controls, values["id"])
						}
					case "form":
						inForm = strings.EqualFold(values["method"], "post")
					}
					if node.Data == "table" && !inTableWrapper {
						tableOutsideWrapper = true
					}
					if hasClass(values["class"], "section-card") {
						sectionLabels = append(sectionLabels, values["aria-labelledby"])
					}
					inTableWrapper = inTableWrapper || hasClass(values["class"], "table-wrapper")
					if node.Data == "img" && !hasAttribute(node, "alt") {
						t.Error("image is missing alt attribute")
					}
					if inForm && node.Data == "form" && !formHasCSRF(node) {
						t.Error("POST form is missing csrf_token")
					}
				}
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					walk(child, inForm, inTableWrapper, inLabel || node.Type == html.ElementNode && node.Data == "label")
				}
			}
			walk(root, false, false, false)
			if ids["__main"] != 1 || ids["__h1"] != 1 || titledH1 != 1 || pageHeaders != 1 {
				t.Errorf("page has %d main elements, %d h1 elements, %d page-title h1 elements, and %d page headers", ids["__main"], ids["__h1"], titledH1, pageHeaders)
			}
			for _, label := range sectionLabels {
				if label == "" || !headingIDs[label] {
					t.Errorf("section card aria-labelledby %q does not reference an h2", label)
				}
			}
			if target == "/server/about" {
				body := response.Body.String()
				if !strings.Contains(body, "Source repository") || !strings.Contains(body, applicationVersion()) || !strings.Contains(body, "self-hosted") {
					t.Error("About page must describe the IDEA-defined product and show version and source link")
				}
			}
			if len(firstAnchors) < 2 || !strings.Contains(firstAnchors[0], "skip-link") || !strings.Contains(firstAnchors[1], "skip-link") {
				t.Error("skip links are not the first two links")
			}
			if executableInlineScript {
				t.Error("inline executable script is forbidden")
			}
			if tableOutsideWrapper {
				t.Error("table is outside table-wrapper")
			}
			if len(controls) > 0 {
				for _, id := range controls {
					if id == "" || !labels[id] {
						t.Errorf("form control %q has no accessible label", id)
					}
				}
			}
			for _, link := range links {
				if !routes[link] {
					t.Errorf("internal server link %q does not resolve", link)
				}
			}
			if target == "/server/help" || target == "/server/preferences" {
				if breadcrumbCount != 0 {
					t.Error("hub page must not have a breadcrumb")
				}
			}
			if target == "/server/help/mcp" && (breadcrumbCount != 1 || breadcrumbHref != "/server/help") {
				t.Errorf("help sub-page must have one link to its hub; count=%d href=%q", breadcrumbCount, breadcrumbHref)
			}
			if target == "/server/preferences/sync" && (breadcrumbCount != 1 || breadcrumbHref != "/server/preferences") {
				t.Errorf("preferences sub-page must have one link to its hub; count=%d href=%q", breadcrumbCount, breadcrumbHref)
			}
		})
	}
}

func TestSubPageRegistryDrivesHubPagesAndSitemap(t *testing.T) {
	handler := New(&config.Config{}, nil).router
	serverAlias := httptest.NewRecorder()
	handler.ServeHTTP(serverAlias, httptest.NewRequest(http.MethodGet, "/server", nil))
	if serverAlias.Code != http.StatusMovedPermanently || serverAlias.Header().Get("Location") != "/server/about" {
		t.Errorf("/server registry alias redirect = %d %q", serverAlias.Code, serverAlias.Header().Get("Location"))
	}
	sitemap := httptest.NewRecorder()
	handler.ServeHTTP(sitemap, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	for _, entry := range pageRegistry {
		if entry.Hub == "" {
			continue
		}
		if entry.HandlerKey == "" || entry.TitleKey == "" || entry.DescriptionKey == "" {
			t.Errorf("registered sub-page %q lacks a handler or translation keys", entry.Path)
		}
		if !strings.HasPrefix(entry.Path, entry.Hub+"/") || strings.Contains(strings.TrimPrefix(entry.Path, entry.Hub+"/"), "/") {
			t.Errorf("registered path %q is not a direct child of %q", entry.Path, entry.Hub)
		}
		request := httptest.NewRequest(http.MethodGet, entry.Path, nil)
		request.Header.Set("Accept", "text/html")
		page := httptest.NewRecorder()
		handler.ServeHTTP(page, request)
		if page.Code != http.StatusOK {
			t.Errorf("registered route %q returned %d", entry.Path, page.Code)
		}
		hubRequest := httptest.NewRequest(http.MethodGet, entry.Hub, nil)
		hubRequest.Header.Set("Accept", "text/html")
		hubPage := httptest.NewRecorder()
		handler.ServeHTTP(hubPage, hubRequest)
		if strings.Count(hubPage.Body.String(), `href="`+entry.Path+`"`) != 1 {
			t.Errorf("hub %q does not link to registered page %q exactly once", entry.Hub, entry.Path)
		}
		footerRequest := httptest.NewRequest(http.MethodGet, "/server/about", nil)
		footerRequest.Header.Set("Accept", "text/html")
		footerPage := httptest.NewRecorder()
		handler.ServeHTTP(footerPage, footerRequest)
		footerRoot, err := html.Parse(strings.NewReader(footerPage.Body.String()))
		if err != nil {
			t.Fatal(err)
		}
		footer := findElement(footerRoot, "footer", "site-footer")
		if footer == nil || countLinksTo(footer, entry.Hub) != 1 {
			t.Errorf("footer does not link to registered hub %q exactly once", entry.Hub)
		}
		if strings.Contains(sitemap.Body.String(), entry.Path) != entry.Sitemap {
			t.Errorf("sitemap inclusion for %q = %t, want %t", entry.Path, strings.Contains(sitemap.Body.String(), entry.Path), entry.Sitemap)
		}
		if entry.Hub == "/server/help" {
			apiRequest := httptest.NewRequest(http.MethodGet, "/api/v1/server/help", nil)
			apiRequest.Header.Set("Accept", "application/json")
			apiResponse := httptest.NewRecorder()
			handler.ServeHTTP(apiResponse, apiRequest)
			if strings.Count(apiResponse.Body.String(), `"path":"`+entry.Path+`"`) != 1 {
				t.Errorf("help API does not expose registered page %q exactly once", entry.Path)
			}
		}
	}
}

func countLinksTo(node *html.Node, target string) int {
	count := 0
	if node.Type == html.ElementNode && node.Data == "a" && attributeValue(node, "href") == target {
		count++
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		count += countLinksTo(child, target)
	}
	return count
}

func TestOperatorHeaderFragmentIsLastChildAndPreservesHeaderElements(t *testing.T) {
	requestPage := func(handler http.Handler) *html.Node {
		request := httptest.NewRequest(http.MethodGet, "/server/about", nil)
		request.Header.Set("Accept", "text/html")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		root, err := html.Parse(strings.NewReader(response.Body.String()))
		if err != nil {
			t.Fatal(err)
		}
		return findElement(root, "header", "site-header")
	}
	baseline := requestPage(New(&config.Config{}, nil).router)
	if baseline == nil {
		t.Fatal("default header is missing")
	}
	cfg := &config.Config{}
	cfg.Server.Branding.Content.Header = config.BrandingPageContentConfig{
		Format: "html",
		Body:   config.LocalizedContent{"default": `<span id="operator-header">Operator notice</span>`},
	}
	configured := requestPage(New(cfg, nil).router)
	if configured == nil || configured.LastChild == nil || configured.LastChild.Type != html.ElementNode || configured.LastChild.Data != "span" || !hasAttributeValue(configured.LastChild, "id", "operator-header") {
		t.Fatal("configured header fragment is not the final header child")
	}
	if got, want := directElementSignatures(configured, "operator-header"), directElementSignatures(baseline, ""); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("adding the header fragment changed other header elements: got %v, want %v", got, want)
	}
}

func findElement(node *html.Node, tag, class string) *html.Node {
	if node.Type == html.ElementNode && node.Data == tag && hasClass(attributeValue(node, "class"), class) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, tag, class); found != nil {
			return found
		}
	}
	return nil
}

func attributeValue(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val
		}
	}
	return ""
}

func hasAttributeValue(node *html.Node, name, value string) bool {
	return attributeValue(node, name) == value
}

func directElementSignatures(node *html.Node, excludedID string) []string {
	var signatures []string
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode || excludedID != "" && hasAttributeValue(child, "id", excludedID) {
			continue
		}
		signatures = append(signatures, child.Data+":"+attributeValue(child, "class")+":"+attributeValue(child, "href"))
	}
	return signatures
}

func hasAttribute(node *html.Node, name string) bool {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return true
		}
	}
	return false
}

func formHasCSRF(form *html.Node) bool {
	var found bool
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "input" {
			for _, attr := range node.Attr {
				if attr.Key == "name" && attr.Val == "csrf_token" {
					found = true
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(form)
	return found
}

func hasClass(classList, target string) bool {
	for _, className := range strings.Fields(classList) {
		if className == target {
			return true
		}
	}
	return false
}
