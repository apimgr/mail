# Translating

Mail currently supports six locales: Arabic (`ar`), English (`en`), French
(`fr`), German (`de`), Italian (`it`), and Spanish (`es`). Locale JSON files
live in `src/common/i18n/locales/`; a shared registry defines locale codes,
native names, text direction, and plural categories. Loading validates that
every embedded locale has the same message keys as English. Missing values
fall back to English.

## Translation workflow

English (`en.json`) is the source for message keys and meaning. For an existing
language, edit only that locale's JSON file. Keep the key names and order,
UTF-8 encoding, and two-space indentation. Translate values, not keys. Keep
every `{placeholder}` exactly as written in English, and provide all CLDR
plural forms required by the target language. Write complete, natural
sentences in one consistent register; do not join translated fragments.

When adding a language, add its locale file and update the shared language
registry with its code, native name, text direction, and CLDR plural
categories. Check exact key parity and placeholder parity against English.
Never leave a locale out of sync when an English message is added, changed, or
removed.

| English term | Usage in Mail |
|---|---|
| token | API credential; keep distinct from a mailbox password |
| owner token | Credential for an anonymous owner identity; this does not grant operator access to mail records |
| linked token | A credential created under an owner's primary token |
| rotate | Replace a credential and invalidate the previous value |
| revoke | Permanently disable a credential |
| consent | A visitor's recorded choice about optional cookies or data use |
| mailbox | A mail recipient's virtual mailbox, not a panel user account |

Keep recurring terms consistent within a locale. Product names, protocol
names, code, file names, configuration keys, URLs, and literal values typed by
users remain unchanged.

## Selecting a language

The page footer links to language preferences. Select a language and submit
the normal POST form; the renderer stores the selection in a one-year,
HTTP-only `lang` cookie. Each choice is shown by its native name and language
tag. The Automatic option shows the language the browser would select from its
`Accept-Language` header. Choose Automatic to remove the stored selection and
use that browser preference. `?lang=xx` remains a shareable
one-shot override that selects and stores a supported locale. With no explicit
selection, the server negotiates `Accept-Language` and falls back to English.
Arabic pages use right-to-left document direction.

The built-in privacy policy text for data collection, data usage, and data
security is translated in every locale. The API returns the data-usage text in
the selected locale as well. Deployment-specific facts and operator-provided
privacy extensions come from configuration and retain the operator's wording.

The header's no-JavaScript theme form posts the next theme selected by the
server. Its inline SVG uses `currentColor`, and the visible and accessible
button labels name that target mode using the localized `Dark` or `Light`
value, so the action remains clear without browser scripting.

## Current limitations

The translation package provides CLDR plural selection and named-placeholder
formatting. Health views use localized uptime plurals, locale-aware count
separators, and locale date/time layouts; privacy dates use the locale date
layout. Most application surfaces still need localized number/date handling,
and not every human-readable string across the server, client, API, and
documentation is translated. Complete application coverage and accessibility
review remain required by AI.md PART 30.

## Credits

Contributors may request a name or handle be listed here when submitting a
translation correction. No human translator credits have been submitted for
the current locale files. Do not add generated-by or AI attribution.
