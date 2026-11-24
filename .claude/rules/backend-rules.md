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

For complete details, see AI.md PART 9, 10, 11, 31.
