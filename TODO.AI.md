# mail implementation backlog

`AI.md` is the implementation contract and `IDEA.md` defines the mail
infrastructure management product. This repository is an early backend shell;
the items below are still required before it can be described as production
ready. The existing GitHub Actions workflow targets the current root-level Go
packages.

## Completed in this pass

- [x] CI dependency state: `go.sum` is tracked and `go.mod` includes the
      project dependencies. The CI workflow runs vet, staticcheck, tests,
      build, vulnerability scanning, secret scanning, and workflow policy.
- [x] Replace deprecated chi `middleware.RealIP` usage with
      `ClientIPFromRemoteAddr`, which does not trust unvalidated forwarding
      headers.
- [x] Add request path traversal rejection and baseline HTTP security headers.
- [x] Expose canonical `/server/healthz` and versioned health routes; health
      checks now verify both database connections and return HTTP 503 when
      either is unavailable.
- [x] Report listener failures through the server lifecycle instead of calling
      `log.Fatalf` from a goroutine; format IPv4/IPv6 listen addresses safely.
- [x] Save first-run configuration with owner-only permissions and stop startup
      if the generated setup token cannot be persisted.
- [x] Correct Makefile toolchain image, module/build cache paths, VCS flags, and
      build targets for the current package layout.

## Required product implementation

- [ ] Implement the initial setup wizard and establish the operator token/auth
      flow required by AI.md. Until that exists, do not expose mutating API
      endpoints.
- [ ] Implement authenticated admin UI and API handlers for mail servers,
      domains, mailboxes, aliases, forwards, service configuration, queue,
      statistics, logs, health, and backups described by IDEA.md.
- [ ] Implement real component discovery and service control for Postfix,
      Dovecot, SpamAssassin/Rspamd, ClamAV, OpenDKIM, Fail2ban, and optional
      components. Never report a component status that was not detected.
- [ ] Implement configuration validation, safe backup/restore, DNS helper
      output, DKIM key management, SPF/DMARC records, and integration with
      system mail services. Current database tables alone do not implement
      these behaviors.
- [ ] Implement multi-server agent authentication and communication, then
      validate that cluster operations cannot cross server/domain ownership.
- [ ] Implement TLS/Let's Encrypt lifecycle, webmail, Sieve, calendar/contacts,
      mailing lists, and other IDEA.md features that the README currently
      describes but the code does not provide.
- [ ] Add secure password hashing and session/token lifecycle for any admin
      account flow. Existing admin/session tables are schema only.

## Shared AI.md requirements still missing

- [ ] Move all Go source into `src/`, add required `src/client/`, and update
      module imports, Makefile, and CI commands to match the canonical layout.
- [ ] Add `docker/Dockerfile`, production/development/test compose files,
      rootfs entrypoint and ignore rules, plus architecture-appropriate runtime
      packaging. The current Makefile docker target references nonexistent
      files.
- [ ] Implement the required CLI flags and client commands. `--status`,
      `--color`, `--debug`, daemon mode, shell completions, service,
      maintenance, and update commands are currently incomplete or stubs.
- [ ] Implement the remaining AI.md server requirements: full middleware and
      trusted-proxy handling, authentication, API behavior/content negotiation,
      SSL/TLS, metrics, scheduler jobs, GeoIP, backup/restore, update/service
      management, Tor/I2P options, i18n, and accessible SSR frontend.
- [ ] Add required OpenAPI/GraphQL support where AI.md specifies it and keep
      generated/documented APIs aligned with implementation.
- [ ] Add `Jenkinsfile`, GitHub release workflow, Renovate configuration,
      project security/governance files, docs site and required test scripts.
      Current CI is GitHub CI only; it does not produce releases or exercise
      packaged/container deployments.
- [x] Add baseline Go tests for configuration, paths, PID files, SQLite schema
      idempotency, transaction lifecycle, scheduler behavior, logging, server
      routes, and CLI setup/help output. `make test` now passes with 61.2%
      coverage and includes vet plus the mandatory 60% threshold.
- [ ] Expand integration coverage for authenticated setup, mail component
      discovery/control, admin API/UI behavior, and backup/restore workflows.
- [ ] Reconcile README claims with delivered features as each feature lands;
      current runtime only exposes health/about/version endpoints and does not
      serve the documented setup/admin/webmail interfaces.
