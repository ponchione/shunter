# Epic 2: Struct Tag Parser

**Parent:** [SPEC-006-schema.md](../SPEC-006-schema.md) §3
**Blocked by:** Nothing — leaf epic
**Blocks:** Epic 4 (Reflection-Path Registration)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 2.1 | [story-2.1-tag-parser.md](story-2.1-tag-parser.md) | Directive definitions, tag parse function, parametric forms |
| 2.2 | [story-2.2-tag-validation.md](story-2.2-tag-validation.md) | Directive validation rules, contradiction checks, default index naming |

## Implementation Order

```
Story 2.1 (Tag parser)
  └── Story 2.2 (Validation + index naming)
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 2.1 | `schema/tag.go`, `schema/tag_test.go` |
| 2.2 | `schema/tag.go` (extend validation), `schema/tag_test.go` |
