# Implementation backlog

This list records verified implementation gaps against AI.md and IDEA.md. It
is not a claim that these features are implemented. Do not remove an item until
its implementation is complete and committed.

## Product foundation

- [ ] Replace the current root-level prototype layout with the required `src/`
  packages and complete the mandatory project structure from AI.md PART 3.
- [ ] Complete the runtime initialization, configuration, CLI flags, modes,
  path handling, and platform behavior in AI.md PARTS 4–8.
- [ ] Finish service management, maintenance, update, and daemon CLI behavior;
  remove placeholder TODOs from production code when completed. `--status` now
  queries the local JSON health endpoint with a bounded timeout.

## Data, security, and API

- [ ] Reconcile database drivers, schemas, migrations, timeouts, and models
  with AI.md PART 10 and mail's data requirements in IDEA.md.
- [ ] Implement the API token model, authorization, audit/security logging,
  rate limits, and middleware required by AI.md PART 11.
- [ ] Implement the complete health, version, API, and content-negotiation
  behavior required by AI.md PARTS 13–14.
- [ ] Implement every mail route listed in IDEA.md, including validation,
  database persistence, system integration, safe config updates, and error
  responses.
- [ ] Implement TLS/certificate management and required discovery behavior
  from AI.md PART 15.

## Browser and mail features

- [ ] Complete AI.md PART 16's server-rendered page behavior. The base shell and
  initial home, about, privacy, contact, help, legal, security, preferences,
  health, and API reference status pages now exist, but they remain static
  scaffold content; the contact workflows, configurable page content, MCP help
  guidance, health details, token/preferences subpages, and full route-to-page
  parity are still missing.
- [ ] Complete the shared shell: final navigation and route parity, complete
  theme persistence/contrast review, mobile and RTL review, SEO edge cases,
  error/maintenance states, and full no-JavaScript behavior from PART 16.
- [ ] Complete embedded locales with exact key parity validation, CLDR plural
  handling, localized formats, and full accessibility coverage from PART 30.
  Current pages load six JSON locales with English fallback and Arabic RTL.
- [ ] Implement component detection and mail service control for supported
  platforms without assuming optional daemons are installed.
- [ ] Implement virtual/system users, domains, aliases, forwards, transports,
  queue operations, configuration validation, and DNS record guidance.
- [ ] Implement spam/virus integrations, DKIM key management, TLS, logs,
  delivery statistics, alerts, and backup/restore according to IDEA.md and
  AI.md PARTS 17–24.
- [ ] Implement scheduler persistence, catch-up behavior, health checks,
  certificate tasks, GeoIP, metrics, and backups required by AI.md PARTS
  18–21.

## Build, packaging, and release

- [ ] Complete the required Makefile targets, safe temporary build output, and
  all-platform static release builds from AI.md PART 25.
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
  required by AI.md PART 28. `make test` now passes the 60% statement-coverage
  gate, and the basic Docker binary smoke suite exists, but Incus and browser
  E2E coverage and tests for the complete product behavior are still missing.

## Documentation and compliance

- [ ] Expand the new ReadTheDocs pages so they document every shipped browser,
  CLI, API, configuration, and integration behavior as implementation lands.
- [ ] Review and expand the `.claude/rules/` summaries so each contains the
  required non-negotiable rules extracted from its assigned AI.md PARTs.
- [ ] Reconcile remaining repository files with AI.md FINAL CHECKPOINT,
  including service files, assets, PWA resources, workflows, and deployment
  artifacts.
- [ ] Keep README.md and docs/ synchronized after every feature change.
