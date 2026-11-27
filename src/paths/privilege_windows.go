//go:build windows

package paths

import (
	"fmt"
	"os"
	"os/user"
	"strings"
)

func isRootUser() bool { return false }

type ServiceIdentity struct {
	Name string
	Home string
	UID  int
	GID  int
}

func EnsureServiceIdentity(name, home string) (ServiceIdentity, error) {
	current, err := user.Current()
	if err != nil {
		return ServiceIdentity{}, fmt.Errorf("look up current Windows service identity: %w", err)
	}
	virtualAccount := `NT SERVICE\` + strings.ToLower(name)
	if !strings.EqualFold(current.Username, virtualAccount) {
		return ServiceIdentity{Name: current.Username, Home: home}, nil
	}
	return ServiceIdentity{Name: virtualAccount, Home: home}, nil
}

func DropPrivileges(identity ServiceIdentity) error {
	current, err := user.Current()
	if err != nil {
		return fmt.Errorf("verify current Windows identity: %w", err)
	}
	if identity.Name != "" && !strings.EqualFold(identity.Name, current.Username) {
		return fmt.Errorf("Windows process identity %q does not match the installed virtual service account %q", current.Username, identity.Name)
	}
	if identity.Home != "" {
		if err := os.Setenv("HOME", identity.Home); err != nil {
			return fmt.Errorf("set service HOME: %w", err)
		}
	}
	return nil
}
