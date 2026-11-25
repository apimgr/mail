# Service Rules (PART 23, 24)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never broaden process privileges or perform host service actions without the authorization and safety checks required by AI.md.

## CRITICAL - ALWAYS DO

- Use the defined privilege-escalation and service lifecycle flows; keep service support and system integration bounded and auditable.

## Key rules summary

- Service control applies to detected mail components only.
- Check cross-references in AI.md before implementing a dependent feature.
- Update README.md and docs/ whenever shipped behavior changes.

## KEY DECISIONS

| Question | Answer | Spec reference |
|---|---|---|
| Which services can be controlled? | Only supported, detected mail components. | PART 24 |
| How is elevated work performed? | Through the specified narrow privilege helper and audited allowlist. | PART 23 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| System service | An operating-system service managed by the application |
| Privilege helper | The narrowly scoped mechanism for authorized privileged actions |

## QUICK REFERENCE

- Detect platform, package manager, service manager, executable, and service account before acting.
- Validate and preview configuration changes; retain backups and support rollback.
- Never invoke a shell with user-controlled input or assume an optional daemon is installed.

For complete details, see AI.md PART 23, 24.
