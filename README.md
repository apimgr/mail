# Mail

Mail is intended to be a web-based control panel for managing Postfix,
Dovecot, spam filtering, virtual mailboxes, and related mail infrastructure.
The product requirements are in [IDEA.md](IDEA.md), and [AI.md](AI.md) defines
the implementation contract.

## Current implementation

The current executable initializes configuration, SQLite/PostgreSQL/MySQL
connections, database schemas, a scheduler, and an HTTP server. Its available
HTTP endpoints are:

- `GET /server/healthz` and `GET /healthz` for database-aware health status.
- `GET /api/v1/server/healthz` for versioned health status.
- `GET /api/v1/server/about` and `GET /api/v1/server/version` for application
  metadata.

The setup wizard, authenticated administration, mail service integration,
mailbox/domain APIs, webmail, and container packaging are not implemented yet.
See [TODO.AI.md](TODO.AI.md) for the production implementation backlog.

## Configuration

On first startup the program creates a `server.yml` under the operating
system's application configuration directory and generates a setup token.
The configuration file is written with owner-only permissions. Operating
system-specific config, data, log, and database paths are selected by the
`paths` package.

## Development

The project uses a Go module and a container-based Makefile toolchain. The
GitHub Actions workflow performs static analysis, tests, build, vulnerability
scanning, secret scanning, and workflow-policy checks. No test suite currently
exists; the 0% coverage threshold is temporary and tracked in TODO.AI.md.

## License

MIT. See [LICENSE.md](LICENSE.md).
