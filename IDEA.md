# mail - Email Stack Management Panel

**Project**: mail  
**Organization**: apimgr  
**Type**: Web Administration Panel for Mail Infrastructure

---

## Overview

A unified web management interface for controlling and configuring the traditional mail server stack (Postfix, Dovecot, SpamAssassin, ClamAV, etc.). Provides a single pane of glass for managing all email infrastructure components.

## Tagline

Web-based control panel for the complete mail server stack.

## Description

mail is a Go-based administration panel that simplifies managing complex mail server infrastructure. Instead of manually editing dozens of configuration files across multiple services, administrators use a clean web interface to configure Postfix, Dovecot, spam filtering, virus scanning, and more. Handles both system users and virtual users, supports multiple domains, and provides real-time monitoring of mail queue, delivery stats, and security metrics.

---

## Core Features

### Service Management
- **Component Detection**: Auto-detect installed mail services (Postfix, Dovecot, SpamAssassin, etc.)
- **Service Control**: Start, stop, restart, reload mail services
- **Configuration Editor**: Web-based config file editing with validation
- **Template System**: Pre-configured templates for common setups
- **Backup/Restore**: Save and restore complete mail stack configurations

### User Management
- **System Users**: Manage Unix system users with email access
- **Virtual Users**: Database-backed virtual mailboxes (no shell access)
- **User Mapping**: Map between system users and email identities
- **Bulk Operations**: Import/export users from CSV
- **Password Management**: Change passwords, enforce policies

### Domain Management
- **Virtual Domains**: Host multiple email domains on one server
- **Domain Aliases**: Mirror one domain to another
- **Catchall Addresses**: Catch-all inbox for undefined addresses
- **Domain DKIM**: Per-domain DKIM signing keys
- **DNS Helper**: Show required DNS records (MX, SPF, DKIM, DMARC)

### Alias & Forwarding
- **Email Aliases**: Create aliases pointing to local or remote addresses
- **Forwarding Rules**: Forward mail to multiple destinations
- **Vacation Messages**: Auto-reply configuration
- **Sieve Filters**: Advanced mail filtering rules (if Dovecot supports)

### Spam & Virus Protection
- **SpamAssassin**: Configure spam detection rules and scores
- **Rspamd**: Alternative modern spam filtering
- **Pyzor/Razor**: Collaborative spam detection networks
- **Amavisd-new**: Content filter integration
- **ClamAV**: Virus scanning for attachments
- **Greylisting**: Temporary rejection for spam reduction
- **Blocklists**: DNS-based blocklists (RBL/DNSBL)
- **Whitelists**: Trusted sender lists

### Security & Authentication
- **TLS/SSL**: Configure TLS for SMTP, IMAP, POP3
- **Let's Encrypt**: Automatic certificate generation and renewal
- **DKIM**: DomainKeys Identified Mail signing
- **SPF**: Sender Policy Framework validation
- **DMARC**: Domain-based Message Authentication
- **SASL**: SMTP authentication configuration
- **Fail2ban**: Brute force protection (if installed)

### Monitoring & Logs
- **Mail Queue**: View and manage Postfix queue
- **Delivery Stats**: Success/failure rates, bounce tracking
- **Spam Statistics**: Detection rates, false positives
- **Service Status**: Real-time health of all components
- **Log Viewer**: Search and filter mail logs
- **Alerts**: Email/webhook notifications for issues

---

## Managed Components

