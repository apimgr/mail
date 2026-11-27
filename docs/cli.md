# Command line tools

## Server binary

The server binary uses Go's standard flag parser for `--help`, `--version`,
`--status`, configuration paths, address, port, mode, color, language, and the
shell, service, maintenance, and update command flags. `--status` checks the
local JSON health endpoint with a bounded timeout. `--shell` completion and
init commands work for bash, zsh, fish, POSIX-style shells, and PowerShell.
On Unix, daemon re-execution is available for `--daemon` and
`server.daemonize: true` outside application containers. The `--service`
command family detects the current platform's service manager and supports
status, install, start, stop, restart, reload, disable, and uninstall. An
unprivileged invocation manages the per-user service where supported; system
service actions require an elevated process. Uninstall requires interactive
confirmation and removes application data while keeping the binary. Startup
reads `server.port`, `server.address`, `server.daemonize`, `server.pidfile`,
and `server.logs` from `server.yml` and ordered `yaml.d` files before binding.
When started as root, the server
prepares the `mail` service account and paths, binds one privileged HTTP
listener if needed, drops privileges, then initializes logging, PID state, and
the full configuration. Dual HTTP/HTTPS listeners, TLS listeners, and raw mail
protocol sockets are not implemented yet. `--update check`, `--update yes`, and
`--update branch stable|beta|devel` are implemented, including checksum
verification before replacement. Scheduled checks honor the defer window.
When `auto_install` is enabled, a process that started as root stages a
verified release as its service user, gracefully shuts down, and returns 75;
the next root startup re-fetches the official checksum and applies it. A
non-elevated process records that automatic installation was skipped and
notifies the operator. Supervisor restart behavior depends on the service
manager policy. Windows service installation uses a virtual service account;
Windows service dispatcher integration remains incomplete.
`--maintenance update [CMD]` dispatches to the same updater and defaults to
`yes` when no command is supplied. `--maintenance email test [--to ADDRESS]`
validates SMTP and sends a test message to the configured reply-to address or
an explicit recipient; `--dry-run` previews and validates every email template
without sending. `--maintenance mode production|development` changes the
configured runtime mode; the server must be restarted to apply it. These
commands require the operator token or an elevated account. Backup, restore,
setup, secret rotation, and security-report maintenance operations remain
incomplete.

### Server flags

| Flag | Description |
|---|---|
| `--help`, `-h` | Show help. |
| `--version`, `-v` | Show version and build information. |
| `--status` | Query the local health endpoint. |
| `--config`, `--data`, `--cache`, `--log`, `--backup` | Override runtime directories. |
| `--pid` | Override the PID-file path. |
| `--address`, `--port` | Set the listener IP and port. Port 0 is invalid. |
| `--baseurl` | Set the URL path prefix. |
| `--mode`, `--debug` | Select runtime mode and debug endpoints. |
| `--daemon` | Re-execute as a background daemon outside containers. |
| `--lang` | Set the output language. |
| `--color auto\|yes\|no` | Set terminal color behavior. |
| `--shell completions [SHELL]`, `--shell init [SHELL]` | Generate shell integration. |
| `--service COMMAND` | Manage the detected system or per-user service (`status`, `install`, `start`, `stop`, `restart`, `reload`, `disable`, `uninstall`). |
| `--maintenance COMMAND` | Maintenance entrypoint; `update`, `mode`, and `email test [--to ADDRESS] [--dry-run]` are implemented. Backup, restore, setup, secret rotation, and security-report commands remain incomplete. |
| `--update [check\|yes\|branch NAME]` | Check, install, or select the update channel. |

`--i2p` is accepted, but I2P startup is not yet implemented.

`--port` accepts one port from 1 through 65535. Port `0` is rejected. A
non-root process cannot bind ports below 1024. The listen address must be an IP
literal; the default is `[::]`.

## Client binary

`mail-cli` uses Cobra and exposes server information, service, domain, user,
alias, forward, queue, config, statistics, logs, and health commands. Mailbox
list/get/create/update/delete/password and CSV import, domain CRUD/DNS/DKIM,
alias list/create/delete, virtual-mailbox forwarding list/create/delete, and service status/control have matching
operator-protected server handlers. Queue list/detail, message flush/hold/
release/delete, flush-all, and queue statistics have operator-protected
handlers; the Postfix process permissions still determine whether privileged
queue mutations can run. Queue POST actions require `--force` in non-interactive
runs or terminal confirmation, like DELETE commands. Config, delivery/spam
statistics, and log operations remain pending.

```sh
mail-cli --server https://mail.example status
mail-cli about --server https://mail.example
mail-cli users import ./mailboxes.csv --server https://mail.example --token "$MAIL_TOKEN"
```

The mailbox CSV header is `email,password,quota_bytes,enabled`. Each input row
returns an individual success or failure result; passwords are never included
in responses or audit details.

The server URL can also come from `MAIL_SERVER_PRIMARY`,
`~/.config/apimgr/mail/cli.yml` (`server.primary`), `OFFICIAL_SITE`, or the
embedded official site. The API version comes from `server.api_version` and
defaults to `v1`. A token can come from
`MAIL_TOKEN`, `auth.token`, `auth.token_file`, `--token TOKEN`, or
`--token-file PATH`; it is sent as a Bearer credential. Use `--config PATH` to
select another YAML file. The client also supports `--help`, `--version`,
`--shell completions [SHELL]`, and `--shell init [SHELL]`. Completions include
the client commands and flags currently implemented.

Client environment settings include `MAIL_SERVER_PRIMARY` and `OFFICIAL_SITE`
for server selection, `MAIL_TOKEN` for authorization, `MAIL_OUTPUT_FORMAT`,
`MAIL_COLOR`, and `MAIL_LANG` for output preferences, plus `MAIL_DEBUG`,
`MAIL_QUIET`, and `MAIL_VERBOSE` for diagnostics and output controls. `LANG`,
`NO_COLOR`, and `TERM` provide locale and terminal fallbacks. Explicit command
flags take precedence over these environment settings; command-line tokens
should be avoided when process listings are exposed.

With no command in an interactive terminal, the Bubble Tea interface opens.
When no server is configured, it prompts for a URL and optional token, checks
the server and token, and saves the profile with owner-only permissions. The
menu exposes the API command tree and a Preferences screen. Preference values
are persisted in `cli.yml`; mouse, theme, cache, log, pager, and several
display settings are not all active in the request/UI behavior yet. GUI mode
is unavailable in the static build.

Output supports table, JSON, YAML, plain text, and CSV formats. Destructive
commands require `--force` in non-interactive runs or a confirmation at the
terminal. Config writes are atomic; server and token command-line values are
saved only when their configured values are empty or invalid. Retry execution,
client automatic update checking/downloading/checksum verification/re-exec,
complete TUI preference behavior, and GUI remain incomplete and are tracked in
[TODO.AI.md](https://github.com/apimgr/mail/blob/main/TODO.AI.md).

Use `make dev` to build the server and client binaries in a project-scoped
temporary directory. The Makefile performs the build inside Docker.
