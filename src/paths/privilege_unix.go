//go:build !windows

package paths

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func isRootUser() bool { return os.Geteuid() == 0 }

type ServiceIdentity struct {
	Name string
	Home string
	UID  int
	GID  int
}

var reservedServiceIDs = map[int]bool{
	65534: true,
	999:   true, 998: true, 997: true, 996: true, 995: true,
	994: true, 993: true, 992: true, 991: true, 990: true,
	989: true, 988: true, 987: true, 986: true, 985: true,
	984: true, 983: true, 982: true, 981: true, 980: true,
	101: true, 102: true, 103: true, 104: true, 105: true,
	106: true, 107: true, 108: true, 109: true, 110: true,
	170: true, 171: true, 172: true, 173: true, 174: true,
	175: true, 176: true, 177: true, 178: true, 179: true,
}

func EnsureServiceIdentity(name, home string) (ServiceIdentity, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !validServiceAccountName(name) {
		return ServiceIdentity{}, fmt.Errorf("invalid service account name %q", name)
	}
	if !filepath.IsAbs(home) {
		return ServiceIdentity{}, fmt.Errorf("service account home must be an absolute path")
	}
	if current, err := user.Lookup(name); err == nil {
		return validateExistingServiceIdentity(name, home, current)
	} else {
		var unknown user.UnknownUserError
		if !errors.As(err, &unknown) {
			return ServiceIdentity{}, fmt.Errorf("look up service account %q: %w", name, err)
		}
	}
	if !isRootUser() {
		return ServiceIdentity{}, fmt.Errorf("creating service account %q requires root", name)
	}
	id, err := findAvailableServiceID()
	if err != nil {
		return ServiceIdentity{}, err
	}
	if _, err := user.LookupGroup(name); err == nil {
		return ServiceIdentity{}, fmt.Errorf("group %q already exists without its service account; refusing to take it over", name)
	} else {
		var unknown user.UnknownGroupError
		if !errors.As(err, &unknown) {
			return ServiceIdentity{}, fmt.Errorf("look up service group %q: %w", name, err)
		}
	}
	if err := createServiceAccount(name, home, id); err != nil {
		return ServiceIdentity{}, err
	}
	created, err := user.Lookup(name)
	if err != nil {
		cleanupErr := rollbackCreatedServiceIdentity(name)
		if cleanupErr != nil {
			return ServiceIdentity{}, fmt.Errorf("verify created service account %q: %w; rollback failed: %v", name, err, cleanupErr)
		}
		return ServiceIdentity{}, fmt.Errorf("verify created service account %q: %w", name, err)
	}
	identity, err := validateCreatedServiceIdentity(name, home, id, created)
	if err != nil {
		cleanupErr := rollbackCreatedServiceIdentity(name)
		if cleanupErr != nil {
			return ServiceIdentity{}, fmt.Errorf("validate created service account %q: %w; rollback failed: %v", name, err, cleanupErr)
		}
		return ServiceIdentity{}, err
	}
	return identity, nil
}

func validateCreatedServiceIdentity(name, home string, id int, account *user.User) (ServiceIdentity, error) {
	identity, err := validateExistingServiceIdentity(name, home, account)
	if err != nil {
		return ServiceIdentity{}, err
	}
	if identity.UID != id || identity.GID != id || account.HomeDir != home {
		return ServiceIdentity{}, fmt.Errorf("created service account %q does not match requested UID, GID, and home", name)
	}
	return identity, nil
}

func rollbackCreatedServiceIdentity(name string) error {
	if err := rollbackServiceUser(name); err != nil {
		return err
	}
	return rollbackServiceGroup(name)
}

func validateExistingServiceIdentity(name, home string, account *user.User) (ServiceIdentity, error) {
	uid, uidErr := strconv.Atoi(account.Uid)
	gid, gidErr := strconv.Atoi(account.Gid)
	if uidErr != nil || gidErr != nil || uid < 1 || uid > 999 || gid < 1 || gid > 999 {
		return ServiceIdentity{}, fmt.Errorf("existing account %q is not a safe system service account", name)
	}
	nologin, err := serviceNoLoginShell(name)
	if err != nil {
		return ServiceIdentity{}, fmt.Errorf("verify service account shell %q: %w", name, err)
	}
	if !nologin {
		return ServiceIdentity{}, fmt.Errorf("existing account %q has a login shell; refusing to take it over", name)
	}
	group, err := user.LookupGroup(name)
	if err != nil {
		return ServiceIdentity{}, fmt.Errorf("existing account %q has no matching primary group %q: %w", name, name, err)
	}
	groupID, err := strconv.Atoi(group.Gid)
	if err != nil || groupID != gid {
		return ServiceIdentity{}, fmt.Errorf("existing account %q primary group conflicts with service group %q", name, name)
	}
	return ServiceIdentity{Name: name, Home: home, UID: uid, GID: gid}, nil
}

