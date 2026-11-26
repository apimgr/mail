# Project Audit

Started: 2026-11-26

These are confirmed open implementation gaps against read-only `AI.md` and
`IDEA.md`. Passing tests does not close these items; they remain open until
implemented, verified, and committed.

## Pass 1: Security
- [ ] API authorization and ownership are incomplete across the IDEA mail API. The registry now includes operator-protected domain CRUD, alias list/create/delete, and service routes, but most user, forwarding, queue, config, statistics, log routes and the remaining ownership checks do not exist (`src/server/api_routes.go`; IDEA.md API routes). Mail infrastructure resources are declared system-owned in IDEA.md per AI.md PART 11.
- [ ] Update installation is not service-account-safe. The direct updater replaces the executable in-process, while AI.md PART 22 requires a service-user staging phase and root re-verification before replacement (`src/update/update.go:168-192`; AI.md PART 22, “Update Privileges”).

## Pass 2: Code Quality
- [ ] Production CLI still has explicit service-management, maintenance, and Tor TODOs (`src/main.go:434-435,469-470,741-742`; AI.md PARTS 8, 21, 23, 24, 31).
- [ ] Several scheduler handlers intentionally return “unavailable” errors rather than implementing required providers (`src/scheduler/scheduler.go:312-321,753-766`; AI.md PARTS 18, 19, 21).

## Pass 3: Logic
- [ ] Startup still lacks the required privilege drop. Backup validation is now warning-only instead of aborting directory setup, but it runs under the current effective identity; AI.md PART 8 requires verifying `{backup_dir}` as the dropped service user (`src/main.go`, `src/paths/paths.go`; AI.md PART 8 steps 8h–8j).
- [ ] Update flow lacks scheduled staging, root-start detection, privileged digest re-fetch, exit-75 restart handoff, failure cleanup, notifications, defer filtering, and service-manager restart behavior (`src/update/update.go`; AI.md PART 22).
- [ ] Scheduler registration includes 14 tasks but several handlers have no provider; Tor/I2P tasks and owner idle purge remain absent (`src/scheduler/scheduler.go:308-329,753-766`; AI.md PARTS 18, 19, 21, 31).
- [ ] Mail-management routes and persistence remain absent for users, forwards, transports, queue operations, configuration, statistics, and logs (`src/server/api_routes.go`; IDEA.md API routes). Operator-only domain CRUD, paginated alias list/create/delete, stored DNS guidance, and public DKIM read/key generation now have persistence, opaque UUIDv7 identifiers, input validation, and audit coverage; live MTA synchronization and user-facing workflows remain open.
- [ ] TLS/certificate lifecycle and discovery are not implemented (`src/server/server.go`, `src/mailservice`; AI.md PART 15 and IDEA.md).
- [ ] API projects still lack the required MCP transport/tool registry and a working Swagger UI and GraphQL/GraphiQL endpoint; the current pages report unavailable status (`src/server/openapi.go`, `src/server/api_routes.go`, locale status strings; AI.md PART 14).

## Pass 4: Documentation
- [ ] ReadTheDocs documentation does not yet cover all shipped CLI, browser, API, config, service, and integration behavior (`docs/`; AI.md PART 29; TODO.AI.md).
- [ ] README and docs need a final sync after implementation stabilizes (`README.md`, `docs/`; AI.md PARTS 8, 25–30).

## Pass 5: Spec Compliance
- [ ] AI.md PARTS 8, 21–24 require complete service, maintenance, backup/restore, update, privilege-drop, and service-supervision behavior. The CLI handlers and scheduled providers remain partial (`src/main.go`, `src/update`, `src/scheduler`).
- [ ] AI.md PARTS 14–17 and IDEA.md require complete mail/API behavior, TLS, mail integrations, notifications, and page flows; only portions of service, token, domain, and public-page behavior exist (`src/server`, `src/mailservice`, `src/email`).
- [ ] AI.md PARTS 25–32 still require final build/release, container, CI provider, test/browser coverage, i18n/accessibility, overlay network, and client alignment. The latest full test gate passed at 61.3% statement coverage after the path/PID fixes; container smoke passed in that run, but full build/release and CI verification remain pending (`Makefile`, `.github/`, `docker/`, `src/client`, `src/common/i18n`; TODO.AI.md).

## Pass 6: Code Flow Trace
- [ ] Client commands still describe server operations whose corresponding API handlers are absent, so user, forward, queue, config, statistics, and log flows cannot complete (`src/client/cobra_cli.go`, `src/server/api_routes.go`). Alias list/create/delete CLI commands now match the implemented operator-only API endpoints; live MTA synchronization is still absent.
- [ ] `update.Check` now follows same-origin GitHub release pagination until it finds the running version, but scheduled defer selection, privileged staging/re-verification, and completion behavior remain absent (`src/update/update.go`; AI.md PART 22, “Channel Semantics”). Pagination changes have focused tests but still need the full final gate.

## Completed
- Existing findings about domain reads, shell integration, current public SSR pages, scheduler state persistence, locale parity, and partial service control were rechecked against the current worktree and folded into the open findings above where gaps remain.
