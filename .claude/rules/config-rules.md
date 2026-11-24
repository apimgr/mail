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

For complete details, see AI.md PART 5, 6, 12.
