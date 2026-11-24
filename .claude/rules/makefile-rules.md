# Makefile Rules (PART 25)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never run Go build/test directly on the host, write build output into source paths, or use Makefile recipes in CI.

## CRITICAL - ALWAYS DO

- Keep standard targets, Docker toolchain, persistent cache mounts, and project-scoped temporary output.

## Key rules summary

- The current Makefile has build/local/release/docker/test/dev/clean targets; lint is not defined.
- Check cross-references in AI.md before implementing a dependent feature.
- Update README.md and docs/ whenever shipped behavior changes.

For complete details, see AI.md PART 25.
