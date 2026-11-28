# Configuration

The configuration package currently reads YAML from the path supplied by the
server startup flow, expands environment variables, validates basic settings,
and applies defaults. The server configuration includes listener address and
port, runtime mode (`production`, `development`, or `debug`), admin path,
setup token, maintenance/debug flags, trusted proxies,
timeouts, session defaults, rate limits, i18n preferences, tracking, privacy,
and scheduler settings. The optional root health alias is disabled by default
and can be enabled with:

## Environment Variables

These environment variables affect the server's current runtime behavior.
There is no generic environment-variable override for arbitrary server config
keys.

| Variable | Behavior |
|---|---|
| `PORT` | Listener port; precedence is `--port`, `PORT`, early config, then context default. |
| `LISTEN` | Listener IP address; precedence is `--address`, `LISTEN`, early config, then `[::]`. |
| `MODE` | Runtime mode: `production`, `development`, or explicit `debug`. |
| `DEBUG` | Enables diagnostics; debug routes also require the operator Bearer token. |
| `DOMAIN` | Overrides the detected host name and contributes trusted public CORS origins. |
| `PID_FILE` | PID-file path; overridden by `--pid`. |
| `NO_COLOR`, `TERM` | Control terminal color and ANSI output. |
| `CONFIG_DIR`, `DATA_DIR`, `CACHE_DIR`, `LOG_DIR`, `BACKUP_DIR` | Override the corresponding runtime directories. |
| `DATABASE_DIR`, `SECURITY_DIR`, `SSL_DIR` | Override the SQLite, security-data, and SSL directories. |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_TLS`, `SMTP_FROM_NAME`, `SMTP_FROM_EMAIL` | Override SMTP and sender settings at runtime. `SMTP_HOST` also disables saving an autodetected host. |

`HOME`, `HOSTNAME`, and platform or service-manager variables are used for
path, hostname, or runtime-context detection; they are not application
settings. Secrets should be supplied through protected configuration files or
the relevant environment variable, and must not be placed in command-line
arguments that may appear in process listings.

## Configuration sections

The current loader exposes the following sections. Some settings are reserved
for features that are not implemented yet; their presence in the YAML model
does not mean the corresponding behavior is available.

| Section | Current keys and purpose |
|---|---|
| `server` | Listener (`port`, `address`, `pidfile`, `daemonize`), `baseurl`, mode/logging, FQDN reference, admin path, setup token, maintenance/debug flags, database and SSL settings, update channel/policy, scheduler, trusted proxies, CORS, request limits, session defaults, rate limits, notifications, token limits, i18n, tracking, contact addresses, branding content, help pages, privacy/consent/cookies/retention/third parties, and optional root health alias. |
| `cli` | `binary_download.dir` selects the operator-provisioned CLI release directory (default `{data_dir}/cli`); `binary_download.require_auth` optionally requires a Bearer token for downloads (default `false`). Configure this in `yaml.d/maintenance.yml`. |
| `database` and `server.database` | Driver, URL, token, and connection pool limits. SQLite is the default; libSQL is remote-only. Legacy top-level settings are migrated into the server configuration. |
| `ssl` and `server.ssl` | SSL enablement, certificate and key paths, Let's Encrypt enablement, contact email, and staging mode. Listener TLS and certificate lifecycle are not implemented. |
| `email` | Legacy SMTP settings. Current outgoing notification delivery uses `server.notifications.email`. |
| `web` | `security.policy` and `robots.allow`/`robots.deny`. |

Sensitive values such as `server.token`, encryption keys, database tokens, and
SMTP passwords belong in `secrets.yml`; do not include them in `server.yml`,
drop-ins, examples, or command arguments.

```yaml
server:
  mode: production
  logs:
    level: ""
    defaults:
      custom: ""
      rotate: weekly,50MB
      keep: none
    access:
      format: apache
      rotate: monthly
      keep: none
    audit:
      enabled: true
      format: json
      rotate: daily
    debug:
      enabled: false
      format: text
  healthz:
    root:
      enabled: true
