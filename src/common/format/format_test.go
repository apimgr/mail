package format

import (
	"testing"
	"time"

	"github.com/apimgr/mail/src/common/i18n"
)

func TestCountUsesLocaleSeparators(t *testing.T) {
	tests := []struct {
		language string
		want     string
	}{
		{language: "en", want: "1,234,567"},
		{language: "de", want: "1.234.567"},
	}
	for _, test := range tests {
		t.Run(test.language, func(t *testing.T) {
			if got := Count(test.language, 1234567); got != test.want {
				t.Errorf("Count(%q) = %q, want %q", test.language, got, test.want)
			}
		})
	}
}

func TestCountClampsNegativeValuesAndFallsBackToEnglish(t *testing.T) {
	if got := Count("not-a-locale", -12); got != "0" {
		t.Errorf("Count(unsupported, negative) = %q, want 0", got)
	}
}

func TestDateTimeUsesLocaleLayoutAndDigits(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	value := time.Date(2024, time.December, 31, 14, 30, 0, 0, time.UTC)
	tests := []struct {
		language string
		want     string
	}{
		{language: "en", want: "12/31/2024 2:30 PM UTC"},
		{language: "de", want: "31.12.2024 14:30 UTC"},
		{language: "ar", want: "٣١/١٢/٢٠٢٤ ٢:٣٠ م UTC"},
	}
	for _, test := range tests {
		t.Run(test.language, func(t *testing.T) {
			if got := DateTime(value, test.language, catalog); got != test.want {
				t.Errorf("DateTime(%q) = %q, want %q", test.language, got, test.want)
			}
		})
	}
}

func TestDurationUsesPluralMessagesAndAtMostTwoUnits(t *testing.T) {
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		value    time.Duration
		language string
		want     string
	}{
		{name: "seconds", value: 45 * time.Second, language: "en", want: "45 seconds"},
		{name: "singular minute", value: time.Minute, language: "en", want: "1 minute"},
		{name: "minutes and seconds", value: time.Minute + 5*time.Second, language: "es", want: "1 minuto 5 segundos"},
		{name: "hours and minutes", value: 2*time.Hour + 16*time.Minute, language: "en", want: "2 hours 16 minutes"},
		{name: "days and hours", value: 26*time.Hour + 3*time.Minute, language: "en", want: "1 day 2 hours"},
		{name: "Arabic dual", value: 2 * time.Second, language: "ar", want: "ثانيتان"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Duration(test.value, test.language, catalog); got != test.want {
				t.Errorf("Duration(%s, %q) = %q, want %q", test.value, test.language, got, test.want)
			}
		})
	}
}