func serviceNoLoginShell(name string) (bool, error) {
	shell := ""
	switch runtime.GOOS {
	case "darwin":
		output, err := exec.Command("dscl", ".", "-read", "/Users/"+name, "UserShell").Output()
		if err != nil {
			return false, err
		}
		fields := strings.Fields(string(output))
		if len(fields) >= 2 {
			shell = fields[len(fields)-1]
		}
	case "freebsd":
		output, err := exec.Command("pw", "usershow", name, "-7").Output()
		if err != nil {
			return false, err
		}
		fields := strings.Split(strings.TrimSpace(string(output)), ":")
		if len(fields) >= 10 {
			shell = fields[len(fields)-1]
		}
	default:
		if getent, err := exec.LookPath("getent"); err == nil {
			output, err := exec.Command(getent, "passwd", name).Output()
			if err != nil {
				return false, err
			}
			fields := strings.Split(strings.TrimSpace(string(output)), ":")
			if len(fields) >= 7 && fields[0] == name {
				shell = fields[6]
			}
		}
		if shell == "" {
			data, err := os.ReadFile("/etc/passwd")
			if err != nil {
				return false, err
			}
			for _, row := range strings.Split(string(data), "\n") {
				fields := strings.Split(row, ":")
				if len(fields) >= 7 && fields[0] == name {
					shell = fields[6]
					break
				}
			}
		}
	}
	switch filepath.Clean(shell) {
	case "/sbin/nologin", "/usr/sbin/nologin", "/usr/bin/false", "/bin/false":
		return true, nil
	default:
		return false, nil
	}
}

func findAvailableServiceID() (int, error) {
	minimum, maximum := 200, 899
	if runtime.GOOS == "darwin" {
		maximum = 399
	}
	for id := maximum; id >= minimum; id-- {
		if reservedServiceIDs[id] {
			continue
		}
		if _, err := user.LookupId(strconv.Itoa(id)); err == nil {
			continue
		} else {
			var unknown user.UnknownUserIdError
			if !errors.As(err, &unknown) {
				return 0, fmt.Errorf("check service UID %d: %w", id, err)
			}
		}
		if _, err := user.LookupGroupId(strconv.Itoa(id)); err == nil {
			continue
		} else {
			var unknown user.UnknownGroupIdError
			if !errors.As(err, &unknown) {
				return 0, fmt.Errorf("check service GID %d: %w", id, err)
			}
		}
		return id, nil
	}
	return 0, fmt.Errorf("no available service UID/GID in safe range %d-%d", minimum, maximum)
}

func createServiceAccount(name, home string, id int) error {
	var groupCreated bool
	var userCreated bool
	var err error
	switch runtime.GOOS {
	case "darwin":
		groupCreated, userCreated, err = createMacServiceAccount(name, home, id)
	case "freebsd":
		err = runAccountCommand("pw", "groupadd", "-n", name, "-g", strconv.Itoa(id))
		if err == nil {
			groupCreated = true
			shell, shellErr := noLoginShell()
			if shellErr != nil {
				err = shellErr
			} else {
				err = runAccountCommand("pw", "useradd", "-n", name, "-u", strconv.Itoa(id), "-g", strconv.Itoa(id), "-d", home, "-s", shell, "-c", name+" service account")
				userCreated = err == nil
			}
		}
	default:
		groupadd, groupErr := exec.LookPath("groupadd")
		useradd, userErr := exec.LookPath("useradd")
		if groupErr == nil && userErr == nil {
			err = runAccountCommand(groupadd, "--system", "--gid", strconv.Itoa(id), name)
			if err == nil {
				groupCreated = true
				shell, shellErr := noLoginShell()
				if shellErr != nil {
					err = shellErr
				} else {
					err = runAccountCommand(useradd, "--system", "--uid", strconv.Itoa(id), "--gid", strconv.Itoa(id), "--home-dir", home, "--no-create-home", "--shell", shell, "--comment", name+" service account", name)
					userCreated = err == nil
				}
			}
		} else {
			err = runAccountCommand("addgroup", "-S", "-g", strconv.Itoa(id), name)
			if err == nil {
				groupCreated = true
				shell, shellErr := noLoginShell()
				if shellErr != nil {
					err = shellErr
				} else {
					err = runAccountCommand("adduser", "-S", "-D", "-H", "-u", strconv.Itoa(id), "-G", name, "-h", home, "-s", shell, "-g", name+" service account", name)
					userCreated = err == nil
				}
			}
		}
	}
	if err != nil {
		if !userCreated && groupCreated {
			userCreated = serviceAccountCreatedByAttempt(name, id)
		}
		var rollbackErr error
		if userCreated {
			rollbackErr = rollbackServiceUser(name)
			if rollbackErr == nil && groupCreated {
				rollbackErr = rollbackServiceGroup(name)
			}
		} else if groupCreated {
			rollbackErr = rollbackServiceGroup(name)
		}
		if rollbackErr != nil {
			return fmt.Errorf("create service account %q: %w; rollback failed: %v", name, err, rollbackErr)
		}
		return fmt.Errorf("create service account %q: %w", name, err)
	}
	return nil
}

