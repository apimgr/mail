# AI Assistant Rules (PART 0, 1)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never edit AI.md, guess missing product behavior, or add undocumented features.

## CRITICAL - ALWAYS DO

- Read the relevant AI.md PART before changes. Keep IDEA.md as WHAT and AI.md as HOW. Verify changes and keep documentation synchronized.

## Key rules summary

- AI.md is read-only. Search `~/Projects/github/apimgr/TODO.API.md` before logging an AI.md-only defect; keep project implementation gaps in `TODO.AI.md`.
- Check cross-references in AI.md before implementing a dependent feature.
- Update README.md and docs/ whenever shipped behavior changes.

## KEY DECISIONS

| Question | Answer | Spec reference |
|---|---|---|
| Can AI.md be edited? | No; record product scope in IDEA.md and template defects in the designated aggregate after searching for duplicates. | PART 0, 1 |
| Which document defines behavior? | AI.md defines HOW; IDEA.md defines product WHAT. | PART 0 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| AI.md | Read-only implementation specification |
| IDEA.md | Product scope and canonical project variables |

## QUICK REFERENCE

- Read relevant sections and cross-references before coding.
- Keep project gaps in TODO.AI.md; report only AI.md defects to TODO.API.md.
- Do not guess behavior or claim checks that were not run.

For complete details, see AI.md PART 0, 1.
