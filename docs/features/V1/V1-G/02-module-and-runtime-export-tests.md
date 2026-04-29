# Superseded V1-G Task 02

This older task draft was superseded by `02-description-export-tests.md` and
`2026-04-24_074206-hosted-runtime-v1g-export-introspection-implplan.md`.

Use the current V1-G plan for the live contract:
- `Module.Describe()` returns detached module identity and declaration metadata.
- `Runtime.ExportSchema()` delegates to the existing schema export.
- `Runtime.Describe()` reports module identity plus runtime health only.

Do not use this file to require a separate reducer summary or runtime status
model beyond the landed `RuntimeDescription`/`RuntimeHealth` surface.
