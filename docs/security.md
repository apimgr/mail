# Security

The current scaffold adds basic HTTP security headers and rejects path
traversal patterns. The production token model, full middleware chain,
rate-limiting, audit events, redaction, and security reporting required by
AI.md are not complete. The application is not ready for production exposure.

Current health routes return only a generic status string and do not reveal
configuration values. Do not add credentials, internal paths, or mail content
to health responses or logs.

For vulnerability reports, use GitHub's private vulnerability reporting for
the [apimgr/mail repository](https://github.com/apimgr/mail/security/advisories/new).
