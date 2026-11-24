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

Mail does not yet have a production-ready release, published container image,
or service installation package. Docker build and Compose definitions are
provided, but the application is a development scaffold and should not be
exposed to an untrusted network.

## Configuration

The current configuration package reads YAML and applies defaults for server,
database, SSL, and outgoing email settings. The supported configuration
contract is incomplete and is tracked in [TODO.AI.md](TODO.AI.md).

## API

The scaffold includes server-rendered home, help, legal, security, and
preferences pages. The preferences page supports consent-gated theme and
language cookies, cookie-consent and CCPA controls, reset, and a stateless
cross-device sync page. A server-rendered consent banner and CSRF protection
work without JavaScript. The public shell includes a PWA manifest, service
worker, and offline fallback. `/server/healthz` negotiates HTML, JSON, and
plain text. The health payload includes project/build/runtime information,
feature states, coarse component checks, and request-counter fields;
runtime providers and request tracking remain incomplete, so it is not a
production readiness signal. API text aliases are `/api/healthz.txt` and
`/api/v1/server/healthz.txt`; JSON health routes are `/api/healthz` and
`/api/v1/server/healthz`; `/healthz` is an optional alias. Public `robots.txt`,
sitemap, feed, and AI/security discovery resources are also served. See the [API
documentation](docs/api.md) and [documentation index](docs/index.md). Mail
management API routes are planned but not implemented. A companion `mail-cli`
currently supports the public `status` and `about` API operations; the complete
command set is tracked in [TODO.AI.md](TODO.AI.md).

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
