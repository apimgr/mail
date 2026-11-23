# Mail

[![CI](https://github.com/apimgr/mail/actions/workflows/ci.yml/badge.svg)](https://github.com/apimgr/mail/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/apimgr/mail)](https://github.com/apimgr/mail/releases)
[![License](https://img.shields.io/github/license/apimgr/mail)](LICENSE.md)

## About

Mail is a self-hosted service for managing Postfix, Dovecot, and related mail
infrastructure. Product scope is documented in [IDEA.md](IDEA.md); [AI.md](AI.md)
is the read-only implementation specification.

## Features

The planned product covers mail service status and control, system and virtual
mailboxes, domains, aliases and forwarding, queue operations, spam and virus
filtering, TLS and DKIM, DNS guidance, logs, and delivery statistics. Most of
these features are not implemented yet; see [TODO.AI.md](TODO.AI.md).

## Production

Mail does not yet have a production-ready release, container image, or service
installation package. The current code is a development scaffold and should
not be exposed to an untrusted network.

## Configuration

The current configuration package reads YAML and applies defaults for server,
database, SSL, and outgoing email settings. The supported configuration
contract is incomplete and is tracked in [TODO.AI.md](TODO.AI.md).

## API

The scaffold includes server-rendered home, help, legal, security, preferences,
and API reference status pages. `/server/healthz` negotiates HTML, JSON, and
plain text. JSON health routes are `/api/healthz` and
`/api/v1/server/healthz`; `/healthz` is an optional alias. See the [API
documentation](docs/api.md) and [documentation index](docs/index.md). Mail
management API routes are planned but not implemented.

## Other

The repository's [MkDocs documentation](docs/index.md) is configured for
ReadTheDocs using its default project URL. The hosted documentation project is
not confirmed as deployed, so there is no documentation badge yet.

## Development

The Makefile provides Docker-backed `make dev`, `make build`, and `make test`
targets using `casjaysdev/go:latest`. Go build and test commands must not run
directly on the host. See [development documentation](docs/development.md).

## Disclaimer

The current scaffold is incomplete and is not suitable for production mail
administration. Verify behavior and service integration before relying on it.

## License

MIT. See [LICENSE.md](LICENSE.md).