### Core Mail Services
| Component | Purpose | Config Files Managed |
|-----------|---------|---------------------|
| **Postfix** | SMTP server (MTA) | main.cf, master.cf, virtual, transport, etc. |
| **Dovecot** | IMAP/POP3 server | dovecot.conf, conf.d/*, protocols, auth |

### Spam & Content Filtering
| Component | Purpose | Config Files Managed |
|-----------|---------|---------------------|
| **SpamAssassin** | Content-based spam filter | local.cf, rules |
| **Rspamd** | Fast spam filter (alternative) | rspamd.conf, modules.d/* |
| **Amavisd-new** | Content filter interface | amavisd.conf |
| **Pyzor** | Collaborative spam detection | servers |
| **Razor** | Spam signature database | razor-agent.conf |
| **ClamAV** | Antivirus scanner | clamd.conf, freshclam.conf |

### Optional Components
| Component | Purpose | Config Files Managed |
|-----------|---------|---------------------|
| **OpenDKIM** | DKIM signing daemon | opendkim.conf, keys |
| **Fail2ban** | Brute force protection | jail.local (mail sections) |
| **Policyd** | Policy server | policyd.conf |
| **Postgrey** | Greylisting daemon | postgrey.conf |

---

## Package Mapping

### Alpine Linux (apk)
```
postfix
dovecot
dovecot-pigeonhole-plugin
spamassassin
amavisd-new
pyzor
razor
rspamd
clamav
clamav-daemon
opendkim
opendkim-utils
fail2ban
```

### Debian/Ubuntu (apt)
```
postfix
postfix-mysql
postfix-pgsql
dovecot-core
dovecot-imapd
dovecot-pop3d
dovecot-lmtpd
dovecot-managesieved
spamassassin
amavisd-new
pyzor
razor
rspamd
clamav
clamav-daemon
opendkim
opendkim-tools
fail2ban
```

### RHEL/CentOS/Rocky/Alma (yum/dnf)
```
postfix
dovecot
dovecot-mysql
dovecot-pgsql
dovecot-pigeonhole
spamassassin
amavisd-new
pyzor
razor
rspamd
clamav
clamav-server
clamav-update
opendkim
fail2ban
```

### FreeBSD (pkg)
```
postfix
dovecot
dovecot-pigeonhole
spamassassin
amavisd-new
pyzor
razor
rspamd
clamav
opendkim
fail2ban
```

### User Mapping

| Component | Alpine | Debian/Ubuntu | RHEL/CentOS | FreeBSD |
|-----------|--------|---------------|-------------|---------|
| **Postfix** | postfix:postfix | postfix:postfix | postfix:postfix | postfix:postfix |
| **Dovecot** | dovecot:dovecot | dovecot:dovecot | dovecot:dovecot | dovecot:dovecot |
| **Amavis** | amavis:amavis | amavis:amavis | amavis:amavis | vscan:vscan |
| **ClamAV** | clamav:clamav | clamav:clamav | clam:clam | clamav:clamav |
| **Virtual Mail** | vmail:vmail | vmail:vmail | vmail:vmail | vmail:vmail |
| **OpenDKIM** | opendkim:opendkim | opendkim:opendkim | opendkim:opendkim | opendkim:opendkim |

**Note:** Virtual mail user (vmail) is created by the management panel for virtual mailbox storage.

---

## Setup Wizard

### Initial Configuration Flow

**Step 1: System Detection**
- Detect OS (Alpine, Debian, Ubuntu, RHEL, CentOS, Rocky, Alma, FreeBSD)
- Scan for existing mail components
- Check installed packages
- Identify running services
- Display findings to user

**Step 2: Component Selection**
Present configuration profiles:
- **Minimal** (Postfix + Dovecot only)
- **Standard** (Postfix + Dovecot + SpamAssassin + ClamAV)
- **Advanced** (Postfix + Dovecot + Rspamd + ClamAV + DKIM)
- **Custom** (User selects individual components)

For each missing component, offer:
- Install automatically (if admin/root)
- Show installation commands (if no install permission)
- Skip component

**Step 3: Service User Setup**
- Create vmail user (UID 5000, GID 5000) for virtual mailboxes
- Set mailbox base directory (default: /var/vmail)
- Set permissions

**Step 4: Domain Configuration**
- Add first domain
- Set admin email address
- Generate DKIM keys
- Show DNS records to configure (MX, SPF, DKIM, DMARC)

**Step 5: Authentication Method**
Choose user storage:
- **System Users** (Unix /etc/passwd)
- **Virtual Users** (Database: SQLite, MySQL, PostgreSQL)
- **Both** (Mixed mode)

For database:
- Create database schema
- Set connection details
- Test connection

**Step 6: TLS/SSL**
- Scan for existing certificates
- Offer Let's Encrypt setup
- Manual certificate upload
- Generate self-signed (dev only)

**Step 7: First User**
- Create admin account (for this panel)
- Create first email account
- Send test email

**Step 8: Review & Apply**
- Show summary of changes
- Preview generated config files
- Apply configuration
- Start/restart services
- Verify services running

---

## API Endpoints

### Service Management
- `GET /api/v1/services` - List all mail services
- `GET /api/v1/services/{service}` - Get service status
- `POST /api/v1/services/{service}/start` - Start service
- `POST /api/v1/services/{service}/stop` - Stop service
- `POST /api/v1/services/{service}/restart` - Restart service
- `POST /api/v1/services/{service}/reload` - Reload config

### User Management
- `GET /api/v1/users` - List users (system + virtual)
- `GET /api/v1/users/{id}` - Get user details
- `POST /api/v1/users` - Create user
- `PUT /api/v1/users/{id}` - Update user
- `DELETE /api/v1/users/{id}` - Delete user
- `POST /api/v1/users/{id}/password` - Change password
- `POST /api/v1/users/import` - Bulk import from CSV

### Domain Management
- `GET /api/v1/domains` - List domains
- `GET /api/v1/domains/{domain}` - Get domain details
- `POST /api/v1/domains` - Add domain
- `PUT /api/v1/domains/{domain}` - Update domain
- `DELETE /api/v1/domains/{domain}` - Delete domain
- `GET /api/v1/domains/{domain}/dns` - Get DNS records
- `GET /api/v1/domains/{domain}/dkim` - Get DKIM keys
- `POST /api/v1/domains/{domain}/dkim/generate` - Generate DKIM keys

### Alias & Forwarding
- `GET /api/v1/aliases` - List aliases
- `POST /api/v1/aliases` - Create alias
- `DELETE /api/v1/aliases/{id}` - Delete alias
- `GET /api/v1/forwards` - List forwards
- `POST /api/v1/forwards` - Create forward
- `DELETE /api/v1/forwards/{id}` - Delete forward

### Queue Management
- `GET /api/v1/queue` - List mail queue
- `GET /api/v1/queue/{id}` - Get message details
- `POST /api/v1/queue/{id}/flush` - Force delivery
- `POST /api/v1/queue/{id}/hold` - Hold message
- `POST /api/v1/queue/{id}/release` - Release held message
- `DELETE /api/v1/queue/{id}` - Delete from queue
- `POST /api/v1/queue/flush-all` - Flush entire queue

### Configuration
- `GET /api/v1/config/{component}` - Get component config
- `PUT /api/v1/config/{component}` - Update component config
- `POST /api/v1/config/{component}/validate` - Validate config
- `GET /api/v1/config/{component}/templates` - List templates
- `POST /api/v1/config/{component}/backup` - Backup config
- `POST /api/v1/config/{component}/restore` - Restore from backup

### Monitoring
- `GET /api/v1/stats/delivery` - Delivery statistics
- `GET /api/v1/stats/spam` - Spam statistics
- `GET /api/v1/stats/queue` - Queue statistics
- `GET /api/v1/logs/{component}` - Get component logs
- `GET /api/v1/health` - Overall system health

---

## Deployment

- Single static binary
- Docker image with docker-compose
- Systemd service support
- Let's Encrypt automatic TLS
- Support for 8 platforms: linux, darwin, windows, freebsd × amd64, arm64

---

## Future Considerations

- CalDAV/CardDAV support for calendars and contacts
- Sieve filtering support
- OAuth2 authentication
- S3-compatible storage backend
- Full-text search with advanced query syntax
- Two-factor authentication

---

## Notes

This project follows the AI.md specification (PARTS 0-37) for all implementation details.

All features are free and open source (MIT license). No premium tiers, no feature gating, no usage limits beyond server protection (rate limiting, quotas for stability).

---

## Data Models

### Virtual User (Database)
```go
type VirtualUser struct {
    ID           int64
    Email        string    // Full email address
    LocalPart    string    // Username part
    Domain       string    // Domain part
    PasswordHash string    // Argon2id for panel, crypt for Dovecot
    Quota        int64     // Bytes (0 = unlimited)
    Enabled      bool
    CreatedAt    time.Time
    UpdatedAt    time.Time
}
```

### System User (Unix)
```go
type SystemUser struct {
    Username     string
    UID          int
    GID          int
    HomeDir      string
    Shell        string
    MailEnabled  bool      // Whether this user should receive mail
    Quota        int64
}
```

### Virtual Domain
```go
type VirtualDomain struct {
    ID           int64
    Domain       string
    Active       bool
    DKIMEnabled  bool
    DKIMSelector string    // e.g., "mail", "default"
    DKIMPrivateKey string  // PEM format
    DKIMPublicKey  string  // TXT record value
    CreatedAt    time.Time
    UpdatedAt    time.Time
}
```

### Alias
```go
type Alias struct {
    ID          int64
    Source      string    // Alias email address
    Destination string    // Target email address (can be multiple, comma-separated)
    Domain      string
    Active      bool
    CreatedAt   time.Time
}
```

### Mail Forward
```go
type Forward struct {
    ID          int64
    Source      string    // Source email address
    Destinations []string // List of forward destinations
    KeepLocal   bool      // Also keep copy in local mailbox
    Active      bool
    CreatedAt   time.Time
}
```

### Transport (Routing)
```go
type Transport struct {
    ID          int64
    Domain      string    // Domain or email pattern
    Transport   string    // e.g., "smtp:[relay.example.com]:587"
    NextHop     string    // Relay host
    Username    string    // SMTP auth username
    Password    string    // SMTP auth password
    Active      bool
}
```

### Queue Entry (Read-only, from Postfix)
```go
type QueueEntry struct {
    ID          string    // Postfix queue ID
    Size        int64     // Message size in bytes
    From        string    // Sender address
    Recipients  []string  // Recipient addresses
    ArrivalTime time.Time // When entered queue
    Status      string    // active, deferred, hold
    Reason      string    // Delivery status/error
}
```

### Service Status
```go
type ServiceStatus struct {
    Name        string    // postfix, dovecot, etc.
    Running     bool
    Enabled     bool      // Auto-start on boot
    PID         int
    Uptime      time.Duration
    Version     string    // Component version
}
```

### Mail Statistics
```go
type MailStats struct {
    Period      string    // hour, day, week, month
    Sent        int64     // Messages sent
    Received    int64     // Messages received
    Bounced     int64     // Bounce messages
    Rejected    int64     // Rejected by policy
    SpamBlocked int64     // Blocked as spam
    VirusBlocked int64    // Blocked by antivirus
    QueueSize   int64     // Current queue size
}
```

---

## Configuration Details

See AI.md PART 5 for full configuration specification.

### Panel Configuration (server.yml)

```yaml
server:
  port: 64580
  admin_path: admin

mail:
  # Base directory for virtual mailboxes
  mailbox_base: /var/vmail
  
  # Virtual mail user (created by setup wizard)
  vmail_user: vmail
  vmail_uid: 5000
  vmail_gid: 5000
  
  # User authentication mode
  auth_mode: virtual  # system, virtual, both
  
  # Database for virtual users (only if auth_mode = virtual or both)
  database:
    driver: postgres
    host: localhost
    port: 5432
    name: mailserver
    username: mailuser
    password: ${DB_PASSWORD}
  
  # Component paths (auto-detected, can override)
  postfix:
    config_dir: /etc/postfix
    queue_dir: /var/spool/postfix
  
  dovecot:
    config_dir: /etc/dovecot
    
  spamassassin:
    config_dir: /etc/mail/spamassassin
    
  clamav:
    config_dir: /etc/clamav
```

### Generated Postfix main.cf (examples)

**Virtual users mode:**
```
virtual_mailbox_domains = proxy:pgsql:/etc/postfix/pgsql-virtual-mailbox-domains.cf
virtual_mailbox_maps = proxy:pgsql:/etc/postfix/pgsql-virtual-mailbox-maps.cf
virtual_alias_maps = proxy:pgsql:/etc/postfix/pgsql-virtual-alias-maps.cf
virtual_transport = lmtp:unix:private/dovecot-lmtp
```

**System users mode:**
```
local_recipient_maps = unix:passwd.byname $alias_maps
mailbox_transport = lmtp:unix:private/dovecot-lmtp
```

### Generated Dovecot conf.d/auth-sql.conf.ext (virtual users)

```
passdb {
  driver = sql
  args = /etc/dovecot/dovecot-sql.conf.ext
}

userdb {
  driver = sql
  args = /etc/dovecot/dovecot-sql.conf.ext
}
```

