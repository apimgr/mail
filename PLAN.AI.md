# mail - Implementation Plan

**Status**: Planning Phase  
**Date**: 2026-02-05

---

## Project Scope

**Type**: Web management panel for mail server stack (Postfix, Dovecot, SpamAssassin, etc.)

**NOT**: A mail server itself - manages existing components

---

## Decisions Made ✅

### DNS Management
- ✅ Support bind9 (install + manage)
- ✅ Support PowerDNS (install + manage) 
- ✅ Support Cloudflare API
- ✅ Support Namecheap API
- ✅ Support other providers (Route53, DO, Gandi, etc.)
- ✅ Manual option (show formatted records)
- ✅ DNS validation (verify records are correct)
- ✅ Export formats: bind9, PowerDNS SQL, generic, provider-ready

### Package Management
- ✅ Auto-detect OS (Alpine, Debian, Ubuntu, RHEL, CentOS, Rocky, Alma, FreeBSD)
- ✅ Offer to install missing components OR show install commands
- ✅ Support configuration profiles (Minimal, Standard, Advanced)

### DKIM Management
- ✅ Generate new keys (panel creates)
- ✅ Import existing keys (user uploads)
- ✅ Support OpenDKIM format
- ✅ Support Rspamd format
- ✅ Support raw PEM format
- ✅ Import KeyTable, SigningTable, InternalHosts, TrustedHosts
- ✅ Support both OpenDKIM daemon and Rspamd built-in

### Monitoring Level
- ✅ Advanced monitoring target
  - Service status
  - Queue stats
  - Per-domain statistics
  - Spam/virus detection rates
  - Disk usage per mailbox
  - Real-time log viewer
  - Time-series metrics (Prometheus/Grafana)
  - Deliverability scoring
  - Reputation monitoring (RBL checks)
  - Performance metrics
  - Alerting (email/webhook)

### Database (per AI.md spec)
- ✅ SQLite default (server.db + users.db)
- ✅ Support PostgreSQL for clusters
- ✅ Support MySQL/MariaDB
- ✅ Support MSSQL
- ✅ Support MongoDB
- ✅ Support LibSQL/Turso
- ✅ Valkey/Redis for caching/clustering

---

## Outstanding Decisions ⏳

### Implementation Order
- ✅ DECIDED: Setup wizard (end-to-end from scratch)

**All key decisions made! Ready to create detailed technical plan.**

### User Experience
- ✅ Per AI.md spec:
  - Server: Web-based setup at `/{admin_path}/server/setup`
  - Requires setup token (shown in console on first run)
  - Creates Primary Admin
  - CLI: TUI wizard for connecting to server (not for setup)

### Configuration Approach
- ✅ Two modes (chosen during server setup):
  
  **1. Fully Managed Mode** (recommended for new setups)
  - Panel owns all config files completely
  - Generates from templates
  - Full control, no manual edits needed
  - Simpler, less chance of conflicts
  - Best for: new servers, users who want "hands-off" management
  
  **2. Hybrid Mode** (for advanced users)
  - Panel manages specific sections (marked with comments)
  - Preserves custom settings outside managed sections
  - Allows manual tweaking
  - More flexible but requires understanding
  - Best for: migrating existing setups, power users
  
  **Switching modes:**
  - Can switch from Hybrid → Fully Managed (regenerates all configs)
  - Can switch from Fully Managed → Hybrid (preserves current, adds markers)
  - Warns about implications before switching

### Privilege Model
- ✅ Per AI.md spec:
  - Always require root/sudo/doas (for managing system files)
  - Follow AI.md PART 1 escalation rules
  - Service runs as root, drops to mail user after binding ports
  - Panel needs root to:
    - Install packages
    - Manage services (systemctl, etc.)
    - Edit /etc config files
    - Create system users (vmail, etc.)
    - Manage file permissions
  - See PART 1: Privilege Escalation & Service
  - See PART 1: File & Directory permissions

---

## Next Steps

1. Decide implementation priority
2. Decide configuration approach
3. Decide privilege model
4. Create detailed technical plan
5. Start implementation

---

## Notes

