# Implementation backlog

This list records verified implementation gaps against AI.md and IDEA.md. It
is not a claim that these features are implemented. Do not remove an item until
its implementation is complete and committed.

## Product foundation

- [ ] Complete the mandatory project structure from AI.md PART 3. Go packages
  and the server entrypoint now live under `src/`; `src/common/i18n` provides
  the shared locale catalog, and `src/client/` provides basic status/about
  commands. The full client/TUI, other shared packages, `src/swagger`,
  `src/graphql`, and remaining required structure are incomplete.
- [ ] Complete the runtime initialization, configuration, CLI flags, modes,
  path handling, and platform behavior in AI.md PARTS 4–8.
- [ ] Finish service management, maintenance, update, and daemon CLI behavior;
  remove placeholder TODOs from production code when completed. `--status` now
  queries the local JSON health endpoint with a bounded timeout. The companion
  client currently supports only status/about; the required client command set,
  config management, update flow, and interactive TUI remain incomplete.

## Data, security, and API

- [ ] Reconcile database drivers, schemas, migrations, timeouts, and models
  with AI.md PART 10 and mail's data requirements in IDEA.md. The SQLite
  connection now applies the required busy timeout, WAL, synchronous and
  foreign-key pragmas, and idempotently adds the previous-window rate-limit
  counter; the read/write store split, full migration strategy, retry/backup
  behavior and mail models remain incomplete.
- [ ] Implement the API token model, authorization, audit/security logging,
  rate limits, and middleware required by AI.md PART 11. This includes the
  owner and linked-token lifecycle, `/server/preferences/tokens` page, API
  mirrors, token restore/reveal/rotate/revoke behavior, and API/MCP bearer
  authorization. Transactional owner/primary-token persistence, active-token
  lookup, initial-token issuance, strict per-IP limiting, auditing, unbiased
  token generation, hashing and AES-GCM helpers now exist. The initial token
  endpoint returns the secret once and persists an encrypted recovery copy.
  The API token-list endpoint returns masked metadata with primary-token
  Bearer authorization; primary reveal decrypts only on request, audits it,
  and sets `no-store`. Rotation, revocation, linked-token lifecycle, and the
  required server-rendered token page remain incomplete.
  Legacy token rows receive UUIDv7 public identifiers without losing their
  prior numeric key. The preserved legacy key still needs final schema
  reconciliation; expiry lifecycle, management, full middleware authorization,
  and general rate-limit classes remain absent. Startup creates the version 1
  32-byte AES key in `secrets.yml`; report encryption and other sensitive-data
  encryption remain unimplemented.
  Startup now generates a base62 `server.token`, writes it atomically to
  `secrets.yml` with mode `0600`, migrates it out of `server.yml` and drop-ins,
  and prints it only on generation. The AES key is also generated there and
  audited by key name only. Secret rotation, operator-token audit coverage and
  the rest of secret-file lifecycle remain incomplete.
  CORS now has wildcard-by-default,
  explicit-origin credentials, `DOMAIN` and trusted-proxy origin resolution,
  preflight handling, and regression tests; it still needs the full global
  middleware and security-policy integration. The MCP transport, protocol
  validation and route-derived tool registry are also absent; the current MCP
  help page only reports that no endpoint exists.
- [ ] Finish debug-mode alignment in AI.md PART 6. `/debug/*` is now absent
  unless enabled and its pprof, expvar, sanitized config, route, cache,
  database, scheduler, memory, goroutine, and themed-error endpoints require
  the operator Bearer token. The CLI flag/environment precedence is covered;
  remaining mode flag parsing, runtime redaction review, and end-to-end
  diagnostics coverage are incomplete.
- [ ] Complete the public guest-token and cookie-consent flows from AI.md
  PARTS 8, 12, and 16. CSRF forms, consent banner/state, CCPA controls,
  consent-gated theme/language writes, reset and initial preference-sync
  routes now exist. Initial owner-token issuance exists, but the token-management page is absent;
  preference sync still needs the full validation/redirect/cookie-attribute
  contract, API parity and complete edge-case coverage.
- [ ] Complete health, version, API, and content negotiation per AI.md PARTS
  13–14. The health payload now has canonical fields and JSON/text/API
  negotiation; the page renders project/build/runtime/features/checks/stats,
  uses human-readable values, and has translated labels. The `.txt` API
  aliases are registered and override `Accept`. Privacy, DPO, and disclosure
  policy JSON mirrors are present; help also has a Markdown mirror and a
  registered-page list. Runtime metadata still lacks
  branding values, Tor/I2P/GeoIP providers, cache-provider and disk-capacity
  probes. Current checks confirm configured directory presence and scheduler
  state only. Aggregate request counters now persist in `server.db` with
  minute-batched writes and shutdown flushing; in-flight HTTP requests are
  tracked at the outer handler. Public-page API
  mirrors beyond privacy/DPO/policy/help and the rest of PART 14 are missing.
- [ ] Implement every mail route listed in IDEA.md, including validation,
  database persistence, system integration, safe config updates, and error
  responses.
- [ ] Implement TLS/certificate management and required discovery behavior
  from AI.md PART 15.

## Browser and mail features

