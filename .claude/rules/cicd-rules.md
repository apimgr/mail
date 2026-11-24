# CI/CD Rules (PART 27)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never use Makefile in CI, floating third-party action references, or skip required security checks.

## CRITICAL - ALWAYS DO

- Use provider-native workflows, SHA-pinned actions, full platform matrices, security scans, and verify the actual CI run after pushes.

## Key rules summary

- This repository uses GitHub Actions; see `.github/workflows/`.
- Check cross-references in AI.md before implementing a dependent feature.
- Update README.md and docs/ whenever shipped behavior changes.

For complete details, see AI.md PART 27.
