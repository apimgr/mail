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
`~/.config/apimgr/mail/cli.yml` (`server.primary`). The client reads an optional
`token` from that file or accepts `--token TOKEN`; it sends the token as a
Bearer credential. Use `--config PATH` to select another YAML file. The
`--help` and `--version` flags are also available.

The client currently implements only the public health and about operations.
The complete client command set and interactive TUI remain in
[TODO.AI.md](https://github.com/apimgr/mail/blob/main/TODO.AI.md).

Use `make dev` to build the server and client binaries in a project-scoped
temporary directory. The Makefile performs the build inside Docker.
