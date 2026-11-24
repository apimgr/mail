# Features Rules (PART 17, 18, 19, 20, 21, 22)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never attempt email delivery without working SMTP, depend on external cron, or expose sensitive data through metrics/backups.

## CRITICAL - ALWAYS DO

- Use the built-in scheduler and implement email, GeoIP, metrics, backups, and updates exactly as specified, with safe failure behavior.

## Key rules summary

- Mail management capabilities are scoped by IDEA.md and available installed services.
- Check cross-references in AI.md before implementing a dependent feature.
- Update README.md and docs/ whenever shipped behavior changes.

For complete details, see AI.md PART 17, 18, 19, 20, 21, 22.
