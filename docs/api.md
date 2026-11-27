# API

This page describes the API handlers currently registered in the server. The
mail-management route inventory is in the
[project definition](https://github.com/apimgr/mail/blob/main/IDEA.md). The
registered routes currently cover public server pages, browser preferences,
owner-token management, operator-protected virtual mailbox CRUD and CSV import,
domain, alias, forwarding, security-report submission and status, Postfix queue,
queue-statistics, and service status and control operations. OpenAPI JSON includes protected routes
and declares their Bearer security; `llms.txt` uses the shared API registry.
The query-only GraphQL API exposes current mail read operations through a
separate operator-authenticated GraphQL response format.

| Method | Path | Current behavior |
|---|---|---|
| `GET` | `/api/v1/server/healthz` | Returns the public health payload; HTTP 503 indicates unhealthy or maintenance state. It includes project/build/runtime metadata, feature states, cached checks, persisted aggregate request counts, and the in-flight request count. `version` matches the server's `--version`; `build.commit` is the short commit ID and `build.date` is the UTC build time. |
| `GET` | `/api/v1/server/healthz.txt` | Returns the same health data as flat text; the `.txt` suffix overrides `Accept`. |
| `GET` | `/api/v1/server/auth/check` | Machine-only token check. Requires one Bearer credential, returns `{ "kind": "operator" }` or `{ "kind": "owner" }`; invalid credentials return 401. Rate limited and never echoes the token. |
| `GET` | `/api/v1/server/swagger` | Generates an OpenAPI JSON document from routes currently registered by the server, including protected routes with their authorization schemes. |
| `POST` | `/api/v1/server/mcp` | Streamable HTTP MCP endpoint. Requires a valid Bearer token and the `MCP-Protocol-Version`, `Mcp-Method`, and, for `tools/call`, `Mcp-Name` headers. Provides registered API operations as tools, filtered to the caller's authorization tier. |
| `GET`, `POST` | `/api/v1/server/graphql` | Executes query-only GraphQL operations; requires the operator Bearer token. GET accepts `query`, `variables`, and `operationName`; POST accepts a JSON GraphQL request. |
| `GET` | `/api/v1/server/about` | Returns localized title, project Markdown/content, and format, using configured About content when present. |
| `GET` | `/api/v1/server/version` | Returns the same version, short commit ID, and UTC build time as the health payload, plus Go version and platform. |
| `GET` | `/api/v1/server/privacy` | Returns the public privacy policy data, configured cookie descriptions, retention details, tracking state, and consent/CCPA state. |
| `GET` | `/api/v1/server/dpo` | Returns the configured DPO name, DPO-or-general contact email, and request-derived absolute privacy URL. |
| `GET` | `/api/v1/server/security` | Returns configured public reporting contacts, time-limited report URL, expiry, policy/thanks URLs, and active validated PGP v4 key availability, URL, and fingerprint. |
| `GET` | `/api/v1/server/security/policy` | Returns the configured disclosure policy Markdown or the built-in coordinated-disclosure policy. |
| `GET` | `/api/v1/server/security/thanks` | Lists disclosed researcher acknowledgments without contact details. |
| `GET` | `/api/v1/server/contact` | Returns the public contact-report form descriptor, supported report types, CAPTCHA details, field constraints, and short-lived form token. |
| `POST` | `/api/v1/server/contact` | Submits a report through the review-and-confirm flow. Security reports require a valid `security_id`, form token, and CAPTCHA. Report body and contact fields are encrypted at rest; success returns a tracking ID. |
| `GET` | `/api/v1/server/help` | Returns the localized help title, Markdown content, format, and registered help links with localized descriptions. |
| `GET` | `/api/v1/server/help/mcp` | Returns the request-resolved MCP endpoint and aliases, Bearer authentication note, live standard-token tool list, and configured Mail-specific guidance. |
| `GET` | `/api/v1/server/terms` | Returns localized terms title, Markdown content, and configured/default format. |
| `GET` | `/api/v1/server/preferences` | Returns the current browser theme, language, and empty app-specific preference map. |
| `GET` | `/api/v1/server/preferences/sync` | Returns current preferences plus a shareable URL and short code. |
| `GET` | `/api/v1/server/preferences/sync/import` | Applies valid theme/language values only when preference-cookie consent is enabled; otherwise reports `consent_required`. Returns `applied` and `ignored` arrays, including when empty. |
| `POST` | `/api/v1/server/preferences` | Applies a partial theme/language update from JSON or form data. It accepts no consent controls or CSRF token; writes still require existing preference-cookie consent. |
| `POST` | `/api/v1/server/preferences/tokens/primary` | Creates the initial owner and primary token and returns the secret once. A valid operator or owner Bearer token returns `409`; per-IP strict limiting returns `429` with `Retry-After`. |
| `GET` | `/api/v1/server/preferences/tokens` | Lists masked owner-token metadata for a primary-token Bearer credential. Missing/invalid credentials return `401`; linked tokens return `403`. |
| `POST` | `/api/v1/server/preferences/tokens/primary/reveal` | Returns the encrypted primary token to its primary-token holder, audits the reveal, and sets `Cache-Control: no-store`; strict per-IP limiting applies. |
| `POST` | `/api/v1/server/preferences/tokens` | Creates a linked token; primary-token authorization is required. The plaintext is returned only at creation. |
| `POST` | `/api/v1/server/preferences/tokens/{id}/reveal` | Reveals a linked token to the primary-token holder with `Cache-Control: no-store`. |
| `POST` | `/api/v1/server/preferences/tokens/{id}/rotate` | Rotates a linked token; primary-token authorization is required. |
| `PATCH` | `/api/v1/server/preferences/tokens/{id}` | Updates a linked token's name or expiry; primary-token authorization is required. |
| `DELETE` | `/api/v1/server/preferences/tokens/{id}` | Revokes a linked token; primary-token authorization is required. |
| `GET` | `/api/v1/users` | Lists virtual mailbox addresses, quotas, enabled state, and timestamps with standard pagination. Password verifiers are never returned. Requires the operator Bearer token. |
| `GET` | `/api/v1/users/{id}` | Returns one virtual mailbox by opaque UUIDv7 ID without its password verifier. Requires the operator Bearer token. |
| `POST` | `/api/v1/users` | Creates a virtual mailbox on an enabled configured domain. Stores the mailbox password as an Argon2id verifier and returns only non-secret fields. Requires the operator Bearer token. |
| `PUT` | `/api/v1/users/{id}` | Updates a virtual mailbox's quota or enabled state. Deletion dependencies are not implicitly removed. Requires the operator Bearer token. |
| `DELETE` | `/api/v1/users/{id}` | Deletes a virtual mailbox only when no dependent forwarding, key, sieve, or archive records remain. Requires the operator Bearer token. |
| `POST` | `/api/v1/users/{id}/password` | Changes a virtual mailbox password using Argon2id; password material is never returned or audited. Requires the operator Bearer token. |
| `POST` | `/api/v1/users/import` | Imports virtual mailboxes from a `text/csv` request up to 1 MiB and 100 data rows, with the header `email,password,quota_bytes,enabled`; returns per-row results without credential material. Requires the operator Bearer token. |
| `GET` | `/api/v1/domains` | Lists configured mail domains with opaque UUIDv7 IDs and standard page/limit pagination. Requires the operator Bearer token; response omits owner identifiers and private DKIM material. |
| `GET` | `/api/v1/domains/{domain}` | Returns one configured mail domain with an opaque UUIDv7 ID. Requires the operator Bearer token; response omits owner identifiers and private DKIM material. |
| `POST` | `/api/v1/domains` | Creates a system-owned virtual domain. Requires the operator Bearer token. |
| `PUT` | `/api/v1/domains/{domain}` | Replaces virtual domain metadata. Requires the operator Bearer token. |
| `DELETE` | `/api/v1/domains/{domain}` | Deletes a domain only when it has no dependent mail resources. Requires the operator Bearer token. |
| `GET` | `/api/v1/domains/{domain}/dns` | Returns configured MX/SPF/DKIM/DMARC records (including MX preference) and the record types still missing. Requires the operator Bearer token. |
| `GET` | `/api/v1/domains/{domain}/dkim` | Returns enabled public DKIM records. Requires the operator Bearer token; private keys are never returned. |
| `POST` | `/api/v1/domains/{domain}/dkim/generate` | Generates a 2048-bit RSA DKIM key and returns its public DNS value. Requires the operator Bearer token; the private key is AES-256-GCM encrypted at rest and never returned. |
| `GET` | `/api/v1/aliases` | Lists mail aliases with opaque UUIDv7 IDs and standard page/limit pagination. Requires the operator Bearer token. |
| `POST` | `/api/v1/aliases` | Creates an alias from a valid source address on a configured domain to a destination address. Requires the operator Bearer token. |
| `DELETE` | `/api/v1/aliases/{id}` | Deletes one alias by opaque UUIDv7 ID. Requires the operator Bearer token. |
| `GET` | `/api/v1/forwards` | Lists virtual-mailbox forwarding rules with standard pagination. Requires the operator Bearer token. |
| `POST` | `/api/v1/forwards` | Creates a forwarding rule for an enabled virtual mailbox with 1–20 destination addresses and a keep-local flag. Requires the operator Bearer token. |
| `DELETE` | `/api/v1/forwards/{id}` | Deletes a forwarding rule by opaque UUIDv7 ID. Requires the operator Bearer token. |
| `GET` | `/api/v1/queue` | Lists Postfix queue metadata with standard pagination. Requires the operator Bearer token. |
| `GET` | `/api/v1/queue/{id}` | Returns queue sender, recipients, size, arrival time, queue state, and delay reason. Requires the operator Bearer token. |
| `POST` | `/api/v1/queue/{id}/flush`, `/hold`, `/release` | Requests one fixed Postfix queue operation. Requires the operator Bearer token; hold/release require suitable Postfix process permissions. |
| `DELETE` | `/api/v1/queue/{id}` | Deletes a queued message using `postsuper`. Requires the operator Bearer token and suitable process permissions. |
| `POST` | `/api/v1/queue/flush-all` | Requests Postfix delivery for the queue. Requires the operator Bearer token. |
| `GET` | `/api/v1/stats/delivery` | Returns delivery-log counts grouped by recorded status. Requires the operator Bearer token. |
| `GET` | `/api/v1/stats/spam` | Returns aggregate spam and virus detections. Requires the operator Bearer token. |
| `GET` | `/api/v1/stats/queue` | Returns current queue count, bytes, and counts by Postfix queue state. Requires the operator Bearer token. |
| `GET` | `/api/v1/services` | Lists supported mail components with detected status; operator Bearer authorization is required. |
| `GET` | `/api/v1/services/{service}` | Returns one supported mail component's detected status; operator Bearer authorization is required. |
| `GET` | `/api/v1/health` | Returns mail component status and installed/running/stopped/unknown counts; operator Bearer authorization is required. |
| `POST` | `/api/v1/services/{service}/start`, `/stop`, `/restart`, `/reload` | Requests an allow-listed service operation; operator Bearer authorization is required. Unsupported service managers and missing services return explicit errors. |
| `GET` | `/api/healthz` | Direct versionless health alias; JSON by explicit `Accept`, plain text for non-interactive clients. |
| `GET` | `/api/healthz.txt` | Direct versionless plain-text health alias; `.txt` overrides `Accept`. |
| `GET` | `/api/swagger` | Direct alias of `/api/v1/server/swagger`. |
| `GET` | `/server/healthz` | Content-negotiated health page: HTML for browsers, JSON or text for clients. |
| `GET` | `/healthz` | Optional alias to the same content-negotiated handler; disabled unless `server.healthz.root.enabled` is true. |

Mail domain and alias list endpoints default to `page=1&limit=250`; `limit`
must be between 1 and 250. Responses include `data` and sibling `pagination`
metadata. Alias creation accepts a source address on a configured mail domain
and one destination address:

Delivery and spam statistics accept `period=24h`, `7d`, `30d`, `90d`, or `365d`;
the default is `30d`. Responses include the selected period and its UTC start
time. Delivery totals are grouped by the status stored in the delivery log.
Spam and virus totals are summed from daily mail statistics.

```json
{
  "source": "info@example.test",
  "destination": "operations@example.test"
}
```

Alias IDs are opaque UUIDv7 values. Create and delete actions are recorded in
the audit log by alias ID; addresses are not copied into audit details.

Public discovery resources include `/robots.txt`, `/sitemap.xml`, `/feed.xml`,
`/rss.xml`, `/.well-known/security.txt`, `/.well-known/llms.txt`, and
`/llms.txt`. They accept `GET` and `HEAD` only. Preference sync responses are
not cacheable and never include consent or credential cookies in an export.

The one-shot researcher status page is `/server/security/report/{tracking_id}`.
Its emailed status token can be used once per day; the page exposes only
triage status and sends `no-store` and `no-referrer` headers. It is not a
general report-management API. PGP delivery to the security contact and
researcher status-change email are still incomplete. Operator maintenance
commands list/show report metadata, update forward-only triage, and export only
stored ciphertext.

The server-rendered token page is `/server/preferences/tokens`; it supports
primary-token restore, reveal, rotation, and revocation plus linked-token
creation, reveal, rotation, update, and revocation. Destructive operations use
CSRF-protected forms and confirmation screens.

The public health statistics count completed non-health and non-metrics
requests. Lifetime totals persist in the database; the 24-hour total uses
hourly buckets. Updates flush every 30 seconds and during graceful
shutdown. The in-flight request count includes the current health request.

The service API detects and controls allow-listed local daemons when a
supported service manager is available. Domain list, read, create, update,
delete, DNS guidance, and DKIM key generation are operator-authorized. Domain
records use opaque UUIDv7 IDs and the reserved `system` owner; private DKIM
keys are encrypted at rest and never returned. Deletion is rejected while
mailboxes, aliases, DKIM/SPF/DMARC/archive policies, or mail-server mappings
depend on the domain. DNS records are derived from the current database
configuration and missing record types are reported. Basic alias list/create/
delete API operations, virtual-mailbox CRUD/password/CSV import, and
virtual-mailbox forwarding list/create/delete are available. Queue list/detail,
single-message flush/hold/release/delete, flush-all, and live queue counts use
the installed Postfix commands; hold/release/delete require the server process
to have the permissions Postfix requires. User-facing mail pages and
integration with the live MTA configuration remain incomplete. Unix mail-account
management, component configuration, delivery/spam statistics, and log
management are not implemented. The generated OpenAPI schemas are
generic for several responses and omit complete query parameters and error
metadata. GraphQL currently exposes paginated users, user detail, paginated
domains and domain detail, DNS and public DKIM records, aliases, forwards,
queue entries and statistics, complete detected service details (including
packages, executable, and service account), and aggregate mail health. It
applies operator Bearer authorization, rejects mutations by using a query-only
schema, and caps query depth at 8 and query size at 32 KiB. Pagination accepts
limits from 1 through 250. The schema does not yet cover all public server
reads or future mail routes. The GraphiQL page is
`/server/docs/graphql`; enter `Authorization: Bearer <operator-token>` in its
header editor. The header editor does not persist headers. Without JavaScript,
the page links to the plain GraphQL endpoint. Health checks do not yet probe
the mail daemons; Tor/I2P/GeoIP providers also remain incomplete.
Do not treat this development API as a stable production contract.

Swagger UI is available at `/server/docs/swagger` and loads the OpenAPI document
from `/api/v1/server/swagger`. MCP is available at `/api/v1/server/mcp`; the
direct aliases `/api/mcp` and `/mcp` use the same POST-only handler. MCP
requests require the `2026-07-28` protocol version and a Bearer token. The
operator token can invoke operator-authorized API operations; owner tokens only
see public operations because mail records are globally operator-managed. MCP
tool calls dispatch through the registered API routes, preserving their
validation, middleware, and authorization checks. MCP has no initialization
handshake or session state. Connection instructions and the live tool list are
available at `/server/help/mcp` and `GET /api/v1/server/help/mcp`; these resolve
the endpoint against the request host and report standard-token-visible tools.
