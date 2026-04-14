# Story 1.4: BSATN Error Types

**Epic:** [Epic 1 — BSATN Codec](EPIC.md)  
**Spec ref:** SPEC-002 §3.3, §9  
**Depends on:** Nothing  
**Blocks:** Nothing (consumed by Stories 1.2, 1.3)

---

## Summary

Error types for BSATN decode failures.

## Deliverables

| Error | Type | Trigger |
|---|---|---|
| `ErrUnknownValueTag` | struct | tag byte not in 0–12 range |
| `ErrTypeTagMismatch` | struct | decoded tag doesn't match schema-expected type |
| `ErrRowShapeMismatch` | struct | decoded column count != schema column count |
| `ErrRowLengthMismatch` | sentinel | byte count consumed != row_len frame |
| `ErrInvalidUTF8` | sentinel | string payload fails `utf8.Valid` |

- `ErrUnknownValueTag` fields: `Tag uint8`
- `ErrTypeTagMismatch` fields: `Column string`, `Expected ValueKind`, `Got uint8`
- `ErrRowShapeMismatch` fields: `TableName string`, `Expected int`, `Got int`

All implement `error` with descriptive messages.

## Acceptance Criteria

- [ ] Each error type satisfies `error` interface
- [ ] `errors.Is` works for sentinels (ErrRowLengthMismatch, ErrInvalidUTF8)
- [ ] `errors.As` works for struct errors
- [ ] Messages include relevant context (tag value, column name, expected/got counts)
