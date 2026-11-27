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
protected secrets file. Owner-token recovery data, DKIM private keys, contact
data, and security-report bodies without an active PGP key are encrypted at
rest with this key. When an active PGP key exists, report bodies use OpenPGP
encryption. AES encryption-key rotation and re-encryption of retained data
remain incomplete; PGP key rotation is documented below.

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
fallback is not implemented, and full audit coverage and automated secure
maintainer delivery remain incomplete. Future mail routes must retain operator
authorization.
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
`/server/security` page, which renders the configured channels and the
`security.txt` expiry. When `server.pages.contact.enabled` is true, its
security-report link opens `/server/contact?security_id=...`; the page also
links to the form without a security ID for terms violations, abusive content,
DMCA notices, and privacy requests. Setting the contact form to disabled makes
both its GET and POST routes return 404. The security API exposes the same
public contact/expiry details and validated PGP v4 key metadata, without the
private admin address. The form validates the submission, stores
the report with encrypted body and contact fields, and returns a tracking ID.
When the acknowledgment email is successfully handed to SMTP, the response
also says that a confirmation email is on its way. The email contains only the
tracking ID and a one-shot status link. If SMTP is unavailable, the response
does not promise an email. When a researcher PGP key is supplied, receipt
delivery is skipped until PGP encryption is implemented. The status page shows triage state and
does not disclose report content. Closed reports are removed after the
configured retention period by the daily security cleanup task; open reports
are retained. PGP delivery to the configured security contact and status-change
email are not implemented yet. PGP rotation re-encrypts retained PGP reports in
one database transaction, then installs the staged keypair with startup recovery.
The previous encrypted private key is retained for 30 days and removed by daily
security cleanup. Import accepts
one valid, expiring v4 private key only when no active key exists; an identity
mismatch requires a second typed override confirmation.
Operators can export the decrypted private key only to a new file after an
exact typed confirmation, a reason, token re-entry when not elevated, and a
one-attempt-per-hour limit per OS user. Confirmed key deletion removes active
and retired key files and marks the retained fingerprint revoked; reports
encrypted to that key become unreadable. The operator-only
`--maintenance security list [--status STATUS]` and `show TRACKING_ID`
commands show permitted metadata without decrypting report content. The
`update TRACKING_ID` command supports forward-only triage, researcher comments,
disclosure dates, severity re-triage, and opted-in acknowledgments. Maintainers therefore do not receive the
report through an automated secure delivery path. `export TRACKING_ID` writes
the stored ciphertext only, using mode `0600` and refusing to overwrite an
existing file; `--output -` writes ciphertext to stdout. The
`/server/security/thanks` page lists public credits for disclosed reports only
when researchers opted into a name, handle, or anonymous credit.

## Public security resources

| Resource | Current status |
|---|---|
| `/server/security` and `/server/security/policy` | Server-rendered pages; the overview reflects configured public contacts, expiry, and an available PGP key |
| `GET /api/v1/server/security` | Public JSON mirror of contacts, expiry, PGP key availability/URL/fingerprint, policy, thanks, and the optional contact form URL |
| `/.well-known/security.txt` | Includes the configured GitHub, enabled security form, and public security email channels, plus expiry, policy, and a valid PGP key URL when available |
| `/.well-known/pgp-key.asc` | Serves the active ASCII-armored, valid OpenPGP v4 public key only when its fingerprint matches active database metadata; returns 404 if absent, expired, revoked, or invalid. Operators can create a keypair with `--maintenance pgp generate`, rotate it with `--maintenance pgp rotate`, publish it with `--maintenance pgp publish`, and export its public key with `--maintenance pgp export public [FILE]`. Rotation transactionally re-encrypts retained PGP reports, recovers staged files on startup, and retains the previous encrypted private key for 30 days. Keyserver retries are persisted in `config/security/keyservers.state` and run every 15 minutes. |
| `/.well-known/llms.txt` and `/llms.txt` | Served with the currently registered public API routes |
| Unknown `/.well-known/*` paths | Return the normal 404 page |

The public web manifest at `/manifest.json` describes the PWA and is not a
security disclosure channel. These discovery files are generated by the
server; they do not imply that the unfinished mail-management API is
production-ready. Remaining security work is tracked in the
[project backlog](https://github.com/apimgr/mail/blob/main/TODO.AI.md).
