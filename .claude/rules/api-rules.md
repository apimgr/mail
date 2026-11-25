# API Rules (PART 13, 14, 15)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never retain project-owned legacy routes or create unversioned project APIs beyond the exact aliases AI.md explicitly permits.

## CRITICAL - ALWAYS DO

- Use canonical versioned paths, documented content negotiation and error formats, request IDs, required auth, and AI.md TLS rules.

## Key rules summary

- IDEA.md is the route inventory for project-specific mail endpoints.
- Check cross-references in AI.md before implementing a dependent feature.
- Update README.md and docs/ whenever shipped behavior changes.

## KEY DECISIONS

| Question | Answer | Spec reference |
|---|---|---|
| How are API paths registered? | Use the canonical API base-path helper; versioned APIs are the default. | PART 14 |
| Which unversioned routes are allowed? | Only the explicit aliases listed by AI.md. | PART 14 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| API route | Versioned machine endpoint under the canonical API prefix |
| Frontend route | Public server-rendered information page or documented stateful flow |

## QUICK REFERENCE

- Use the specified JSON envelope, RFC 7807 errors, request IDs, and auth rules.
- Respect JSON/API, HTML/browser, and text/HTTP-tool content negotiation.
- Implement TLS and certificate behavior only as prescribed by PART 15.

For complete details, see AI.md PART 13, 14, 15.
