# Testing, Docs, and I18N Rules (PART 28, 29, 30)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never write test output into the repository, claim unrun checks, or document unimplemented behavior as shipped.

## CRITICAL - ALWAYS DO

- Use the container-backed test gate, maintain route coverage, publish MkDocs through ReadTheDocs, and embed complete translation resources.

## Key rules summary

- The current code has Go unit test files. This task intentionally did not run validation.
- Check cross-references in AI.md before implementing a dependent feature.
- Update README.md and docs/ whenever shipped behavior changes.

For complete details, see AI.md PART 28, 29, 30.
