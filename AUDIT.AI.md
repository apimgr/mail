# Project Audit

Started: 2026-11-26

These are confirmed open implementation gaps against read-only `AI.md` and
`IDEA.md`. Passing tests does not close these items; they remain open until
implemented, verified, and committed.

## Pass 1: Security
- [ ] API authorization and ownership are incomplete across the IDEA mail API. The registry includes operator-protected virtual-mailbox CRUD/password/CSV import, domain CRUD, alias and forward operations, service operations, and Postfix queue list/detail/actions plus queue counts. Mailbox passwords use Argon2id verifiers, and domains, aliases, virtual mailboxes, and forwards persist the reserved system owner. System-user management, configuration, delivery/spam statistics, log routes, live MTA synchronization, and ownership checks for remaining mail resources remain absent. Postfix hold/release/delete also require suitable permissions for the server process (`src/server/api_routes.go`, `src/database/schema_mail.go`, `src/mailservice/queue.go`; IDEA.md API routes; AI.md PART 11).
- [ ] Update installation is not service-account-safe. The direct updater replaces the executable in-process, while AI.md PART 22 requires a service-user staging phase and root re-verification before replacement (`src/update/update.go:168-192`; AI.md PART 22, “Update Privileges”).

## Pass 2: Code Quality
- [ ] Production CLI still has explicit service-management, maintenance, and Tor TODOs (`src/main.go:434-435,469-470,741-742`; AI.md PARTS 8, 21, 23, 24, 31).
- [ ] Several scheduler handlers intentionally return “unavailable” errors rather than implementing required providers (`src/scheduler/scheduler.go:312-321,753-766`; AI.md PARTS 18, 19, 21).

## Pass 3: Logic
- [ ] Startup still lacks the required privilege drop. Backup validation is now warning-only instead of aborting directory setup, but it runs under the current effective identity; AI.md PART 8 requires verifying `{backup_dir}` as the dropped service user (`src/main.go`, `src/paths/paths.go`; AI.md PART 8 steps 8h–8j).
- [ ] Update flow lacks scheduled staging, root-start detection, privileged digest re-fetch, exit-75 restart handoff, failure cleanup, notifications, defer filtering, and service-manager restart behavior (`src/update/update.go`; AI.md PART 22).
- [ ] Scheduler registration includes 14 tasks but several handlers have no provider; Tor/I2P tasks and owner idle purge remain absent (`src/scheduler/scheduler.go:308-329,753-766`; AI.md PARTS 18, 19, 21, 31).
- [ ] Mail-management routes and persistence remain absent for system users, transports, configuration, delivery/spam statistics, and logs (`src/server/api_routes.go`; IDEA.md API routes). Postfix queue list/detail/actions and queue counts now execute through bounded fixed-argument commands, with operator authorization and audit coverage; hold/release/delete still depend on runtime Postfix permissions. Operator-only virtual-mailbox CRUD/password/CSV import, forwarding CRUD, domain CRUD, paginated alias list/create/delete, stored DNS guidance, and public DKIM read/key generation have persisted behavior, opaque UUIDv7 identifiers where applicable, input validation, and audit coverage; live MTA synchronization and user-facing workflows remain open.
- [ ] TLS/certificate lifecycle and discovery are not implemented (`src/server/server.go`, `src/mailservice`; AI.md PART 15 and IDEA.md).
- [ ] API projects still lack the required MCP transport/tool registry and a working Swagger UI and GraphQL/GraphiQL endpoint; the current pages report unavailable status (`src/server/openapi.go`, `src/server/api_routes.go`, locale status strings; AI.md PART 14).

## Pass 4: Documentation
- [ ] ReadTheDocs documentation does not yet cover all shipped CLI, browser, API, config, service, and integration behavior (`docs/`; AI.md PART 29; TODO.AI.md).
- [ ] README and docs need a final sync after implementation stabilizes (`README.md`, `docs/`; AI.md PARTS 8, 25–30).

## Pass 5: Spec Compliance
- [ ] AI.md PARTS 8, 21–24 require complete service, maintenance, backup/restore, update, privilege-drop, and service-supervision behavior. The CLI handlers and scheduled providers remain partial (`src/main.go`, `src/update`, `src/scheduler`).
- [ ] AI.md PARTS 14–17 and IDEA.md require complete mail/API behavior, TLS, mail integrations, notifications, and page flows; current coverage includes portions of service, token, mailbox, forwarding, domain, alias, and public-page behavior (`src/server`, `src/mailservice`, `src/email`).
- [ ] AI.md PARTS 25–32 still require release builds, CI provider coverage, broader test/browser coverage, i18n/accessibility, overlay networking, and client alignment. The latest full `make test` gate passed at 61.4% coverage; strict documentation build passed after the docs/locales updates. Release matrix and hosted CI verification remain pending (`Makefile`, `.github/`, `docker/`, `src/client`, `src/common/i18n`; TODO.AI.md).

## Pass 6: Code Flow Trace
- [ ] Client commands still describe server operations whose corresponding API handlers are absent, so system-user, config, delivery/spam statistics, and log flows cannot complete (`src/client/cobra_cli.go`, `src/server/api_routes.go`). Queue list/detail/actions and queue statistics now match registered handlers, alongside mailbox CRUD/password/CSV import, forwarding list/create/delete, alias list/create/delete, domain, and service commands; runtime Postfix permissions and live MTA synchronization remain open.
- [ ] `update.Check` now follows same-origin GitHub release pagination until it finds the running version, but scheduled defer selection, privileged staging/re-verification, and completion behavior remain absent (`src/update/update.go`; AI.md PART 22, “Channel Semantics”). Pagination and alias ownership migrations pass the full project test gate; overall feature completion remains open.

## Completed
- Existing findings about domain reads, shell integration, current public SSR pages, scheduler state persistence, locale parity, and partial service control were rechecked against the current worktree and folded into the open findings above where gaps remain.
