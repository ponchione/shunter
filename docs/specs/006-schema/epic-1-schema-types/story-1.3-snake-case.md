# Story 1.3: Snake_case Conversion Utility

**Epic:** [Epic 1 — Schema Types & Type Mapping](EPIC.md)
**Spec ref:** SPEC-006 §4.1, §11, §11.2
**Depends on:** Nothing
**Blocks:** Epic 4 (column and table naming)

---

## Summary

Convert Go CamelCase identifiers to snake_case for default table and column names.

## Deliverables

- `ToSnakeCase(s string) string` — converts a CamelCase Go identifier to snake_case:
  - Insert underscore before each uppercase letter that follows a lowercase letter or digit
  - Insert underscore between a run of uppercase letters and the following lowercase letter (acronym boundary): `PlayerID` → `player_id`, `HTTPServer` → `http_server`
  - Lowercase everything
  - No leading or trailing underscores
  - Empty string → empty string

## Acceptance Criteria

- [ ] Simple CamelCase identifiers convert correctly: `"Player"` → `"player"`, `"Score"` → `"score"`, `"A"` → `"a"`
- [ ] Multi-word identifiers convert correctly: `"PlayerSession"` → `"player_session"`, `"ExpiresAt"` → `"expires_at"`
- [ ] Acronym boundaries convert correctly: `"PlayerID"` → `"player_id"`, `"GuildID"` → `"guild_id"`, `"HTTPServer"` → `"http_server"`, `"URL"` → `"url"`
- [ ] All-caps identifier `"ID"` converts to `"id"` without extra underscores
- [ ] Mixed-case acronym example `"getHTTPSUrl"` converts to `"get_https_url"`
- [ ] Empty string returns empty string

## Design Notes

- This is a well-known algorithm with many open-source implementations. The Go standard library does not include one. The implementation should handle the uppercase-run-to-lowercase transition correctly since Go idiom uses all-caps acronyms (`ID`, `URL`, `HTTP`).
- Only ASCII letters are relevant since Go exported identifiers start with an uppercase ASCII letter.
