# Mail documentation

Mail is a self-hosted mail infrastructure management service. Its product
scope includes Postfix, Dovecot, mail users and domains, filtering, queue
operations, and service monitoring. The current implementation includes
server information and service discovery/control, virtual mailbox CRUD and CSV
import, domain and DNS/DKIM management, alias CRUD, virtual-mailbox forwarding,
Postfix queue operations, and allow-listed service discovery/control. Mail
records are global operator-managed resources under the reserved `system`
owner; owner tokens do not grant access to mail-stack records. Remaining mail
administration features are open in the
[implementation backlog](https://github.com/apimgr/mail/blob/main/TODO.AI.md).

## What is currently available

The current server scaffold exposes server-rendered information pages and
database-aware health and application metadata routes:

- `/` (home)
- `/server/about`, `/server/privacy`, `/server/contact`, `/server/help`,
  `/server/help/mcp`, `/server/terms`, `/server/dpo`
- `/server/security`, `/server/security/policy`, `/server/security/thanks`
- `/server/preferences`, `/server/preferences/sync`, `/server/docs/swagger`,
  `/server/docs/graphql`, `/server/preferences/tokens`
- `/manifest.json`, `/sw.js`, `/offline.html`, and versioned `/static/*` assets
- `/robots.txt`, `/sitemap.xml`, `/feed.xml`, `/rss.xml`, `/llms.txt`,
  `/.well-known/security.txt`, and `/.well-known/llms.txt`
- `GET /server/healthz`
- `GET /healthz` only when `server.healthz.root.enabled` is true
- `GET /api/healthz`
- `GET /api/healthz.txt`
- `GET /api/v1/server/healthz`
- `GET /api/v1/server/healthz.txt`
- `GET /api/v1/server/about`
- `GET /api/v1/server/version`
- `GET /api/v1/services` and `/api/v1/services/{service}` (operator token)
- `POST /api/v1/services/{service}/{start|stop|restart|reload}` (operator token)
- Operator-protected virtual mailbox list/detail/create/update/delete/password
  change and CSV import under `/api/v1/users`
- Operator-protected domain list/detail/CRUD/DNS/DKIM and alias list/create/delete
- Operator-protected virtual-mailbox forwarding list/create/delete under
  `/api/v1/forwards`
- Operator-protected Postfix queue list/detail/actions and queue statistics under
  `/api/v1/queue` and `/api/v1/stats/queue`
- Owner-token list, reveal, creation, rotation, update, revocation, and initial
  issuance routes under `/api/v1/server/preferences/tokens`

The information routes return server-rendered HTML. The `/server/healthz`
route negotiates HTML, JSON, or plain text based on the request's `Accept`
header and client type. Health responses contain public project/build/runtime
metadata, feature states, cached checks, persisted aggregate request counters,
and in-flight request counts. Health does not probe mail daemons; Tor/I2P/GeoIP
providers are incomplete, so health is not a production readiness signal.
Mail management pages, Unix mail-account management, live MTA synchronization,
component configuration, delivery/spam statistics, and log routes remain
incomplete. Postfix queue operations require the server process to have the
permissions Postfix requires.
The GraphiQL explorer at `/server/docs/graphql` uses locally served assets and
the operator-authenticated, read-only GraphQL endpoint. See the [API guide](api.md)
for its current mail query surface and limits.
Preferences use CSRF-protected POST forms without JavaScript; theme and
language persistence requires cookie consent. The sync page supports
shareable preference exports and imports. The public shell includes a consent
banner, installable manifest, and network-first service worker with an offline
fallback. The first HTML response sets a device-local build stamp; a changed
stamp sends `Clear-Site-Data` for cache and storage so stale app assets are
purged.

## Guides

- [Installation](installation.md)
- [Configuration](configuration.md)
- [API](api.md)
- [CLI](cli.md)
- [Security](security.md)
- [Integrations](integrations.md)
- [Development](development.md)
- [Translation](translating.md)
