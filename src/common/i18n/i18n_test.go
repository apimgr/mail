package i18n

import (
	"encoding/json"
	"testing"
)

func TestEmbeddedLocaleRegistryAndKeyParity(t *testing.T) {
	catalog, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.ValidateKeyParity(); err != nil {
		t.Fatal(err)
	}
	if len(Languages()) != 6 {
		t.Fatalf("supported languages = %d, want 6", len(Languages()))
	}
	if got := catalog.Language("ar").Direction; got != "rtl" {
		t.Fatalf("Arabic direction = %q", got)
	}
}

func TestTranslateFallbackAndFormat(t *testing.T) {
	catalog := &Catalog{messages: map[string]map[string]json.RawMessage{
		"en": {"hello": json.RawMessage(`"Hello, {name}"`), "fallback": json.RawMessage(`"English"`)},
		"es": {"hello": json.RawMessage(`"Hola, {name}"`)},
	}}
	if got := catalog.TranslateFormat("es", "hello", map[string]string{"name": "Ana"}); got != "Hola, Ana" {
		t.Fatalf("formatted translation = %q", got)
	}
	if got := catalog.Translate("es", "fallback"); got != "English" {
		t.Fatalf("fallback translation = %q", got)
	}
	if got := catalog.Translate("unknown", "missing"); got != "missing" {
		t.Fatalf("missing translation = %q", got)
	}
}

func TestTranslatePluralUsesCLDRCategories(t *testing.T) {
	catalog := &Catalog{messages: map[string]map[string]json.RawMessage{
		"en": {"items": json.RawMessage(`{"one":"{count} item","other":"{count} items"}`)},
		"ar": {"items": json.RawMessage(`{"zero":"no items","one":"{count} item","two":"{count} items","few":"{count} items","many":"{count} items","other":"{count} items"}`)},
	}}
	if got := catalog.TranslatePlural("en", "items", 1); got != "1 item" {
		t.Fatalf("English singular = %q", got)
	}
	if got := catalog.TranslatePlural("en", "items", 2); got != "2 items" {
		t.Fatalf("English plural = %q", got)
	}
	if got := catalog.TranslatePlural("ar", "items", 0); got != "no items" {
		t.Fatalf("Arabic zero = %q", got)
	}
}

func TestMatchAcceptLanguage(t *testing.T) {
	catalog, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.MatchAcceptLanguage("fr-CA,es;q=0.8,en;q=0.5"); got != "fr" {
		t.Fatalf("matched language = %q", got)
	}
	if got := catalog.MatchAcceptLanguage("zh-CN"); got != "en" {
		t.Fatalf("unsupported language = %q", got)
	}
}
