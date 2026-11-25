# Project Rules (PART 2, 3, 4)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never put runtime data, generated configuration, secrets, or temporary artifacts in source directories. Follow AI.md PART 25's `binaries/` and `releases/` build-output contract for the declared Makefile targets.

## CRITICAL - ALWAYS DO

- Keep the required Go layout, embedded assets, platform path behavior, and project files aligned with AI.md. Preserve the MIT license and dependency license notices.

## Key rules summary

- Resolve project identity and paths from IDEA.md variables. The repository uses `apimgr/mail` as its canonical identity.
- Go source belongs under `src/`; embedded web assets belong under `src/server/web/`; container files belong under `docker/`.
- Check cross-references in AI.md before implementing a dependent feature.
- Update README.md and docs/ whenever shipped behavior changes.

## KEY DECISIONS

| Question | Answer | Spec reference |
|---|---|---|
| Where do Go packages live? | Under `src/`, with required shared, client, server, config, and database packages. | PART 3 |
| Where do identity values come from? | IDEA.md project variables; remote and directory detection are fallbacks only. | PART 2, 3 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| Project name | Public repository/product name from IDEA.md |
| Internal name | Stable internal identifier from IDEA.md |

## QUICK REFERENCE

- Keep runtime data and secrets in configured runtime paths, not source directories.
- Keep Docker assets under `docker/` and embedded public assets under `src/server/web/`.
- Preserve the declared MIT licensing and dependency notices.

For complete details, see AI.md PART 2, 3, 4.
