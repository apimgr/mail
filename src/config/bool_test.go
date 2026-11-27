package config

import "testing"

func TestParseBool(t *testing.T) {
	for _, value := range []string{"true", "YES", " enabled ", "1", "oui", "si", "da", "hai", "affirmative", "accept", "allow", "grant", "sure", "totally"} {
		if got, err := ParseBool(value, false); err != nil || !got {
			t.Errorf("ParseBool(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"false", "off", "0", "nein", "non", "niet", "iie", "lie", "negative", "reject", "block", "revoke", "deny", "never", "noway"} {
		if got, err := ParseBool(value, true); err != nil || got {
			t.Errorf("ParseBool(%q) = true, want false", value)
		}
	}
	if got, err := ParseBool("", true); err != nil || !got {
		t.Fatalf("empty ParseBool = %v, %v", got, err)
	}
	if _, err := ParseBool("unknown", false); err == nil {
		t.Fatal("ParseBool accepted an invalid value")
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
	if !IsTruthy("hai") || IsTruthy("maybe") || !IsFalsy("nein") || IsFalsy("maybe") {
		t.Fatal("explicit truthy/falsy checks returned an unexpected value")
	}
}
