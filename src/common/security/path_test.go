package security

import (
	"path/filepath"
	"testing"
)

func TestSafePath(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		valid       bool
	}{
		{"/myadmin/", "myadmin", true},
		{"//my//admin", "my/admin", true},
		{"../secret", "", false},
		{"Admin", "", false},
		{"a/b", "a/b", true},
	} {
		got, err := SafePath(tc.input)
		if (err == nil) != tc.valid || got != tc.want {
			t.Errorf("SafePath(%q) = %q, %v; want %q, valid=%v", tc.input, got, err, tc.want, tc.valid)
		}
	}
}

func TestSafeDirPathPreservesAbsolutePaths(t *testing.T) {
	input := filepath.Join(string(filepath.Separator), "var", "lib", "mail")
	got, err := SafeDirPath(input)
	if err != nil || got != input || !filepath.IsAbs(got) {
		t.Fatalf("SafeDirPath(%q) = %q, %v", input, got, err)
	}
	if _, err := SafeDirPath("data/../outside"); err == nil {
		t.Fatal("SafeDirPath accepted traversal")
	}
}

func TestSafeResourcePath(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		valid       bool
	}{
		{"static/css/main.css", "static/css/main.css", true},
		{"Report_2024.pdf", "Report_2024.pdf", true},
		{"report..pdf", "report..pdf", true},
		{"../secret", "", false},
		{".hidden", "", false},
		{"", "", false},
	} {
		got, err := SafeResourcePath(tc.input)
		if (err == nil) != tc.valid || got != tc.want {
			t.Errorf("SafeResourcePath(%q) = %q, %v; want %q, valid=%v", tc.input, got, err, tc.want, tc.valid)
		}
	}
}

func TestSafeFilePathKeepsResourceInsideBase(t *testing.T) {
	base := filepath.Join("tmp", "mail")
	got, err := SafeFilePath(base, "docs/readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "docs", "readme.txt")
	want, _ = filepath.Abs(want)
	if got != want {
		t.Fatalf("SafeFilePath = %q, want %q", got, want)
	}
}
