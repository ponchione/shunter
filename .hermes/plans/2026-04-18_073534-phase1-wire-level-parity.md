# Phase 1 — Wire-Level Protocol Envelope Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close four narrow wire-level protocol divergences between Shunter and SpacetimeDB (subprotocol negotiation, compression tags, close-code/lifecycle, frame-boundary message-family divergences) so the protocol boundary is less observably different.

**Architecture:** Each slice is a TDD-driven parity closure. A failing protocol-boundary test names the reference behavior, a minimal code change closes the gap (or explicitly pins the retained divergence), then the `docs/parity-phase0-ledger.md` bucket row is advanced from `in_progress` → `closed` or annotated with a deferred divergence. Slices are independent; no one slice blocks another. Stop after any single slice lands if Phase 1 is proving larger than this plan — per prompt stop rule.

**Tech Stack:** Go 1.22+, `github.com/coder/websocket` (forked per SPEC-WS-FORK-001), RTK for all shell/git commands, clean-room: no Rust ports from `reference/SpacetimeDB/`.

**Hard rules reminder:**
- All shell/git via `rtk`.
- Strict TDD: failing test first, watch it fail, minimal fix, focused tests, broader verification.
- No widening into Phase 1.5 (no `TransactionUpdate` heavy/light split; no SubscribeMulti/Single; no `CallReducer.flags`; no SQL OneOffQuery; no `ReducerCallResult` enum restructure). Those are parity surfaces handled by Phase 1.5 / Phase 2.
- Every intentional deferral must be recorded explicitly in `docs/parity-phase0-ledger.md` and/or `SPEC-AUDIT.md`.
- Do not copy code from `reference/SpacetimeDB/`. Reference identifiers/constants only.

**Reference outcome corpus (what each slice matches):**
- Reference subprotocol token: `v1.bsatn.spacetimedb` (`reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:24`)
- Reference compression tag constants:
  - `SERVER_MSG_COMPRESSION_TAG_NONE = 0`
  - `SERVER_MSG_COMPRESSION_TAG_BROTLI = 1`
  - `SERVER_MSG_COMPRESSION_TAG_GZIP = 2`
  (`reference/SpacetimeDB/crates/client-api-messages/src/websocket/common.rs:48-54`)
- Reference close codes: RFC 6455 standard (`1000`/`1002`/`1008`/`1011`).

---

## File Structure

Phase 1 touches the protocol package and parity docs only.

### Create
- `protocol/parity_subprotocol_test.go` — pins subprotocol negotiation parity behavior.
- `protocol/parity_compression_test.go` — pins compression tag parity.
- `protocol/parity_close_codes_test.go` — pins close-code + rejection status parity.
- `protocol/parity_message_family_test.go` — locks current frame-boundary message-family behavior so intentional deferrals are visible.

### Modify
- `protocol/upgrade.go` (lines 15-18, 127-136) — accept reference subprotocol.
- `protocol/compression.go` (lines 11-15, 53-76, 82-106) — renumber gzip to `0x02`, reserve brotli slot `0x01`, update unknown-tag behavior.
- `protocol/compression_test.go` — update tests to the new tag numbering.
- `protocol/dispatch_test.go`, `protocol/close_test.go`, `protocol/fanout_adapter_test.go` — fix any call sites that pass compression constants numerically.
- `docs/parity-phase0-ledger.md` — advance bucket statuses after each slice.
- `SPEC-AUDIT.md` — mark closed or retained-with-reason for each touched divergence.
- `docs/decomposition/005-protocol/SPEC-005-protocol.md` — align the normative wire contract with whatever lands.

### No changes
- No `subscription/`, `executor/`, `commitlog/`, or `store/` edits.
- No new public API for `TransactionUpdate`/`CallReducer.flags`/etc. Those are Phase 1.5+.

---

## Pre-Flight

- [ ] **Step P.1: Verify baseline is green**

Run: `rtk go test ./...`
Expected: `ok` for all packages; `920 passed` or higher.

- [ ] **Step P.2: Confirm no in-flight diff under `protocol/` or `docs/parity-phase0-ledger.md`**

Run: `rtk git status --short protocol/ docs/parity-phase0-ledger.md SPEC-AUDIT.md`
Expected: empty output. If anything is dirty, stop and resolve first.

- [ ] **Step P.3: Re-read Phase 1 of `docs/spacetimedb-parity-roadmap.md` (§3 Phase 1, §4 Slice 2, §5 rules) and the `P0-PROTOCOL-00*` rows in `docs/parity-phase0-ledger.md`**

Expected: mental model loaded. No action.

---

## Task 1: Subprotocol Parity Slice

**Parity behavior being matched:** The `/subscribe` WebSocket upgrade accepts the reference subprotocol identifier `v1.bsatn.spacetimedb` exactly as the reference server does, and returns it as the selected subprotocol so a SpacetimeDB-compatible client can negotiate without knowing Shunter.

**Decision (per roadmap §3 Phase 1 recommendation line 327):** accept the reference token. Retain `v1.bsatn.shunter` as an additional legacy identifier for now so existing Shunter clients/tests are not broken; mark that retention as an intentional deferral.

