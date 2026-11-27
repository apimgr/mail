# Development

## Repository

Mail is a Go project. The current code is a scaffold and its supported API is
limited to the routes in [API documentation](api.md). Product requirements
are maintained in [IDEA.md](https://github.com/apimgr/mail/blob/main/IDEA.md),
implementation requirements in
[AI.md](https://github.com/apimgr/mail/blob/main/AI.md), and verified remaining
work in [TODO.AI.md](https://github.com/apimgr/mail/blob/main/TODO.AI.md).

## Build and checks

The Makefile provides `make dev`, `make local`, `make build`, `make test`,
`make test-smoke`, `make release`, and `make docker`; Go commands run inside the
`casjaysdev/go:latest` Docker image. Go commands must not run directly on the
host. `make test` verifies dependency licenses, runs vet and govulncheck, runs
the unit tests, and enforces the 60% coverage gate. `make test-smoke` runs the
containerized server, client, API, and rendered-page integration checks.
Both `make build` and `make test` run the GraphQL schema-first validator before
building or testing, so SDL/resolver drift fails the normal Docker gates.
`make test SMOKE=1` runs the integration suite with unit tests. The smoke
target creates and removes its own project-scoped temporary build directory.
`make local`
builds local server and client binaries, and `make build` builds both for all
eight release platforms. CI runs vet, Staticcheck, and govulncheck in its
dedicated lint and vulnerability jobs. `make release` creates a
stable GitHub release, and `make docker` publishes
multi-platform images to `ghcr.io/apimgr/mail` by default; set `REGISTRY` to
publish to a different registry.

GitHub Actions has a Docker workflow that builds multi-architecture images on
pushes and publishes the `devel` image on its daily schedule and default-branch
updates. Tagged image publication includes version, `latest`, year-month, and
commit tags. Registry publication is performed by CI and requires the
repository's normal GitHub Packages permissions.

## Contributions

Keep implementation and documentation aligned with AI.md and IDEA.md. Do not
add runtime data, build outputs, credentials, or generated files to the source
tree.
