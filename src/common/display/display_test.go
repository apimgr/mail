package display

import "testing"

func TestEmojiEnabled(t *testing.T) {
	for _, tc := range []struct {
		name      string
		terminal  bool
		term      string
		noColor   string
		wantEmoji bool
	}{
		{name: "terminal", terminal: true, wantEmoji: true},
		{name: "redirected", term: "xterm", wantEmoji: false},
		{name: "dumb terminal", terminal: true, term: "dumb", wantEmoji: false},
		{name: "no color", terminal: true, term: "xterm", noColor: "1", wantEmoji: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := EmojiEnabled(tc.terminal, tc.term, tc.noColor); got != tc.wantEmoji {
				t.Fatalf("EmojiEnabled() = %t, want %t", got, tc.wantEmoji)
			}
		})
	}
}

func TestIsDumbTerminal(t *testing.T) {
	if !IsDumbTerminal("dumb") || IsDumbTerminal("xterm-256color") {
		t.Fatal("unexpected dumb terminal detection")
	}
}