**Files:**
- Modify: `protocol/upgrade.go:15-18` (constant block), `protocol/upgrade.go:127-136` (subprotocol check + `websocket.Accept` call)
- Create: `protocol/parity_subprotocol_test.go`
- Modify: `docs/parity-phase0-ledger.md` `P0-PROTOCOL-001` row
- Modify: `SPEC-AUDIT.md` subprotocol bullet

- [ ] **Step 1.1: Write the failing parity test**

Create `protocol/parity_subprotocol_test.go`:

```go
package protocol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/ponchione/shunter/auth"
)

// TestPhase1ParityReferenceSubprotocolAccepted locks the Phase 1
// parity decision: the upgrade handler admits a client that offers the
// SpacetimeDB reference subprotocol token "v1.bsatn.spacetimedb", and
// returns that exact token as the selected subprotocol.
//
// Reference outcome matched: reference/SpacetimeDB subprotocol token
// v1.bsatn.spacetimedb declared in
// crates/client-api-messages/src/websocket/v1.rs (ref constant).
func TestPhase1ParityReferenceSubprotocolAccepted(t *testing.T) {
	srv := newParityTestServer(t)
	ts := httptest.NewServer(http.HandlerFunc(srv.HandleSubscribe))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") +
		"?connection_id=" + nonZeroConnectionIDHex()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{SubprotocolReference},
	})
	if err != nil {
		t.Fatalf("dial with reference subprotocol: %v", err)
	}
	defer conn.CloseNow()

	if got := conn.Subprotocol(); got != SubprotocolReference {
		t.Fatalf("server selected subprotocol = %q, want %q",
			got, SubprotocolReference)
	}
}

// TestPhase1ParityLegacyShunterSubprotocolStillAccepted pins the
// intentional deferral: the Shunter-native token "v1.bsatn.shunter"
// remains accepted so existing clients do not break. Update this test
// when the retention window closes.
func TestPhase1ParityLegacyShunterSubprotocolStillAccepted(t *testing.T) {
	srv := newParityTestServer(t)
	ts := httptest.NewServer(http.HandlerFunc(srv.HandleSubscribe))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") +
		"?connection_id=" + nonZeroConnectionIDHex()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{SubprotocolV1},
	})
	if err != nil {
		t.Fatalf("dial with legacy subprotocol: %v", err)
	}
	defer conn.CloseNow()

	if got := conn.Subprotocol(); got != SubprotocolV1 {
		t.Fatalf("server selected subprotocol = %q, want %q",
			got, SubprotocolV1)
	}

	_ = auth.AuthModeAnonymous // keeps import live for helpers
}
```

The helpers `newParityTestServer`, `dialTimeout`, and `nonZeroConnectionIDHex` likely already exist in `protocol/test_helpers_test.go` (an Explore agent confirmed a helpers file exists). If any helper is missing, add it alongside the test in the same commit. Do not invent a new helpers file.

- [ ] **Step 1.2: Run test to verify it fails**

Run: `rtk go test ./protocol -run TestPhase1ParityReferenceSubprotocol -v`
Expected: FAIL with `undefined: SubprotocolReference` or `dial: bad handshake`.

- [ ] **Step 1.3: Add the reference constant and admit it in upgrade**

Edit `protocol/upgrade.go` lines 15-18:

```go
// SubprotocolV1 is the Shunter-native WebSocket subprotocol token,
// retained for backward compatibility while Phase 1 parity work
// introduces the reference token. See docs/parity-phase0-ledger.md
// P0-PROTOCOL-001 for the retention rationale.
const SubprotocolV1 = "v1.bsatn.shunter"

// SubprotocolReference is the SpacetimeDB reference WebSocket
// subprotocol token (SPEC-005 §2.2 parity target). A client that
// offers this token MUST be admitted as a Phase 1 parity requirement.
const SubprotocolReference = "v1.bsatn.spacetimedb"

// acceptedSubprotocols lists every token the server admits, in the
// order preferred when multiple are offered. The reference token is
// preferred so a client offering both negotiates the parity-aligned
// identifier.
var acceptedSubprotocols = []string{SubprotocolReference, SubprotocolV1}
```

Replace the subprotocol-check block at lines 127-136:

```go
	// 4. subprotocol check — client MUST offer at least one of the
	// accepted tokens. Reference token is preferred.
	selected, ok := negotiateSubprotocol(r, acceptedSubprotocols)
	if !ok {
		http.Error(w,
			"Sec-WebSocket-Protocol must include "+SubprotocolReference+
				" or "+SubprotocolV1,
			http.StatusBadRequest)
		return
	}

	// 5. Upgrade.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{selected},
	})
```

Add `negotiateSubprotocol` near the existing `clientOffersSubprotocol` helper (find it with `rtk grep clientOffersSubprotocol protocol/`):

