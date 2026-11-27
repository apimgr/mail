# Mail

[![CI](https://github.com/apimgr/mail/actions/workflows/ci.yml/badge.svg)](https://github.com/apimgr/mail/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/apimgr/mail)](https://github.com/apimgr/mail/releases)
[![License](https://img.shields.io/github/license/apimgr/mail)](LICENSE.md)

## About

Mail is a self-hosted service for managing Postfix, Dovecot, and related mail
infrastructure. Product scope is documented in [IDEA.md](IDEA.md); [AI.md](AI.md)
is the read-only implementation specification.

## Features

The product covers mail service status and control, system and virtual
mailboxes, domains, aliases and forwarding, queue operations, spam and virus
filtering, TLS and DKIM, DNS guidance, logs, and delivery statistics. Current
implementation includes operator-protected service discovery/control,
virtual-mailbox CRUD/password/CSV import, domain CRUD and DNS/DKIM operations,
alias and forwarding CRUD, and Postfix queue operations through the API and
CLI. Mail records are global operator-managed resources under the reserved
`system` owner; owner tokens do not authorize mail-stack access. Queue actions
that need Postfix privileges require those permissions for the server process.
Unix mail-account management, live MTA synchronization, transport and filtering
configuration, certificate management, logs, and delivery statistics remain
incomplete; see
[TODO.AI.md](TODO.AI.md) for the verified backlog.

## Production

Mail does not yet have a production-ready release, published container image,
or service installation package. Docker build and Compose definitions are
provided, but the application is a development scaffold and should not be
exposed to an untrusted network.

## Configuration

The current configuration package reads `server.yml`, ordered `yaml.d/`
drop-ins, and `secrets.yml`, applying defaults for implemented settings. It
generates the operator token and version 1, 32-byte AES encryption key into
`secrets.yml` with private file permissions. It protects owner-token recovery,
DKIM private-key, report-contact, and AES-fallback report-body ciphertext;
reports use the active PGP key when one is configured. PGP rotation
re-encrypts retained PGP reports and keeps the old private key for 30 days.
AES encryption-key rotation and the broader configuration contract remain
incomplete and are tracked in
[TODO.AI.md](TODO.AI.md).

The scheduled updater checks the configured stable, beta, or devel channel and
honors `server.update.defer_days`. With `auto_install: true`, only a
server process that started as root stages an eligible release; a later
privileged startup re-verifies and applies it. Non-elevated processes notify
the operator and record that installation was skipped.

## API

The scaffold includes server-rendered home, help, legal, security, and
preferences pages. The preferences page supports consent-gated theme and
language cookies, cookie-consent and CCPA controls, reset, and a stateless
cross-device sync page. A server-rendered consent banner and CSRF protection
work without JavaScript. The public shell includes a PWA manifest, service
worker, and offline fallback. `/server/healthz` negotiates HTML, JSON, and
plain text. The health payload includes project/build/runtime information,
feature states, cached dependency checks, persisted aggregate request counts,
and in-flight request counts; health does not yet probe mail daemons and is not
a production readiness signal. API text aliases are `/api/healthz.txt` and
`/api/v1/server/healthz.txt`; JSON health routes are `/api/healthz` and
`/api/v1/server/healthz`; `/healthz` is an optional alias. Public `robots.txt`,
sitemap, feed, and AI/security discovery resources are also served. See the [API
documentation](docs/api.md) and [documentation index](docs/index.md).
Mail APIs are operator-protected and cover domains, DNS/DKIM, virtual
mailboxes, aliases, forwarding, Postfix queue operations, and service
management. The contact form accepts security reports and stores encrypted
report data. Operators can list, inspect, update triage, and export stored
ciphertext; PGP delivery to the security contact and researcher status-change
email remain incomplete. Live MTA synchronization and Unix mail-account management
remain incomplete. Initial owner-token issuance,
token reveal/rotation/revocation, linked-token management, and the
server-rendered token page are available. A companion `mail-cli` provides the
Cobra command tree and an interactive Bubble Tea setup/menu. It supports
server/token profiles and output preferences; automatic updates and complete
TUI preference behavior remain tracked in [TODO.AI.md](TODO.AI.md). The
`status`, `about`, token, service, mailbox, forward, alias, domain, DNS, and DKIM
CLI operations have matching server routes. The OpenAPI document and machine-only token check are
generated from the registered API routes.
The operator-authenticated GraphQL endpoint is query-only and currently covers
mailbox, domain/DNS/DKIM, alias/forward, queue, service, and mail-health reads.
Its themed GraphiQL explorer is available at `/server/docs/graphql`, uses
locally vendored browser assets, and does not persist authorization headers.
The GraphQL schema still lacks public server-read parity and translated schema
descriptions; see [the API reference](docs/api.md) and
[GraphiQL asset notes](docs/graphql-assets.md).

## Other

The repository's [MkDocs documentation](docs/index.md) is configured for
ReadTheDocs using its default project URL. The hosted documentation project is
not confirmed as deployed, so there is no documentation badge yet. Locale
catalogs currently contain six languages: Arabic, English, French, German,
Italian, and Spanish; localization coverage is still incomplete. See the
[translation guide](docs/translating.md).

## Development

The Makefile provides Docker-backed `make dev`, `make build`, and `make test`
targets using `casjaysdev/go:latest`. Go build and test
commands must not run directly on the host. See
[development documentation](docs/development.md).
`make test` verifies dependency licenses, runs vet and govulncheck, runs unit
tests, and enforces the 60% coverage floor. Run `bash tests/run_tests.sh` for
the Docker-based server, client, API, and rendered-page smoke suite.

## Disclaimer

The current scaffold is incomplete and is not suitable for production mail
administration. Verify behavior and service integration before relying on it.

## License

MIT. See [LICENSE.md](LICENSE.md).
