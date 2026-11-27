# Security policy

## Supported versions

Mail has no published stable release yet. Security fixes are developed against
the current `main` branch; there is no older release support policy at this
time. Do not expose this development scaffold to an untrusted network.

## Report a vulnerability

Use GitHub's [private vulnerability reporting form](https://github.com/apimgr/mail/security/advisories/new)
or open the repository's Security tab and select **Report a vulnerability**.
This is the primary reporting channel. Do not file suspected vulnerabilities
as public issues or discussions. Repositories mirrored from GitHub should
direct reporters to the GitHub origin first.

For a vulnerability in a deployed Mail instance, use that instance's
`/server/security` page and its security-report link. Reports submitted there
are stored with encrypted body and contact fields, but automated PGP delivery
to the instance's security contact is not implemented yet. Do not submit
credentials or unrelated sensitive data through that form.

Include the affected version or commit, affected component, impact, and steps
to reproduce. Do not include real credentials or personal data in a report.

## Response and disclosure

Reports are triaged privately. The maintainers will acknowledge the report,
investigate its impact, coordinate a fix and any release, and agree on public
disclosure with the reporter. Do not publish exploit details while a fix is
being coordinated. Response timing depends on severity and the information
available in the report.

For general project security information, see the
[security page](/server/security).
