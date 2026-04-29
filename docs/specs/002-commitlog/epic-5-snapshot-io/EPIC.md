# Epic 5: Snapshot I/O

**Parent:** [SPEC-002-commitlog.md](../SPEC-002-commitlog.md) §5  
**Blocked by:** Epic 1 (BSATN Codec), SPEC-001 (CommittedState, Table, Sequence, state export hooks)  
**Blocks:** Epic 6 (Recovery), Epic 7 (Log Compaction)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 5.1 | [story-5.1-schema-snapshot-codec.md](story-5.1-schema-snapshot-codec.md) | Encode/decode SchemaSnapshot binary format |
| 5.2 | [story-5.2-snapshot-writer.md](story-5.2-snapshot-writer.md) | Write full snapshot: header, schema_len, sequences, nextID state, rows, Blake3 hash, lockfile protocol |
| 5.3 | [story-5.3-snapshot-reader.md](story-5.3-snapshot-reader.md) | Read snapshot: verify hash, decode schema_len/schema, sequences, nextID state, and rows |
| 5.4 | [story-5.4-snapshot-integrity.md](story-5.4-snapshot-integrity.md) | Blake3 hash verification, lockfile detection, snapshot-in-progress error, constants |

## Implementation Order

```
Story 5.1 (Schema codec)
  └── Story 5.2 (Writer) ← Story 5.4
  └── Story 5.3 (Reader) ← Story 5.4
Story 5.4 (Integrity) — parallel with 5.1
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 5.1 | `commitlog/schema_snapshot.go`, `commitlog/schema_snapshot_test.go` |
| 5.2 | `commitlog/snapshot_writer.go`, `commitlog/snapshot_writer_test.go` |
| 5.3 | `commitlog/snapshot_reader.go`, `commitlog/snapshot_reader_test.go` |
| 5.4 | `commitlog/snapshot_errors.go` |
