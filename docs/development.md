# Development

## Repository

Mail is a Go project. The current code is a scaffold and its supported API is
limited to the routes in [API documentation](api.md). Product requirements
are maintained in [IDEA.md](https://github.com/apimgr/mail/blob/main/IDEA.md),
implementation requirements in
[AI.md](https://github.com/apimgr/mail/blob/main/AI.md), and verified remaining
work in [TODO.AI.md](https://github.com/apimgr/mail/blob/main/TODO.AI.md).

## Build and checks

The Makefile has six targets: `build`, `test`, `release`, `docker`, `clean`,
and `help`. Go commands run inside the `casjaysdev/go:latest` Docker image and
must not run directly on the host. `make build` creates unstripped debug
binaries for the local platform and all eight supported distribution targets.
`make test` builds the current tree, runs dependency-license verification,
`go vet`, Staticcheck, govulncheck, ShellCheck, shfmt, unit tests with the 60%
coverage floor, and the Docker server/client/API/page smoke suite. `make
release` builds stripped production binaries for all eight platforms and
creates a source archive and checksums in `releases/`; it never tags, uploads,
or publishes. `make docker` builds and loads the host-platform image into the
local Docker image store without pushing it.

CI workflows build and publish multi-architecture images and tagged releases;
local Makefile targets never publish artifacts.

## Contributions

Keep implementation and documentation aligned with AI.md and IDEA.md. Do not
add runtime data, build outputs, credentials, or generated files to the source
tree.
