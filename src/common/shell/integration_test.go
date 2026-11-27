package shell

import (
	"strings"
	"testing"
)

func TestRenderCompletionsForSupportedShells(t *testing.T) {
	for _, shellName := range []string{"bash", "zsh", "fish", "sh", "dash", "ksh", "powershell", "pwsh"} {
		t.Run(shellName, func(t *testing.T) {
			output, err := Render("completions", shellName, "mail-cli")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output, "status") || (!strings.Contains(output, "--server") && !strings.Contains(output, "-l server")) {
				t.Fatalf("completion output omits supported command or flag: %q", output)
			}
		})
	}
}

func TestRenderInitAutoDetectsAndQuotesBinaryName(t *testing.T) {
	output, err := Render("init", "bash", "mail;touch")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "'mail;touch' --shell completions bash") {
		t.Fatalf("init output did not safely quote binary name: %q", output)
	}
	defaultOutput, err := Render("init", "", "mail-cli")
	if err != nil || !strings.Contains(defaultOutput, "--shell completions bash") {
		t.Fatalf("default shell output = %q, %v", defaultOutput, err)
	}
}

func TestRenderRejectsUnknownActionAndShell(t *testing.T) {
	for _, input := range [][2]string{{"bad", "bash"}, {"init", "unknown"}} {
		if _, err := Render(input[0], input[1], "mail"); err == nil {
			t.Errorf("Render(%q, %q) succeeded", input[0], input[1])
		}
	}
}