```go
// negotiateSubprotocol inspects Sec-WebSocket-Protocol and returns the
// first token from `preferred` that the client also offered. Falls back
// to false when no overlap exists.
func negotiateSubprotocol(r *http.Request, preferred []string) (string, bool) {
	header := r.Header.Values("Sec-WebSocket-Protocol")
	offered := make(map[string]struct{}, len(header))
	for _, line := range header {
		for _, raw := range strings.Split(line, ",") {
			tok := strings.TrimSpace(raw)
			if tok != "" {
				offered[tok] = struct{}{}
			}
		}
	}
	for _, want := range preferred {
		if _, ok := offered[want]; ok {
			return want, true
		}
	}
	return "", false
}
```

Leave `clientOffersSubprotocol` in place — other tests/integrations likely call it. If not, delete it in the same commit to avoid dead code.

- [ ] **Step 1.4: Run focused tests**

Run: `rtk go test ./protocol -run 'TestPhase1ParityReferenceSubprotocol|TestPhase1ParityLegacyShunterSubprotocolStillAccepted|TestPhase1ParityReferenceSubprotocolPreferred' -v`
Expected: PASS (reference accepted, legacy still accepted).

- [ ] **Step 1.5: Run broader protocol tests**

Run: `rtk go test ./protocol`
Expected: PASS — no existing subprotocol test regresses.

- [ ] **Step 1.6: Update ledger + audit docs**

Edit `docs/parity-phase0-ledger.md` `P0-PROTOCOL-001` row:
- change status from `in_progress` to `closed`
- in "Next slice note" column, record: "Phase 1 closed: reference subprotocol `v1.bsatn.spacetimedb` accepted and preferred; `v1.bsatn.shunter` retained as an intentional deferral until legacy-client cutover — see `SPEC-AUDIT.md` subprotocol bullet."

Edit `SPEC-AUDIT.md` subprotocol bullet (find it with `rtk grep -n "v1.bsatn.shunter" SPEC-AUDIT.md`). Replace the open-divergence description with:

> **[RETAINED-DEFERRAL]** Shunter admits both `v1.bsatn.spacetimedb` (reference, preferred) and `v1.bsatn.shunter` (legacy). Reference admission closes the Phase 1 parity gap; legacy retention is intentional until existing Shunter-token clients are cut over.

If `docs/decomposition/005-protocol/SPEC-005-protocol.md` §2.2 still declares only the Shunter token, update it the same way in the same commit.

- [ ] **Step 1.7: Run full suite**

Run: `rtk go test ./...`
Expected: all packages green.

- [ ] **Step 1.8: Commit**

```bash
rtk git add protocol/upgrade.go protocol/parity_subprotocol_test.go docs/parity-phase0-ledger.md SPEC-AUDIT.md docs/decomposition/005-protocol/SPEC-005-protocol.md
rtk git commit -m "protocol(parity): admit SpacetimeDB reference subprotocol — P0-PROTOCOL-001"
```

---

## Task 2: Compression Tag Parity Slice

**Parity behavior being matched:** The compression envelope uses the same tag numbering as the reference server so a reference-compatible client can decode server frames byte-for-byte:
- `0x00` = none
- `0x01` = brotli (reserved; Shunter does not implement — explicit deferral)
- `0x02` = gzip

Current Shunter numbering collides: `CompressionGzip = 0x01` where reference says `0x01 = brotli`.

**Scope boundary:** do NOT implement brotli. Reserve the byte, return a dedicated "brotli-unsupported" close path when a client sends it, and document the deferral.

**Files:**
- Modify: `protocol/compression.go:11-15` (constant block), `53-76` (`WrapCompressed`), `82-106` (`UnwrapCompressed`)
- Modify: `protocol/compression_test.go` — update expected tag byte values
- Modify: call sites that pass raw `0x01`/`0x02` numerically (find them, do not guess). Run `rtk grep -n "CompressionGzip\\|CompressionNone\\|CompressionBrotli" protocol/` before editing.
- Create: `protocol/parity_compression_test.go`
- Modify: `docs/parity-phase0-ledger.md` `P0-PROTOCOL-002`, `SPEC-AUDIT.md` compression bullet, `docs/decomposition/005-protocol/SPEC-005-protocol.md` §3.3

- [ ] **Step 2.1: Survey current usages before editing**

Run: `rtk grep -n "CompressionGzip\|CompressionNone\|CompressionBrotli\|0x01.*compression\|compression.*0x01" protocol/`
Record all hits. Every one must be audited in step 2.5.

Run: `rtk grep -n "CompressionGzip\|CompressionNone" --glob '!**/vendor/**'`
Record any non-`protocol/` callers (e.g., `subscription/`, `executor/`). If there are callers outside `protocol/`, the constant rename below must preserve the Go identifier even though the byte value moves.

- [ ] **Step 2.2: Write the failing parity test**

Create `protocol/parity_compression_test.go`:

