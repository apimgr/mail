# Installation

Mail does not yet have a supported release or published deployment package.
The repository includes Docker and Compose build definitions, but the
application remains a development scaffold. Release binaries and service
installation are tracked in [TODO.AI.md](https://github.com/apimgr/mail/blob/main/TODO.AI.md).

## Build prerequisites

Use Docker and Make. The repository Makefile runs Go build and test commands
inside `casjaysdev/go:latest`; do not run Go tooling directly on the host.

## Current runtime

The server initializes its configuration and database packages before it
starts listening. Do not expose an instance to an untrusted network until the
authentication, API, and deployment requirements in AI.md are implemented.
