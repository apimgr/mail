# Binary Rules (PART 7, 8, 32)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never use CGO, external runtime dependencies beyond the documented overlay daemons, or direct host Go builds.

## CRITICAL - ALWAYS DO

- Build static binaries for all eight OS/architecture combinations. Implement the complete server CLI and required companion client behavior from AI.md.

## Key rules summary

- The main binary is `mail`; use Docker-backed Makefile targets.
- Check cross-references in AI.md before implementing a dependent feature.
- Update README.md and docs/ whenever shipped behavior changes.

For complete details, see AI.md PART 7, 8, 32.
