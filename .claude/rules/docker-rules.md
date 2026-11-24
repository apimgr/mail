# Docker Rules (PART 26)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never put Dockerfiles at repository root or commit runtime volumes, secrets, or generated rootfs data.

## CRITICAL - ALWAYS DO

- Keep Docker assets under docker/, use multi-stage builds, static binaries, OCI labels, tini, health checks, and the required compose patterns.

## Key rules summary

- No Docker packaging currently exists; implementation remains in TODO.AI.md.
- Check cross-references in AI.md before implementing a dependent feature.
- Update README.md and docs/ whenever shipped behavior changes.

For complete details, see AI.md PART 26.
