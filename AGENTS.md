# Project SPEC

Project: mail
Role: Efficient loader for AI.md

⚠️ **THIS FILE IS AUTO-LOADED EVERY CONVERSATION. FOLLOW IT EXACTLY.** ⚠️

`AI.md` is the read-only implementation source of truth. `IDEA.md` defines
mail's product scope and canonical project variables. Read the relevant AI.md
PART before changing code or documentation, and keep docs aligned with the
shipped behavior.

## Core rules

- Never edit AI.md. Put product scope in IDEA.md.
- Preserve existing user changes and keep all project writes inside this repo.
- Do not guess product behavior, security policy, credentials, or external
  service assumptions.
- The server uses Go templates and must work without JavaScript for core
  functionality. Follow AI.md PART 16 for every HTML page.
- Go build and test commands run through the project Docker Makefile targets;
  do not invoke Go tooling directly on the host.
- Read TODO.AI.md and TODO.md when present. Remove completed AI-owned tasks
  only after their changes have been committed; preserve human TODO entries.
- Never commit secrets, credentials, tokens, private keys, or generated runtime
  data.

## Key references

- Configuration: AI.md PART 5 and PART 12
- Server, API, security: AI.md PARTS 11, 13, 14
- Web pages and accessibility: AI.md PART 16
- Mail and notifications: AI.md PART 17
- Build, containers, workflows, tests, docs, i18n: AI.md PARTS 25–30
- Project features and routes: IDEA.md