func createMacServiceAccount(name, home string, id int) (bool, bool, error) {
	if err := runAccountCommand("dscl", ".", "-create", "/Groups/"+name); err != nil {
		return serviceGroupRecordExists(name), false, err
	}
	groupCommands := [][]string{
		{"dscl", ".", "-create", "/Groups/" + name, "PrimaryGroupID", strconv.Itoa(id)},
		{"dscl", ".", "-create", "/Groups/" + name, "Password", "*"},
	}
	for _, command := range groupCommands {
		if err := runAccountCommand(command[0], command[1:]...); err != nil {
			return true, false, err
		}
	}
	if err := runAccountCommand("dscl", ".", "-create", "/Users/"+name); err != nil {
		return true, serviceUserRecordExists(name), err
	}
	userCommands := [][]string{
		{"dscl", ".", "-create", "/Users/" + name, "UniqueID", strconv.Itoa(id)},
		{"dscl", ".", "-create", "/Users/" + name, "PrimaryGroupID", strconv.Itoa(id)},
		{"dscl", ".", "-create", "/Users/" + name, "UserShell", "/usr/bin/false"},
		{"dscl", ".", "-create", "/Users/" + name, "RealName", name + " service account"},
		{"dscl", ".", "-create", "/Users/" + name, "NFSHomeDirectory", home},
		{"dscl", ".", "-create", "/Users/" + name, "Password", "*"},
		{"dscl", ".", "-create", "/Users/" + name, "IsHidden", "1"},
	}
	for _, command := range userCommands {
		if err := runAccountCommand(command[0], command[1:]...); err != nil {
			return true, true, err
		}
	}
	return true, true, nil
}

func serviceGroupRecordExists(name string) bool {
	return exec.Command("dscl", ".", "-read", "/Groups/"+name).Run() == nil
}

func serviceUserRecordExists(name string) bool {
	return exec.Command("dscl", ".", "-read", "/Users/"+name).Run() == nil
}

func serviceAccountCreatedByAttempt(name string, id int) bool {
	account, err := user.Lookup(name)
	if err != nil {
		return false
	}
	uid, uidErr := strconv.Atoi(account.Uid)
	gid, gidErr := strconv.Atoi(account.Gid)
	group, groupErr := user.LookupGroup(name)
	if groupErr != nil {
		return false
	}
	groupID, groupIDErr := strconv.Atoi(group.Gid)
	return uidErr == nil && gidErr == nil && groupIDErr == nil && uid == id && gid == id && groupID == id
}

func rollbackServiceUser(name string) error {
	switch runtime.GOOS {
	case "freebsd":
		return runAccountCommand("pw", "userdel", "-n", name)
	case "darwin":
		return runAccountCommand("dscl", ".", "-delete", "/Users/"+name)
	default:
		if userdel, err := exec.LookPath("userdel"); err == nil {
			return runAccountCommand(userdel, name)
		}
		return runAccountCommand("deluser", name)
	}
}

func rollbackServiceGroup(name string) error {
	if runtime.GOOS == "freebsd" {
		return runAccountCommand("pw", "groupdel", "-n", name)
	}
	if runtime.GOOS == "darwin" {
		return runAccountCommand("dscl", ".", "-delete", "/Groups/"+name)
	}
	if groupdel, err := exec.LookPath("groupdel"); err == nil {
		return runAccountCommand(groupdel, name)
	}
	return runAccountCommand("delgroup", name)
}

func runAccountCommand(name string, args ...string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("required account-management tool %q is unavailable", name)
	}
	if output, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %s", filepath.Base(name), strings.TrimSpace(string(output)))
	}
	return nil
}

func noLoginShell() (string, error) {
	for _, shell := range []string{"/sbin/nologin", "/usr/sbin/nologin", "/usr/bin/false", "/bin/false"} {
		if info, err := os.Stat(shell); err == nil && !info.IsDir() {
			return shell, nil
		}
	}
	return "", fmt.Errorf("no supported nologin or false shell is available")
}

func DropPrivileges(identity ServiceIdentity) error {
	if !isRootUser() {
		if os.Getuid() == identity.UID && os.Getgid() == identity.GID {
			return setServiceHome(identity)
		}
		return fmt.Errorf("cannot switch to service account %q without root", identity.Name)
	}
	if err := unix.Setgroups([]int{}); err != nil {
		return fmt.Errorf("clear supplementary groups: %w", err)
	}
	if err := unix.Setgid(identity.GID); err != nil {
		return fmt.Errorf("set service GID: %w", err)
	}
	if err := unix.Setuid(identity.UID); err != nil {
		return fmt.Errorf("set service UID: %w", err)
	}
	if os.Geteuid() == 0 || os.Getuid() != identity.UID || os.Getgid() != identity.GID {
		return fmt.Errorf("privilege drop to service account %q did not take effect", identity.Name)
	}
	return setServiceHome(identity)
}

func setServiceHome(identity ServiceIdentity) error {
	if err := os.Setenv("HOME", identity.Home); err != nil {
		return fmt.Errorf("set service HOME: %w", err)
	}
	if err := os.Setenv("USER", identity.Name); err != nil {
		return fmt.Errorf("set service USER: %w", err)
	}
	return nil
}

func validServiceAccountName(name string) bool {
	if len(name) == 0 || len(name) > 31 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, char := range name[1:] {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' && char != '-' {
			return false
		}
	}
	return true
}
