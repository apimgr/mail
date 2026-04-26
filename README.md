# Mail - Email Infrastructure Management Panel

Complete email infrastructure management with built-in webmail, CalDAV/CardDAV, and multi-server support.

## Overview

Mail is a comprehensive email management panel that provides:
- **Mail Server Management**: Postfix, Dovecot, SpamAssassin, ClamAV, etc.
- **Built-in Webmail**: Server-side rendered, GPG support, Sieve filters
- **CalDAV/CardDAV**: Calendar and contacts server
- **Mailing Lists**: Built-in mailing list manager
- **Multi-server**: Manage multiple mail servers with different roles
- **Multi-tenant**: Support for users and organizations

## Features

### Mail Infrastructure
- Virtual domains and mailboxes
- Email aliases and forwards
- DKIM, SPF, and DMARC support
- GPG/PGP key management
- Sieve mail filtering
- Mail archiving and retention

### Server Roles
- **Primary MX**: Main server with local mailboxes
- **Backup MX**: Secondary relay, queues during outages
- **Satellite**: Internal relay only
- **Outbound Gateway**: Dedicated outbound mail
- **Smarthost**: Accepts from satellites

### Built-in Services
- Webmail interface
- CalDAV/CardDAV server
- Mailing list manager
- Push notifications
- Progressive Web App (PWA)
- Let's Encrypt automation

### External Integration
- Optional SnappyMail or Roundcube
- Optional DNS management (bind9/PowerDNS)
- LDAP/Active Directory support
- OIDC authentication

## Installation

### System Requirements

- Linux, FreeBSD, macOS, or Windows
- AMD64 or ARM64 architecture
- Root/sudo access required
- Supported distributions:
  - Alpine Linux
  - Debian/Ubuntu
  - RHEL/CentOS/Rocky/Alma
  - FreeBSD

### Quick Start

```bash
# Download latest release
curl -sL https://mail.apimgr.us/install.sh | sudo bash

# Or with Docker
docker run -p 64500:64500 ghcr.io/apimgr/mail:latest

# Access setup wizard
# The panel will display a setup URL with token on first run
```

### Manual Installation

```bash
# Download binary
wget https://github.com/apimgr/mail/releases/latest/download/mail-linux-amd64
chmod +x mail-linux-amd64
sudo mv mail-linux-amd64 /usr/local/bin/mail

# Run setup
sudo mail server setup
```

## Configuration

Configuration is stored in `/etc/apimgr/mail/server.yml` (or database in cluster mode).

### Single Server Mode

```yaml
server:
  port: 64500
  fqdn: mail.example.com

database:
  driver: sqlite
```

### Cluster Mode

```yaml
server:
  port: 64500
  fqdn: panel.example.com

database:
  driver: postgres
  host: db.example.com
  port: 5432
  name: mail
  username: mail
  password: ${DB_PASSWORD}
```

## Usage

### Admin Panel

Access at `https://your-server:64500/admin` (setup token shown on first run).

- Manage mail servers
- Configure domains and mailboxes
- View queue and logs
- Monitor statistics

### Webmail

Access at `https://your-server:64500/webmail`

- Read and compose email
- Manage folders and filters
- Configure GPG keys
- Calendar and contacts

### API

REST API at `https://your-server:64500/api/v1/`

- Manage domains and mailboxes
- Monitor server status
- Access mail statistics

## Architecture

### Database Organization

- **server.db**: Server config, sessions, audit logs
- **users.db**: User accounts, mail data, calendars

### Multi-Server Support

Panel manages multiple mail servers via mail-agent:

```
Panel Nodes (cluster)
  ↓ manages ↓
Mail Servers (agents)
  - Primary MX
  - Backup MX
  - Satellite
```

## Documentation

Full documentation available at https://mail.apimgr.us/docs

- [Installation Guide](https://mail.apimgr.us/docs/installation)
- [Configuration Reference](https://mail.apimgr.us/docs/configuration)
- [API Documentation](https://mail.apimgr.us/docs/api)
- [Admin Guide](https://mail.apimgr.us/docs/admin)

## Support

- **Issues**: https://github.com/apimgr/mail/issues
- **Discussions**: https://github.com/apimgr/mail/discussions
- **Documentation**: https://mail.apimgr.us/docs

## License

MIT License - see [LICENSE.md](LICENSE.md)

---

## Development

### Prerequisites

- Docker (REQUIRED - no local Go installation needed)
- GNU Make

### Build

```bash
# Development build
make dev

# Run tests
make test

# Build for all platforms
make build
```

### Project Structure

```
src/              # All Go source code
docker/           # Docker files
tests/            # Test scripts
docs/             # ReadTheDocs documentation
```

### Testing

```bash
# Run all tests
cd tests && ./run_tests.sh

# Docker testing
cd tests && ./docker.sh

# Incus testing
cd tests && ./incus.sh
```

### Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run tests: `make test`
5. Submit a pull request

### Development Guidelines

- All code must work with CGO_ENABLED=0
- Use container-only development (never local `go` commands)
- Follow AI.md specification exactly
- Server-side rendering only (no React/Vue)
- Mobile-first responsive design

## Related Projects

- [Postfix](http://www.postfix.org/) - SMTP server
- [Dovecot](https://www.dovecot.org/) - IMAP/POP3 server
- [SpamAssassin](https://spamassassin.apache.org/) - Spam filter
- [ClamAV](https://www.clamav.net/) - Antivirus

## Acknowledgments

Built with pure Go using:
- chi router
- modernc.org/sqlite (CGO-free SQLite)
- pgx (PostgreSQL driver)
- go-webauthn (passkey support)
- And many other excellent libraries

---

**Official Site**: https://mail.apimgr.us
**Repository**: https://github.com/apimgr/mail
