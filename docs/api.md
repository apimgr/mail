# API

Currently registered API and paired health routes are listed below. The full mail
management API inventory is in the
[project definition](https://github.com/apimgr/mail/blob/main/IDEA.md), but those routes are not
implemented yet.

| Method | Path | Current behavior |
|---|---|---|
| `GET` | `/api/v1/server/healthz` | Returns the public health payload; HTTP 503 indicates unhealthy or maintenance state. It includes project/build/runtime metadata, feature states, cached checks, persisted aggregate request counts, and the in-flight request count. |
| `GET` | `/api/v1/server/healthz.txt` | Returns the same health data as flat text; the `.txt` suffix overrides `Accept`. |
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
| `GET` | `/api/healthz` | Direct versionless health alias; JSON by explicit `Accept`, plain text for non-interactive clients. |
| `GET` | `/api/healthz.txt` | Direct versionless plain-text health alias; `.txt` overrides `Accept`. |
| `GET` | `/server/healthz` | Content-negotiated health page: HTML for browsers, JSON or text for clients. |
| `GET` | `/healthz` | Optional alias to the same content-negotiated handler; disabled unless `server.healthz.root.enabled` is true. |

Public discovery resources include `/robots.txt`, `/sitemap.xml`, `/feed.xml`,
`/rss.xml`, `/.well-known/security.txt`, `/.well-known/llms.txt`, and
`/llms.txt`. They accept `GET` and `HEAD` only. Preference sync responses are
not cacheable and never include consent or credential cookies in an export.

The public health statistics count completed non-health and non-metrics
requests. Lifetime totals persist in the database; the 24-hour total uses
hourly buckets. Updates flush every 30 seconds and during graceful
shutdown. The in-flight request count includes the current health request.

The current health response reports unavailable runtime providers as errors;
the Tor/I2P/GeoIP managers and mail-service probes are not implemented yet.
The primary-token issuance endpoint is implemented, but token authorization
for mail resources and the remaining API contract are incomplete. Do not treat
this development API as a stable production contract.
