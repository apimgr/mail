# Command line tools

## Server binary

The server binary uses Go's standard flag parser for `--help`, `--version`,
`--status`, configuration paths, address, port, mode, color, language, and the
shell, service, maintenance, and update command flags. `--status` checks the
local JSON health endpoint with a bounded timeout. `--shell` completion and
init commands work for bash, zsh, fish, POSIX-style shells, and PowerShell.
Daemon re-execution is supported on Unix when `--daemon` is used outside an
application container. `--update check`, `--update yes`, and
`--update branch stable|beta|devel` are implemented, including checksum
verification before replacement. The updater still lacks the scheduled
service-user staging, privileged re-verification, deferred update policy, and
service-manager restart flow required by AI.md PART 22. Service and maintenance
operations remain incomplete.

## Client binary

`mail-cli` uses Cobra and exposes server information, service, domain, user,
alias, forward, queue, config, statistics, logs, and health commands. Only
`status`, `about`, and operator-protected domain list/get API operations currently have
matching server handlers; the remaining mail operations are pending server
implementation.

```sh
mail-cli --server https://mail.example status
mail-cli about --server https://mail.example
```

The server URL can also come from `MAIL_SERVER_PRIMARY`,
`~/.config/apimgr/mail/cli.yml` (`server.primary`), `OFFICIAL_SITE`, or the
embedded official site. The API version comes from `server.api_version` and
defaults to `v1`. A token can come from
`MAIL_TOKEN`, `auth.token`, `auth.token_file`, `--token TOKEN`, or
`--token-file PATH`; it is sent as a Bearer credential. Use `--config PATH` to
select another YAML file. The client also supports `--help`, `--version`,
`--shell completions [SHELL]`, and `--shell init [SHELL]`. Completions include
the client commands and flags currently implemented.

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
