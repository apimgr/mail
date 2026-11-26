# Project Audit

Started: 2026-11-26

This audit records confirmed implementation gaps against the read-only AI.md and
IDEA.md. The implementation remains incomplete; passing tests do not close these
items. Findings remain open until implemented, verified, and committed.

## Pass 1: Security
- [ ] P1 API authorization is not yet integrated across the complete project API. The router protects the service routes with the operator token, but the IDEA-defined mail resource routes are absent, so their required read/operator authorization has not been implemented (IDEA.md lines 130–178; `src/server/server.go:295-325`).

## Pass 2: Code Quality
- [ ] P1 Production CLI still contains explicit unimplemented TODO handlers for daemon mode, service management, maintenance, update, and Tor startup (`src/main.go:103-106,274-276,309-311,341-343,470-471`). Configured log-level selection and shell init/completion generation are now implemented. AI.md assigns the remaining behaviors to PARTS 8, 20–22, and 31.

## Pass 3: Logic
- [ ] P1 The server exposes service discovery/control and an operator-only component health summary; user, domain, alias, forward, queue, component-configuration, statistics, and logs handlers remain absent from `setupRoutes` (`src/server/server.go:295-325`; IDEA.md lines 130–178).
- [ ] P1 Database driver selection is not complete: `openDB` forwards the configured driver to `sql.Open`, while the repository registers only the SQLite driver; non-SQLite/libSQL operation has no registered implementation (`src/database/database.go:15,84-115`). Reconcile with the configured driver requirements in AI.md PART 10 before claiming alternate database support.

## Pass 4: Documentation
- [x] P2 Updated `docs/api.md` and `docs/integrations.md` to document the current allow-listed service discovery/control API and distinguish it from the unimplemented mail-management routes (`src/server/server.go`, `src/mailservice/control.go`).

## Pass 5: Spec Compliance
- [ ] P0 IDEA.md's required user/domain/alias/forward/queue/configuration/statistics/log API inventory is not implemented; only service management and the component health endpoint are present (`src/server/server.go:295-325`; IDEA.md lines 130–178).
- [ ] P1 AI.md PARTS 8, 20–22, and 31 still have explicit production placeholders for daemon/service/maintenance/update/Tor (`src/main.go:103-106,274-276,309-311,341-343,470-471`). Built-in shell integration and configured log-level selection now exist; the remaining operations are incomplete.
- [ ] P1 AI.md PART 10's configured non-SQLite/libSQL driver behavior is not delivered; the project imports SQLite only and has no other SQL driver registration (`src/database/database.go:15,84-115`).
- [ ] P1 AI.md PART 15 TLS/certificate management is not represented in the current route tree or service implementation (`src/server/server.go:295-325`; AI.md PART 15 begins at line 21150).

## Pass 6: Code Flow Trace
- [ ] P1 CLI's advertised service, maintenance, and update commands terminate with “not yet implemented” errors instead of reaching an operational backend (`src/main.go:265-276,297-311,329-343`).

## Completed
- Audit evidence was gathered from the actual route registration, CLI handlers, database driver registration, IDEA.md route inventory, and relevant AI.md parts. One documentation finding is resolved; implementation findings remain open.
