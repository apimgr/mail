package mailservice

import (
	"reflect"
	"testing"
	"time"
)

func TestManagerCommandUsesAllowlistedArguments(t *testing.T) {
	for _, test := range []struct {
		manager string
		want    string
	}{
		{manager: "systemd", want: "systemctl"},
		{manager: "openrc", want: "rc-service"},
		{manager: "sysv", want: "service"},
		{manager: "freebsd", want: "service"},
	} {
		command, args, err := managerCommand(serviceManager{name: test.manager}, "postfix", "restart")
		if err != nil || command != test.want || !reflect.DeepEqual(args, []string{"postfix", "restart"}) && test.manager != "systemd" {
			t.Errorf("managerCommand(%q) = %q %v, err=%v", test.manager, command, args, err)
		}
		if test.manager == "systemd" && !reflect.DeepEqual(args, []string{"restart", "postfix"}) {
			t.Errorf("systemd args = %v", args)
		}
	}
	if _, _, err := managerCommand(serviceManager{name: "unsupported"}, "postfix", "start"); err != ErrManagerMissing {
		t.Fatalf("unsupported manager error = %v", err)
	}
}

func TestInspectComponentWithoutManagerOrInstallation(t *testing.T) {
	notInstalled := inspectComponent(serviceManager{name: "systemd"}, Component{Name: "postfix"})
	if notInstalled.Status != "not_installed" || notInstalled.Running != nil {
		t.Fatalf("missing component inspection = %+v", notInstalled)
	}
	installed := inspectComponent(serviceManager{}, Component{Name: "policyd", Installed: true})
	if installed.Status != "unknown" || installed.Running != nil {
		t.Fatalf("installed component without manager = %+v", installed)
	}
}

func TestParseSystemdTimestampAndElapsedSince(t *testing.T) {
	for _, value := range []string{"Mon 2026-11-23 10:30:00 UTC", "Mon 2026-11-23 10:30:00 +0000"} {
		if parsed := parseSystemdTimestamp(value); parsed.IsZero() {
			t.Errorf("parseSystemdTimestamp(%q) returned zero", value)
		}
	}
	if parsed := parseSystemdTimestamp("not a timestamp"); !parsed.IsZero() {
		t.Fatalf("invalid timestamp parsed as %s", parsed)
	}
	if got := elapsedSince(time.Time{}); got != "" {
		t.Fatalf("elapsedSince(zero) = %q", got)
	}
	if got := elapsedSince(time.Now().Add(time.Minute)); got != "" {
		t.Fatalf("elapsedSince(future) = %q", got)
	}
	if got := elapsedSince(time.Now().Add(-time.Hour)); got == "" {
		t.Fatal("elapsedSince(past) returned empty string")
	}
}

func TestServiceUnitAndPIDParsing(t *testing.T) {
	if got := serviceUnit("amavis"); got != "amavisd" {
		t.Fatalf("amavis service unit = %q", got)
	}
	for input, want := range map[string]int{"postfix is running as pid 1234": 1234, "stopped": 0, "pid not-a-number": 0} {
		if got := parsePID(input); got != want {
			t.Errorf("parsePID(%q) = %d, want %d", input, got, want)
		}
	}
}
