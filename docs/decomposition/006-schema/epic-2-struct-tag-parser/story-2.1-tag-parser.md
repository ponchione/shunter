# Story 2.1: Directive Definitions & Tag Parser

**Epic:** [Epic 2 — Struct Tag Parser](EPIC.md)
**Spec ref:** SPEC-006 §3, §3.1
**Depends on:** Nothing
**Blocks:** Story 2.2

---

## Summary

Parse the `shunter:"..."` struct tag into a structured set of directives. This is pure string parsing with no schema or type system dependencies.

## Deliverables

- Directive constants or iota:
  - `DirectivePrimaryKey`
  - `DirectiveAutoIncrement`
  - `DirectiveUnique`
  - `DirectiveIndex` (plain, single-column)
  - `DirectiveIndexNamed` (parametric: `index:<name>`)
  - `DirectiveNameOverride` (parametric: `name:<column-name>`)
  - `DirectiveExclude` (the `-` directive)

- `TagDirectives` struct:
  ```go
  type TagDirectives struct {
      PrimaryKey    bool
      AutoIncrement bool
      Unique        bool
      Index         bool   // plain single-column index
      IndexName     string // non-empty when index:<name> present
      NameOverride  string // non-empty when name:<col> present
      Exclude       bool   // the "-" directive
  }
  ```

- `ParseTag(raw string) (*TagDirectives, error)`:
  1. If `raw` is empty → return zero `TagDirectives` (plain column, no directives)
  2. Split on `,` — no spaces allowed
  3. For each token:
     - `"primarykey"` → set PrimaryKey
     - `"autoincrement"` → set AutoIncrement
     - `"unique"` → set Unique
     - `"index"` → set Index
     - `"index:<name>"` → set IndexName (strip prefix)
     - `"name:<col>"` → set NameOverride (strip prefix)
     - `"-"` → set Exclude
     - Anything else → return error with unknown directive name

## Acceptance Criteria

- [ ] Empty tag (`""`) returns zero `TagDirectives` with no error
- [ ] Primary-key forms parse correctly: `"primarykey"` and `"primarykey,autoincrement"`
- [ ] Parametric directives parse correctly: `"index:guild_score"` → `IndexName`, `"name:player_id"` → `NameOverride`
- [ ] Mixed valid directives parse correctly: `"name:player_id,primarykey"` and `"unique,index:guild_score"`
- [ ] `"-"` parses as `Exclude: true`
- [ ] Unknown directive `"foo"` returns an error mentioning `"foo"`
- [ ] `"index:"` and `"name:"` return errors for empty parametric values

## Design Notes

- The parser does not validate directive combinations — that is Story 2.2. This story only parses and rejects unknown tokens.
- `index` (plain) and `index:<name>` (named) are mutually exclusive forms of the same concept but parsed into different fields. Mutual exclusion validation belongs in Story 2.2.