```go
package protocol

import (
	"bytes"
	"compress/gzip"
	"testing"
)

// TestPhase1ParityCompressionTagByteValues pins the reference byte
// numbering: 0x00 none, 0x01 brotli (reserved, unsupported), 0x02
// gzip. Reference outcome matched:
// crates/client-api-messages/src/websocket/common.rs
// SERVER_MSG_COMPRESSION_TAG_{NONE,BROTLI,GZIP}.
func TestPhase1ParityCompressionTagByteValues(t *testing.T) {
	if CompressionNone != 0x00 {
		t.Errorf("CompressionNone = 0x%02x, want 0x00", CompressionNone)
	}
	if CompressionBrotli != 0x01 {
		t.Errorf("CompressionBrotli = 0x%02x, want 0x01",
			CompressionBrotli)
	}
	if CompressionGzip != 0x02 {
		t.Errorf("CompressionGzip = 0x%02x, want 0x02", CompressionGzip)
	}
}

// TestPhase1ParityCompressionGzipEnvelopeByte pins the over-the-wire
// byte sequence so a reference-compatible client sees gzip signaled as
// 0x02.
func TestPhase1ParityCompressionGzipEnvelopeByte(t *testing.T) {
	frame, err := WrapCompressed(TagTransactionUpdate, []byte("body"),
		CompressionGzip)
	if err != nil {
		t.Fatalf("WrapCompressed gzip: %v", err)
	}
	if len(frame) < 2 {
		t.Fatalf("frame too short: %d", len(frame))
	}
	if frame[0] != 0x02 {
		t.Errorf("compression byte = 0x%02x, want 0x02 (gzip)", frame[0])
	}
	if frame[1] != TagTransactionUpdate {
		t.Errorf("tag byte = 0x%02x, want 0x%02x",
			frame[1], TagTransactionUpdate)
	}
	gr, err := gzip.NewReader(bytes.NewReader(frame[2:]))
	if err != nil {
		t.Fatalf("gzip decode: %v", err)
	}
	defer gr.Close()
}

// TestPhase1ParityCompressionBrotliReservedRejected pins the deferral:
// brotli is recognized as a known tag but Shunter does not implement
// it. Server-side emit must reject it; decode must return a dedicated
// ErrBrotliUnsupported (distinct from ErrUnknownCompressionTag) so
// callers can distinguish "reserved-but-unimplemented" from "bogus
// byte".
func TestPhase1ParityCompressionBrotliReservedRejected(t *testing.T) {
	_, err := WrapCompressed(TagTransactionUpdate, []byte("body"),
		CompressionBrotli)
	if err == nil {
		t.Fatal("WrapCompressed brotli: want error, got nil")
	}
	if !errorsIs(err, ErrBrotliUnsupported) {
		t.Errorf("err = %v, want ErrBrotliUnsupported", err)
	}

	// A frame arriving with 0x01 from a peer must decode to the same
	// reserved-unsupported error so the dispatch loop can close with
	// a specific reason.
	frame := []byte{CompressionBrotli, TagSubscribe, 0xAA}
	_, _, derr := UnwrapCompressed(frame)
	if !errorsIs(derr, ErrBrotliUnsupported) {
		t.Errorf("UnwrapCompressed brotli err = %v, want ErrBrotliUnsupported",
			derr)
	}
}

// errorsIs is a local wrapper so the test file does not need a direct
// stdlib errors import if the package already re-exports one.
func errorsIs(err, target error) bool {
	if err == nil || target == nil {
		return err == target
	}
	for e := err; e != nil; {
		if e == target {
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := e.(unwrapper)
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
```

If `protocol/` already imports the stdlib `errors` package in test files, drop the local `errorsIs` and use `errors.Is` directly — remove the helper in the same commit.

- [ ] **Step 2.3: Run test to verify it fails**

Run: `rtk go test ./protocol -run TestPhase1ParityCompression -v`
Expected: FAIL — `undefined: CompressionBrotli`, `undefined: ErrBrotliUnsupported`, and/or gzip byte assertion fails because current value is `0x01`.

- [ ] **Step 2.4: Renumber the constants and implement reserved-brotli behavior**

Edit `protocol/compression.go` lines 11-15:

```go
// Compression byte values (SPEC-005 §3.3, parity-aligned with
// reference/SpacetimeDB
// crates/client-api-messages/src/websocket/common.rs
// SERVER_MSG_COMPRESSION_TAG_*).
const (
	CompressionNone   uint8 = 0x00
	CompressionBrotli uint8 = 0x01 // reserved; ErrBrotliUnsupported.
	CompressionGzip   uint8 = 0x02
)
```

Add below the existing error vars (lines 19-22):

```go
// ErrBrotliUnsupported is returned when a peer requests brotli
// compression. The tag is recognized (Phase 1 parity) but Shunter does
// not implement brotli; callers should treat it as a distinct protocol
// deferral rather than an unknown tag.
var ErrBrotliUnsupported = errors.New("protocol: brotli compression unsupported")
```

Update `WrapCompressed` (lines 53-76) `switch mode` to add a brotli arm before the default:

```go
	case CompressionBrotli:
		return nil, ErrBrotliUnsupported
```

Update `UnwrapCompressed` (lines 82-106) similarly — add a brotli case before the default:

```go
	case CompressionBrotli:
		return 0, nil, ErrBrotliUnsupported
```

- [ ] **Step 2.5: Update existing compression tests and callers**

For every hit from Step 2.1, inspect and update:
- Tests that asserted a gzip frame starts with `0x01`: change to `0x02` (or better, to `CompressionGzip`).
- Tests that asserted unknown-tag behavior with `0x02`: that byte now means gzip. Pick a different out-of-range byte (e.g., `0x03`).
- Any test helpers that construct envelopes with a literal `0x01` for gzip: rewrite via `CompressionGzip`.

