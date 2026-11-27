package theme

import "testing"

func TestTerminalPaletteFor(t *testing.T) {
	if got := TerminalPaletteFor("dark"); got != TerminalPaletteDark {
		t.Fatalf("dark palette = %#v", got)
	}
	if got := TerminalPaletteFor("light"); got != TerminalPaletteLight {
		t.Fatalf("light palette = %#v", got)
	}
}

func TestThemePalettesExposeRequiredRoles(t *testing.T) {
	for name, palette := range map[string]ThemePalette{"dark": ThemePaletteDark, "light": ThemePaletteLight} {
		for role, value := range map[string]string{
			"background": palette.Background, "foreground": palette.Foreground,
			"primary": palette.Primary, "secondary": palette.Secondary,
			"accent": palette.Accent, "success": palette.Success,
			"warning": palette.Warning, "error": palette.Error,
			"info": palette.Info, "surface": palette.Surface,
			"surface_alt": palette.SurfaceAlt, "border": palette.Border,
			"muted": palette.Muted,
		} {
			if len(value) != 7 || value[0] != '#' {
				t.Errorf("%s %s color = %q", name, role, value)
			}
		}
	}
}
