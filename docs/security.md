# Security

Startup now generates `server.token` when missing, saves it in `secrets.yml`
with mode `0600`, and prints it once to stdout. Later starts log only a masked
form. When debug mode is enabled, every `/debug/*` diagnostics route requires
that token in an `Authorization: Bearer` header. Initial owner-token issuance
is available at `POST /api/v1/server/preferences/tokens/primary`; a valid
operator or owner Bearer token prevents duplicate issuance, and a strict
per-IP rate limit applies. Primary-token authorization protects token
management and linked-token lifecycle operations. Operator authorization
protects mail domains, virtual mailboxes, aliases, forwards, queue operations,
and service discovery/control. These mail records are global resources owned
by the reserved `system` owner; owner tokens do not grant mail-stack access.

Startup also generates a version 1, base64-encoded 32-byte AES key at
`server.security.encryption_key` when missing and stores it in the same
protected secrets file. Owner-token recovery data, DKIM private keys, and
security-report body and contact data are encrypted at rest with this key.
Automated key rotation and re-encryption of retained data remain incomplete.

Access logs remove query parameters whose names indicate credentials or
one-time tokens (`token`, `code`, `password`, `secret`, `access_token`, and
related aliases). Request handlers still receive the original URL.

The current scaffold normalizes trailing slashes before request-ID and path
security middleware, emits the required baseline HTTP security headers,
rejects path traversal patterns, applies the configured CORS policy, and uses
a stateless
double-submit CSRF token for server-rendered POST forms. Read, write, health,
global-burst, and failed-authentication limits are enforced; valid Bearer
tokens use per-token counters and unauthenticated requests use per-IP
counters. Strict token actions currently use per-IP limits. CORS defaults to `*`
without credentials; credentialed responses require an explicitly allowed
origin. Strict token actions still need per-owner buckets, external cache
fallback is not implemented, full audit coverage and PGP-encrypted reporting
remain incomplete. Future mail routes must retain operator authorization.
The application is not ready for production exposure.

The baseline response headers include `X-Content-Type-Options`,
`X-Frame-Options: SAMEORIGIN`, `X-XSS-Protection`,
`Referrer-Policy: strict-origin-when-cross-origin`,
`X-Permitted-Cross-Domain-Policies`, `Origin-Agent-Cluster`, a restrictive
`Permissions-Policy`, and a same-origin Content Security Policy. Configurable
HSTS, request reporting endpoints, and the complete policy integration from
AI.md remain incomplete.

Health routes return public project/build/runtime metadata, cached coarse
dependency checks, persisted aggregate request counters, and live in-flight
request counts. Database, cache, disk, and scheduler details stay vague;
credentials, internal paths, configuration values, and mail content must
never appear in health responses or logs. Mail-service status is available
through the operator-only service API, but health does not yet probe those
services. Tor/I2P/GeoIP providers are incomplete, so this health response is
not a production readiness signal.

For vulnerability reports, use GitHub's private vulnerability reporting for
the [apimgr/mail repository](https://github.com/apimgr/mail/security/advisories/new).
This is the primary channel for vulnerabilities in the repository's source
code. For a vulnerability in a deployed instance, use that instance's
`/server/security` page and its security-report link to open the report form at
`/server/contact?security_id=...`. The form validates the submission, stores
the report with encrypted body and contact fields, and returns a tracking ID.
When no researcher PGP key is supplied, the receipt contains only that ID and
a one-shot status link. When a key is supplied, receipt delivery is skipped
until PGP encryption is implemented. The status page shows triage state and
does not disclose report content. Closed reports are removed after the
configured retention period by the daily security cleanup task; open reports
are retained. PGP delivery to the configured security contact, maintenance
update/export, and retired-key cleanup are not implemented yet. The operator-only `--maintenance security list [--status
STATUS]` command shows metadata without report content; show, update, and
encrypted export remain incomplete. Maintainers therefore do not receive the
report through an automated secure delivery path. The
`/server/security/thanks` page is informational only.

## Public security resources

| Resource | Current status |
|---|---|
| `/server/security` and `/server/security/policy` | Available as server-rendered pages |
| `/.well-known/security.txt` | Served with GitHub private vulnerability reporting and the disclosure policy |
| `/.well-known/llms.txt` and `/llms.txt` | Served with the currently registered public API routes |
| Unknown `/.well-known/*` paths | Return the normal 404 page |

The public web manifest at `/manifest.json` describes the PWA and is not a
security disclosure channel. These discovery files are generated by the
server; they do not imply that the unfinished mail-management API is
production-ready. Remaining security work is tracked in the
[project backlog](https://github.com/apimgr/mail/blob/main/TODO.AI.md).