- [ ] Complete AI.md PART 16's server-rendered page behavior. The base shell and
  initial home, about, privacy, contact, help, legal, security, preferences,
  health, and API reference status pages now exist. The DPO page now reflects
  configured DPO/general contact details. The health page shares its
  runtime result across HTML/text/JSON, and the privacy page now renders its
  configured data, cookies, retention, tracking, third parties, rights and
  CCPA controls. Built-in collection, usage, and security text now uses the
  six-locale catalog in both the privacy page and the privacy API; the sold-data
  notice and consent-banner accessible name are localized too. DPO and
  disclosure policy pages now reflect configuration;
  the help page accepts configured Markdown and publishes its registered links.
  Other information pages remain scaffold content. The
  contact workflows, the full configurable page-content integration (about,
  privacy, terms, header and footer support localized safe rendering, but config
  validation, hot reload and warning/default behavior are incomplete), full
  MCP help guidance, token management, security-report flow, complete page
  registry and route parity remain missing. A shared page registry now covers
  the current server-rendered GET routes and drives sub-page routes, hub/footer
  links, the help API list, and sitemap inclusion. State-changing web routes,
  API mirrors, and the larger API route inventory are not yet in that registry.
  Consent controls and the first sync
  page are present but still need the full AI.md behavior. The current token
  page requirements specify owner/linked token workflows, restore, and
  primary-token revocation; these remain absent. A first
  generated sitemap and configurable `web.robots` allow/deny rules now exist;
  route-registry generation, full SEO metadata and the remaining discovery
  contract are still incomplete.
- [ ] Complete the shared shell: final navigation and route parity, complete
  theme persistence/contrast review, mobile and RTL review, SEO edge cases,
  error/maintenance states, and full no-JavaScript behavior from PART 16.
  First middleware now canonicalizes trailing slashes; CORS, robots allow/deny
  and sitemap filtering are implemented with tests. A rendered-page conformance
  test now checks the current page set for the shared shell, controls, CSRF,
  duplicate attributes, scripts, styles, tables, and internal route links. The
  header theme form now appears on Preferences and labels its next mode, but the
  full registry-driven test matrix and complete SEO/middleware integration
  remain.
- [ ] Complete embedded locales with exact key parity validation, CLDR plural
  handling, localized formats, and full accessibility coverage from PART 30.
  The shared registry and embed loader now verify all six locale files and key
  parity, provide English fallback, named interpolation and CLDR plural
  selection, and drive the SSR language selector. Human-readable output across
  the whole server/client/API, broader plural message coverage, other
  localized dates, and complete WCAG review remain outstanding. The shared
  formatter now renders health uptime plurals, locale-aware health counts, and
  localized dates in health/privacy views. The preferences language selector
  now labels Automatic with the negotiated language and marks each native-name
  option with its language tag.
- [ ] Implement component detection and mail service control for supported
  platforms without assuming optional daemons are installed.
- [ ] Implement virtual/system users, domains, aliases, forwards, transports,
  queue operations, configuration validation, and DNS record guidance.
- [ ] Implement spam/virus integrations, DKIM key management, TLS, logs,
  delivery statistics, alerts, and backup/restore according to IDEA.md and
  AI.md PARTS 17–24.
- [ ] Implement scheduler persistence, catch-up behavior, health checks,
  certificate tasks, GeoIP, metrics, and backups required by AI.md PARTS
  18–21. Task state and execution history now persist in `server.db`, stale
  `running` tasks recover as `interrupted`, and the default one-hour catch-up
  window dispatches recent missed runs. Five-field cron and interval parsing
  use embedded timezone data and the America/New_York default. The token
  cleanup task now removes expired CAPTCHA challenges from `server.db` using
  the required indexed table. The obsolete session-cleanup task and new
  `admin_sessions` table creation were removed to match the token-only identity
  model. Per-task and timezone configuration overrides, owner idle purge,
  remaining required tasks, retry behavior, and functional task handlers are
  still incomplete.

## Build, packaging, and release

- [ ] Complete the required Makefile targets, safe temporary build output, and
  all-platform static release builds from AI.md PART 25. `make build` now
  succeeds for the host binaries and all eight OS/architecture targets; target
  safety, release behavior and remaining required targets are still incomplete.
- [ ] Complete container integration from AI.md PART 26: devel/release image
  publishing, privilege setup/drop, service-aware startup, and Valkey
  integration. Dockerfiles, Compose files, OCI annotations, and the basic
  isolated Docker test harness now exist.
- [ ] Finish CI and release alignment from AI.md PART 27: verify the workflows
  on every provider, satisfy CODEOWNERS and branch-protection rules, complete
  the release flow, and resolve post-push CI results. GitHub private
  vulnerability reporting, secret scanning, and push protection are enabled;
  CI/security workflows and public community files now exist.
- [ ] Expand the unit, integration, browser, and cross-platform coverage
  required by AI.md PART 28. The latest `make test` passes with 72.3% statement
  coverage, and the basic Docker binary
  smoke suite exists, but Incus and browser E2E coverage and tests for the
  complete product behavior are still missing.

## Documentation and compliance

- [ ] Expand the new ReadTheDocs pages so they document every shipped browser,
  CLI, API, configuration, and integration behavior as implementation lands.
- [ ] Finish reviewing the `.claude/rules/` summaries against their assigned
  AI.md PARTs. All 13 now include the required structural sections, and stale
  Docker, CI-provider, test-validation, route-exception, and template-issue
  guidance was corrected; each rule still needs a detailed per-PART coverage
  review.
- [ ] Reconcile remaining repository files with AI.md FINAL CHECKPOINT,
  including service files, complete PWA assets/update recovery, workflows, and
  deployment artifacts. The installable manifest, service worker, offline page,
  versioned static assets, build-stamp cache purge, robots/sitemap/feed,
  security.txt and llms.txt now exist, but full icon variants, robust
  update/recovery behavior, metadata, and browser verification remain.
- [ ] Keep README.md and docs/ synchronized after every feature change.
