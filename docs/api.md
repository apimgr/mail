# API

All currently registered JSON API routes are listed below. The full mail
management API inventory is in the
[project definition](https://github.com/apimgr/mail/blob/main/IDEA.md), but those routes are not
implemented yet.

| Method | Path | Current behavior |
|---|---|---|
| `GET` | `/api/v1/server/healthz` | Returns healthy/unhealthy status based on database availability. |
| `GET` | `/api/v1/server/about` | Returns application name, description, and version. |
| `GET` | `/api/v1/server/version` | Returns version, commit/build metadata when available, Go version, and platform. |
| `GET` | `/api/v1/server/preferences` | Returns the current browser theme, language, and empty app-specific preference map. |
| `GET` | `/api/v1/server/preferences/sync` | Returns current preferences plus a shareable URL and short code. |
| `GET` | `/api/v1/server/preferences/sync/import` | Applies valid theme/language values only when preference-cookie consent is enabled; otherwise reports `consent_required`. Returns `applied` and `ignored` arrays, including when empty. |
| `POST` | `/api/v1/server/preferences` | Applies a partial theme/language update from JSON or form data. No cookie consent or CSRF token is accepted by this API route. |
| `GET` | `/api/healthz` | Direct versionless JSON health alias. |
| `GET` | `/server/healthz` | Content-negotiated health page: HTML for browsers, JSON or text for clients. |
| `GET` | `/healthz` | Optional alias to the same content-negotiated handler; disabled unless `server.healthz.root.enabled` is true. |

Public discovery resources include `/robots.txt`, `/sitemap.xml`, `/feed.xml`,
`/rss.xml`, `/.well-known/security.txt`, `/.well-known/llms.txt`, and
`/llms.txt`. They accept `GET` and `HEAD` only. Preference sync responses are
not cacheable and never include consent or credential cookies in an export.

The current scaffold does not yet provide the final token authorization,
error envelope, and request ID behavior required by AI.md. Do not treat this
development API as a stable production contract.