Expected hit sites include at minimum `protocol/compression_test.go` (lines 9-130 per the Explore inventory). Audit every test in that file. Do not guess — run the file's tests and fix regressions that appear.

- [ ] **Step 2.6: Run focused tests**

Run: `rtk go test ./protocol -run TestPhase1ParityCompression -v`
Expected: PASS.

Run: `rtk go test ./protocol -run TestWrapCompressed -v`
Run: `rtk go test ./protocol -run TestUnwrapCompressed -v`
Run: `rtk go test ./protocol -run TestEncodeFrame -v`
Expected: PASS — existing compression tests now use the new numbering.

- [ ] **Step 2.7: Run protocol package + broad suite**

Run: `rtk go test ./protocol`
Expected: PASS.

Run: `rtk go test ./...`
Expected: PASS. If non-`protocol/` packages regressed, fix the constant use sites found in Step 2.1 — do not revert the tag values.

- [ ] **Step 2.8: Decide how the dispatch loop surfaces `ErrBrotliUnsupported`**

A client sending brotli today hits `ErrUnknownCompressionTag` → `1002 ProtocolError` per `protocol/close_test.go:TestUnknownCompressionTag_Closes1002`. With the parity change, brotli becomes a known-but-unsupported tag. Two acceptable outcomes:

1. Close with `1002 ProtocolError` and reason `"brotli unsupported"`. Matches existing behavior; one less close-code divergence. **Recommended.**
2. Close with a new policy-like code. Do NOT do this in Phase 1 — it widens scope.

Pick (1). Verify the dispatch code path (`protocol/dispatch.go:88-102` per the Explore inventory) already routes `ErrBrotliUnsupported` to `1002`. If it currently only matches `ErrUnknownCompressionTag`, add an `errors.Is(err, ErrBrotliUnsupported)` branch with the reason string `"brotli unsupported"`.

Add a test in `protocol/parity_compression_test.go`:

```go
// TestPhase1ParityCompressionBrotliFrameClosesWithReason verifies a
// peer sending a brotli-tagged frame causes a 1002 close whose reason
// identifies the deferral, so the client sees a specific signal
// instead of a generic "unknown tag".
func TestPhase1ParityCompressionBrotliFrameClosesWithReason(t *testing.T) {
	// Drive via the same test harness already used by
	// TestUnknownCompressionTag_Closes1002 in protocol/close_test.go.
	// Assert the CloseError code is 1002 and the reason contains
	// "brotli".
	// ... see protocol/close_test.go for the helper pattern.
}
```

Flesh out the test body by mirroring `TestUnknownCompressionTag_Closes1002` — do not duplicate the harness, reuse it.

- [ ] **Step 2.9: Run protocol package again**

Run: `rtk go test ./protocol`
Expected: PASS including the new brotli-reason test.

- [ ] **Step 2.10: Update ledger + audit docs**

Edit `docs/parity-phase0-ledger.md` `P0-PROTOCOL-002`:
- status → `closed`
- Next slice note → "Phase 1 closed: tag numbering parity-aligned (None=0x00, Brotli=0x01 reserved, Gzip=0x02). Brotli is a recognized-but-deferred tag returning `ErrBrotliUnsupported` and closing with 1002 reason `brotli unsupported`. Brotli implementation is a Phase 2+ decision."

Edit `SPEC-AUDIT.md` compression bullet: replace the open-divergence row with "[CLOSED] Compression tag numbering aligned; brotli retained as explicit deferred-tag per `ErrBrotliUnsupported`."

Edit `docs/decomposition/005-protocol/SPEC-005-protocol.md` §3.3: update the tag numbering table to match.

- [ ] **Step 2.11: Commit**

```bash
rtk git add protocol/compression.go protocol/compression_test.go protocol/parity_compression_test.go protocol/dispatch.go docs/parity-phase0-ledger.md SPEC-AUDIT.md docs/decomposition/005-protocol/SPEC-005-protocol.md
rtk git commit -m "protocol(parity): align compression tag numbering with reference — P0-PROTOCOL-002"
```

Include any other files the survey identified as needing the constant rename.

---

## Task 3: Close-Code + Handshake Rejection Parity Slice

**Parity behavior being matched:** Shunter's close-code usage and HTTP-upgrade rejection codes match RFC 6455 + reference server conventions, and the matching is *pinned by a parity test* so future refactors cannot drift.

**Current state:** `protocol/close.go:11-16` already uses standard codes. This slice is a pin + audit, not a rewrite. The real risk is quiet drift (a future change uses `1011` where reference uses `1008`).

**Files:**
- Create: `protocol/parity_close_codes_test.go`
- Modify: `docs/parity-phase0-ledger.md` `P0-PROTOCOL-003`
- Possibly modify: `protocol/disconnect.go`, `protocol/lifecycle.go`, `protocol/close.go` if the audit in Step 3.2 finds any code path using the wrong code.

- [ ] **Step 3.1: Write the failing parity test**

