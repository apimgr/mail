package format

import (
	"strings"
	"time"

	"github.com/apimgr/mail/src/common/i18n"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

func Count(lang string, value int64) string {
	if value < 0 {
		value = 0
	}
	tag, err := language.Parse(strings.TrimSpace(lang))
	if err != nil {
		tag = language.English
	}
	return message.NewPrinter(tag).Sprintf("%v", number.Decimal(value))
}

func Date(value time.Time, lang string, catalog *i18n.Catalog) string {
	return localizedTime(value, lang, "format.date", catalog)
}

func Time(value time.Time, lang string, catalog *i18n.Catalog) string {
	return localizedTime(value, lang, "format.time", catalog)
}

func DateTime(value time.Time, lang string, catalog *i18n.Catalog) string {
	return localizedTime(value, lang, "format.datetime", catalog)
}

func Duration(value time.Duration, lang string, catalog *i18n.Catalog) string {
	if value < 0 {
		value = 0
	}
	totalSeconds := int(value / time.Second)
	if totalSeconds < 60 {
		return plural("plurals.duration.seconds", totalSeconds, lang, catalog)
	}
	days := totalSeconds / (24 * 60 * 60)
	hours := (totalSeconds / (60 * 60)) % 24
	minutes := (totalSeconds / 60) % 60
	seconds := totalSeconds % 60
	parts := make([]string, 0, 2)
	if days > 0 {
		parts = append(parts, plural("plurals.duration.days", days, lang, catalog))
	}
	if hours > 0 {
		parts = append(parts, plural("plurals.duration.hours", hours, lang, catalog))
	}
	if days == 0 && hours == 0 {
		parts = append(parts, plural("plurals.duration.minutes", minutes, lang, catalog))
		if seconds > 0 {
			parts = append(parts, plural("plurals.duration.seconds", seconds, lang, catalog))
		}
	} else if minutes > 0 && days == 0 {
		parts = append(parts, plural("plurals.duration.minutes", minutes, lang, catalog))
	}
	return strings.Join(parts, " ")
}

func plural(key string, count int, lang string, catalog *i18n.Catalog) string {
	if catalog == nil {
		return Count(lang, int64(count)) + " " + key
	}
	return strings.ReplaceAll(catalog.TranslatePlural(lang, key, count), "{count}", Count(lang, int64(count)))
}

func localizedTime(value time.Time, lang, key string, catalog *i18n.Catalog) string {
	if catalog == nil {
		return value.Format(time.RFC3339)
	}
	formatted := value.Format(catalog.Translate(lang, key))
	formatted = strings.ReplaceAll(formatted, "AM", catalog.Translate(lang, "format.am"))
	formatted = strings.ReplaceAll(formatted, "PM", catalog.Translate(lang, "format.pm"))
	digits := []rune(catalog.Translate(lang, "format.digits"))
	if len(digits) != 10 {
		return formatted
	}
	var result strings.Builder
	for _, char := range formatted {
		if char >= '0' && char <= '9' {
			char = digits[char-'0']
		}
		result.WriteRune(char)
	}
	return result.String()
}
