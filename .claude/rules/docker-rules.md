# Docker Rules (PART 26)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never put Dockerfiles at repository root or commit runtime volumes, secrets, or generated rootfs data.

## CRITICAL - ALWAYS DO

- Keep Docker assets under `docker/`. The repository has server and development Dockerfiles plus production, development, and test Compose files; keep them aligned with the required multi-stage static build, OCI labels, tini, health checks, privilege setup, and service-aware startup.

## Key rules summary

- Docker packaging exists; runtime privilege handling, service-aware startup, Valkey behavior, and complete release integration remain tracked in `TODO.AI.md`.
- Check cross-references in AI.md before implementing a dependent feature.
- Update README.md and docs/ whenever shipped behavior changes.

## KEY DECISIONS

| Question | Answer | Spec reference |
|---|---|---|
| Where are container definitions stored? | Under `docker/`, never at repository root. | PART 26 |
| Which process is PID 1? | `tini`, with the application managing its runtime identity and privileges. | PART 26 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| Runtime image | Final minimal image that runs the static service binary |
| Root filesystem overlay | Project-owned startup and privilege-management files under `docker/rootfs/` |

## QUICK REFERENCE

- Keep production, development, and test Compose configurations separate.
- Keep volumes for configuration/data explicit and never commit their runtime contents.
- Validate health checks, signal forwarding, service startup, and privilege drop in the container flow.

For complete details, see AI.md PART 26.
