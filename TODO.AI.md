# TODO

## CI/CD follow-ups (found while adding .github/workflows/)

- [ ] Add test coverage. No `_test.go` files exist anywhere in the repo.
      `ci.yml`'s coverage threshold is temporarily set to 0 — raise it to
      the fleet standard of 60 once tests exist.
- [ ] Resolve the source-layout mismatch with AI.md: AI.md's Directory
      Structure and Build & Binary Rules sections require `src/main.go`,
      `src/config/`, `src/server/`, and a required `src/client/` CLI, plus
      a `docker/Dockerfile`. The actual repo has `main.go`, `config/`,
      `server/`, etc. at the repo root, no `src/` tree, no CLI, and no
      Dockerfile. The Makefile itself also still references `./src` and
      `./src/client`, which do not exist. CI workflows were written
      against the actual root-level layout (verified to build), not the
      AI.md-specified `./src` layout — either the code should be moved
      under `src/` (and a CLI + Dockerfile added) to match AI.md, or
      AI.md should be updated to describe this repo's actual layout.
- [ ] Add `renovate.json` at repo root for automated dependency updates
      (required by cicd_conventions.md for public repos). Note: sibling
      repos `api`, `pastebin`, `ipgaze` are also missing it — this is a
      fleet-wide gap, not specific to `mail`.
- [x] `go.sum` was missing entirely and was gitignored (`.gitignore` line
      `go.sum`), even though AI.md's Allowed Root Files table marks
      `go.sum` required and not gitignored. Without it every CI job
      (lint/test/build/vuln-scan) fails on a fresh checkout with
      "missing go.sum entry" — regenerated it with `go mod tidy` in the
      Docker Go toolchain and removed the `go.sum` line from `.gitignore`
      so it's tracked, since CI cannot build without it.
- [x] Bumping `go-chi/chi/v5` to v5.3.0 (CVE fix, see above) deprecated
      `middleware.RealIP` itself (staticcheck SA1019) — it was the exact
      IP-spoofing source the CVEs were about. Swapped to
      `middleware.ClientIPFromRemoteAddr` in `server/server.go`
      (`setupMiddleware`), which never trusts client-supplied headers.
- [ ] AI.md PART 12 specifies a full `trusted_proxies`-aware client-IP
      resolution system (multi-header priority: X-Real-IP,
      X-Forwarded-For, CF-Connecting-IP, True-Client-IP, X-Client-IP;
      gated by whether the immediate TCP peer is in `trusted_proxies`
      config; original TCP peer must be preserved in context before any
      rewrite of `r.RemoteAddr`, so `isTrustedPeer()` and other gates
      always evaluate the real peer, not a spoofable rewritten value).
      None of that is implemented — the `ClientIPFromRemoteAddr` swap
      above is the minimal safe stopgap (ignores all forwarding headers,
      so it can't be spoofed, but it also means the app will log/see the
      proxy's IP instead of the real client IP when run behind a
      reverse proxy). Implementing the full `trusted_proxies` system is
      a real feature, out of scope for this CI/CD task.
- [x] AI.md's canonical CI build-info design (PART 13 / build rules, and the
      release/daily/beta workflow templates) embeds `main.BuildEpoch` via
      `-ldflags` and derives `main.BuildDate` from it at process start via
      an `init()` func. `main.go` previously declared `BuildDate` directly
      with no `BuildEpoch` variable and no derivation `init()`, so the
      `-X 'main.BuildEpoch=...'` in daily.yml/release.yml/beta.yml was a
      silent linker no-op. Added `BuildEpoch`, `buildEpoch()`, and the
      derivation `init()` to `main.go` to match AI.md's canonical pattern.
- [ ] AI.md requires a `Jenkinsfile` at repo root (per cicd_conventions.md)
      and forbids a `config/` directory at root (config should be
      embedded/runtime-generated in OS dirs), but this repo has neither
      a Jenkinsfile nor complies with the no-root-config rule — same
      spec-vs-code mismatch as the `src/` layout item above, not fixed
      here (out of scope for a GitHub Actions-only task).

## Pre-existing lint findings (found by go-lint while verifying the above, not caused by this CI/CD change)

- [ ] Makefile line 50: `GO_DOCKER_RUN` image is `golang:alpine` — must
      be `casjaysdev/go:latest` per convention.
- [ ] Makefile line 50: `GO_DOCKER_RUN` is missing
      `-e GOFLAGS=-buildvcs=false`.
- [ ] Makefile lines 70, 80, 93, 107, 130: `go build` invocations are
      missing `-buildvcs=false` and `-trimpath`.
- [ ] main.go line 133: `--color` flag is documented in help text but not
      implemented.
- [ ] main.go: `NO_COLOR` environment variable is not checked anywhere.