Create `protocol/parity_close_codes_test.go`:

```go
package protocol

import (
	"testing"

	"github.com/coder/websocket"
)

// TestPhase1ParityCloseCodeConstants pins the four close codes used by
// the server. Reference: RFC 6455 §7.4.1 + reference/SpacetimeDB
// standard usage.
func TestPhase1ParityCloseCodeConstants(t *testing.T) {
	if CloseNormal != websocket.StatusNormalClosure {
		t.Errorf("CloseNormal = %d, want 1000", CloseNormal)
	}
	if CloseProtocol != websocket.StatusProtocolError {
		t.Errorf("CloseProtocol = %d, want 1002", CloseProtocol)
	}
	if ClosePolicy != websocket.StatusPolicyViolation {
		t.Errorf("ClosePolicy = %d, want 1008", ClosePolicy)
	}
	if CloseInternal != websocket.StatusInternalError {
		t.Errorf("CloseInternal = %d, want 1011", CloseInternal)
	}
}

// TestPhase1ParityHandshakeRejectionStatuses pins the HTTP status
// codes the server returns before the WebSocket upgrade for each
// rejection class. See protocol/upgrade.go.
func TestPhase1ParityHandshakeRejectionStatuses(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(query *testQuery)
		wantStatus int
	}{
		{"zero_connection_id",
			func(q *testQuery) { q.connectionID = "00000000000000000000000000000000" },
			400},
		{"missing_subprotocol",
			func(q *testQuery) { q.subprotocol = "" },
			400},
		{"invalid_compression_param",
			func(q *testQuery) { q.compression = "bogus" },
			400},
		{"strict_auth_no_token",
			func(q *testQuery) { q.omitToken = true; q.strictAuth = true },
			401},
		{"invalid_token",
			func(q *testQuery) { q.token = "not.a.jwt" },
			401},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := newDefaultTestQuery()
			tc.mutate(q)
			resp := runUpgradeAttempt(t, q)
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}
}
```

`testQuery`, `newDefaultTestQuery`, `runUpgradeAttempt` are helpers that likely already exist in `protocol/test_helpers_test.go` (confirm via `rtk grep -n "runUpgradeAttempt\|newDefaultTestQuery" protocol/`). If any helper is missing, add it in the same commit — minimal implementation.

- [ ] **Step 3.2: Audit the code for any non-matching close codes**

Run: `rtk grep -n "StatusNormalClosure\|StatusProtocolError\|StatusPolicyViolation\|StatusInternalError\|CloseNormal\|CloseProtocol\|ClosePolicy\|CloseInternal" protocol/`

For each hit, check: does the surrounding condition match the parity convention below?

| Condition | Expected code |
|-----------|---------------|
| Graceful shutdown / client-initiated close | `CloseNormal` / `1000` |
| Unknown tag, malformed frame, invalid envelope, brotli unsupported | `CloseProtocol` / `1002` |
| Auth failure post-upgrade, buffer/queue overflow, flood | `ClosePolicy` / `1008` |
| Unexpected server error, panic recovery | `CloseInternal` / `1011` |

If any call site uses the wrong code (e.g., uses `1011` for a backpressure overflow), fix it AND add a targeted regression test before moving on. Do not fix and leave untested.

- [ ] **Step 3.3: Run failing test**

Run: `rtk go test ./protocol -run TestPhase1ParityCloseCode -v`
Run: `rtk go test ./protocol -run TestPhase1ParityHandshakeRejection -v`
Expected: if no audit drift was found in Step 3.2, PASS immediately. If the audit did find drift, the test may fail until Step 3.2's edits land — that is acceptable; fix and rerun.

- [ ] **Step 3.4: Run broad protocol suite**

Run: `rtk go test ./protocol`
Expected: PASS.

- [ ] **Step 3.5: Update ledger + audit docs**

Edit `docs/parity-phase0-ledger.md` `P0-PROTOCOL-003`: status → `closed`; note what the parity test now pins.

Edit `SPEC-AUDIT.md` close-code bullet: update to reflect parity-pinned state. If the original bullet already said codes were fine, mark it `[CLOSED]` with a reference to the parity test.

- [ ] **Step 3.6: Commit**

```bash
rtk git add protocol/parity_close_codes_test.go protocol/close.go protocol/disconnect.go protocol/lifecycle.go docs/parity-phase0-ledger.md SPEC-AUDIT.md
rtk git commit -m "protocol(parity): pin close-code and handshake-rejection parity — P0-PROTOCOL-003"
```

Only stage files actually modified.

---

## Task 4: Frame-Boundary Message-Family Parity Pin

**Scope discipline:** This is the most likely slice to balloon. DO NOT implement any of the following in Phase 1 — they are Phase 1.5/Phase 2:
- `TransactionUpdate` heavy/light split
- `SubscribeMulti` / `SubscribeSingle` / `QuerySetId`
- `CallReducer.flags`
- SQL-string OneOffQuery
- `ReducerCallResult` status-enum restructuring

