# Frontend Rules (PART 16)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never use client-side rendering frameworks, inline styles, inline script handlers, or JavaScript for functionality native HTML supports.

## CRITICAL - ALWAYS DO

- Render all pages with Go templates. Use the shared shell/components, translation keys, responsive accessible design, and POST+CSRF for mutations. Core pages work without JS.

## Key rules summary

- Only public information/preferences pages are allowed by the API project frontend model; no admin dashboard.
- Check cross-references in AI.md before implementing a dependent feature.
- Update README.md and docs/ whenever shipped behavior changes.

## KEY DECISIONS

| Question | Answer | Spec reference |
|---|---|---|
| Does core behavior require JavaScript? | No. Use native links and standard POST forms with CSRF protection. | PART 16 |
| Is an administrative dashboard allowed? | No; the browser surface is limited to the documented public pages and flows. | PART 16 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| Server-rendered | HTML is produced from Go templates before reaching the browser |
| No-JavaScript flow | Navigation and form submissions work without client-side code |

## QUICK REFERENCE

- Reuse the shared page shell, registry, localization, and accessible controls.
- Escape and sanitize content by context; keep internal routes and API mirrors in parity.
- Test pages for keyboard use, responsive behavior, and no-JavaScript operation.

For complete details, see AI.md PART 16.
