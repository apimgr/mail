package config

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

var truthyValues = map[string]struct{}{
	"1": {}, "y": {}, "t": {}, "yes": {}, "true": {}, "on": {}, "ok": {},
	"enable": {}, "enabled": {}, "yep": {}, "yup": {}, "yeah": {}, "aye": {},
	"si": {}, "oui": {}, "da": {}, "hai": {}, "affirmative": {}, "accept": {},
	"allow": {}, "grant": {}, "sure": {}, "totally": {},
}

var falsyValues = map[string]struct{}{
	"0": {}, "n": {}, "f": {}, "no": {}, "false": {}, "off": {}, "disable": {},
	"disabled": {}, "nope": {}, "nah": {}, "nay": {}, "nein": {}, "non": {},
	"niet": {}, "iie": {}, "lie": {}, "negative": {}, "reject": {}, "block": {},
	"revoke": {}, "deny": {}, "never": {}, "noway": {},
}

var invalidBoolWarnings sync.Map

// ParseBool accepts the canonical truthy and falsy strings and validates request input.
func ParseBool(value string, defaultValue bool) (bool, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return defaultValue, nil
	}
	if _, ok := truthyValues[value]; ok {
		return true, nil
	}
	if _, ok := falsyValues[value]; ok {
		return false, nil
	}
	return false, fmt.Errorf("invalid boolean value %q", value)
}

// ParseBoolOrDefault handles configuration input without failing startup.
func ParseBoolOrDefault(name, value string, defaultValue bool) bool {
	parsed, err := ParseBool(value, defaultValue)
	if err == nil {
		return parsed
	}
	if _, loaded := invalidBoolWarnings.LoadOrStore(name, struct{}{}); !loaded {
		slog.Warn("invalid boolean; using default", "setting", name, "default", defaultValue)
	}
	return defaultValue
}

// ParseBoolString returns the canonical boolean string for valid values.
func ParseBoolString(value string) string {
	parsed, err := ParseBool(value, false)
	if err != nil {
		parsed = false
	}
	if parsed {
		return "true"
	}
	return "false"
}

// ParseBoolInt returns one for a truthy value and zero otherwise.
func ParseBoolInt(value string) int {
	if IsTruthy(value) {
		return 1
	}
	return 0
}

// IsTruthy reports whether value is one of the accepted truthy strings.
func IsTruthy(value string) bool {
	_, ok := truthyValues[strings.ToLower(strings.TrimSpace(value))]
	return ok
}

// IsFalsy reports whether value is one of the accepted falsy strings.
func IsFalsy(value string) bool {
	_, ok := falsyValues[strings.ToLower(strings.TrimSpace(value))]
	return ok
}