Phase 1's job here is to make every frame-boundary divergence **explicitly visible** in the parity harness rather than silent. That means: write one parity test per intentional deferral that asserts the current behavior AND names the reference behavior it is not yet matching. Then advance the `P0-PROTOCOL-004` ledger row from `in_progress` to `closed (divergences explicit)`.

**Parity behavior being matched:** the test file names every visible message-family gap against a reference-outcome string. A future Phase 1.5/2 session can flip each test when the divergence is closed.

**Files:**
- Create: `protocol/parity_message_family_test.go`
- Modify: `docs/parity-phase0-ledger.md` `P0-PROTOCOL-004`
- Modify: `SPEC-AUDIT.md` — promote each message-family bullet to `[TRACKED]` with a pointer to its parity-pinning test.

- [ ] **Step 4.1: Enumerate the deferred divergences**

The deferrals to pin (one test each):
1. `TransactionUpdate` has no heavy/light split — Shunter has one `TransactionUpdate{TxID, Updates}` (`protocol/server_messages.go:37-40`).
2. `Subscribe` has no `QueryId`; no `SubscribeMulti`/`SubscribeSingle` variants — single tag only (`protocol/tags.go:9`, `protocol/client_messages.go:13-17`).
3. `CallReducer` has no `flags` field (`protocol/client_messages.go:29-33`).
4. `OneOffQuery` carries structured predicates, not a SQL string (`protocol/client_messages.go:36-40`).
5. `ReducerCallResult.Status` is a flat `uint8` enum, not a tagged union (`protocol/server_messages.go:49-56`).

- [ ] **Step 4.2: Write the pinning tests**

Create `protocol/parity_message_family_test.go`:

```go
package protocol

import (
	"reflect"
	"testing"
)

// These tests are *pins*, not parity implementations. Each pins the
// current Phase 1 deferral so the divergence is explicit and a later
// phase can flip the test when the divergence is closed. Each test
// documents the reference outcome in its own comment; the test body
// asserts the current (not the target) shape.

// TestPhase1DeferralTransactionUpdateNoHeavyLightSplit pins the
// intentional deferral: Shunter has a single TransactionUpdate shape,
// whereas reference/SpacetimeDB distinguishes TransactionUpdate (heavy)
// vs TransactionUpdateLight (delta-only). Flip this test when the
// split lands.
func TestPhase1DeferralTransactionUpdateNoHeavyLightSplit(t *testing.T) {
	fields := structFieldNames(TransactionUpdate{})
	// Current shape: TxID, Updates. No caller-side reducer metadata
	// on the wire object.
	want := []string{"TxID", "Updates"}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("TransactionUpdate fields = %v, want %v (reference has heavy/light split — deferral)",
			fields, want)
	}
}

// TestPhase1DeferralSubscribeNoQueryIdOrMultiVariants pins the
// deferral: Shunter uses a single Subscribe message with a structured
// query list; reference/SpacetimeDB exposes SubscribeSingle /
// SubscribeMulti variants with a QueryId. Flip when grouping lands.
func TestPhase1DeferralSubscribeNoQueryIdOrMultiVariants(t *testing.T) {
	if TagSubscribe != 1 {
		t.Errorf("TagSubscribe = %d, want 1 (Phase 1 deferral: no Multi/Single split)",
			TagSubscribe)
	}
	fields := structFieldNames(SubscribeMessage{})
	if containsString(fields, "QueryId") {
		t.Fatal("SubscribeMessage has QueryId — deferral has been closed; update this test and the P0-PROTOCOL-004 ledger row")
	}
}

// TestPhase1DeferralCallReducerNoFlagsField pins the deferral:
// reference/SpacetimeDB CallReducer carries a flags field
// (e.g., NoSuccessfulUpdate). Shunter does not. Flip when flags land.
func TestPhase1DeferralCallReducerNoFlagsField(t *testing.T) {
	fields := structFieldNames(CallReducerMessage{})
	if containsString(fields, "Flags") {
		t.Fatal("CallReducerMessage has Flags — deferral closed; update this test and the P0-PROTOCOL-004 ledger row")
	}
}

// TestPhase1DeferralOneOffQueryStructuredNotSQL pins the deferral:
// reference uses a SQL string; Shunter uses structured predicates.
// Flip when the SQL front door lands (Phase 2 Slice 1).
func TestPhase1DeferralOneOffQueryStructuredNotSQL(t *testing.T) {
	fields := structFieldNames(OneOffQueryMessage{})
	if containsString(fields, "QueryString") || containsString(fields, "SQL") {
		t.Fatal("OneOffQueryMessage carries a SQL string — deferral closed; update this test and the P0-PROTOCOL-004 ledger row")
	}
}

// TestPhase1DeferralReducerCallResultFlatStatus pins the deferral:
// reference uses a tagged union (UpdateStatus); Shunter uses a flat
// uint8. Flip when the enum is restructured.
func TestPhase1DeferralReducerCallResultFlatStatus(t *testing.T) {
	var r ReducerCallResult
	if reflect.TypeOf(r.Status).Kind() != reflect.Uint8 {
		t.Errorf("ReducerCallResult.Status kind = %v, want Uint8 (deferral)",
			reflect.TypeOf(r.Status).Kind())
	}
}

func structFieldNames(v any) []string {
	t := reflect.TypeOf(v)
	names := make([]string, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		names[i] = t.Field(i).Name
	}
	return names
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
```

