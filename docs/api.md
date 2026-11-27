# API

This page describes the API handlers currently registered in the server. The
mail-management route inventory is in the
[project definition](https://github.com/apimgr/mail/blob/main/IDEA.md). The
registered routes currently cover public server pages, browser preferences,
owner-token management, operator-protected virtual mailbox CRUD and CSV import,
domain, alias, and forwarding operations, and operator service status and control. OpenAPI JSON includes protected routes
and declares their Bearer security; `llms.txt` uses the shared API registry.

| Method | Path | Current behavior |
|---|---|---|
| `GET` | `/api/v1/server/healthz` | Returns the public health payload; HTTP 503 indicates unhealthy or maintenance state. It includes project/build/runtime metadata, feature states, cached checks, persisted aggregate request counts, and the in-flight request count. |
| `GET` | `/api/v1/server/healthz.txt` | Returns the same health data as flat text; the `.txt` suffix overrides `Accept`. |
| `GET` | `/api/v1/server/auth/check` | Machine-only token check. Requires one Bearer credential, returns `{ "kind": "operator" }` or `{ "kind": "owner" }`; invalid credentials return 401. Rate limited and never echoes the token. |
| `GET` | `/api/v1/server/swagger` | Generates an OpenAPI JSON document from routes currently registered by the server, including protected routes with their authorization schemes. |
| `GET` | `/api/v1/server/about` | Returns localized title, project Markdown/content, and format, using configured About content when present. |
| `GET` | `/api/v1/server/version` | Returns version, commit/build metadata when available, Go version, and platform. |
| `GET` | `/api/v1/server/privacy` | Returns the public privacy policy data, configured cookie descriptions, retention details, tracking state, and consent/CCPA state. |
| `GET` | `/api/v1/server/dpo` | Returns the configured DPO name, DPO-or-general contact email, and request-derived absolute privacy URL. |
| `GET` | `/api/v1/server/security/policy` | Returns the configured disclosure policy Markdown or the built-in coordinated-disclosure policy. |
| `GET` | `/api/v1/server/help` | Returns the localized help title, Markdown content, format, and registered help links with localized descriptions. |
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
integration with the live MTA configuration remain incomplete. System-user
management, component configuration, delivery/spam statistics, and log
management are not implemented. The generated OpenAPI schemas are
generic for several responses and omit complete query parameters and error
metadata. The GraphQL endpoint and interface are unavailable. Health checks do
not yet probe the mail daemons; Tor/I2P/GeoIP providers also remain incomplete.
Do not treat this development API as a stable production contract.
