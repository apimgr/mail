# Project Audit

Started: 2026-11-26

This audit records confirmed implementation gaps against the read-only AI.md and
IDEA.md. The implementation remains incomplete; passing tests do not close these
items. Findings remain open until implemented, verified, and committed.

## Pass 1: Security
- [ ] P1 API authorization is not yet integrated across the complete project API. The router protects service routes with the operator token; public domain list/get handlers exist and omit owner IDs and private DKIM material, but resource writes and the remaining IDEA-defined routes and ownership checks are absent (IDEA.md lines 130–178; `src/server/server.go:320-323`).

## Pass 2: Code Quality
- [ ] P1 Production CLI still contains unimplemented handlers for service management, maintenance, update, and Tor startup (`src/main.go:384-465,650`). The server primary flag parser, Unix manual daemon re-execution, configured log-level selection, and shell init/completion generation exist; config-driven daemonization and the remaining behaviors are incomplete. AI.md assigns those behaviors to PARTS 8, 20–22, and 31.

## Pass 3: Logic
- [ ] P1 The server exposes service discovery/control and an operator-only component health summary. Public domain list/get handlers are registered, but user management, domain writes, alias, forward, queue, component-configuration, statistics, and logs handlers remain absent from `setupRoutes` (`src/server/server.go:320-327`; IDEA.md lines 130–178).
- [ ] P1 The required libSQL driver is registered and remote URL validation is implemented, and startup now creates the declared mail tables idempotently. The required `srv_` table/index mapping is absent, and operation against a live libSQL endpoint is unverified (`src/database/database.go`; AI.md PART 10).

## Pass 4: Documentation
- [x] P2 Updated `docs/api.md` and `docs/integrations.md` to document the current allow-listed service discovery/control API and distinguish it from the unimplemented mail-management routes (`src/server/server.go`, `src/mailservice/control.go`).

## Pass 5: Spec Compliance
- [ ] P0 IDEA.md's required user/domain/alias/forward/queue/configuration/statistics/log API inventory is incomplete; only public domain reads, service management, and the component health endpoint are present (`src/server/server.go:320-327`; IDEA.md lines 130–178).
- [ ] P1 AI.md PARTS 8, 20–22, and 31 still have production placeholders for service/maintenance/update and Tor (`src/main.go:384-465,650`). Built-in shell integration, server flag parsing, Unix manual daemon re-execution, and configured log-level selection now exist; the remaining operations are incomplete.
- [ ] P1 AI.md PART 10's configured libSQL driver is registered and mail schema setup now runs on startup, but `srv_` name mapping, live remote connection, and read/write behavior need implementation and integration verification (`src/database/database.go`; AI.md PART 10).
- [ ] P1 AI.md PART 15 TLS/certificate management is not represented in the current route tree or service implementation (`src/server/server.go:295-325`; AI.md PART 15 begins at line 21150).

## Pass 6: Code Flow Trace
- [ ] P1 Server CLI service, maintenance, and update command flags still terminate with “not yet implemented” errors instead of reaching an operational backend (`src/main.go:384-465`). The Cobra client exposes corresponding mail API command families, but most server routes are not yet registered.

## Completed
- Audit evidence was gathered from the actual route registration, CLI handlers, database driver registration, IDEA.md route inventory, and relevant AI.md parts. One documentation finding is resolved; implementation findings remain open.
