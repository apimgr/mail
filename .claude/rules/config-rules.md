# Configuration Rules (PART 5, 6, 12)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never ignore unknown keys, store secrets in ordinary configuration, or let development mode enable debug endpoints.

## CRITICAL - ALWAYS DO

- Apply CLI > environment > file > defaults. Validate configuration, parse boolean forms consistently, and keep server.yml/secrets.yml behavior aligned with AI.md.

## Key rules summary

- Configuration is file-based; API projects have no runtime administration UI.
- Check cross-references in AI.md before implementing a dependent feature.
- Update README.md and docs/ whenever shipped behavior changes.

## KEY DECISIONS

| Question | Answer | Spec reference |
|---|---|---|
| How are settings resolved? | CLI overrides environment, which overrides files and defaults. | PART 5 |
| What happens to unknown settings? | Reject them with a clear configuration error. | PART 5 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| `server.yml` | Primary operator configuration |
| `secrets.yml` | Restricted secret values, separate from ordinary settings |

## QUICK REFERENCE

- Keep defaults, validation, environment parsing, and hot reload consistent.
- Development mode must not implicitly enable debug endpoints.
- Never log secret values or write generated configuration into source paths.

For complete details, see AI.md PART 5, 6, 12.
