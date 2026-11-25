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

## KEY DECISIONS

| Question | Answer | Spec reference |
|---|---|---|
| Who runs scheduled work? | The built-in scheduler; external cron and platform schedulers are prohibited. | PART 18 |
| Are optional daemons assumed present? | No. Detect components and report unavailable integrations safely. | PART 17, 20 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| Built-in task | A scheduled operation managed and observed by the application |
| Optional component | A separately installed mail, filtering, or network service |

## QUICK REFERENCE

- Persist scheduler state, recover missed work within the configured window, and isolate task panics.
- Keep email, GeoIP, metrics, backups, and update behavior behind specified configuration and safety checks.
- Never expose mail content or secrets in metrics, logs, notifications, or backups.

For complete details, see AI.md PART 17, 18, 19, 20, 21, 22.
