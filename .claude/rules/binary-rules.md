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

## KEY DECISIONS

| Question | Answer | Spec reference |
|---|---|---|
| How is Go built? | In `casjaysdev/go:latest` through the Makefile, with CGO disabled. | PART 7, 25 |
| Which release platforms are required? | Linux, macOS, Windows, and FreeBSD on amd64 and arm64. | PART 7 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| Server binary | Main `mail` service executable |
| Client binary | Companion `mail-cli` executable |

## QUICK REFERENCE

- Keep server and client versions/build metadata aligned.
- Never execute a built binary directly on the local machine; use Docker or the approved VM flow.
- Implement CLI operations against the documented server API and response contract.

For complete details, see AI.md PART 7, 8, 32.
