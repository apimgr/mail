# Testing, Docs, and I18N Rules (PART 28, 29, 30)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never write test output into the repository, claim unrun checks, or document unimplemented behavior as shipped.

## CRITICAL - ALWAYS DO

- Run Phase 1 with the wrapped `make test` gate; run Phase 2 binary tests only when explicitly requested. Keep route coverage, build MkDocs with the declared ReadTheDocs dependencies, embed locale resources, and verify translation-key parity and accessibility.

## Key rules summary

- Phase 1 Go tests run through the Docker-backed Makefile target; Go tools never run directly on the local machine. Integration scripts are separate from unit tests and must run in Docker or the approved VM flow.
- Check cross-references in AI.md before implementing a dependent feature.
- Update README.md and docs/ whenever shipped behavior changes.

## KEY DECISIONS

| Question | Answer | Spec reference |
|---|---|---|
| What is the automatic test gate? | `make test` through the required wrapper and Docker toolchain. | PART 28 |
| When are binary tests run? | Manually, after a build, when explicitly requested. | PART 28 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| Phase 1 | Unit and toolchain tests with the coverage threshold |
| Phase 2 | Docker/Incus binary and integration validation |

## QUICK REFERENCE

- Keep tests alongside Go code and test both success and failure behavior.
- Keep route coverage, cross-platform builds, and integration tests aligned with shipped features.
- MkDocs must build from `docs/requirements.txt`; translations need key parity, fallback, RTL, and accessibility checks.

For complete details, see AI.md PART 28, 29, 30.
