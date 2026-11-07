package config

import "testing"

func TestParseBool(t *testing.T) {
	for _, value := range []string{"true", "YES", " enabled ", "1", "oui", "si", "ja", "da", "sim"} {
		if !ParseBool(value) {
			t.Errorf("ParseBool(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"", "false", "off", "0", "unknown"} {
		if ParseBool(value) {
			t.Errorf("ParseBool(%q) = true, want false", value)
		}
	}
}

func TestBooleanHelpers(t *testing.T) {
	if got := ParseBoolString("yes"); got != "true" {
		t.Fatalf("ParseBoolString(yes) = %q", got)
	}
	if got := ParseBoolString("no"); got != "false" {
		t.Fatalf("ParseBoolString(no) = %q", got)
	}
	if ParseBoolInt("on") != 1 || ParseBoolInt("off") != 0 {
		t.Fatal("ParseBoolInt returned an unexpected value")
	}
	if !IsTruthy("oui") || IsTruthy("maybe") || !IsFalsy("nein") || IsFalsy("maybe") {
		t.Fatal("explicit truthy/falsy checks returned an unexpected value")
	}
}