**IMPORTANT:** before writing the test, run `rtk grep -n "type TransactionUpdate \|type SubscribeMessage \|type CallReducerMessage \|type OneOffQueryMessage \|type ReducerCallResult " protocol/` to confirm the exact Go type names. The Explore agent reported `TransactionUpdate` and `ReducerCallResult` directly but may have abbreviated others. Use the real names — do not invent identifiers.

- [ ] **Step 4.3: Run the pins and confirm they reflect reality**

Run: `rtk go test ./protocol -run TestPhase1Deferral -v`
Expected: PASS. Any FAIL means the current shape does not match the pin — fix the pin to reflect reality, not the other way around. Phase 1 is not changing these types.

- [ ] **Step 4.4: Run broad suite**

Run: `rtk go test ./protocol`
Run: `rtk go test ./...`
Expected: PASS.

- [ ] **Step 4.5: Update docs**

Edit `docs/parity-phase0-ledger.md` `P0-PROTOCOL-004`:
- status → `closed (divergences explicit)`
- Next slice note → "Phase 1 closed: five message-family deferrals are each pinned by a named parity test in `protocol/parity_message_family_test.go`. Phase 1.5 closes the `TransactionUpdate`/`ReducerCallResult` pair; Phase 2 Slice 2 closes `SubscribeMulti`/`SubscribeSingle`/`QueryId`; Phase 2 Slice 1 closes SQL OneOffQuery; `CallReducer.flags` tracks with Phase 1.5."

Edit `SPEC-AUDIT.md`: for each of the five bullets, append `[TRACKED — pinned by protocol/parity_message_family_test.go::<test name>]`. This makes every deferral explicit per roadmap Rule 4 ("do not leave divergences implicit").

- [ ] **Step 4.6: Commit**

```bash
rtk git add protocol/parity_message_family_test.go docs/parity-phase0-ledger.md SPEC-AUDIT.md
rtk git commit -m "protocol(parity): pin message-family deferrals explicitly — P0-PROTOCOL-004"
```

---

## Post-Flight

- [ ] **Step F.1: Run the full suite one last time**

Run: `rtk go test ./...`
Expected: all packages green.

- [ ] **Step F.2: Update `docs/current-status.md` test count**

Edit the line that currently says `919 passed in 9 packages` / `920 passed in 9 packages` to whatever the final suite reports. Keep the wording identical except the number.

- [ ] **Step F.3: Refresh the next-session handoff**

Overwrite `NEXT-SESSION-PROMPT.md`. The new prompt should:
- record that Phase 1 wire-level parity landed (all four `P0-PROTOCOL-00*` slices are `closed` or `closed (divergences explicit)`)
- point the next session at Phase 1.5 (end-to-end delivery parity: `ReducerCallResult`/`TransactionUpdate` model decision + caller/non-caller routing) per roadmap §3 Phase 1.5 and §4 Slice 3
- list `subscription/phase0_parity_test.go` + the new `protocol/parity_*_test.go` files as the current parity harness anchors
- keep the Phase 0 harness frozen — do not re-open

Match the writing style of the current prompt (direct, prescriptive, with a "Required reading order" list, "Hard rules", and a "Stop rule").

- [ ] **Step F.4: Commit the docs sweep**

```bash
rtk git add docs/current-status.md NEXT-SESSION-PROMPT.md
rtk git commit -m "docs(parity): record Phase 1 wire-level closure and point next session at Phase 1.5"
```

- [ ] **Step F.5: Stop**

Do not start Phase 1.5 in the same session. Per prompt stop rule. Report completion to the user with: slices landed, suite result, and the next-session path.

---

## Self-Review

**Spec coverage (against roadmap §3 Phase 1 and prompt's priority list):**
- ✅ Slice 1 Subprotocol decision — Task 1
- ✅ Slice 2 Compression-envelope/tag parity — Task 2
- ✅ Slice 3 Handshake/close-code alignment — Task 3
- ✅ Slice 4 Message-family cleanup at frame boundary — Task 4 (pinned-as-deferred, not implemented — matches Phase 1 scope)

**Placeholder scan:** No "TBD"/"similar to Task N"/"appropriate error handling". Every code block is concrete. One deliberate "look it up before editing" step in Task 4 Step 4.2 around exact Go type names — that is a *verification directive*, not a placeholder, because the Explore agent's inventory may have abbreviated names.

**Type consistency:**
- `SubprotocolReference` constant name used consistently between Task 1 test and implementation.
- `CompressionBrotli`, `ErrBrotliUnsupported` used consistently across Task 2 test, constant, and error definitions.
- `structFieldNames` / `containsString` helpers defined inline in Task 4.

**Scope discipline check:** No task implements `TransactionUpdate` heavy/light, `SubscribeMulti`, `CallReducer.flags`, SQL OneOffQuery, or `ReducerCallResult` enum restructure. All five are pinned-as-deferred in Task 4, matching the prompt's "do not widen into Phase 1.5" rule.
