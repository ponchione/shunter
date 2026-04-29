# Story 1.4: Compression Envelope

**Epic:** [Epic 1 — Message Types & Wire Encoding](EPIC.md)
**Spec ref:** SPEC-005 §3.3
**Depends on:** Story 1.1
**Blocks:** Epic 5 (server message delivery with compression)

---

## Summary

Optional gzip compression for server→client messages. When compression is negotiated, a compression byte prefix wraps the message body.

## Deliverables

- Compression constants:
  ```go
  const (
      CompressionNone uint8 = 0x00
      CompressionGzip uint8 = 0x01
  )
  ```

- `func WrapCompressed(tag uint8, body []byte, mode uint8) ([]byte, error)` — produces `[compression][tag][body]` or `[compression][tag][gzip(body)]` depending on mode. `CompressionNone` passes body through unchanged but includes the explicit compression byte.

- `func UnwrapCompressed(frame []byte) (tag uint8, body []byte, err error)` — reads compression byte, decompresses if gzip, returns tag + uncompressed body.

- `func EncodeFrame(tag uint8, body []byte, compressionEnabled bool, mode uint8) []byte` — if `compressionEnabled`, calls `WrapCompressed`; if not, returns `[tag][body]` (no compression byte at all).

- Error types:
  - `ErrUnknownCompressionTag` — compression byte is not `0x00` or `0x01`
  - `ErrDecompressionFailed` — gzip decompress fails

## Acceptance Criteria

- [ ] Compression disabled: frame is `[tag][body]`, no compression byte
- [ ] Compression enabled, mode=None: frame is `[0x00][tag][body]`
- [ ] Compression enabled, mode=Gzip: frame is `[0x01][tag][gzip(body)]`
- [ ] Unwrap gzip frame → original body matches
- [ ] Unwrap uncompressed envelope (`0x00`) → body matches
- [ ] Unknown compression byte (`0x02`) → `ErrUnknownCompressionTag`
- [ ] Invalid gzip payload → `ErrDecompressionFailed`
- [ ] Empty body compresses and decompresses correctly
- [ ] Large body (1 MiB) compresses and decompresses correctly

## Design Notes

- Compression is server→client only in v1. Client→server messages never use the compression envelope.
- When compression is negotiated as `none` at connection level, the compression byte is omitted entirely. The `compressionEnabled` flag controls this.
- Per-message compression decision (whether to gzip a specific message) is a server choice. Small messages may be cheaper to send uncompressed even when compression is negotiated.
- **Recommendation from spec:** default to `none`. Add gzip when large delta messages become a profiling concern.
