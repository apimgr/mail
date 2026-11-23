# Implementation backlog

This list records verified implementation gaps against AI.md and IDEA.md. It
is not a claim that these features are implemented. Do not remove an item until
its implementation is complete and committed.

## Product foundation

- [ ] Replace the current root-level prototype layout with the required `src/`
  packages and complete the mandatory project structure from AI.md PART 3.
- [ ] Complete the runtime initialization, configuration, CLI flags, modes,
  path handling, and platform behavior in AI.md PARTS 4–8.
- [ ] Finish service management, maintenance, update, daemon, and status CLI
  behavior; remove placeholder TODOs from production code when completed.

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
- [ ] Complete the shared page shell's navigation, language discoverability,
  preference controls, theme contrast, mobile and RTL review, SEO metadata,
  error/maintenance states, and full no-JavaScript behavior from PART 16.
- [ ] Add embedded translations and the required locale coverage, plural
  handling, and accessibility behavior from AI.md PART 30.
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
- [ ] Add the required Dockerfiles, rootfs, Compose files, and runtime
  configuration from AI.md PART 26.
- [ ] Reconcile GitHub workflows, pinned actions, security scans, matrix
  coverage, release artifacts, and post-push CI requirements with AI.md PART
  27 and GitHub conventions.
- [ ] Implement the required unit and integration coverage and coverage gate
  from AI.md PART 28. The current repository has tests, but coverage and route
  coverage have not been established in this task.

## Documentation and compliance

- [ ] Expand the new ReadTheDocs pages so they document every shipped browser,
  CLI, API, configuration, and integration behavior as implementation lands.
- [ ] Review and expand the `.claude/rules/` summaries so each contains the
  required non-negotiable rules extracted from its assigned AI.md PARTs.
- [ ] Reconcile remaining repository files with AI.md FINAL CHECKPOINT,
  including service files, assets, PWA resources, workflows, and deployment
  artifacts.
- [ ] Keep README.md and docs/ synchronized after every feature change.
