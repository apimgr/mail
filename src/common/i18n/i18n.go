package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

//go:embed locales/*.json
var localeFiles embed.FS

type Language struct {
	Code      string
	Native    string
	Direction string
	Plurals   []string
}

var registry = []Language{
	{Code: "en", Native: "English", Direction: "ltr", Plurals: []string{"one", "other"}},
	{Code: "es", Native: "Español", Direction: "ltr", Plurals: []string{"one", "other"}},
	{Code: "fr", Native: "Français", Direction: "ltr", Plurals: []string{"one", "other"}},
	{Code: "de", Native: "Deutsch", Direction: "ltr", Plurals: []string{"one", "other"}},
	{Code: "it", Native: "Italiano", Direction: "ltr", Plurals: []string{"one", "many", "other"}},
	{Code: "ar", Native: "العربية", Direction: "rtl", Plurals: []string{"zero", "one", "two", "few", "many", "other"}},
}

var placeholderPattern = regexp.MustCompile(`\{([a-z][a-z0-9_]*)\}`)

type Catalog struct {
	messages map[string]map[string]json.RawMessage
}

func Load() (*Catalog, error) {
	entries, err := fs.ReadDir(localeFiles, "locales")
	if err != nil {
		return nil, fmt.Errorf("reading embedded locales: %w", err)
	}
	files := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		files[strings.TrimSuffix(entry.Name(), ".json")] = struct{}{}
	}
	if len(files) != len(registry) {
		return nil, fmt.Errorf("locale registry has %d languages but embedded assets have %d", len(registry), len(files))
	}

	catalog := &Catalog{messages: make(map[string]map[string]json.RawMessage, len(registry))}
	for _, lang := range registry {
		if _, ok := files[lang.Code]; !ok {
			return nil, fmt.Errorf("locale %q is missing its embedded file", lang.Code)
		}
		data, err := fs.ReadFile(localeFiles, "locales/"+lang.Code+".json")
		if err != nil {
			return nil, fmt.Errorf("reading locale %q: %w", lang.Code, err)
		}
		messages := make(map[string]json.RawMessage)
		if err := json.Unmarshal(data, &messages); err != nil {
			return nil, fmt.Errorf("parsing locale %q: %w", lang.Code, err)
		}
		catalog.messages[lang.Code] = messages
	}
	if err := catalog.ValidateKeyParity(); err != nil {
		return nil, err
	}
	return catalog, nil
}

func Languages() []Language {
	result := make([]Language, len(registry))
	for i, lang := range registry {
		result[i] = lang
		result[i].Plurals = append([]string(nil), lang.Plurals...)
	}
	return result
}

func LanguageCodes() []string {
	codes := make([]string, len(registry))
	for i, lang := range registry {
		codes[i] = lang.Code
	}
	return codes
}

func IsRegistered(code string) bool {
	for _, lang := range registry {
		if lang.Code == code {
			return true
		}
	}
	return false
}

func (c *Catalog) IsSupported(code string) bool {
	_, ok := c.messages[strings.ToLower(strings.TrimSpace(code))]
	return ok
}

func (c *Catalog) Language(code string) Language {
	for _, lang := range registry {
		if lang.Code == code {
			return lang
		}
	}
	for _, lang := range registry {
		if lang.Code == "en" {
			return lang
		}
	}
	return Language{Code: "en", Native: "English", Direction: "ltr"}
}

func (c *Catalog) MatchAcceptLanguage(header string) string {
	tags, _, err := language.ParseAcceptLanguage(header)
	if err != nil || len(tags) == 0 {
		return "en"
	}
	supported := make([]language.Tag, 0, len(registry))
	for _, lang := range registry {
		supported = append(supported, language.MustParse(lang.Code))
	}
	tag, _, _ := language.NewMatcher(supported).Match(tags...)
	base, _ := tag.Base()
	code := strings.ToLower(base.String())
	if c.IsSupported(code) {
		return code
	}
	return "en"
}

func (c *Catalog) Translate(lang, key string) string {
	if value := c.message(lang, key); value != "" {
		return value
	}
	if value := c.message("en", key); value != "" {
		return value
	}
	return key
}

