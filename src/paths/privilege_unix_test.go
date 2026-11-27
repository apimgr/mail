//go:build !windows

package paths

import (
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"testing"
)

func TestServiceAccountNameValidation(t *testing.T) {
	for _, name := range []string{"mail", "mail_service2", "mail-service"} {
		if !validServiceAccountName(name) {
			t.Errorf("validServiceAccountName(%q) = false", name)
		}
	}
	for _, name := range []string{"", "-mail", "Mail", "mail/name", strings.Repeat("a", 32)} {
		if validServiceAccountName(name) {
			t.Errorf("validServiceAccountName(%q) = true", name)
		}
	}
}

func TestEnsureServiceIdentityRejectsInvalidInput(t *testing.T) {
	if _, err := EnsureServiceIdentity("../mail", "/var/lib/mail"); err == nil {
		t.Fatal("invalid account name was accepted")
	}
	if _, err := EnsureServiceIdentity("mail", "relative/home"); err == nil {
		t.Fatal("relative service home was accepted")
	}
}

func TestFindAvailableServiceIDUsesSafeRange(t *testing.T) {
	id, err := findAvailableServiceID()
	if err != nil {
		t.Fatal(err)
	}
	maximum := 899
	if runtime.GOOS == "darwin" {
		maximum = 399
	}
	if id < 200 || id > maximum || reservedServiceIDs[id] {
		t.Fatalf("findAvailableServiceID() = %d, outside the safe range", id)
	}
}

func TestValidateExistingServiceIdentityRejectsRoot(t *testing.T) {
	account := &user.User{Username: "root", Uid: "0", Gid: "0", HomeDir: "/root"}
	if _, err := validateExistingServiceIdentity("root", "/var/lib/mail", account); err == nil {
		t.Fatal("root account was accepted as the service identity")
	}
}

func TestValidateExistingServiceIdentityRejectsUnsafeIDs(t *testing.T) {
	for _, account := range []*user.User{
		{Username: "mail", Uid: "not-a-number", Gid: "8"},
		{Username: "mail", Uid: "8", Gid: "1000"},
	} {
		if _, err := validateExistingServiceIdentity("mail", "/var/lib/mail", account); err == nil {
			t.Fatalf("unsafe account IDs were accepted: uid=%q gid=%q", account.Uid, account.Gid)
		}
	}
}

func TestSetServiceHome(t *testing.T) {
	identity := ServiceIdentity{Name: "mail", Home: "/var/lib/mail"}
	if err := setServiceHome(identity); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("HOME"); got != identity.Home {
		t.Errorf("HOME = %q, want %q", got, identity.Home)
	}
	if got := os.Getenv("USER"); got != identity.Name {
		t.Errorf("USER = %q, want %q", got, identity.Name)
	}
}

func TestServiceHelpersRejectUnavailableResources(t *testing.T) {
	if _, err := noLoginShell(); err != nil {
		t.Fatalf("noLoginShell() error = %v", err)
	}
	if err := runAccountCommand("mail-account-tool-that-does-not-exist"); err == nil {
		t.Fatal("missing account-management tool was accepted")
	}
	if _, err := serviceNoLoginShell("codex-account-that-does-not-exist"); err == nil {
		t.Fatal("missing account was accepted as a no-login account")
	}
}

func TestDropPrivilegesHelper(t *testing.T) {
	if os.Getenv("MAIL_DROP_PRIVILEGES_HELPER") == "1" {
		if err := DropPrivileges(ServiceIdentity{Name: "mail-test", Home: "/tmp", UID: 65534, GID: 65534}); err != nil {
			t.Fatalf("DropPrivileges() error = %v", err)
		}
		if os.Geteuid() == 0 || os.Getuid() != 65534 || os.Getgid() != 65534 {
			t.Fatalf("identity after drop = %d:%d euid=%d", os.Getuid(), os.Getgid(), os.Geteuid())
		}
		os.Exit(0)
	}
	if os.Geteuid() != 0 {
		t.Skip("subprocess privilege drop requires root")
	}
	command := exec.Command(os.Args[0], "-test.run=^TestDropPrivilegesHelper$")
	command.Env = append(os.Environ(), "MAIL_DROP_PRIVILEGES_HELPER=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("privilege-drop helper failed: %v: %s", err, output)
	}
}
