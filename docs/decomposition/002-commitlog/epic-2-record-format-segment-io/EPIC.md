# Epic 2: Record Format & Segment I/O

**Parent:** [SPEC-002-commitlog.md](../SPEC-002-commitlog.md) §2  
**Blocked by:** Nothing  
**Blocks:** Epic 4 (Durability Worker), Epic 6 (Recovery), Epic 7 (Log Compaction)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 2.1 | [story-2.1-segment-header.md](story-2.1-segment-header.md) | 8-byte file header: magic, version, flags, padding |
| 2.2 | [story-2.2-record-framing.md](story-2.2-record-framing.md) | Record struct, CRC32C computation, serialization |
| 2.3 | [story-2.3-segment-writer.md](story-2.3-segment-writer.md) | Append records to segment file, track size |
| 2.4 | [story-2.4-segment-reader.md](story-2.4-segment-reader.md) | Iterate records with validation, handle truncation |
| 2.5 | [story-2.5-segment-error-types.md](story-2.5-segment-error-types.md) | Segment/record error types |

## Implementation Order

```
Story 2.5 (Error types) — parallel
Story 2.1 (Header)
  └── Story 2.2 (Record framing)
        ├── Story 2.3 (Writer)
        └── Story 2.4 (Reader)
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 2.1–2.2 | `commitlog/record.go` |
| 2.3 | `commitlog/segment_writer.go`, `commitlog/segment_writer_test.go` |
| 2.4 | `commitlog/segment_reader.go`, `commitlog/segment_reader_test.go` |
| 2.5 | `commitlog/errors.go` |
