# Installation

Mail does not yet have a supported release or published deployment package.
The repository includes Dockerfiles and Compose definitions for development,
testing, and deployment, but there is no published image or release binary to
install. Service installation and lifecycle commands are not implemented.
Do not use this development build to administer a production mail server.

## Build prerequisites

Use Docker and Make. The repository Makefile runs Go build and test commands
inside `casjaysdev/go:latest`; do not run Go tooling directly on the host.

## Build from source

From the repository root, run `make build` to produce server and client
binaries for the supported release targets under `binaries/`. For a
host-platform development build, `make dev` writes binaries to a project-scoped
temporary directory and prints its location. The Makefile targets invoke Go
inside Docker. Use `make test` and `bash tests/run_tests.sh` before evaluating a
change; see the [development guide](development.md).

## Current runtime

The server creates its configuration and database, generates an operator
token on first start, and listens on the selected port. Compose files describe
the expected deployment and development images, but those images are not
published and the compose files do not build them from source. The service and
systemd installer commands are still pending. Do not expose an instance to an
untrusted network until the remaining authentication, MTA synchronization,
TLS, and deployment requirements are implemented.
