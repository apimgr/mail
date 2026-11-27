package mailservice

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Component describes one supported mail-related service without changing host state.
type Component struct {
	Name           string   `json:"name"`
	Installed      bool     `json:"installed"`
	Status         string   `json:"status"`
	Running        *bool    `json:"running,omitempty"`
	Enabled        *bool    `json:"enabled,omitempty"`
	PID            int      `json:"pid,omitempty"`
	Uptime         string   `json:"uptime,omitempty"`
	Version        string   `json:"version,omitempty"`
	Executable     string   `json:"executable,omitempty"`
	Packages       []string `json:"packages,omitempty"`
	ServiceAccount string   `json:"service_account,omitempty"`
}

type componentDefinition struct {
	name        string
	executables []string
	packages    map[string][]string
	account     string
}

var definitions = []componentDefinition{
	{
		name: "postfix", executables: []string{"postfix", "postconf", "postqueue"},
		packages: map[string][]string{
			"alpine": {"postfix"}, "debian": {"postfix", "postfix-mysql", "postfix-pgsql"},
			"rhel": {"postfix"}, "freebsd": {"postfix"},
		},
		account: "postfix:postfix",
	},
	{
		name: "dovecot", executables: []string{"dovecot"},
		packages: map[string][]string{
			"alpine":  {"dovecot", "dovecot-pigeonhole-plugin"},
			"debian":  {"dovecot-core", "dovecot-imapd", "dovecot-pop3d", "dovecot-lmtpd", "dovecot-managesieved"},
			"rhel":    {"dovecot", "dovecot-mysql", "dovecot-pgsql", "dovecot-pigeonhole"},
			"freebsd": {"dovecot", "dovecot-pigeonhole"},
		},
		account: "dovecot:dovecot",
	},
	{
		name: "spamassassin", executables: []string{"spamassassin", "spamd"},
		packages: map[string][]string{
			"alpine": {"spamassassin"}, "debian": {"spamassassin"},
			"rhel": {"spamassassin"}, "freebsd": {"spamassassin"},
		},
	},
	{
		name: "amavis", executables: []string{"amavisd", "amavisd-new"},
		packages: map[string][]string{
			"alpine": {"amavisd-new"}, "debian": {"amavisd-new"},
			"rhel": {"amavisd-new"}, "freebsd": {"amavisd-new"},
		},
		account: "amavis:amavis",
	},
	{
		name: "pyzor", executables: []string{"pyzor"},
		packages: map[string][]string{
			"alpine": {"pyzor"}, "debian": {"pyzor"},
			"rhel": {"pyzor"}, "freebsd": {"pyzor"},
		},
	},
	{
		name: "razor", executables: []string{"razor-admin", "razor-check"},
		packages: map[string][]string{
			"alpine": {"razor"}, "debian": {"razor"},
			"rhel": {"razor"}, "freebsd": {"razor"},
		},
	},
	{
		name: "rspamd", executables: []string{"rspamd"},
		packages: map[string][]string{
			"alpine": {"rspamd"}, "debian": {"rspamd"},
			"rhel": {"rspamd"}, "freebsd": {"rspamd"},
		},
	},
	{
		name: "clamav", executables: []string{"clamd", "clamdscan", "clamscan", "freshclam"},
		packages: map[string][]string{
			"alpine": {"clamav", "clamav-daemon"}, "debian": {"clamav", "clamav-daemon"},
			"rhel":    {"clamav", "clamav-server", "clamav-update"},
			"freebsd": {"clamav"},
		},
	},
	{
		name: "opendkim", executables: []string{"opendkim", "opendkim-testkey"},
		packages: map[string][]string{
			"alpine": {"opendkim", "opendkim-utils"}, "debian": {"opendkim", "opendkim-tools"},
			"rhel": {"opendkim"}, "freebsd": {"opendkim"},
		},
		account: "opendkim:opendkim",
	},
	{
		name: "fail2ban", executables: []string{"fail2ban-client", "fail2ban-server"},
		packages: map[string][]string{
			"alpine": {"fail2ban"}, "debian": {"fail2ban"},
			"rhel": {"fail2ban"}, "freebsd": {"fail2ban"},
		},
	},
	{name: "policyd", executables: []string{"policyd", "policyd-spf", "policyd-rate-limit"}},
	{name: "postgrey", executables: []string{"postgrey"}},
}

// Discover reports supported components and package names for the current host.
func Discover() []Component {
	return discover(runtime.GOOS, readDistributionID(), findExecutable)
}

func discover(goos, distribution string, find func(string) string) []Component {
	family := platformFamily(goos, distribution)
	components := make([]Component, 0, len(definitions))
	for _, definition := range definitions {
		component := Component{Name: definition.name}
		if packages, ok := definition.packages[family]; ok {
			component.Packages = append([]string(nil), packages...)
		}
		if family == "freebsd" && definition.name == "amavis" {
			component.ServiceAccount = "vscan:vscan"
		} else if family != "" {
			component.ServiceAccount = definition.account
		}
		for _, executable := range definition.executables {
			if path := find(executable); path != "" {
				component.Installed = true
				component.Executable = path
				break
			}
		}
		components = append(components, component)
	}
	return components
}

func platformFamily(goos, distribution string) string {
	switch goos {
	case "freebsd":
		return "freebsd"
	case "linux":
		for _, id := range strings.Fields(strings.ToLower(distribution)) {
			switch id {
			case "alpine":
				return "alpine"
			case "debian", "ubuntu":
				return "debian"
			case "rhel", "fedora", "centos", "rocky", "almalinux", "ol", "amzn":
				return "rhel"
			}
		}
	}
	return ""
}

func readDistributionID() string {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	return parseDistributionID(file)
}

func parseDistributionID(file io.Reader) string {
	scanner := bufio.NewScanner(file)
	var id, like string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "ID=") {
			id = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "ID=")), "\"'")
		} else if strings.HasPrefix(line, "ID_LIKE=") {
			like = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "ID_LIKE=")), "\"'")
		}
	}
	return strings.TrimSpace(id + " " + like)
}

func findExecutable(name string) string {
	if runtime.GOOS == "windows" {
		path, err := exec.LookPath(name)
		if err == nil {
			return path
		}
		return ""
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	for _, directory := range []string{"/usr/sbin", "/sbin", "/usr/local/sbin", "/usr/bin", "/usr/local/bin"} {
		path := filepath.Join(directory, name)
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return path
		}
	}
	return ""
}
