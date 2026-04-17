# Story 2.2: Tag Validation & Index Name Generation

**Epic:** [Epic 2 — Struct Tag Parser](EPIC.md)
**Spec ref:** SPEC-006 §3.2, §3.1
**Depends on:** Story 2.1
**Blocks:** Epic 4 (Reflection uses validated tag output)

---

## Summary

Validate parsed tag directives for contradictions, duplicates, and illegal combinations. Generate default index names for implicit single-column indexes.

## Deliverables

- Validation is folded into `ParseTag` in v1 so callers get one parse+validate step. A separate exported `ValidateTag` helper is optional, but the normative contract is that `ParseTag` never returns an unchecked `TagDirectives` value. Required checks:
  1. `-` must appear alone: if `Exclude` is true, no other directive may be set → error
  2. Duplicate directives: detected during parse (same token twice in tag string) → error
  3. `primarykey` may not combine with `index` or `index:<name>` → error
  4. Both `index` (plain) and `index:<name>` on same field → error
  5. Unknown directives → error (already handled in 2.1, but defense in depth)

- Default index name generation given a column name:
  - `func DefaultIndexName(columnName string, isPK bool, isUnique bool) string`:
    - `primarykey` → `"pk"`
    - `unique` (non-PK) → `"<column>_uniq"`
    - `index` (plain) → `"<column>_idx"`
    - Named index (`index:<name>`) → use the provided name as-is

## Acceptance Criteria

- [ ] `"-,index"` and `"-,primarykey"` both fail because exclude must appear alone
- [ ] `"primarykey,index"` and `"primarykey,index:foo"` both fail because PK cannot be combined with index directives
- [ ] `"index,index"` and `"unique,unique"` both fail as duplicate directives
- [ ] `"index,index:foo"` fails because plain and named index forms cannot both appear on one field
- [ ] `"primarykey,autoincrement"` remains valid
- [ ] `"unique,index:guild_score"` remains valid for named composite unique participation
- [ ] `DefaultIndexName("id", true, true)` → `"pk"`
- [ ] `DefaultIndexName("email", false, true)` and `DefaultIndexName("name", false, false)` produce `"email_uniq"` and `"name_idx"`

## Design Notes

- Validation is intentionally folded into `ParseTag`; the story split exists only to keep parse grammar and policy rules readable in the docs.
- The "mixed `unique` flags across fields for same named composite index" rule is a cross-field check and belongs in Epic 5 validation, not here. This story only validates per-field tag consistency.
- Directive duplicate detection requires tracking which tokens have been seen during the comma-split loop.
- When `isPK` is true, `DefaultIndexName` always returns `"pk"`; the `isUnique` argument is ignored because primary keys are unique by definition.
