package shell

import (
	"fmt"
	"path/filepath"
	"strings"
)

var supported = map[string]bool{
	"bash": true, "zsh": true, "fish": true, "sh": true,
	"dash": true, "ksh": true, "powershell": true, "pwsh": true,
}

// Render returns the completion script or shell initialization command for a binary.
func Render(action, shell, binaryName string) (string, error) {
	if action != "init" && action != "completions" {
		return "", fmt.Errorf("unknown shell action %q", action)
	}
	if shell == "" {
		shell = "bash"
	}
	if !supported[shell] {
		return "", fmt.Errorf("unsupported shell %q", shell)
	}
	binaryName = filepath.Base(binaryName)
	if binaryName == "." || binaryName == string(filepath.Separator) || binaryName == "" {
		return "", fmt.Errorf("invalid binary name")
	}
	if action == "init" {
		switch shell {
		case "bash", "zsh":
			return fmt.Sprintf("source <(%s --shell completions %s)\n", posixQuote(binaryName), shell), nil
		case "fish":
			return fmt.Sprintf("%s --shell completions fish | source\n", fishQuote(binaryName)), nil
		case "powershell", "pwsh":
			return fmt.Sprintf("Invoke-Expression (& %s --shell completions powershell)\n", psQuote(binaryName)), nil
		default:
			return fmt.Sprintf("eval \"$(%s --shell completions %s)\"\n", posixQuote(binaryName), shell), nil
		}
	}
	return completionScript(shell, binaryName), nil
}

func completionScript(shell, name string) string {
	commands := "status about"
	flags := "--help --version --server --token --token-file --config --debug --color --lang --output --shell"
	switch shell {
	case "bash":
		return fmt.Sprintf("_mail_complete() {\n  local cur=\"${COMP_WORDS[COMP_CWORD]}\"\n  COMPREPLY=( $(compgen -W \"%s %s\" -- \"$cur\") )\n}\ncomplete -F _mail_complete -- %s\n", commands, flags, posixQuote(name))
	case "zsh":
		return fmt.Sprintf("#compdef %s\n_arguments '*:command:(%s %s)'\n", name, commands, flags)
	case "fish":
		q := fishQuote(name)
		return fmt.Sprintf("complete -c %s -f -a '%s'\ncomplete -c %s -l server -r\ncomplete -c %s -l token -r\ncomplete -c %s -l token-file -r\ncomplete -c %s -l config -r\ncomplete -c %s -l color -r -a 'auto yes no'\ncomplete -c %s -l lang -r\ncomplete -c %s -l output -r -a 'json table plain'\ncomplete -c %s -l shell -r -a 'completions init help'\ncomplete -c %s -s h -l help\ncomplete -c %s -s v -l version\ncomplete -c %s -l debug\n", q, commands, q, q, q, q, q, q, q, q, q, q, q)
	case "powershell", "pwsh":
		return fmt.Sprintf("Register-ArgumentCompleter -Native -CommandName %s -ScriptBlock { param($wordToComplete, $commandAst, $cursorPosition) '%s','%s' | Where-Object { $_ -like \"$wordToComplete*\" } }\n", psQuote(name), strings.ReplaceAll(commands, " ", "','"), strings.ReplaceAll(flags, " ", "','"))
	default:
		return fmt.Sprintf("# Basic POSIX completion candidates for %s\n_mail_completion_candidates() {\n  printf '%%s\\n' '%s' '%s'\n}\n", name, commands, flags)
	}
}

func posixQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func fishQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "\\'") + "'"
}

func psQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
