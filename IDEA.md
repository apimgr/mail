## Project description

Mail is a self-hosted management service for administrators who configure and
monitor Postfix, Dovecot, and related mail infrastructure. It brings mail
service status, domains, mailboxes, routing, spam and virus controls, TLS,
queue operations, and delivery monitoring into one product. The management
surface includes the documented API, command line, and server-rendered browser
workflows for user-facing mail features required by AI.md.

## Project variables

project_name: mail
project_org: apimgr
# STABLE — set at first-time setup; change only on an explicit rename or fork reset
internal_name: mail
# STABLE — set at first-time setup; change only on an explicit rename or fork reset
internal_org: apimgr
owner_token: mail_owner_token_aqhqfbeq

## Business logic

### Product scope & non-goals

Mail manages traditional mail infrastructure, including Postfix, Dovecot,
SpamAssassin, Rspamd, Amavisd-new, Pyzor, Razor, ClamAV, OpenDKIM, Fail2ban,
Policyd, and Postgrey when present. It supports system and virtual users,
multiple domains, aliases, forwarding, queue operations, component
configuration, backups, DNS guidance, TLS, DKIM, SPF, DMARC, SASL, service
health, logs, delivery statistics, spam statistics, and alerts. Optional
integrations are available only when their corresponding service is installed
and configured. CalDAV/CardDAV, OAuth2, S3 storage, full-text search, and
two-factor authentication are future considerations, not current requirements.

The product provides public server information pages, browser workflows for
user-facing mail features, and API/CLI access. It does not add an
account-based administration dashboard. Operator-only actions follow the
authorization requirements defined by AI.md.

All features are free and open source under MIT, with no premium tiers,
feature gating, or usage limits beyond stability protections such as rate
limits and quotas.

### Roles & permissions

- **Operator:** configures the mail stack and can perform privileged service,
  configuration, queue, user, domain, backup, and security operations.
- **Mail recipient:** may be represented by a Unix system account or a virtual
  mailbox; virtual mailboxes have no shell access.
- **Unauthenticated caller:** may access only endpoints AI.md defines as
  public, such as public server information and health surfaces.

The project has no panel-specific user account or login role. API credential
and endpoint authorization rules follow AI.md.

### Data model & sensitivity

Mail data includes system users, virtual users, virtual domains, aliases,
forwards, transports, queue entries, service status, and delivery/spam/queue
statistics. Virtual users include an email address, domain, password hash,
mailbox quota, and enabled status. Domains include activation status and
optional DKIM key material. Transports may contain relay credentials. Queue
records include sender, recipients, arrival time, status, and delivery reason.

The product-level record fields are:

- **Virtual user:** ID, full email, local part, domain, panel password hash,
  quota in bytes (zero means unlimited), enabled flag, and created/updated
  timestamps. The mail service may require a separate compatible password
  representation.
- **System user:** username, UID/GID, home directory, shell, mail-enabled
  flag, and quota.
- **Virtual domain:** ID, domain, active flag, DKIM-enabled flag, selector,
  private/public DKIM key material, and created/updated timestamps.
- **Alias:** ID, source address, one or more destinations, domain, active
  flag, and creation time.
- **Forward:** ID, source, destination list, keep-local flag, active flag,
  and creation time.
- **Transport:** ID, domain/pattern, transport, next hop, optional SMTP
  username/password, and active flag.
- **Queue entry:** Postfix queue ID, size, sender, recipients, arrival time,
  state, and delivery reason. It is read-only except for supported queue
  actions.
- **Service status:** component name, running/enabled flags, PID, uptime, and
  version.
- **Mail statistics:** period, sent/received/bounced/rejected counts,
  spam/virus blocked counts, and current queue size.

Passwords, private DKIM keys, relay credentials, access tokens, and mail
content are sensitive. Queue metadata, user addresses, domain configuration,
and logs may also contain personal or operationally sensitive information.
Secrets must not be exposed through public health pages, API errors, or logs.

### Trust boundaries & external services

The application manages external mail daemons and operating-system services;
it does not own their SMTP, IMAP, filtering, or queue protocols. Installed
Postfix, Dovecot, spam-filter, antivirus, DNS, certificate, and database
services are external dependencies whose availability and configuration are
not trusted implicitly. The application must report unavailable or
unsupported components and preserve existing configuration when an operation
fails. Remote mail relays and DNS responses are untrusted inputs.

### Threat model & abuse cases

Protected assets include mailbox credentials, DKIM private keys, relay
credentials, mail routing/configuration, queued message metadata, and service
availability. Attackers may attempt unauthorized service control, mailbox
creation or deletion, credential theft, queue deletion, configuration
injection, log disclosure, or resource exhaustion through bulk operations,
logs, or API requests. Mail addresses, uploaded CSV values, configuration
content, DNS answers, and mail-service output are untrusted. Authorization,
input validation, safe configuration writes, rate and size limits, redaction,
and the security controls in AI.md are required defenses. The product does
not claim to prevent spam or malware beyond the installed and configured
filtering services.

### Security decisions & exceptions

- System-user management and mail-service control are privileged operations
  and require the operator authorization defined by AI.md.
- System-user and virtual-mailbox modes are both supported; selecting system
  users does not grant the panel permission to weaken operating-system account
  security.
- Component detection is informational. A missing optional daemon does not
  prevent the management service from starting.
- The product does not expose an account-based administration dashboard.
  Browser workflows for user-facing features follow the authorization model
  defined by AI.md.

### API routes

