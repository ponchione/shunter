# Epic 3: Changeset Codec

**Parent:** [SPEC-002-commitlog.md](../SPEC-002-commitlog.md) §3.1, §3.2  
**Blocked by:** Epic 1 (BSATN Codec)  
**Blocks:** Epic 4 (Durability Worker), Epic 6 (Recovery)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 3.1 | [story-3.1-changeset-encoder.md](story-3.1-changeset-encoder.md) | Encode Changeset to payload bytes |
| 3.2 | [story-3.2-changeset-decoder.md](story-3.2-changeset-decoder.md) | Decode payload bytes to Changeset with schema validation |

## Implementation Order

```
Story 3.1 (Encoder)
  └── Story 3.2 (Decoder)
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 3.1–3.2 | `commitlog/changeset_codec.go`, `commitlog/changeset_codec_test.go` |
