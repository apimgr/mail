# Contributing to Mail

## Development setup

Mail is a Go project. Docker is required for the supported build and test
workflow.

```sh
make test
make build
./tests/run_tests.sh
mkdocs build --strict
```

The test target runs through `casjaysdev/go:latest`; CI runs vet, Staticcheck,
and govulncheck in separate jobs. `make build` produces the supported platform
binaries. `tests/run_tests.sh` runs the basic Docker integration suite.
`mkdocs build --strict` checks the documentation.

## Changes

Create a topic branch and open a pull request against `main`; do not push
directly to the default branch. Include tests for behavior changes and update
the README or documentation when user-visible behavior, commands, or
configuration changes.

Before requesting review, run the checks above and describe their results in
the pull request. Do not include credentials, private keys, personal data, or
unredacted logs.

## Security reports

Do not report suspected vulnerabilities in public issues. Use the private
vulnerability reporting flow described in [SECURITY.md](SECURITY.md). If that
flow is unavailable, contact the GitHub origin repository before reporting on
a mirror.