- AI.md defines HOW (read-only, don't modify)
- IDEA.md defines WHAT (update as we plan)
- This file tracks decisions and progress

---

## Implementation Roadmap

### Phase 1: Foundation (Week 1-2)
**Goal**: Get basic server running with OS detection

**Tasks**:
- [ ] Set up Go project structure (per AI.md PART 3)
- [ ] Implement config package with bool.go (PART 5)
- [ ] Implement paths package (OS-specific directories)
- [ ] OS detection (Alpine, Debian, Ubuntu, RHEL, CentOS, Rocky, Alma, FreeBSD)
- [ ] Package manager detection (apk, apt, yum/dnf, pkg)
- [ ] Service manager detection (systemd, OpenRC, FreeBSD rc.d, launchd)
- [ ] Basic HTTP server setup
- [ ] Setup token generation and validation
- [ ] Database initialization (SQLite default)

**Deliverable**: Binary starts, detects system, shows setup token

---

### Phase 2: Web Setup Wizard (Week 3-4)
**Goal**: Complete `/{admin_path}/server/setup` flow

**Tasks**:
- [ ] Setup wizard UI (HTML templates)
- [ ] Step 1: Welcome + system detection display
- [ ] Step 2: Configuration mode selection (Fully Managed vs Hybrid)
- [ ] Step 3: Component selection (profiles: Minimal/Standard/Advanced/Custom)
- [ ] Step 4: Package installation (auto-install or show commands)
- [ ] Step 5: Create Primary Admin account
- [ ] Step 6: Basic server settings (port, FQDN)
- [ ] Complete setup, invalidate token
- [ ] Redirect to admin panel

**Deliverable**: Working setup wizard, creates admin, ready for mail config

---

### Phase 3: Service Management (Week 5-6)
**Goal**: Detect, control, and monitor mail services

**Tasks**:
- [ ] Service detection (scan for installed components)
- [ ] Service control API (start/stop/restart/reload)
- [ ] Service status monitoring
- [ ] Version detection for each component
- [ ] Admin UI for service management
- [ ] Service health checks
- [ ] Process monitoring (PIDs, uptime, memory)

**Deliverable**: Can see and control Postfix, Dovecot, etc. via panel

---

### Phase 4: User & Domain Management (Week 7-8)
**Goal**: Core mail functionality - users and domains

**Tasks**:
- [ ] Virtual domain CRUD (create/read/update/delete)
- [ ] Virtual user CRUD
- [ ] System user integration (read /etc/passwd)
- [ ] Password management (Argon2id for panel, crypt for Dovecot)
- [ ] User quota management
- [ ] Bulk import/export (CSV)
- [ ] Admin UI for users/domains
- [ ] API endpoints for users/domains

**Deliverable**: Can create domains and users via panel

---

### Phase 5: Postfix Configuration (Week 9-10)
**Goal**: Generate and manage Postfix configs

**Tasks**:
- [ ] Postfix template system
- [ ] main.cf generation (virtual/system mode)
- [ ] master.cf management
- [ ] Database lookup files (pgsql/mysql/sqlite)
- [ ] Transport rules
- [ ] Access policies
- [ ] Configuration validation (postfix check)
- [ ] Apply and reload Postfix
- [ ] Fully Managed vs Hybrid mode implementation

**Deliverable**: Panel configures Postfix correctly

---

### Phase 6: Dovecot Configuration (Week 11-12)
**Goal**: Generate and manage Dovecot configs

**Tasks**:
- [ ] Dovecot template system
- [ ] dovecot.conf generation
- [ ] Authentication config (SQL/system)
- [ ] Mail location (Maildir)
- [ ] LMTP delivery setup
- [ ] Protocol configuration (IMAP/POP3)
- [ ] SSL/TLS settings
- [ ] Configuration validation
- [ ] Apply and reload Dovecot

**Deliverable**: Panel configures Dovecot correctly

---

### Phase 7: Spam & Virus Protection (Week 13-14)
**Goal**: Integrate spam filtering and antivirus

**Tasks**:
- [ ] SpamAssassin configuration
- [ ] Rspamd configuration (alternative)
- [ ] Amavisd-new integration
- [ ] ClamAV configuration
- [ ] Pyzor/Razor setup
- [ ] Spam score thresholds
- [ ] Whitelist/blacklist management
- [ ] Admin UI for spam settings

**Deliverable**: Spam and virus filtering configured

---

### Phase 8: DKIM, SPF, DMARC (Week 15-16)
**Goal**: Email authentication

**Tasks**:
- [ ] DKIM key generation
- [ ] DKIM key import (existing keys)
- [ ] OpenDKIM configuration
- [ ] Rspamd DKIM (if using Rspamd)
- [ ] SPF record generation
- [ ] DMARC record generation
- [ ] DNS record display (all formats)
- [ ] Admin UI for DKIM management

**Deliverable**: Email authentication configured

---

### Phase 9: DNS Integration (Week 17-18)
**Goal**: Automated DNS management

**Tasks**:
- [ ] bind9 integration (install + manage)
- [ ] PowerDNS integration (install + manage)
- [ ] Cloudflare API client
- [ ] Namecheap API client
- [ ] Route53 API client
- [ ] DigitalOcean API client
- [ ] Generic DNS provider interface
- [ ] DNS validation (query actual DNS)
- [ ] Admin UI for DNS settings

**Deliverable**: Can auto-configure DNS records

---

### Phase 10: Queue Management (Week 19)
**Goal**: View and manage mail queue

**Tasks**:
- [ ] Postfix queue parser
- [ ] Queue list API
- [ ] Queue actions (flush, hold, delete)
- [ ] Queue statistics
- [ ] Admin UI for queue management
- [ ] Real-time queue monitoring

**Deliverable**: Full queue management via panel

---

### Phase 11: Monitoring & Logs (Week 20-21)
**Goal**: Advanced monitoring

**Tasks**:
- [ ] Log file parser (Postfix, Dovecot)
- [ ] Log viewer UI (search, filter)
- [ ] Delivery statistics (sent, received, bounced)
- [ ] Spam statistics (detection rates)
- [ ] Per-domain stats
- [ ] Disk usage monitoring (per mailbox)
- [ ] Performance metrics
- [ ] Prometheus metrics exporter
- [ ] Grafana dashboard templates
- [ ] Alert system (email/webhook)

**Deliverable**: Comprehensive monitoring

---

### Phase 12: Alias & Forwarding (Week 22)
**Goal**: Email routing features

**Tasks**:
- [ ] Alias management (CRUD)
- [ ] Forwarding rules
- [ ] Catchall addresses
- [ ] Vacation/auto-reply (Sieve)
- [ ] Admin UI for aliases/forwards

**Deliverable**: Full routing control

---

### Phase 13: TLS/SSL & Let's Encrypt (Week 23)
**Goal**: Automatic TLS

**Tasks**:
- [ ] Certificate detection
- [ ] Let's Encrypt integration
- [ ] Certificate renewal automation
- [ ] Manual certificate upload
- [ ] TLS configuration for Postfix/Dovecot
- [ ] Admin UI for SSL settings

**Deliverable**: Automatic HTTPS/TLS

---

### Phase 14: Backup & Restore (Week 24)
**Goal**: Full system backup

**Tasks**:
- [ ] Backup all configs
- [ ] Backup database
- [ ] Backup DKIM keys
- [ ] Backup mailboxes (optional)
- [ ] Restore functionality
- [ ] Scheduled backups
- [ ] Admin UI for backup management

**Deliverable**: Reliable backup/restore

---

### Phase 15: CLI Client (Week 25)
**Goal**: Remote management CLI

**Tasks**:
- [ ] TUI wizard for connection setup
- [ ] API client implementation
- [ ] User management commands
- [ ] Domain management commands
- [ ] Queue management commands
- [ ] Service control commands
- [ ] Interactive TUI mode

**Deliverable**: Full-featured CLI client

---

### Phase 16: Polish & Testing (Week 26-28)
**Goal**: Production ready

**Tasks**:
- [ ] Comprehensive testing
- [ ] Documentation (user guide)
- [ ] Docker image
- [ ] CI/CD workflows
- [ ] Security audit
- [ ] Performance optimization
- [ ] Bug fixes
- [ ] Release preparation

**Deliverable**: v1.0.0 release

---

## Quick Wins (MVP for testing)

**Minimum viable setup** (Phases 1-6):
- System detection ✓
- Setup wizard ✓
- Service control ✓
- Users/domains ✓
- Postfix config ✓
- Dovecot config ✓

Result: Basic working mail server managed by panel

**Add value** (Phases 7-8):
- Spam filtering ✓
- Email authentication (DKIM/SPF/DMARC) ✓

Result: Production-grade mail server

**Full platform** (Phases 9-16):
- DNS automation
- Monitoring
- Queue management
- Backup/restore
- CLI client

Result: Complete mail management solution


---

## Additional Planning Decisions ✅

### Architecture & Deployment
- ✅ Cluster support: Multiple panel instances with config sync
- ✅ Multi-server mode: Panel can manage multiple mail servers
- ✅ Agent-based: mail-agent installed on each managed mail server
- ✅ Single+Multi modes: Choose during setup

### User Types & Access
- ✅ Server Admins: Manage panel and mail infrastructure
- ✅ Regular Users (PART 34): Email account owners
- ✅ Organizations (PART 35): Multi-user organizations with shared mailboxes
- ✅ Custom Domains (PART 36): Users/orgs can add their own domains

### Webmail Features
- ✅ Built-in webmail: Beautiful Go-based, server-side rendered
- ✅ External webmail option: Install/manage SnappyMail/Roundcube
- ✅ Core features: Read, compose, folders, search, attachments, contacts
- ✅ Advanced features: GPG, DKIM status display, Sieve filters
- ✅ Spam management: Mark spam/not spam
- ✅ HTML compose: WYSIWYG editor
- ✅ Signatures: Per-user email signatures

### Email Sending
- ✅ Direct SMTP: Webmail → Dovecot/Postfix
- ✅ Via API: Webmail → Panel API → Postfix
- ✅ Both methods configurable
- ✅ External clients: Support SMTP relay for Thunderbird/Outlook/etc

### Mailing Lists
- ✅ Distribution groups
- ✅ Member management
- ✅ Posting permissions (anyone, members, admins)
- ✅ Message archiving
- ✅ Digest mode (daily summaries)

### CalDAV / CardDAV
- ✅ CalDAV server (calendar sync)
- ✅ CardDAV server (contacts sync)
- ✅ Web UI for calendar/contacts
- ✅ Shared calendars
- ✅ Meeting invitations

### Mobile & PWA
- ✅ Responsive web (mobile browsers)
- ✅ API-first design
- ✅ PWA (Progressive Web App) per AI.md spec
- ✅ No native mobile app (PWA sufficient)

### Admin Panel Features
- ✅ Everything per AI.md spec:
  - Service management
  - Domain management
  - User management
  - Alias/forward management
  - Queue management
  - Log viewing
  - Statistics & monitoring
  - Spam/virus settings
  - DKIM configuration
  - DNS management
  - Backup/restore
  - Email templates (system emails)
  - Branding (logo, colors per domain)
  - Rate limiting (per user/domain)
  - IP whitelist/blacklist
  - Custom mail headers
  - All configurable settings

### Mail Storage Decisions
- ✅ Location: Configurable during setup (default /var/vmail)
- ✅ Format: Maildir only (modern standard)
- ✅ Quotas: Both Dovecot (database) and filesystem options
- ✅ vmail UID/GID: Configurable during setup

### Database Organization
- ✅ Per AI.md: server.db + users.db (or mail.db)
- ✅ Mail users location: Configurable (users.db or separate mail.db)

### Migration Support
- ✅ Detect existing setups
- ✅ Import existing configurations
- ✅ Wizard offers import during setup

---

## Project Scope Summary

This is now a **COMPLETE EMAIL PLATFORM**:

### Infrastructure Layer (Original Plan)
- Postfix, Dovecot, SpamAssassin, Rspamd, ClamAV management
- DKIM, SPF, DMARC configuration
- DNS automation (bind9, PowerDNS, Cloudflare, etc.)
- SSL/TLS with Let's Encrypt
- Service monitoring

### User Layer (Added)
- Multi-user support (PART 34)
- Organizations (PART 35)
- Custom domains (PART 36)
- Built-in webmail
- External webmail integration option

### Communication Layer (Added)
- Mailing lists / distribution groups
- CalDAV calendar sync
- CardDAV contacts sync
- Meeting invitations
- GPG support

### Access Methods
- Web UI (server-side rendered)
- Progressive Web App (PWA)
- API (for automation/integration)
- CLI client (mail-cli with TUI)
- External email clients (IMAP/SMTP)

---

## Complexity Assessment

**This is now a VERY ambitious project.**

Comparable to:
- Zimbra (but more modern)
- iRedMail (but with web management)
- Mail-in-a-Box (but more flexible)
- Mailu (but Go-based with more features)

**Estimated total development:** 40-50 weeks (full-time)

### Suggested Phased Approach

**Phase 1: Core Infrastructure (Weeks 1-16)**
- Setup wizard
- Service management (Postfix, Dovecot)
- Basic user/domain management
- Server admin panel only
- No webmail yet

**Phase 2: Email Features (Weeks 17-28)**
- Built-in webmail
- Multi-user support (PART 34)
- Spam/virus integration
- DKIM/SPF/DMARC

**Phase 3: Advanced Features (Weeks 29-40)**
- Organizations (PART 35)
- Custom domains (PART 36)
- Mailing lists
- CalDAV/CardDAV
- Full monitoring suite

**Phase 4: Polish & Production (Weeks 41-50)**
- External webmail integration
- PWA optimization
- Performance tuning
- Security hardening
- Documentation
- Testing
- v1.0.0 release

---

## Next Steps

1. ✅ All planning decisions made
2. [ ] Create detailed IDEA.md (update with all features)
3. [ ] Create technical architecture document
4. [ ] Define database schema (all tables)
5. [ ] Define API endpoints (complete list)
6. [ ] Create UI/UX mockups
7. [ ] Begin Phase 1 implementation

Ready to proceed?


---

## Package Architecture ✅

### Built-in Components (Pure Go)

**Core Panel Features:**
- CalDAV/CardDAV server (calendar & contacts)
- Let's Encrypt ACME client (SSL/TLS automation)
- Backup system (config, database, mailboxes)
- GPG/S/MIME support (Go crypto libraries)
- LDAP client (authentication against AD/OpenLDAP)
- Prometheus metrics exporter (/metrics endpoint)

**Webmail & Communication:**
- Primary webmail (Go-based, server-side rendered)
- Mailing list manager (distribution groups, archives)
- Mail archiving (search, retention policies)
- Push notifications (web push API for mobile)
- Progressive Web App (PWA) support

**Monitoring & Analysis:**
- Log parser & analyzer (Postfix, Dovecot, spam, virus)
- Statistics engine (pflogsumm equivalent)
- Graph generation (RRDtool equivalent)
- Real-time metrics
- Deliverability scoring

### External Packages (System Install)

**Core Mail Services (REQUIRED):**
```
postfix                    # SMTP server
dovecot-core              # IMAP/POP3 server
dovecot-imapd             # IMAP protocol
dovecot-pop3d             # POP3 protocol
dovecot-lmtpd             # LMTP delivery
dovecot-pigeonhole        # Sieve mail filtering
dovecot-managesieved      # Sieve rule management
```

**Spam & Virus Protection (REQUIRED):**
```
amavisd-new               # Content filter interface
clamav                    # Antivirus engine
clamav-daemon             # ClamAV daemon
pyzor                     # Collaborative spam detection
razor                     # Spam signature database
```

**Spam Filter Stack (CHOOSE ONE):**

Option A - Traditional:
```
spamassassin              # Content-based spam filter
opendkim                  # DKIM signing daemon
opendkim-tools/utils      # DKIM utilities
```

Option B - Modern:
```
rspamd                    # Fast spam filter with built-in DKIM
```

**Security (OPTIONAL):**
```
fail2ban                  # Brute force protection
postgrey                  # Greylisting (optional)
postfix-policyd-spf-python # SPF validation
```

**Database Drivers (AUTO-DETECT):**

Single server (default):
```
sqlite3                   # Usually pre-installed
```

Multi-server/cluster:
```
postgresql-client         # If using PostgreSQL
  OR
mysql-client             # If using MySQL
```

**DNS Servers (OPTIONAL):**

Choose one if DNS automation wanted:
```
bind9                     # Traditional DNS server
  OR
powerdns                  # API-driven DNS server
pdns-backend-*           # PowerDNS backend (mysql/pgsql/sqlite)
```

**External Webmail (OPTIONAL):**

Choose if user prefers external over built-in:
```
snappymail                # Modern PHP webmail
  OR
roundcube                 # Traditional PHP webmail
roundcube-plugins         # Roundcube plugins
```

**Utilities (OPTIONAL):**
```
fetchmail                 # Fetch from external POP3/IMAP
  OR
getmail6                  # Alternative mail retriever
openssl                   # S/MIME cert management (usually present)
```

---

## Platform-Specific Package Names

### Alpine Linux (apk)
```bash
# Core mail
postfix dovecot dovecot-pigeonhole-plugin

# Spam/virus (Option A: Traditional)
spamassassin amavisd-new clamav clamav-daemon pyzor razor opendkim opendkim-utils

# Spam/virus (Option B: Modern)  
rspamd amavisd-new clamav clamav-daemon pyzor razor

# Security (optional)
fail2ban postgrey

# DNS (optional)
bind powerdns

# External webmail (optional)
# Note: May need to use composer/manual install

# Database (multi-server)
postgresql-client mysql-client

# Utilities
fetchmail openssl
```

### Debian/Ubuntu (apt)
```bash
# Core mail
postfix postfix-mysql postfix-pgsql dovecot-core dovecot-imapd dovecot-pop3d dovecot-lmtpd dovecot-managesieved

# Spam/virus (Option A: Traditional)
spamassassin amavisd-new clamav clamav-daemon pyzor razor opendkim opendkim-tools

# Spam/virus (Option B: Modern)
rspamd amavisd-new clamav clamav-daemon pyzor razor

# Security (optional)
fail2ban postgrey postfix-policyd-spf-python

# DNS (optional)
bind9 bind9utils
# OR
pdns-server pdns-backend-mysql pdns-backend-pgsql

# External webmail (optional)
snappymail roundcube roundcube-plugins

# Database (multi-server)
postgresql-client mysql-client

# Utilities
fetchmail getmail6 openssl
```

### RHEL/CentOS/Rocky/Alma (yum/dnf)
```bash
# Core mail
postfix dovecot dovecot-mysql dovecot-pgsql dovecot-pigeonhole

# Spam/virus (Option A: Traditional)
spamassassin amavisd-new clamav clamav-server clamav-update pyzor perl-Razor-Agent opendkim

# Spam/virus (Option B: Modern)
rspamd amavisd-new clamav clamav-server clamav-update pyzor perl-Razor-Agent

# Security (optional)
fail2ban postgrey

# DNS (optional)
bind bind-utils
# OR
pdns pdns-backend-mysql pdns-backend-postgresql

# External webmail (optional)
# Note: May need EPEL or manual install

# Database (multi-server)
postgresql mysql

# Utilities
fetchmail openssl
```

### FreeBSD (pkg)
```bash
# Core mail
postfix dovecot dovecot-pigeonhole

# Spam/virus (Option A: Traditional)
spamassassin amavisd-new clamav pyzor razor-agents opendkim

# Spam/virus (Option B: Modern)
rspamd amavisd-new clamav pyzor razor-agents

# Security (optional)
py39-fail2ban postgrey

# DNS (optional)
bind918
# OR
powerdns

# External webmail (optional)
snappymail roundcube

# Database (multi-server)
postgresql15-client mysql80-client

# Utilities
fetchmail openssl
```

---

## Setup Wizard Package Selection

**Step: Component Selection**

User is presented with profiles:

### Profile 1: Minimal
```
✓ Postfix
✓ Dovecot (IMAP/POP3/LMTP/Sieve)
✓ Basic spam filtering (SpamAssassin)
✓ Fail2ban
```

### Profile 2: Standard (RECOMMENDED)
```
✓ Postfix
✓ Dovecot (IMAP/POP3/LMTP/Sieve)
✓ SpamAssassin + OpenDKIM
✓ ClamAV (antivirus)
✓ Amavisd-new (content filter)
✓ Pyzor + Razor (collaborative spam detection)
✓ Fail2ban
✓ SPF validation
```

### Profile 3: Advanced
```
✓ Postfix
✓ Dovecot (IMAP/POP3/LMTP/Sieve)
✓ Rspamd (modern spam filter with DKIM)
✓ ClamAV (antivirus)
✓ Amavisd-new (content filter)
✓ Pyzor + Razor
✓ Fail2ban
✓ Postgrey (greylisting)
✓ SPF validation
✓ DNS server (bind9 OR powerdns - user chooses)
✓ External webmail option (SnappyMail OR Roundcube)
```

### Profile 4: Custom
User manually selects each component with checkboxes.

---

## Package Detection Logic

**On startup, panel detects:**

1. Operating system (Alpine, Debian, Ubuntu, RHEL, CentOS, Rocky, Alma, FreeBSD)
2. Package manager (apk, apt, yum, dnf, pkg)
3. Service manager (systemd, OpenRC, rc.d, launchd)
4. Installed packages (scan for postfix, dovecot, etc.)
5. Running services (check if services are active)
6. Configuration files (detect existing configs)

**Setup wizard shows:**
- ✓ Installed and running
- ⚠ Installed but not running
- ✗ Not installed
- → Offers to install missing components
- → Offers to import existing configurations


---

## Mail Server Roles & Multi-Server Architecture ✅

### Server Role Types

Each mail server has a **server role** that determines its Postfix/Dovecot configuration:

#### 1. Primary MX (Default)
```yaml
server_role: primary_mx
description: "Main mail server - accepts incoming mail, local mailboxes"
features:
  - Accepts mail for local domains (mydestination)
  - Local mailbox delivery (Dovecot LMTP)
  - IMAP/POP3 service (Dovecot)
  - Outbound mail delivery (also acts as smarthost unless explicit smarthost set)
  - Webmail access
  - CalDAV/CardDAV
postfix_config:
  mydestination: "$myhostname, localhost.$mydomain, localhost, example.com"
  relayhost: "" # Empty unless explicit smarthost
  relay_domains: "" # Not a backup MX
dovecot: enabled
services:
  - postfix (SMTP 25, submission 587, submissions 465)
  - dovecot (IMAP 143, IMAPS 993, POP3 110, POP3S 995, LMTP 24)
  - amavisd-new (10024, 10025)
  - clamav
  - spamassassin OR rspamd
```

#### 2. Backup MX
```yaml
server_role: backup_mx
description: "Secondary MX - queues mail when primary down, relays to primary"
features:
  - Accepts mail for relay_domains only
  - No local mailboxes
  - Relays all mail to primary MX
  - Queues mail during primary outage
  - Basic spam filtering (optional, reduces load on primary)
postfix_config:
  mydestination: "$myhostname, localhost" # NOT the domain
  relay_domains: "example.com, example.org" # Domains to accept for relay
  relay_recipient_maps: "proxy:mysql:/etc/postfix/mysql-relay-recipients.cf" # Reject invalid recipients
  relayhost: "[mail.example.com]:25" # Primary MX
  transport_maps: "hash:/etc/postfix/transport" # Route to primary
dovecot: disabled
services:
  - postfix (SMTP 25 only, no submission)
  - Optional: amavisd-new, clamav (basic filtering)
notes:
  - DNS MX records: 10 mail.example.com, 20 mx2.example.com
  - Must validate recipients against primary (avoid backscatter)
  - Can cache recipient list from primary database
```

#### 3. Satellite / Relay
```yaml
server_role: satellite
description: "Internal relay - no mailboxes, forwards through smarthost"
features:
  - No incoming mail (not in DNS)
  - No local mailboxes
  - Relays all outbound mail through smarthost
  - Used for application servers, internal systems
postfix_config:
  mydestination: "$myhostname, localhost" # Local only
  relayhost: "[mail.example.com]:25" # Smarthost
  relay_domains: "" # Does not relay for others
  inet_interfaces: "loopback-only" # Only local apps can send (optional)
dovecot: disabled
services:
  - postfix (submission 587 only, or localhost:25)
  - No spam/virus filtering (smarthost handles it)
notes:
  - Often behind firewall, no public MX
  - Can require SASL auth to smarthost
  - Good for web servers, cron jobs, internal apps
```

#### 4. Outbound Gateway
```yaml
server_role: outbound_gateway
description: "Dedicated outbound mail - accepts from internal, delivers to internet"
features:
  - No incoming mail from internet
  - No local mailboxes
  - Accepts from trusted internal networks/satellites
  - Delivers directly to internet
  - Heavy spam/virus filtering on outbound
  - DKIM signing
  - Rate limiting per sender
postfix_config:
  mydestination: "$myhostname, localhost"
  mynetworks: "10.0.0.0/8, 192.168.0.0/16" # Trusted internal networks
  relayhost: "" # Delivers to internet
  smtpd_recipient_restrictions: "permit_mynetworks, reject" # Only internal
dovecot: disabled
services:
  - postfix (SMTP 25, submission 587 with SASL)
  - amavisd-new (outbound filtering)
  - clamav
  - opendkim (signing)
notes:
  - Requires proper SPF/DKIM/DMARC setup
  - Monitor for spam from compromised internal systems
  - Good for large organizations (separate in/out)
```

#### 5. Smarthost (Explicit)
```yaml
server_role: smarthost
description: "Receives from satellites/relays, sends to internet"
features:
  - Accepts from authenticated satellites
  - No local mailboxes (unless also primary_mx)
  - Full spam/virus filtering
  - DKIM signing
  - Reputation management
postfix_config:
  mydestination: "$myhostname, localhost"
  mynetworks: "" # Require SASL auth
  smtpd_sasl_auth_enable: "yes"
  relayhost: "" # Delivers to internet
dovecot: disabled (unless also primary_mx)
services:
  - postfix (submission 587 with SASL required)
  - amavisd-new, clamav, spamassassin/rspamd
  - opendkim
notes:
  - Primary MX can also be smarthost (default behavior)
  - Dedicated smarthost = better if high volume satellites
```

---

## Multi-Server & Cluster Architecture

### Terminology Clarification

**Panel Nodes (Cluster Nodes):**
- Run the mail management panel (this Go application)
- Share database (PostgreSQL/MySQL) for config sync
- All panel nodes = identical config
- Provide HA for the management interface
- Users access any panel node (load balanced)

**Mail Servers (Managed Nodes):**
- Run actual mail services (Postfix, Dovecot, etc.)
- Each has mail-agent installed (communicates with panel)
- Each can have **different server role**
- Managed by panel via API (not SSH)
- Can be local or remote

### Example Topology

```
┌─────────────────────────────────────────────────────┐
│ PANEL CLUSTER (3 nodes)                             │
│ ┌─────────┐  ┌─────────┐  ┌─────────┐             │
│ │ Panel A │  │ Panel B │  │ Panel C │  (Identical)│
│ └────┬────┘  └────┬────┘  └────┬────┘             │
│      └────────────┴────────────┘                    │
│                   │                                  │
│          ┌────────▼────────┐                        │
│          │ PostgreSQL DB    │ (Shared config)       │
│          └─────────────────┘                        │
└─────────────────────────────────────────────────────┘
                      │
        ┌─────────────┼─────────────┬─────────────┐
        │             │             │             │
┌───────▼──────┐ ┌────▼─────┐ ┌────▼──────┐ ┌────▼──────┐
│ Mail Server A│ │Mail Srv B│ │Mail Srv C │ │Mail Srv D │
│ PRIMARY MX   │ │BACKUP MX │ │SATELLITE  │ │PRIMARY MX │
│              │ │          │ │           │ │           │
│ mail.ex.com  │ │mx2.ex.com│ │relay.int  │ │mail2.ex   │
│ (25,587,993) │ │(25 only) │ │(587 only) │ │(25,587,..)│
│              │ │          │ │           │ │           │
│ Domains:     │ │Relays to │ │Relays to  │ │Domains:   │
│ - example.com│ │Server A  │ │Server A   │ │- other.com│
│ - test.com   │ │          │ │           │ │           │
└──────────────┘ └──────────┘ └───────────┘ └───────────┘
```

**DNS Setup:**
```
example.com.    MX 10 mail.example.com.
example.com.    MX 20 mx2.example.com.
other.com.      MX 10 mail2.example.com.
```

**Server Roles:**
- **Server A**: Primary MX for example.com, test.com - full mailboxes
- **Server B**: Backup MX for example.com, test.com - relay to Server A
- **Server C**: Satellite - internal apps relay through Server A
- **Server D**: Primary MX for other.com - separate customer/domain

---

## Server Configuration in Panel

### Database Schema Addition

**srv_mail_servers table:**
```sql
CREATE TABLE IF NOT EXISTS srv_mail_servers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    hostname VARCHAR(255) NOT NULL UNIQUE, -- mail.example.com
    ip_address VARCHAR(45), -- IPv4 or IPv6
    server_role VARCHAR(50) NOT NULL, -- primary_mx, backup_mx, satellite, outbound_gateway, smarthost
    agent_token VARCHAR(255) NOT NULL, -- Auth token for mail-agent API
    agent_port INTEGER DEFAULT 64100,
    
    -- Role-specific config
    primary_for_domains TEXT, -- JSON array: ["example.com", "test.com"]
    relay_domains TEXT, -- JSON array for backup_mx: ["example.com"]
    relay_to_host VARCHAR(255), -- For backup_mx, satellite: "mail.example.com"
    smarthost VARCHAR(255), -- Explicit smarthost (overrides primary_mx default)
    
    -- Service status (updated by agent)
    postfix_running BOOLEAN DEFAULT FALSE,
    dovecot_running BOOLEAN DEFAULT FALSE,
    amavisd_running BOOLEAN DEFAULT FALSE,
    clamav_running BOOLEAN DEFAULT FALSE,
    spam_filter_running BOOLEAN DEFAULT FALSE, -- spamassassin or rspamd
    
    -- Health monitoring
    last_seen TIMESTAMP, -- Last agent check-in
    status VARCHAR(50) DEFAULT 'unknown', -- online, offline, degraded, unreachable
    load_average VARCHAR(50), -- "1.5 2.0 1.8"
    disk_usage_percent INTEGER,
    queue_size INTEGER, -- Postfix queue count
    
    -- Metadata
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    enabled BOOLEAN DEFAULT TRUE
);
```

### Setup Wizard Addition

**Step: Mail Server Configuration**

After package selection, ask:

```
┌──────────────────────────────────────────────────┐
│ Mail Server Role                                 │
├──────────────────────────────────────────────────┤
│                                                  │
│ What role will THIS server have?                │
│                                                  │
│ ● Primary MX (Recommended)                      │
│   Main mail server with local mailboxes         │
│                                                  │
│ ○ Backup MX                                     │
│   Secondary MX, relays to primary when available│
│                                                  │
│ ○ Satellite / Relay                             │
│   Internal relay, no mailboxes                   │
│                                                  │
│ ○ Outbound Gateway                              │
│   Dedicated outbound mail only                   │
│                                                  │
└──────────────────────────────────────────────────┘
```

**If Backup MX selected:**
```
┌──────────────────────────────────────────────────┐
│ Backup MX Configuration                          │
├──────────────────────────────────────────────────┤
│                                                  │
│ Primary mail server hostname:                    │
│ [mail.example.com                            ]   │
│                                                  │
│ Relay domains (comma-separated):                 │
│ [example.com, example.org                    ]   │
│                                                  │
│ Validate recipients against primary? [Yes/No]    │
│ ● Yes (Recommended - prevents backscatter)       │
│ ○ No (Accept all, may cause backscatter)         │
│                                                  │
└──────────────────────────────────────────────────┘
```

**If Satellite selected:**
```
┌──────────────────────────────────────────────────┐
│ Satellite Configuration                          │
├──────────────────────────────────────────────────┤
│                                                  │
│ Smarthost (relay through):                       │
│ [mail.example.com                            ]   │
│                                                  │
│ Smarthost port:                                  │
│ ● 25 (Standard SMTP)                             │
│ ○ 587 (Submission with SASL)                     │
│                                                  │
│ SASL authentication required? [Yes/No]           │
│ ○ Yes                                            │
│ ● No (Trusted by smarthost mynetworks)           │
│                                                  │
└──────────────────────────────────────────────────┘
```

### Panel UI: Server Management

**Manage > Mail Servers**

```
┌──────────────────────────────────────────────────────────────────┐
│ Mail Servers                                    [+ Add Server]    │
├──────────────────────────────────────────────────────────────────┤
│                                                                   │
│ ● mail.example.com          PRIMARY MX      Online   Load: 0.5  │
│   192.0.2.1:64100           example.com     Queue: 0            │
│                             test.com        Last seen: 2s ago    │
│   [Configure] [Logs] [Stats] [Shell]                            │
│                                                                   │
│ ● mx2.example.com           BACKUP MX       Online   Load: 0.2  │
│   192.0.2.2:64100           → mail.example  Queue: 0            │
│                             example.com     Last seen: 5s ago    │
│   [Configure] [Logs] [Stats] [Shell]                            │
│                                                                   │
│ ● relay.internal.local      SATELLITE       Online   Load: 0.1  │
│   10.0.0.50:64100           → mail.example  Queue: 0            │
│                                             Last seen: 3s ago    │
│   [Configure] [Logs] [Stats] [Shell]                            │
│                                                                   │
│ ⚠ mail2.example.com         PRIMARY MX      Degraded Load: 8.5  │
│   192.0.2.3:64100           other.com       Queue: 342 ⚠        │
│                                             Last seen: 45s ago   │
│   [Configure] [Logs] [Stats] [Shell]                            │
│                                                                   │
│ ✗ old.example.com           PRIMARY MX      Offline              │
│   192.0.2.99:64100                          Last seen: 2h ago    │
│   [Configure] [Remove]                                           │
│                                                                   │
└──────────────────────────────────────────────────────────────────┘
```

**Click [Configure] on a server:**

```
┌──────────────────────────────────────────────────┐
│ Configure: mail.example.com                      │
├──────────────────────────────────────────────────┤
│                                                  │
│ Server Role: [Primary MX          ▼]            │
│                                                  │
│ Primary for domains:                             │
│ ☑ example.com                                    │
│ ☑ test.com                                       │
│ ☐ other.com                                      │
│                                                  │
│ Smarthost (leave empty to deliver direct):       │
│ [                                            ]   │
│                                                  │
│ Services:                                        │
│ ☑ Postfix       ● Running                        │
│ ☑ Dovecot       ● Running                        │
│ ☑ Amavisd-new   ● Running                        │
│ ☑ ClamAV        ● Running                        │
│ ☑ Rspamd        ● Running                        │
│                                                  │
│           [Save Changes]  [Restart Services]     │
│                                                  │
└──────────────────────────────────────────────────┘
```

---

## Configuration Generation Per Role

Panel generates role-specific configs:

### Primary MX (main.cf excerpt)
```
myhostname = mail.example.com
mydomain = example.com
myorigin = $mydomain
mydestination = $myhostname, localhost.$mydomain, localhost, example.com, test.com
relayhost = 
relay_domains = 
virtual_mailbox_domains = proxy:mysql:/etc/postfix/mysql-virtual-domains.cf
virtual_mailbox_maps = proxy:mysql:/etc/postfix/mysql-virtual-mailboxes.cf
virtual_alias_maps = proxy:mysql:/etc/postfix/mysql-virtual-aliases.cf
```

### Backup MX (main.cf excerpt)
```
myhostname = mx2.example.com
mydomain = example.com
myorigin = $mydomain
mydestination = $myhostname, localhost.$mydomain, localhost
relayhost = [mail.example.com]:25
relay_domains = example.com, test.com
relay_recipient_maps = proxy:mysql:/etc/postfix/mysql-relay-recipients.cf
# Reject invalid recipients (prevent backscatter)
smtpd_recipient_restrictions = 
    permit_mynetworks,
    reject_unauth_destination,
    check_recipient_access proxy:mysql:/etc/postfix/mysql-relay-recipients.cf,
    reject
```

### Satellite (main.cf excerpt)
```
myhostname = relay.internal.local
mydomain = internal.local
myorigin = $mydomain
mydestination = $myhostname, localhost.$mydomain, localhost
relayhost = [mail.example.com]:25
relay_domains = 
inet_interfaces = loopback-only
# Or require SASL if going through submission port
```

---

## Agent Communication

**mail-agent** running on each mail server reports to panel:

```json
POST https://panel.example.com:64500/api/v1/agent/heartbeat
Authorization: Bearer <agent_token>

{
  "hostname": "mail.example.com",
  "timestamp": "2026-02-05T05:17:34Z",
  "services": {
    "postfix": {"running": true, "pid": 1234, "uptime_seconds": 86400},
    "dovecot": {"running": true, "pid": 1235, "uptime_seconds": 86400},
    "amavisd": {"running": true, "pid": 1236, "uptime_seconds": 86400},
    "clamav": {"running": true, "pid": 1237, "uptime_seconds": 86400},
    "rspamd": {"running": true, "pid": 1238, "uptime_seconds": 86400}
  },
  "metrics": {
    "load_average": [0.5, 0.6, 0.4],
    "disk_usage_percent": 45,
    "memory_usage_percent": 62,
    "postfix_queue_size": 0,
    "postfix_queue_active": 0,
    "postfix_queue_deferred": 0
  }
}
```

Panel stores in `srv_mail_servers` table and displays in UI.


---

## Database Schema Design ✅

### Database Organization (per AI.md PART 10)

**Single Server Mode:**
```
/var/lib/mail/
├── server.db    # Server config, sessions, audit, cluster
└── users.db     # Admins, users, mail data, calendars, contacts
```

**Cluster Mode (PostgreSQL/MySQL):**
```
Shared Database with table prefixes:
├── config, admin_sessions, rate_limits, audit_log (no prefix)
├── srv_cluster_nodes, srv_mail_servers (server prefix)
└── admins, users, mail_*, cal_*, card_*, list_* (users db)
```

### Table Categories

| Category | Tables | Count |
|----------|--------|-------|
| **Standard Server** (AI.md) | config, admin_sessions, rate_limits, audit_log, scheduler_*, backups | 8 |
| **Standard Users** (AI.md) | admins, users, orgs, org_members, *_preferences, tokens, totp_secrets, passkeys | 12 |
| **Mail Infrastructure** | srv_mail_servers, mail_domains, mail_mailboxes, mail_aliases, mail_forwards | 5 |
| **Mail Security** | mail_dkim_keys, mail_spf_records, mail_dmarc_policies, mail_pgp_keys | 4 |
| **Mail Operations** | mail_queue_log, mail_delivery_log, mail_stats_hourly, mail_stats_daily | 4 |
| **CalDAV** | cal_calendars, cal_events, cal_attendees, cal_shares | 4 |
| **CardDAV** | card_addressbooks, card_contacts, card_shares | 3 |
| **Mailing Lists** | list_lists, list_members, list_archives | 3 |
| **Sieve Filters** | mail_sieve_scripts | 1 |
| **Archiving** | mail_archive_policies, mail_archived_messages | 2 |
| **TOTAL** | | **46 tables** |

### Key Schema Decisions

**1. Owner Pattern (Multi-tenant)**
```sql
owner_type TEXT NOT NULL,  -- admin, user, org
owner_id   INTEGER NOT NULL
```
Used in: domains, mailboxes, calendars, addressbooks, mailing lists
- Allows admins to manage infrastructure
- Users/orgs own their own mail/calendar data
- Single schema supports all ownership models

**2. Server Roles in Database**
```sql
srv_mail_servers.server_role:
  - primary_mx (main server with mailboxes)
  - backup_mx (relay to primary)
  - satellite (internal relay only)
  - outbound_gateway (outbound only)
  - smarthost (accepts from satellites)
```

**3. Multi-Token Support (per AI.md PART 11)**
```sql
tokens table:
  - owner_type: admin, user, org
  - name: user-provided label ("default", "ci-cd", "monitoring")
  - scope: global, read-write, read
  - expires_at: NULL (never) or timestamp
```
Each owner can have multiple named tokens with different scopes/expiration.

**4. CalDAV/CardDAV Integration**
- Uses standard vCard/iCalendar formats
- Stored in ical_data/vcard_data TEXT columns
- Indexed fields (summary, full_name, email) for search
- ETags for sync conflict detection
- Sharing via *_shares tables

**5. Mailing List Architecture**
- Built-in (not Mailman)
- list_type: discussion, announce, newsletter
- Moderation support
- Public archives
- Delivery modes: enabled, digest, disabled

**6. Mail Statistics**
- Hourly and daily aggregates
- Per mail server
- Metrics: sent, received, bounced, deferred, spam, virus, size
- Supports trending/graphs in panel

**7. Sieve Filter Storage**
- One active script per mailbox
- Multiple scripts with priority
- Stored as TEXT (Sieve language)
- Synced to dovecot-managesieved

### Critical Indexes

**Performance-critical indexes:**
```sql
-- Mail lookups
idx_mail_mailboxes_domain (domain_id)
idx_mail_aliases_source (source)

-- Stats queries
idx_stats_hourly_server (mail_server_id)
idx_stats_hourly_time (hour_timestamp)

-- Delivery logs
idx_delivery_timestamp (timestamp)
idx_delivery_recipient (recipient)

-- Calendar queries
idx_events_calendar (calendar_id)
idx_events_start (start_time)

-- Session management
idx_admin_sessions_expires (expires_at)
idx_user_sessions_expires (expires_at)

-- Audit queries
idx_audit_timestamp (timestamp)
idx_audit_category (category)
```

### Schema Self-Creation (per AI.md PART 10)

**All tables created on first startup:**
```go
func EnsureSchema(db *sql.DB) error {
    // 1. Create all tables (idempotent)
    for _, stmt := range createStatements {
        db.Exec(stmt) // IF NOT EXISTS
    }
    
    // 2. Apply schema updates (idempotent)
    for _, stmt := range schemaUpdates {
        db.Exec(stmt) // ADD COLUMN IF NOT EXISTS
    }
    
    return nil
}
```

**Rules:**
- All CREATE TABLE use `IF NOT EXISTS`
- Schema changes are additive only (no DROP)
- New columns must have DEFAULT or be nullable
- Ignore "column exists" errors
- No migration tracking table needed

### Cross-Database References

**server.db references users.db (logical FK, not enforced):**
```sql
-- admin_sessions.admin_id → users.db admins.id
-- Commented as: -- Logical FK to users.db admins.id
```

Application code maintains referential integrity across databases.

### Database Driver Support

| Feature | SQLite | PostgreSQL | MySQL |
|---------|--------|------------|-------|
| CREATE TABLE IF NOT EXISTS | ✓ | ✓ | ✓ |
| Transactions | ✓ | ✓ | ✓ |
| JSON columns | TEXT + JSON funcs | Native JSON/JSONB | Native JSON |
| Connection pooling | N/A (file) | Required | Required |
| Cluster support | ✗ Single server | ✓ | ✓ |

**SQLite:** Single server only, file-based, no connection pool needed
**PostgreSQL/MySQL:** Cluster mode, require shared database, connection pooling mandatory

