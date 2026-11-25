# Backend Rules (PART 9, 10, 11, 31)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never expose secrets, raw tokens, private keys, or sensitive paths. Never use unparameterized SQL or destructive schema changes.

## CRITICAL - ALWAYS DO

- Use stable error responses, idempotent schema creation/updates, bounded queries, secure token storage, audit logging, and the AI.md middleware order.

## Key rules summary

- Treat mail daemons, DNS results, network peers, and user-supplied configuration as untrusted inputs.
- Check cross-references in AI.md before implementing a dependent feature.
- Update README.md and docs/ whenever shipped behavior changes.

## KEY DECISIONS

| Question | Answer | Spec reference |
|---|---|---|
| How is the schema changed? | Idempotent startup schema creation/updates; no destructive schema migrations. | PART 10 |
| How are tokens stored? | Store hashes; reveal raw secrets only in the specified one-time flows. | PART 11 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| Server database | Runtime configuration, sessions, audit, and scheduler state |
| Users database | Owner, user, and mail product data |

## QUICK REFERENCE

- Parameterize SQL, bound every query, and use transaction helpers with rollback.
- Sanitize logs and public errors; never disclose credentials, tokens, private keys, or sensitive paths.
- Keep authorization, rate limits, audit events, and middleware ordering aligned with AI.md.

For complete details, see AI.md PART 9, 10, 11, 31.
