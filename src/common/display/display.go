package display

import "strings"

// EmojiEnabled reports whether terminal output should include emoji glyphs.
func EmojiEnabled(isTerminal bool, term, noColor string) bool {
	return isTerminal && strings.TrimSpace(term) != "dumb" && noColor == ""
}

// IsDumbTerminal reports whether the terminal declares no special capabilities.
func IsDumbTerminal(term string) bool {
	return strings.TrimSpace(term) == "dumb"
}
