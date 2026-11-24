# Command line tools

## Server binary

The server binary supports `--help`, `--version`, and `--status`. The status
flag checks the local JSON health endpoint with a bounded timeout. `--shell`,
`--service`, `--maintenance`, `--update`, and `--daemon` remain placeholders.

## Client binary

`mail-cli` supports `status` and `about` against the server's public API:

```sh
mail-cli --server https://mail.example status
mail-cli about --server https://mail.example
```

The server URL can also come from `MAIL_SERVER_PRIMARY` or
`~/.config/apimgr/mail/cli.yml` (`server.primary`). A token can come from
`MAIL_TOKEN`, `auth.token`, `auth.token_file`, `--token TOKEN`, or
`--token-file PATH`; it is sent as a Bearer credential. Use `--config PATH` to
select another YAML file. The client also supports `--help`, `--version`, and
`--shell init [SHELL]`. Shell completion output is still a placeholder.

The client currently implements only the public health and about operations,
and always prints formatted JSON. Parsed `--debug`, `--color`, `--lang`, and
`--output` options do not yet change behavior. The complete client command
set, output modes, shell completions, auto-update, and interactive TUI/GUI
remain in [TODO.AI.md](https://github.com/apimgr/mail/blob/main/TODO.AI.md).

Use `make dev` to build the server and client binaries in a project-scoped
temporary directory. The Makefile performs the build inside Docker.