func (c *Catalog) TranslateFormat(lang, key string, values map[string]string) string {
	message := c.Translate(lang, key)
	keys := make([]string, 0, len(values))
	for name := range values {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	replacer := make([]string, 0, len(keys)*2)
	for _, name := range keys {
		replacer = append(replacer, "{"+name+"}", values[name])
	}
	return strings.NewReplacer(replacer...).Replace(message)
}

func (c *Catalog) TranslatePlural(lang, key string, count int) string {
	message := c.pluralMessage(lang, key, count)
	if message == "" && lang != "en" {
		message = c.pluralMessage("en", key, count)
	}
	if message == "" {
		return key
	}
	return strings.ReplaceAll(message, "{count}", fmt.Sprint(count))
}

func (c *Catalog) ValidateKeyParity() error {
	base := c.messages["en"]
	for key, english := range base {
		if err := validateTranslation(c.Language("en"), key, english, english); err != nil {
			return err
		}
	}
	for _, lang := range registry {
		if lang.Code == "en" {
			continue
		}
		messages := c.messages[lang.Code]
		for key := range base {
			if _, ok := messages[key]; !ok {
				return fmt.Errorf("locale %q is missing message key %q", lang.Code, key)
			}
		}
		for key := range messages {
			if _, ok := base[key]; !ok {
				return fmt.Errorf("locale %q has unknown message key %q", lang.Code, key)
			}
		}
		for key, english := range base {
			if err := validateTranslation(lang, key, english, messages[key]); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateTranslation(lang Language, key string, english, translated json.RawMessage) error {
	var englishText, translatedText string
	if json.Unmarshal(english, &englishText) == nil {
		if err := json.Unmarshal(translated, &translatedText); err != nil {
			return fmt.Errorf("locale %q message %q must be a string", lang.Code, key)
		}
		if !samePlaceholders(englishText, translatedText, false) {
			return fmt.Errorf("locale %q message %q has different placeholders", lang.Code, key)
		}
		return nil
	}

	var englishForms, translatedForms map[string]string
	if err := json.Unmarshal(english, &englishForms); err != nil || len(englishForms) == 0 {
		return fmt.Errorf("english message %q must be a string or plural object", key)
	}
	if err := json.Unmarshal(translated, &translatedForms); err != nil || len(translatedForms) == 0 {
		return fmt.Errorf("locale %q plural message %q must be an object", lang.Code, key)
	}
	for _, form := range lang.Plurals {
		if translatedForms[form] == "" {
			return fmt.Errorf("locale %q plural message %q is missing %q", lang.Code, key, form)
		}
	}
	for form, text := range translatedForms {
		if !contains(lang.Plurals, form) && form != "zero" {
			return fmt.Errorf("locale %q plural message %q has unsupported category %q", lang.Code, key, form)
		}
		englishText := englishForms[form]
		if englishText == "" {
			englishText = englishForms["other"]
		}
		if englishText == "" || !samePlaceholders(englishText, text, true) {
			return fmt.Errorf("locale %q plural message %q category %q has different placeholders", lang.Code, key, form)
		}
	}
	for form := range englishForms {
		if !contains(lang.Plurals, form) && form != "zero" {
			return fmt.Errorf("english plural message %q has unsupported category %q", key, form)
		}
	}
	return nil
}

func samePlaceholders(left, right string, pluralMessage bool) bool {
	placeholders := func(text string) []string {
		matches := placeholderPattern.FindAllStringSubmatch(text, -1)
		result := make([]string, 0, len(matches))
		for _, match := range matches {
			if pluralMessage && match[1] == "count" {
				continue
			}
			result = append(result, match[1])
		}
		sort.Strings(result)
		return result
	}
	return fmt.Sprint(placeholders(left)) == fmt.Sprint(placeholders(right))
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func FormatNumber(value float64, lang string, decimals int) string {
	if decimals < 0 {
		decimals = 0
	}
	if !IsRegistered(lang) {
		lang = "en"
	}
	return message.NewPrinter(language.MustParse(lang)).Sprintf("%.*f", decimals, value)
}

func (c *Catalog) message(lang, key string) string {
	var value string
	if messages := c.messages[lang]; messages != nil {
		if raw := messages[key]; len(raw) != 0 {
			_ = json.Unmarshal(raw, &value)
		}
	}
	return value
}

func (c *Catalog) pluralMessage(lang, key string, count int) string {
	var forms map[string]string
	if messages := c.messages[lang]; messages != nil {
		if raw := messages[key]; len(raw) != 0 {
			_ = json.Unmarshal(raw, &forms)
		}
	}
	if len(forms) == 0 {
		return ""
	}
	category := pluralCategory(plural.Cardinal.MatchPlural(language.MustParse(lang), count, 0, 0, 0, 0))
	if count == 0 && forms["zero"] != "" {
		return forms["zero"]
	}
	if forms[category] != "" {
		return forms[category]
	}
	return forms["other"]
}

func pluralCategory(form plural.Form) string {
	switch form {
	case plural.Zero:
		return "zero"
	case plural.One:
		return "one"
	case plural.Two:
		return "two"
	case plural.Few:
		return "few"
	case plural.Many:
		return "many"
	default:
		return "other"
	}
}
