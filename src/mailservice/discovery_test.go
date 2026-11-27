package mailservice

import (
	"strings"
	"testing"
)

func TestDiscoverUsesPlatformPackagesAndReportsMissingServices(t *testing.T) {
	components := discover("linux", "alpine", func(string) string { return "" })
	if len(components) != len(definitions) {
		t.Fatalf("component count = %d, want %d", len(components), len(definitions))
	}
	if components[0].Name != "postfix" || len(components[0].Packages) != 1 || components[0].Packages[0] != "postfix" {
		t.Fatalf("Alpine Postfix mapping = %#v", components[0])
	}
	if components[0].Installed || components[0].Executable != "" {
		t.Fatalf("missing Postfix was reported as installed: %#v", components[0])
	}
	if components[3].ServiceAccount != "amavis:amavis" {
		t.Fatalf("Alpine amavis account = %q", components[3].ServiceAccount)
	}
}

func TestDiscoverRecognizesExecutableWithoutChangingHost(t *testing.T) {
	components := discover("linux", "ubuntu", func(name string) string {
		if name == "dovecot" {
			return "/usr/sbin/dovecot"
		}
		return ""
	})
	if !components[1].Installed || components[1].Executable != "/usr/sbin/dovecot" {
		t.Fatalf("Dovecot discovery = %#v", components[1])
	}
	if components[1].ServiceAccount != "dovecot:dovecot" {
		t.Fatalf("Dovecot service account = %q", components[1].ServiceAccount)
	}
}

func TestDiscoverUsesDistroFamilyMappings(t *testing.T) {
	tests := []struct {
		goos, distribution, family string
	}{
		{"linux", "alpine", "alpine"},
		{"linux", "ubuntu debian", "debian"},
		{"linux", "fedora", "rhel"},
		{"freebsd", "", "freebsd"},
		{"darwin", "", ""},
		{"linux", "unknown", ""},
	}
	for _, test := range tests {
		if got := platformFamily(test.goos, test.distribution); got != test.family {
			t.Errorf("platformFamily(%q, %q) = %q, want %q", test.goos, test.distribution, got, test.family)
		}
	}
	components := discover("freebsd", "", func(string) string { return "" })
	if components[3].ServiceAccount != "vscan:vscan" {
		t.Fatalf("FreeBSD amavis account = %q", components[3].ServiceAccount)
	}
}

func TestUnknownDistributionHasNoGuessedPackageNames(t *testing.T) {
	components := discover("linux", "unknown", func(string) string { return "" })
	for _, component := range components {
		if len(component.Packages) != 0 {
			t.Errorf("unknown distribution got guessed packages for %s: %v", component.Name, component.Packages)
		}
	}
}

func TestParseDistributionIDReadsIDAndIDLike(t *testing.T) {
	got := parseDistributionID(strings.NewReader("NAME=Example Linux\nID=linuxmint\nID_LIKE=ubuntu debian\n"))
	if got != "linuxmint ubuntu debian" {
		t.Fatalf("distribution ids = %q", got)
	}
}
