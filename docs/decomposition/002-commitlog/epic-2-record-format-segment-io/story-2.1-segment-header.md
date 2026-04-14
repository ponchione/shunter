# Story 2.1: Segment File Header

**Epic:** [Epic 2 — Record Format & Segment I/O](EPIC.md)  
**Spec ref:** SPEC-002 §2.2  
**Depends on:** Nothing  
**Blocks:** Stories 2.3, 2.4

---

## Summary

The fixed 8-byte header that begins every segment file.

## Deliverables

- Header constants:
  ```go
  var SegmentMagic = [4]byte{'S', 'H', 'N', 'T'}
  const SegmentVersion uint8 = 1
  const SegmentHeaderSize = 8
  ```

- `func WriteSegmentHeader(w io.Writer) error`
  - Writes magic + version(1) + flags(0) + pad(0,0)

- `func ReadSegmentHeader(r io.Reader) error`
  - Reads 8 bytes, validates magic, version, flags, and padding
  - Bad magic → `ErrBadMagic`
  - Bad version → `ErrBadVersion`
  - Non-zero header flags or padding → `ErrBadFlags`

## Acceptance Criteria

- [ ] WriteSegmentHeader produces exactly 8 bytes
- [ ] ReadSegmentHeader on valid header → nil error
- [ ] Wrong magic bytes → `ErrBadMagic`
- [ ] Version != 1 → `ErrBadVersion`
- [ ] Truncated header (< 8 bytes) → `io.ErrUnexpectedEOF`
- [ ] Non-zero flags byte → `ErrBadFlags`
- [ ] Non-zero padding bytes → `ErrBadFlags`

## Design Notes

- Header is tiny and fixed. No versioned extension mechanism — the version byte handles format evolution.
- CRC does NOT cover the segment header. CRC is per-record only.
