# Epic 1: BSATN Codec

**Parent:** [SPEC-002-commitlog.md](../SPEC-002-commitlog.md) §3.3  
**Blocked by:** SPEC-001 Epic 1 (Value, ProductValue, ValueKind)  
**Blocks:** Epic 3 (Changeset Codec), Epic 5 (Snapshot I/O)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 1.1 | [story-1.1-value-encoder.md](story-1.1-value-encoder.md) | Encode single Value to bytes (tag + payload) |
| 1.2 | [story-1.2-value-decoder.md](story-1.2-value-decoder.md) | Decode single Value from bytes with tag validation |
| 1.3 | [story-1.3-product-value-codec.md](story-1.3-product-value-codec.md) | Encode/decode ProductValue with schema validation |
| 1.4 | [story-1.4-bsatn-error-types.md](story-1.4-bsatn-error-types.md) | Decoder error types |

## Implementation Order

```
Story 1.4 (Error types) — parallel with any
Story 1.1 (Value encoder)
  └── Story 1.2 (Value decoder)
        └── Story 1.3 (ProductValue codec)
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 1.1–1.2 | `bsatn/value.go`, `bsatn/value_test.go` |
| 1.3 | `bsatn/product_value.go`, `bsatn/product_value_test.go` |
| 1.4 | `bsatn/errors.go` |