```

Before a privileged listener is bound, startup peeks at `server.port`,
`server.address`, `server.daemonize`, `server.pidfile`, and `server.logs` in
`server.yml` and ordered `yaml.d/*.yml` or `yaml.d/*.yaml` files. This read
skips malformed files and does not read `secrets.yml`; the regular
configuration load happens after a root-started process drops to the `mail`
service account. A configured
port is a single integer from 1 through 65535. The default is port 80 in an
application container and an available 64000–64999 port elsewhere. Port `0`
and dual HTTP/HTTPS port strings are not supported by the current listener.

Log settings select a format and rotation/retention policy per log kind.
Rotation supports `never`, `daily`, `weekly`, `monthly`, `yearly`, size limits
such as `50MB`, and a time-plus-size pair. Retention accepts `none`, a count,
an age such as `14d`, `4w`, or `6m`, or `forever`. `SIGUSR1` reopens active log
files after external rotation; `SIGUSR2` writes an operational status snapshot
without stopping the server. The debug log is disabled by default.

```yaml
server:
  port: 8080
  address: "[::]"
  baseurl: /mail
  pidfile: true
  daemonize: false
```

`server.baseurl` sets a path prefix for direct deployments. The `--baseurl`
flag overrides it. A trusted reverse proxy's `X-Forwarded-Prefix`,
`X-Forwarded-Path`, or `X-Script-Name` takes precedence; forwarded prefix
headers are ignored unless the immediate peer is a configured trusted proxy.
Routes, redirects, generated links, assets, the web manifest, and the service
worker use the resolved prefix. Use `/` for a root deployment.

Trusted proxy configuration follows the nested AI.md schema:

```yaml
server:
  trusted_proxies:
    additional: []
    client_ip_header: auto
```

Loopback, private, link-local, and the configured listener's local subnet are
trusted automatically. Put public proxy IPs, CIDRs, or DNS names in
`additional`; DNS names refresh every five minutes. Forwarded URL and client
identity headers from other peers are ignored.

Database configuration includes driver, connection details, and pool settings.
SQLite is the default. The remote-only libSQL driver accepts a `libsql://` or
`https://` URL and an optional separate token; the token is added as the
`authToken` URL parameter when needed. The configured URL must point to a
reachable libSQL server. SMTP notifications use
`server.notifications.email`, with credentials optionally supplied through the
`SMTP_*` environment variables. The operator email address has no generated
default: set it at `server.contact.admin.email` to enable event notifications.

```yaml
server:
  contact:
    admin:
      email: operator@example.com
  notifications:
    email:
      smtp:
        host: smtp.example.com
        port: 587
        username: ""
        password: ""
        tls: auto
      from:
        name: Mail
        email: mailer@example.com
      events:
        startup: false
        shutdown: false
        security_alert: true
        scheduler_error: true
```

`server.update.branch` selects the channel used by the server updater. Accepted
aliases `daily`, `rolling`, `dev`, and `development` normalize to `devel`.
Scheduled checks honor `defer_days` for stable and beta releases; devel ignores
the age window. With `auto_install: true`, a process that started as root
downloads and verifies a release as the dropped service user, then exits with
code 75 after graceful shutdown so a supervisor can restart it. The next root
startup re-fetches the official checksum, verifies the staged binary, replaces
the installed binary, and re-executes. A process that did not start as root
only notifies the operator and records the skipped installation. Manual
`--update yes` is not gated by the scheduled defer window. `cli_min_version`
is returned in autodiscover; the CLI blocks normal requests when its installed
version is below that minimum. When shutdown email is enabled, a signal shutdown
uses reason `signal`, and an updater-triggered restart uses `update restart`.

The server's CLI release directory and download policy belong in
`yaml.d/maintenance.yml`:

```yaml
cli:
  binary_download:
    dir: "{data_dir}/cli"
    require_auth: false
```

The server reads `version.txt` and `sha256.txt` alongside release files named
`mail-cli-{os}-{arch}[.exe]`. It advertises only binaries whose computed hash
matches the manifest and rechecks the hash before serving a download.

```yaml
server:
  update:
    branch: stable
    auto_install: false
    defer_days: 0
    cli_min_version: ""
```

```yaml
server:
  database:
    driver: sqlite # or libsql for a remote Turso/libSQL database
    url: "" # for libsql, set libsql://host or https://host
    token: "" # optional when the URL already has authToken
    pool:
      max_open: 25
      max_idle: 5
      max_lifetime: 5m
      max_idle_time: 1m
```

The server opens a separate write/read handle pair. SQLite reads use a
query-only connection; remote libSQL reads and writes use the configured
endpoint. Keep database credentials in `secrets.yml` or environment variables.

Supported TLS modes are `auto`, `starttls`, `tls`, and `none`. The server
checks a configured SMTP relay during startup; when no host is configured it
tries the detected host candidates and saves the selected host/port into
`server.yml` without replacing other settings. Set `SMTP_HOST` to override the
file value and disable that persistence. SMTP passwords are never included in
logs.

`server.rate_limit` loads the AI.md request-class defaults and enforces the
read, write, health, and global-burst windows. Valid Bearer tokens use a
per-token bucket; anonymous and invalid-token requests use the client IP.
Failed operator/owner authentication is counted per IP and locks out further
requests from that address when the configured threshold is reached. The
strict per-IP limit also applies to initial owner-token issuance and token
reveal actions:

```yaml
server:
  rate_limit:
    read: {requests: 120, window: 60}
    write: {requests: 10, window: 60}
    health: {requests: 120, window: 60}
    strict: {requests: 10, window: 900}
    auth_failures: {requests: 5, window: 900}
    global_burst: 240
```

CORS defaults to `*` without credentials. Credentials are returned only to an
explicitly allowed origin; a wildcard response never includes
`Access-Control-Allow-Credentials`:

```yaml
server:
  cors:
    allowed_origins: ["*"]
    allow_credentials: true
    max_age: 86400
```

`web.robots` controls the generated crawler policy. Defaults allow `/` and
`/api`; configured deny prefixes are emitted as `Disallow` rules. The sitemap
uses the same rules to omit disallowed public pages:

```yaml
# server.yml or yaml.d/maintenance.yml
server:
  scheduler:
    timezone: America/New_York
    catch_up_window: 1h
    tasks:
      backup_daily:
        enabled: true
        schedule: "0 2 * * *"
      geoip_update:
        enabled: false
        schedule: "0 3 * * 0"
```

The built-in scheduler reads its IANA timezone, catch-up window, and per-task
enabled, schedule, and retry values from `server.scheduler`. Invalid timezone
or catch-up values fall back to `America/New_York` and `1h`. Required task
definitions and their schedules are registered, but most task handlers are
not yet connected to functional providers. The database/self-health probes,
CAPTCHA cleanup, and scheduled update check have functional handlers; backup,
GeoIP, certificate, logging, and other maintenance providers are still
unavailable and are tracked in the project backlog.

```yaml
# yaml.d/web.yml
web:
  robots:
    allow: [/, /api]
    deny: [/server/preferences]
  security:
    policy: ""
server:
  i18n:
    default_language: en
    available_languages: [en, es, fr, de, it, ar]
```

The configuration loader merges `server.yml`, YAML files in `yaml.d/`, and
`secrets.yml` in that order. The generated `server.token` and the 32-byte
`server.security.encryption_key` are stored only in `secrets.yml` with mode
`0600`; existing copies of either value in the base config or drop-ins are
migrated into that file. A missing operator token is generated before the
server starts and printed once to stdout. Later starts show only its masked
prefix and final four characters. The encryption key is base64 encoded,
versioned as `1`, and never printed. Other generated secrets and the complete
secret schema remain unimplemented. Shared settings belong in their
respective area files:

Security reports default to a 365-day retention period after closure. The
daily `security_cleanup` task deletes only closed reports older than
`server.security.reports.retention_days`; open reports are retained.

```yaml
# server.yml
server:
  security:
    reports:
      retention_days: 365
```

`DEBUG=true` enables authenticated `/debug/*` diagnostics in any runtime mode.
`MODE=debug` enables them by default; `DEBUG=false` disables them in that mode.
The explicit `--debug` flag overrides `DEBUG=false`.

```yaml
# yaml.d/privacy.yml
server:
  privacy:
    data:
      sold: false
      stored_on_server: true
    consent:
      show_until_acknowledged: true
      policy:
        text: "Privacy Policy"
        url: "/server/privacy"
        version: "1"
    cookies:
      essential:
        enabled: true
      preferences:
        enabled: true
      analytics:
        enabled: true
    third_party:
      services:
        - name: "Mail provider"
          purpose: "Email delivery"
          data_sent: "Recipient address and message content"
          policy_url: "https://provider.example/privacy"
# yaml.d/contact.yml
server:
  contact:
    security:
      email: "security@{fqdn}"
    dpo:
      name: ""
      email: ""
    general:
      email: ""
# yaml.d/branding.yml
server:
  branding:
    content:
      header:
        format: html
        body: '<span>Operator notice</span>'
      about:
        format: md
        body: ""
      privacy:
        format: md
        body: ""
      terms:
        format: md
        body: ""
      footer:
        format: html
        body: '<p>© {current_year} {project_name}</p>'
# yaml.d/pages.yml
server:
  pages:
    contact:
      enabled: true
    help:
      content: ""
      mcp: ""
```

The privacy API and page include these manually configured third-party services.
An enabled supported `server.tracking.type` also adds its analytics provider.
The DPO page and JSON mirror use `server.contact.dpo.email`, falling back to
`server.contact.general.email`; the admin address is never exposed there.
`server.branding.content.header` is inserted after the built-in header actions
on every public page. `.about` and `.privacy` extend their built-in pages;
`.terms` replaces the built-in terms. `.footer` replaces the custom branding
block above the always-visible application footer. Header and footer accept
HTML only; about, privacy, and terms accept `md`, `html`, or `txt`. Each body
can be a string or a language map with an optional `default` key. Bodies are
limited to 256 KiB and sanitized before HTML insertion. Header and footer use
the strict formatting allowlist; document pages additionally allow tables and
code blocks. Scripts, forms, styles, unsafe URLs, H1 headings, and links to
`/server/contact` are removed. An empty footer body hides the custom branding
block. Available substitutions include `{current_year}`, `{project_name}`,
`{project_org}`, `{project_version}`, and `{build_datetime}`; Tor and I2P
address substitutions are empty when no address is published.
Set `web.security.policy` to replace the built-in coordinated-disclosure
policy shown at `/server/security/policy` and returned by its API mirror.
`web.security.report_url` selects the primary source-code vulnerability
reporting URL. `server.contact.security.email` is the public secondary contact;
set it to an empty string to omit the public mail address. The admin email is
never published. `server.pages.contact.enabled: false` disables the public
contact form and removes its security-report link from the security overview.
`web.security.keyservers` is a list of bare DNS host names for public-key
publication; the default is `keys.openpgp.org`. Invalid entries, URL schemes,
paths, and ports are discarded during configuration validation. An empty list
disables keyserver publication.
The security API mirrors the live reporting contacts and publishes a PGP
fingerprint only when a valid OpenPGP v4 public key is present.
`server.pages.help.content` replaces the project help Markdown rendered on
`/server/help` and returned by its JSON mirror. `server.pages.help.mcp` replaces
the project-specific Mail guidance on `/server/help/mcp` and in
`GET /api/v1/server/help/mcp`; the base connection, authentication, live tool
list, limits, and troubleshooting sections are always rendered.

The public consent banner stores category choices and a Unix timestamp as
percent-encoded compact JSON in the browser's `cookie_consent` cookie. The
server writes it with `url.PathEscape` and reads it with `url.PathUnescape`;
raw JSON cookie values are invalid. Preference writes are blocked until the
visitor enables preference cookies. `sold: true` enables the CCPA opt-out
control; leave it false unless the instance actually sells or shares personal
information.

This is a description of the current code surface, not a complete supported
configuration contract. The complete schema, unknown-key handling, remaining
secret storage, generated defaults, and operator guidance remain in the
[backlog](https://github.com/apimgr/mail/blob/main/TODO.AI.md).

Do not place credentials in committed files. The full configuration contract
will be documented here as those settings are implemented.
