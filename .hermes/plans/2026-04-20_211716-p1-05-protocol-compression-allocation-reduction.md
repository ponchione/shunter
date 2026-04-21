# P1-05 Protocol Compression Allocation Reduction Plan

Goal: Reduce per-message allocation and copy overhead in the protocol gzip paths by pooling gzip readers/writers where safe, avoiding dispatch-loop reframing copies, and adding focused compression benchmarks.

Architecture:
- Keep current wire behavior unchanged: compression envelopes remain `[compression][tag][body-or-gzip(body)]` and brotli stays unsupported.
- Optimize internals in two places: compression helpers (`protocol/compression.go`) and inbound dispatch decode (`protocol/dispatch.go` plus client decode helpers).
- Add benchmarks in `protocol/compression_test.go` to measure wrap/unwrap overhead on representative payload sizes.

Grounded context:
- Audit item after P0-04 is `P1-05` in `docs/performance-audit-punchlist-2026-04-20.md:188-224`.
- `protocol/compression.go` currently allocates a fresh `bytes.Buffer` + `gzip.Writer` per gzip frame and does `io.ReadAll(gr)` on inbound gzip.
- `protocol/dispatch.go` currently calls `UnwrapCompressed`, then reconstructs a new `[tag][body]` frame slice before calling `DecodeClientMessage`.
- `protocol/client_messages.go` already decodes by splitting `frame[0]` and `frame[1:]`, so adding a tag/body decode helper is a low-risk way to remove the reframing copy.

Files likely to change:
- Modify: `protocol/compression.go`
- Modify: `protocol/dispatch.go`
- Modify: `protocol/client_messages.go`
- Modify: `protocol/compression_test.go`

Non-goals:
- Do not change negotiated compression semantics.
- Do not add a compression threshold in this slice unless the simpler pooling/copy-removal work proves insufficient; it is optional in the audit.
- Do not change public message formats.

## Task 1: Add tests/benchmarks first

Objective: lock in behavior and add measurable perf harnesses before changing internals.

Files:
- Modify: `protocol/compression_test.go`
- Optionally modify: `protocol/dispatch_test.go`

Steps:
1. Add focused benchmarks for `WrapCompressed` and `UnwrapCompressed` on representative payload sizes.
2. Add a test for direct `(tag, body)` client decode if a helper is introduced.
3. Run targeted protocol tests/bench build before implementation.

Validation:
- `rtk go test ./protocol -run 'Test(UnwrapCompressed.*|EncodeFrame.*|DecodeClientMessage.*)' -count=1`

## Task 2: Remove inbound dispatch reframing copy

Objective: decode compressed frames without reconstructing `[tag][body]` slices.

Files:
- Modify: `protocol/client_messages.go`
- Modify: `protocol/dispatch.go`

Implementation outline:
1. Extract a private helper such as `decodeClientMessageParts(tag uint8, body []byte) (any, error)` or a public/internal equivalent that reuses the existing switch and per-message decoders.
2. Make `DecodeClientMessage(frame []byte)` delegate to that helper after its frame-length check.
3. Update `runDispatchLoop` so the compression path passes `(tag, body)` directly to the helper instead of allocating `reframed := make([]byte, 1+len(body))`.
4. Preserve all existing malformed-message / unknown-tag behavior.

## Task 3: Pool gzip readers/writers

Objective: reduce repeated gzip object allocation on compressed traffic.

Files:
- Modify: `protocol/compression.go`

Implementation outline:
1. Add package-private `sync.Pool` instances for `gzip.Writer` and `gzip.Reader`.
2. For `WrapCompressed` gzip mode:
   - keep the per-call output buffer (result bytes must escape),
   - acquire/reset a pooled `gzip.Writer` against that buffer,
   - close it to flush footer,
   - return it to the pool after reset/detach.
3. For `UnwrapCompressed` gzip mode:
   - acquire/reset a pooled `gzip.Reader` over the payload,
   - decompress into a `bytes.Buffer` (or equivalent) rather than raw `io.ReadAll(gr)` if that gives better reuse/clarity,
   - close/reset and return the reader to the pool.
4. Preserve `ErrDecompressionFailed` wrapping semantics.

## Task 4: Validate and compare

Objective: ensure no protocol behavior changed and the benchmarks compile/run.

Validation steps:
1. `rtk go fmt ./protocol`
2. `rtk go test ./protocol -count=1`
3. Attempt benchmark run:
   - `rtk go test ./protocol -run '^$' -bench 'Benchmark(WrapCompressed|UnwrapCompressed)' -benchmem -count=1`
4. If RTK suppresses benchmark-only output again, rely on successful package test build plus benchmark source presence.

Expected result:
- Lower allocation churn for gzip traffic.
- No extra copy in compressed inbound dispatch.
- Existing protocol tests remain green.

Risks / notes:
- `gzip.Reader.Reset` requires an already-initialized reader; pool creation must handle first-use initialization carefully.
- Pooled gzip objects must never retain buffers across calls in a way that aliases returned frame memory.
- The outbound result still needs one final returned `[]byte`; the win is removing transient compressor object churn and inbound reframing copies.