Project routes below use `/api/v1`. Read-only mail data routes require the
authorization defined by AI.md; state-changing and operator-only operations
require the operator credential. Requests and responses use the API formats
defined by AI.md.

| Method and path | Purpose and request/response summary |
|---|---|
| `GET /services` | List detected mail services and status. |
| `GET /services/{service}` | Return one service's status. |
| `POST /services/{service}/start`, `/stop`, `/restart`, `/reload` | Control a service; return the operation result and status. |
| `GET /users` | List system and virtual users. |
| `GET /users/{id}` | Return one user's details. |
| `POST /users` | Create a user from the supplied user fields. |
| `PUT /users/{id}` | Update user fields. |
| `DELETE /users/{id}` | Delete a user. |
| `POST /users/{id}/password` | Change a user's password. |
| `POST /users/import` | Import users from CSV and report per-row results. |
| `GET /domains` | List virtual domains. |
| `GET /domains/{domain}` | Return one domain. |
| `POST /domains` | Add a domain. |
| `PUT /domains/{domain}` | Update a domain. |
| `DELETE /domains/{domain}` | Delete a domain. |
| `GET /domains/{domain}/dns` | Return required DNS records. |
| `GET /domains/{domain}/dkim` | Return public DKIM record details; never disclose a private key. |
| `POST /domains/{domain}/dkim/generate` | Generate and store DKIM key material; return public DNS data. |
| `GET /aliases` | List aliases. |
| `POST /aliases` | Create an alias from a source and destination. |
| `DELETE /aliases/{id}` | Delete an alias. |
| `GET /forwards` | List forwarding rules. |
| `POST /forwards` | Create a forward with destinations and local-copy behavior. |
| `DELETE /forwards/{id}` | Delete a forward. |
| `GET /queue` | List queue entries and delivery status. |
| `GET /queue/{id}` | Return queue entry details. |
| `POST /queue/{id}/flush`, `/hold`, `/release` | Perform the requested queue operation. |
| `DELETE /queue/{id}` | Delete a queued message. |
| `POST /queue/flush-all` | Flush the full queue. |
| `GET /config/{component}` | Return the component configuration, with secrets redacted. |
| `PUT /config/{component}` | Validate and update component configuration. |
| `POST /config/{component}/validate` | Validate submitted component configuration without applying it. |
| `GET /config/{component}/templates` | List available configuration templates. |
| `POST /config/{component}/backup` | Back up component configuration. |
| `POST /config/{component}/restore` | Restore a component configuration backup. |
| `GET /stats/delivery`, `/stats/spam`, `/stats/queue` | Return delivery, spam, or queue statistics. |
| `GET /logs/{component}` | Search/filter logs for a component. |
| `GET /health` | Return overall mail-system health. |

The template's own server, health, version, and discovery endpoints are
defined by AI.md and are not repeated here.

### Setup and operator flows

Initial setup detects the supported operating system and installed mail
services, presents minimal/standard/advanced/custom component profiles,
configures virtual-mail storage and domains, selects system/virtual/mixed user
storage, configures TLS, creates the first operator and mailbox credentials,
previews changes, applies configuration, and verifies services. When automatic
installation is unavailable, setup presents installation instructions and
allows the operator to skip a component.

Operators can review DNS records for MX, SPF, DKIM, and DMARC; inspect and
manage mail queues; configure aliases and forwarding; and monitor component
health, delivery, spam, and logs.

### Supported component packages

Package names and service account mappings are distribution-specific. The
supported platforms and component/package mappings are retained in the
project's original feature plan; unavailable optional components are skipped
and reported. The application does not claim a uniform package name across
distributions.

The planned package names are distribution-specific:

| Platform | Core and optional packages |
|---|---|
| Alpine | `postfix`, `dovecot`, `dovecot-pigeonhole-plugin`, `spamassassin`, `amavisd-new`, `pyzor`, `razor`, `rspamd`, `clamav`, `clamav-daemon`, `opendkim`, `opendkim-utils`, `fail2ban` |
| Debian/Ubuntu | `postfix`, `postfix-mysql`, `postfix-pgsql`, `dovecot-core`, `dovecot-imapd`, `dovecot-pop3d`, `dovecot-lmtpd`, `dovecot-managesieved`, `spamassassin`, `amavisd-new`, `pyzor`, `razor`, `rspamd`, `clamav`, `clamav-daemon`, `opendkim`, `opendkim-tools`, `fail2ban` |
| RHEL family | `postfix`, `dovecot`, `dovecot-mysql`, `dovecot-pgsql`, `dovecot-pigeonhole`, `spamassassin`, `amavisd-new`, `pyzor`, `razor`, `rspamd`, `clamav`, `clamav-server`, `clamav-update`, `opendkim`, `fail2ban` |
| FreeBSD | `postfix`, `dovecot`, `dovecot-pigeonhole`, `spamassassin`, `amavisd-new`, `pyzor`, `razor`, `rspamd`, `clamav`, `opendkim`, `fail2ban` |

The expected service accounts are `postfix:postfix`, `dovecot:dovecot`,
`amavis:amavis` (FreeBSD: `vscan:vscan`), ClamAV's distribution account,
`vmail:vmail`, and `opendkim:opendkim`. The setup flow creates the virtual
mail user with UID/GID 5000 when that identity is available.

### Product constraints

- The server is distributed as a single static binary and supports Linux,
  macOS, Windows, and FreeBSD on amd64 and arm64.
- The product is free and open source under MIT, with no premium tiers or
  feature gating.
- Mail services are configured only when present and supported on the host.
