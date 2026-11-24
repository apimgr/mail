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

For complete details, see AI.md PART 16.
