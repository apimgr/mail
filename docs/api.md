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
| `GET` | `/api/healthz` | Direct versionless JSON health alias. |
| `GET` | `/server/healthz` | Content-negotiated health page: HTML for browsers, JSON or text for clients. |
| `GET` | `/healthz` | Optional alias to the same content-negotiated handler; disabled unless `server.healthz.root.enabled` is true. |

The current scaffold does not yet provide the final token authorization,
error envelope, and request ID behavior required by AI.md. Do not treat this
development API as a stable production contract.
