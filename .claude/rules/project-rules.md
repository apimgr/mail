# Project Rules (PART 2, 3, 4)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never put runtime data, generated configuration, secrets, or build outputs in the source tree.

## CRITICAL - ALWAYS DO

- Keep the required Go layout, embedded assets, platform path behavior, and project files aligned with AI.md. Preserve the MIT license and dependency license notices.

## Key rules summary

- Resolve project paths from IDEA.md variables. The repository uses apimgr/mail as its canonical identity.
- Check cross-references in AI.md before implementing a dependent feature.
- Update README.md and docs/ whenever shipped behavior changes.

For complete details, see AI.md PART 2, 3, 4.
