# Mail documentation

Mail is a self-hosted mail infrastructure management service. Its product
scope includes Postfix, Dovecot, mail users and domains, filtering, queue
operations, and service monitoring. Most of that product scope is not yet
implemented in this repository; see the [implementation backlog](https://github.com/apimgr/mail/blob/main/TODO.AI.md).

## What is currently available

The current server scaffold exposes server-rendered information pages and
database-aware health and application metadata routes:

- `/` (home)
- `/server/about`, `/server/privacy`, `/server/contact`, `/server/help`,
  `/server/help/mcp`, `/server/terms`, `/server/dpo`
- `/server/security`, `/server/security/policy`, `/server/security/thanks`
- `/server/preferences`, `/server/docs/swagger`, `/server/docs/graphql`
- `/manifest.json`, `/sw.js`, `/offline.html`, and versioned `/static/*` assets
- `GET /server/healthz`
- `GET /healthz` only when `server.healthz.root.enabled` is true
- `GET /api/healthz`
- `GET /api/v1/server/healthz`
- `GET /api/v1/server/about`
- `GET /api/v1/server/version`

The information routes return server-rendered HTML. The `/server/healthz`
route negotiates HTML, JSON, or plain text based on the request's `Accept`
header. Mail administration pages and the mail-specific API are not available
yet. Preferences use normal POST forms with no-JavaScript operation; the
language selector supports Automatic, and `?lang=` remains a shareable
one-shot override. The public shell includes an installable manifest and a
network-first service worker with an offline fallback.

## Guides

- [Installation](installation.md)
- [Configuration](configuration.md)
- [API](api.md)
- [CLI](cli.md)
- [Security](security.md)
- [Integrations](integrations.md)
- [Development](development.md)
- [Translation](translating.md)
