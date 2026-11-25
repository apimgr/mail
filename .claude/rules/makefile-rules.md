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

## KEY DECISIONS

| Question | Answer | Spec reference |
|---|---|---|
| Does CI invoke Makefile targets? | No; provider workflows run their declared commands directly. | PART 25, 27 |
| Where do Go commands run? | In the maintained Docker toolchain image. | PART 25 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| `make dev` | Fast local-platform development build into a scoped temporary directory |
| `make build` | Static release build for all required OS/architecture pairs |

## QUICK REFERENCE

- Keep the six core targets (`dev`, `local`, `build`, `test`, `release`, `docker`) and documented cleanup/check helpers.
- Use persistent module/build caches, resource limits, and the AI.md project naming variables.
- Do not add a Makefile lint target when CI defines lint as direct Docker toolchain commands.

For complete details, see AI.md PART 25.
