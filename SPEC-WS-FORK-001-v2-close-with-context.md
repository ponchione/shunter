# SPEC-WS-FORK-001 v2: CloseWithContext — Build-Agent-Ready

Status: Ready for execution
Supersedes: SPEC-WS-FORK-001 v1
Scope: This is an internal fork for Shunter. We are not upstreaming. The fork lives on Mitchell's GitHub; Shunter pins it via a `replace` directive in `go.mod`. No GitHub issue, no upstream PR, no maintainer coordination.
Upstream pin: `coder/websocket@d099e16` (commit `d099e1621e8d0f080c9f3b87c5a8587e0b722723`, message: "docs: fix roadmap links (#558)")
Target module: `github.com/coder/websocket` (keep upstream module path unchanged in fork's go.mod — consumers use `replace`)
Agent contract: This document is the sole source of truth. Every WO has atomic diffs, exact commands, deterministic acceptance criteria, and a stop condition.

---

## 0. Global invariants the agent MUST preserve

1. `Close(code, reason)` observable behavior MUST be byte-identical to upstream after every WO.
2. `CloseNow()` observable behavior MUST be byte-identical to upstream after every WO.
3. `go test -race -count=1 ./...` MUST pass after every WO commit.
4. No new module dependencies in `go.mod`. Tests may use only the existing test dependency set (see WO-4 imports).
5. No changes to any file outside `close.go`, `README.md`, and one new `*_test.go` file. If the agent believes another file needs to change, it MUST stop and surface the reason — do not improvise.
6. Module path in `go.mod` stays `github.com/coder/websocket`. Do not rename. Keeps the option open to drop the fork later if upstream adds an equivalent primitive.
7. Every commit message follows Conventional Commits: `feat:`, `refactor:`, `test:`, `docs:`.

## 0.1 Global stop conditions

STOP and surface the problem to the human (do not patch over) if any of these occur:

- `git status` shows modifications to a file not listed in the current WO's "Files touched"
- `go vet ./...` reports new findings
- `go test -race -count=1 ./...` regresses vs WO-1 baseline
- A diff in this spec does not apply cleanly (context lines don't match); do not force
- New or existing goroutine leak in WO-5 exceeds the documented budget

---

## 1. Pre-flight environment

Before WO-1, verify the execution environment:

```bash
go version                   # expect go1.23 or newer (matches go.mod)
git --version                # any recent
cd <fork-root>
git remote -v                # expect `origin` points to Mitchell's fork
git remote add upstream https://github.com/coder/websocket.git 2>/dev/null || true
git fetch upstream
git checkout -b feat/close-with-context upstream/master
```

Confirm baseline commit:

```bash
git log -1 --format='%H'     # record this; must match pre-flight baseline
```

If `go.mod` declares a module path other than `github.com/coder/websocket`, STOP — this spec assumes upstream module path is preserved.

---

## WO-1: Baseline verification

Purpose: Establish that the fork compiles and tests green BEFORE any changes, so later regressions are attributable to our changes.

### Files touched

None.

### Commands

```bash
go build ./...
go vet ./...
go test -race -count=1 -timeout=120s ./...
```

### Acceptance

- `go build ./...` exit 0, no output
- `go vet ./...` exit 0, no output
- `go test -race -count=1 -timeout=120s ./...` exit 0, final line contains `ok` for package `github.com/coder/websocket`

### Artifact

Append to `.fork-notes.md` (create if missing):

```
## WO-1 baseline
- Commit: <hash from pre-flight>
- go version: <output of `go version`>
- test duration: <observed>
- test outcome: PASS
```

### Stop condition

If any of the three commands fail at baseline, STOP. The problem is upstream, not ours. Do not proceed to WO-2.

---

## WO-2: Internal refactor — additive ctx plumbing

Purpose: Pipe `context.Context` through the close handshake helpers without changing any observable behavior. Strictly additive: `writeClose(code, reason)` is kept as a backward-compat wrapper so the two non-handshake call sites in `write.go` and `read.go` are NOT touched.

### Files touched

- `close.go` — only this file

### Unified diff

Apply exactly as shown. Context lines must match.

```diff
--- a/close.go
+++ b/close.go
@@ -110,7 +110,9 @@ func (c *Conn) Close(code StatusCode, reason string) (err error) {
 		}
 	}()
 
-	err = c.closeHandshake(code, reason)
+	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
+	defer cancel()
+	err = c.closeHandshake(ctx, code, reason)
 
 	err2 := c.close()
 	if err == nil && err2 != nil {
@@ -154,19 +156,27 @@ func (c *Conn) CloseNow() (err error) {
 	return err
 }
 
-func (c *Conn) closeHandshake(code StatusCode, reason string) error {
-	err := c.writeClose(code, reason)
+func (c *Conn) closeHandshake(ctx context.Context, code StatusCode, reason string) error {
+	err := c.writeCloseCtx(ctx, code, reason)
 	if err != nil {
 		return err
 	}
 
-	err = c.waitCloseHandshake()
+	err = c.waitCloseHandshake(ctx)
 	if CloseStatus(err) != code {
 		return err
 	}
 	return nil
 }
 
+// writeClose preserves the original signature used by non-handshake callers
+// (writeError in write.go, peer-initiated close echo in read.go). It wraps
+// writeCloseCtx with the historical 5-second default timeout.
 func (c *Conn) writeClose(code StatusCode, reason string) error {
+	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
+	defer cancel()
+	return c.writeCloseCtx(ctx, code, reason)
+}
+
+func (c *Conn) writeCloseCtx(ctx context.Context, code StatusCode, reason string) error {
 	ce := CloseError{
 		Code:   code,
 		Reason: reason,
@@ -181,9 +191,6 @@ func (c *Conn) writeClose(code StatusCode, reason string) error {
 		}
 	}
 
-	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
-	defer cancel()
-
 	err = c.writeControl(ctx, opClose, p)
 	// If the connection closed as we're writing we ignore the error as we might
 	// have written the close frame, the peer responded and then someone else read it
@@ -194,10 +201,7 @@ func (c *Conn) writeClose(code StatusCode, reason string) error {
 	return nil
 }
 
-func (c *Conn) waitCloseHandshake() error {
-	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
-	defer cancel()
-
+func (c *Conn) waitCloseHandshake(ctx context.Context) error {
 	err := c.readMu.lock(ctx)
 	if err != nil {
 		return err
```

### Verification

```bash
# No files changed outside close.go
git status --porcelain | awk '{print $2}' | sort > /tmp/ws-fork-changed.txt
echo "close.go" > /tmp/ws-fork-expected.txt
diff /tmp/ws-fork-expected.txt /tmp/ws-fork-changed.txt

# Still compiles and tests pass — behavior unchanged
go build ./...
go vet ./...
go test -race -count=1 -timeout=120s ./...
```

### Acceptance

- `diff` at verification step exits 0 (only `close.go` changed)
- All three go commands exit 0
- Close-related tests still pass: `go test -race -count=1 -run 'TestConn|TestClose' ./...`

### Commit

```bash
git add close.go
git commit -m "refactor: pipe context through internal close-handshake helpers

Additive refactor that plumbs context.Context through closeHandshake,
waitCloseHandshake, and a new writeCloseCtx helper. writeClose is kept
as a thin wrapper so non-handshake callers in write.go and read.go are
untouched. No observable behavior change for existing callers of Close()
or CloseNow(): the 5s timeouts are now constructed in Close() and in
the writeClose wrapper instead of inside the helpers."
```

### Stop condition

If `go test` regresses, revert the commit (`git reset --hard HEAD~1`) and re-examine the diff. Do not proceed.

---

## WO-3: New public method `CloseWithContext`

Purpose: Add the caller-controlled close primitive. Owns the `casClosing()` gate from the start; guarantees transport death on return regardless of peer behavior.

### Files touched

- `close.go`

### Error-priority contract

The switch below is the entire behavior contract. Verify by reading it before applying the diff.

| Scenario                              | writeErr                 | waitErr                  | Returned err (after defer) |
|---------------------------------------|--------------------------|--------------------------|----------------------------|
| Peer responsive, handshake completes  | nil                      | `CloseError{Code: code}` | nil                        |
| Ctx already canceled at call time     | wraps `context.Canceled` | wraps `context.Canceled` | wraps `context.Canceled`   |
| Ctx deadline expires during wait      | nil                      | wraps `DeadlineExceeded` | wraps `DeadlineExceeded`   |
| Ctx canceled mid-wait                 | nil                      | wraps `context.Canceled` | wraps `context.Canceled`   |
| Transport already dead (gate loser)   | (short-circuits at gate) | —                        | nil (`net.ErrClosed` swallowed) |

The outer defer swallows `net.ErrClosed` only. Context errors surface so callers can detect forced teardown via `errors.Is(err, context.DeadlineExceeded)` / `errors.Is(err, context.Canceled)`.

### Unified diff

Insert the new method immediately after `Close()` (before `CloseNow`). Apply exactly:

```diff
--- a/close.go
+++ b/close.go
@@ -128,6 +128,70 @@ func (c *Conn) Close(code StatusCode, reason string) (err error) {
 	return err
 }
 
+// CloseWithContext performs the WebSocket close handshake under caller-controlled
+// cancellation. It writes a Close frame, then waits for the peer's Close frame
+// until ctx is done. If ctx expires or is cancelled before the peer responds,
+// the underlying transport is forcibly torn down and CloseWithContext returns.
+//
+// Unlike Close, the caller owns the timeout. This method guarantees that the
+// underlying transport is unusable by the time it returns, even if the peer
+// never responds. Existing Close and CloseNow semantics are unchanged.
+//
+// The connection can only be closed once. If Close, CloseNow, or CloseWithContext
+// has already been called, this returns after existing close goroutines have
+// unwound.
+//
+// The maximum length of reason must be 125 bytes. Avoid sending a dynamic reason.
+//
+// CloseWithContext will unblock all goroutines interacting with the connection
+// once complete.
+func (c *Conn) CloseWithContext(ctx context.Context, code StatusCode, reason string) (err error) {
+	defer errd.Wrap(&err, "failed to close WebSocket")
+
+	if c.casClosing() {
+		err = c.waitGoroutines()
+		if err != nil {
+			return err
+		}
+		return net.ErrClosed
+	}
+	defer func() {
+		if errors.Is(err, net.ErrClosed) {
+			err = nil
+		}
+	}()
+
+	// Best-effort close frame write under caller's ctx. If this fails we still
+	// proceed to forced teardown so callers get the transport-death guarantee.
+	writeErr := c.writeCloseCtx(ctx, code, reason)
+
+	// Wait for peer close under caller's ctx. On ctx expiry this returns
+	// promptly with a ctx error and we proceed to forced teardown.
+	waitErr := c.waitCloseHandshake(ctx)
+
+	// Forced transport teardown. c.close() is idempotent (guarded by isClosed),
+	// so this is safe even on the happy path where the handshake completed.
+	closeErr := c.close()
+
+	// Final goroutine-unwinding safety net (15s hardcoded upstream; unchanged).
+	gErr := c.waitGoroutines()
+
+	// Error priority:
+	//   1. write error (most actionable — tells caller the frame didn't go out)
+	//   2. wait error, if it isn't the expected peer Close with matching code
+	//      (ctx.DeadlineExceeded and ctx.Canceled surface here in the forced
+	//      teardown path)
+	//   3. close error
+	//   4. goroutine-unwind error
+	switch {
+	case writeErr != nil && !errors.Is(writeErr, net.ErrClosed):
+		return writeErr
+	case waitErr != nil && CloseStatus(waitErr) != code:
+		return waitErr
+	case closeErr != nil:
+		return closeErr
+	default:
+		return gErr
+	}
+}
+
 // CloseNow closes the WebSocket connection without attempting a close handshake.
 // Use when you do not want the overhead of the close handshake.
 func (c *Conn) CloseNow() (err error) {
```

### Verification

```bash
# Only close.go changed
git status --porcelain | awk '{print $2}' | sort > /tmp/ws-fork-changed.txt
echo "close.go" > /tmp/ws-fork-expected.txt
diff /tmp/ws-fork-expected.txt /tmp/ws-fork-changed.txt

# Compiles, vets, and tests pass
go build ./...
go vet ./...
go test -race -count=1 -timeout=120s ./...

# Public API surface includes the new method
go doc github.com/coder/websocket Conn.CloseWithContext | grep -q "CloseWithContext"
```

### Acceptance

- `diff` exits 0
- All go commands exit 0
- `go doc` grep exits 0
- `CloseWithContext` is exported on `*Conn`

### Commit

```bash
git add close.go
git commit -m "feat: add Conn.CloseWithContext for caller-controlled close timeout

Adds a public method that drives the WebSocket close handshake under
a caller-supplied context. On context expiry or cancellation, the
underlying transport is forcibly torn down — the method guarantees
transport death on return regardless of peer responsiveness.

Existing Close and CloseNow semantics are unchanged. CloseWithContext
takes the same casClosing() gate first so it never races with
Close's lifecycle.

Motivation: the existing Close() method uses two hardcoded 5-second
timeouts (writeControl + waitCloseHandshake) and CloseNow() cannot
preempt an in-flight Close. This leaves no public API for bounded
close-handshake teardown against unresponsive peers."
```

### Stop condition

If `go test` fails, inspect the failure. Most likely causes:
- Typo in the switch statement → check `CloseStatus(waitErr) != code` vs `== code`
- Missing import (none should be needed — `errors`, `net`, `context`, `time`, `errd` are already imported)
Revert and re-apply if unsure.

---

## WO-4: Behavior tests

Purpose: Prove the four behavior guarantees of `CloseWithContext`: responsive path, unresponsive path, repeat-call idempotency, cancellation. Error expectations are hard assertions — no soft warnings. The error-priority table in WO-3 is the contract; if reality disagrees, WO-3 is wrong, not these tests.

### Files touched

- `close_with_context_test.go` — new file, created at repo root

### Complete file contents

Create `close_with_context_test.go` with exactly the following. Package is `websocket_test` (black-box tests use public API only).

````go
//go:build !js

package websocket_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/internal/test/assert"
	"github.com/coder/websocket/internal/xsync"
)

// --- Test helpers --------------------------------------------------------

// unresponsivePeerServer returns an httptest server whose handler accepts a
// WebSocket connection but never reads from it, never writes to it, and never
// closes it. This models a peer that does not answer a close handshake.
//
// Real TCP is required (not wstest.Pipe) because net.Pipe is synchronous —
// the close-frame flush itself would block on a peer that never reads. Under
// httptest.NewServer, the kernel TCP buffer absorbs the tiny close frame, so
// only the handshake wait exercises the new timeout behavior.
func unresponsivePeerServer(t testing.TB) (wsURL string, cleanup func()) {
	t.Helper()
	release := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("server accept: %v", err)
			return
		}
		<-release
		_ = c.CloseNow()
	}))
	return strings.Replace(s.URL, "http://", "ws://", 1), func() {
		close(release)
		s.Close()
	}
}

// responsivePeerServer returns an httptest server whose handler invokes
// CloseRead. CloseRead runs a reader goroutine that auto-responds to close
// frames, so the handshake completes promptly from the client's side.
func responsivePeerServer(t testing.TB) (wsURL string, cleanup func()) {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("server accept: %v", err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		ctx = c.CloseRead(ctx)
		<-ctx.Done()
	}))
	return strings.Replace(s.URL, "http://", "ws://", 1), s.Close
}

func dialTest(t testing.TB, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, nil)
	assert.Success(t, err)
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

// --- Behavior tests ------------------------------------------------------

// TestCloseWithContext_ResponsivePeer: peer echoes Close; method returns
// promptly with nil error; transport is dead after return.
func TestCloseWithContext_ResponsivePeer(t *testing.T) {
	t.Parallel()

	url, cleanup := responsivePeerServer(t)
	defer cleanup()

	c := dialTest(t, url)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	err := c.CloseWithContext(ctx, websocket.StatusNormalClosure, "")
	elapsed := time.Since(start)

	assert.Success(t, err)
	if elapsed > 2*time.Second {
		t.Fatalf("responsive-peer close took %v, expected <2s", elapsed)
	}

	// Transport is dead: Write returns a non-nil error.
	werr := c.Write(context.Background(), websocket.MessageText, []byte("x"))
	assert.Error(t, werr)

	// Gate is taken: CloseNow is a no-op and returns nil.
	assert.Success(t, c.CloseNow())
}

// TestCloseWithContext_UnresponsivePeer_ForcedTeardown: peer never responds;
// method returns inside the ctx budget (not the pre-fork 5s tax); transport
// is dead; error wraps context.DeadlineExceeded.
func TestCloseWithContext_UnresponsivePeer_ForcedTeardown(t *testing.T) {
	t.Parallel()

	url, cleanup := unresponsivePeerServer(t)
	defer cleanup()

	c := dialTest(t, url)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := c.CloseWithContext(ctx, websocket.StatusNormalClosure, "")
	elapsed := time.Since(start)

	// Budget = ctx (100ms) + small goroutine-unwind budget. Pre-fork behavior
	// would pay the hardcoded 5s waitCloseHandshake tax here, blowing past 500ms.
	if elapsed > 500*time.Millisecond {
		t.Fatalf("unresponsive-peer close took %v, expected <500ms (ctx was 100ms)", elapsed)
	}

	// Hard assertion per WO-3 contract: unresponsive peer + deadline-based ctx
	// returns a non-nil error wrapping context.DeadlineExceeded.
	if err == nil {
		t.Fatal("expected wrapped DeadlineExceeded, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected wrapped DeadlineExceeded, got %v", err)
	}

	// Transport is dead.
	werr := c.Write(context.Background(), websocket.MessageText, []byte("x"))
	assert.Error(t, werr)
}

// TestCloseWithContext_RepeatCallsAreNoOps: first call wins the gate; all
// subsequent Close*/CloseNow/CloseWithContext calls return nil.
func TestCloseWithContext_RepeatCallsAreNoOps(t *testing.T) {
	t.Parallel()

	url, cleanup := responsivePeerServer(t)
	defer cleanup()

	c := dialTest(t, url)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	assert.Success(t, c.CloseWithContext(ctx, websocket.StatusNormalClosure, ""))
	assert.Success(t, c.CloseWithContext(ctx, websocket.StatusNormalClosure, ""))
	assert.Success(t, c.Close(websocket.StatusNormalClosure, ""))
	assert.Success(t, c.CloseNow())
}

// TestCloseWithContext_CancellationForcesTeardown: cancel() mid-wait unwinds
// the method and tears down the transport within the same budget as a
// deadline-based ctx.
func TestCloseWithContext_CancellationForcesTeardown(t *testing.T) {
	t.Parallel()

	url, cleanup := unresponsivePeerServer(t)
	defer cleanup()

	c := dialTest(t, url)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := xsync.Go(func() error {
		return c.CloseWithContext(ctx, websocket.StatusNormalClosure, "")
	})

	// Let writeClose complete, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancelStart := time.Now()
	cancel()

	select {
	case err := <-done:
		elapsed := time.Since(cancelStart)
		if elapsed > 500*time.Millisecond {
			t.Fatalf("CloseWithContext did not unwind after cancel within 500ms (took %v)", elapsed)
		}
		if err == nil {
			t.Fatal("expected wrapped context.Canceled, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected wrapped context.Canceled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CloseWithContext did not return after cancel")
	}

	werr := c.Write(context.Background(), websocket.MessageText, []byte("x"))
	assert.Error(t, werr)
}

// TestCloseWithContext_ConcurrentCloseNow: gate is taken by CloseWithContext;
// later CloseNow falls through to waitGoroutines and returns nil; neither hangs.
func TestCloseWithContext_ConcurrentCloseNow(t *testing.T) {
	t.Parallel()

	url, cleanup := unresponsivePeerServer(t)
	defer cleanup()

	c := dialTest(t, url)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var started atomic.Int32
	errA := xsync.Go(func() error {
		started.Add(1)
		return c.CloseWithContext(ctx, websocket.StatusNormalClosure, "")
	})
	// Give A time to win the gate and start its write.
	time.Sleep(20 * time.Millisecond)
	errB := xsync.Go(func() error {
		started.Add(1)
		return c.CloseNow()
	})

	select {
	case <-errA:
		// Any return value is acceptable; we only assert no hang.
	case <-time.After(3 * time.Second):
		t.Fatal("CloseWithContext hung")
	}
	select {
	case err := <-errB:
		// CloseNow as gate loser: returns net.ErrClosed which is swallowed to nil.
		assert.Success(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("CloseNow hung behind gate")
	}
	if started.Load() != 2 {
		t.Fatalf("expected both goroutines to run, got %d", started.Load())
	}
}

// TestCloseWithContext_AlreadyExpiredContext: ctx already expired at call
// time; method returns promptly with transport torn down; error wraps
// context.Canceled (writeCloseCtx short-circuits first via the switch's
// first branch).
func TestCloseWithContext_AlreadyExpiredContext(t *testing.T) {
	t.Parallel()

	url, cleanup := unresponsivePeerServer(t)
	defer cleanup()

	c := dialTest(t, url)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // expire immediately

	start := time.Now()
	err := c.CloseWithContext(ctx, websocket.StatusNormalClosure, "")
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Fatalf("expired-ctx close took %v, expected <500ms", elapsed)
	}
	if err == nil {
		t.Fatal("expected wrapped context.Canceled, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected wrapped context.Canceled, got %v", err)
	}

	werr := c.Write(context.Background(), websocket.MessageText, []byte("x"))
	assert.Error(t, werr)
}

// Keep runtime import used (WO-5 adds a goroutine-leak test that uses it;
// this var prevents import-removal during WO-4 commit when goimports runs).
var _ = runtime.NumGoroutine
````

### Verification

```bash
# File exists and is the only new file
test -f close_with_context_test.go

# Compiles
go vet ./...

# Targeted tests pass
go test -race -count=1 -timeout=60s -run '^TestCloseWithContext_' .

# Tests are stable: 50 repeat runs all green
go test -race -count=50 -timeout=300s -run '^TestCloseWithContext_' .

# Full suite still green
go test -race -count=1 -timeout=120s ./...
```

### Acceptance

- All four verification commands exit 0
- `go test -count=50` shows `PASS` on every run
- Each individual test completes in under 3 seconds

### Commit

```bash
git add close_with_context_test.go
git commit -m "test: add behavior tests for CloseWithContext

Covers responsive peer, unresponsive peer with forced teardown,
repeat-call idempotency, mid-wait cancellation, concurrent close,
and pre-expired context. All error expectations are hard assertions
matching the WO-3 error-priority contract.

Uses httptest.NewServer (real TCP) instead of wstest.Pipe for
unresponsive-peer tests because net.Pipe is synchronous and would
block the close frame flush itself."
```

### Stop condition

- If `TestCloseWithContext_UnresponsivePeer_ForcedTeardown` observes >500ms: the forced teardown path is not triggering. Check WO-3's switch statement and the unconditional `c.close()` call.
- If any hard error-type assertion fails: the WO-3 error-priority contract is wrong. Re-read the table in WO-3 and the switch; do not patch the test to make it pass. Fix WO-3.
- If the 50-repeat run flakes: investigate before committing. Flakiness here indicates a real race, not a test problem.

---

## WO-5: Race and goroutine-leak coverage

Purpose: Exercise `CloseWithContext` with concurrent readers, writers, and high-churn workloads to surface races, deadlocks, or goroutine leaks.

### Files touched

- `close_with_context_test.go` — append tests to the existing file

### Append to close_with_context_test.go

Add the following at the end of the file (after the `var _ = runtime.NumGoroutine` line; remove that line, it is no longer needed once the real uses below are added):

````go
// TestCloseWithContext_Race_ActiveReader: a blocked Read on the same conn
// must unwind when CloseWithContext forces teardown.
func TestCloseWithContext_Race_ActiveReader(t *testing.T) {
	t.Parallel()

	url, cleanup := unresponsivePeerServer(t)
	defer cleanup()

	c := dialTest(t, url)

	readerDone := xsync.Go(func() error {
		readCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _, err := c.Read(readCtx)
		return err
	})

	// Let reader block on the underlying read.
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_ = c.CloseWithContext(ctx, websocket.StatusNormalClosure, "")
	if elapsed := time.Since(start); elapsed > 600*time.Millisecond {
		t.Fatalf("CloseWithContext hung with active reader: %v", elapsed)
	}

	select {
	case err := <-readerDone:
		if err == nil {
			t.Fatal("expected reader to error after forced teardown, got nil")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("reader did not unwind after forced teardown")
	}
}

// TestCloseWithContext_Race_ActiveWriter: a concurrent writer loop must fail
// fast when CloseWithContext tears down the transport.
func TestCloseWithContext_Race_ActiveWriter(t *testing.T) {
	t.Parallel()

	url, cleanup := unresponsivePeerServer(t)
	defer cleanup()

	c := dialTest(t, url)

	writerStop := make(chan struct{})
	writerDone := xsync.Go(func() error {
		payload := []byte("hello world")
		for {
			select {
			case <-writerStop:
				return nil
			default:
			}
			wctx, wcancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			err := c.Write(wctx, websocket.MessageText, payload)
			wcancel()
			if err != nil {
				return err
			}
		}
	})

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_ = c.CloseWithContext(ctx, websocket.StatusNormalClosure, "")
	if elapsed := time.Since(start); elapsed > 600*time.Millisecond {
		close(writerStop)
		t.Fatalf("CloseWithContext hung with active writer: %v", elapsed)
	}

	close(writerStop)
	select {
	case <-writerDone:
	case <-time.After(1 * time.Second):
		t.Fatal("writer did not unwind after forced teardown")
	}
}

// TestCloseWithContext_Race_ActivePing: a Ping awaiting a pong must unwind
// when CloseWithContext forces teardown.
func TestCloseWithContext_Race_ActivePing(t *testing.T) {
	t.Parallel()

	url, cleanup := unresponsivePeerServer(t)
	defer cleanup()

	c := dialTest(t, url)

	pingDone := xsync.Go(func() error {
		pctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return c.Ping(pctx)
	})

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_ = c.CloseWithContext(ctx, websocket.StatusNormalClosure, "")
	if elapsed := time.Since(start); elapsed > 600*time.Millisecond {
		t.Fatalf("CloseWithContext hung with active ping: %v", elapsed)
	}

	select {
	case err := <-pingDone:
		if err == nil {
			t.Fatal("expected ping to error after forced teardown, got nil")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("ping did not unwind after forced teardown")
	}
}

// TestCloseWithContext_NoGoroutineLeaks: repeated open/close cycles must not
// leak goroutines. No external dependency — manual NumGoroutine comparison
// with a settling GC pass.
func TestCloseWithContext_NoGoroutineLeaks(t *testing.T) {
	t.Parallel()

	url, cleanup := unresponsivePeerServer(t)
	defer cleanup()

	// Warm up: first connection brings up httptest, HTTP transport pools, etc.
	// Measure the steady-state delta starting from the second iteration.
	{
		c := dialTest(t, url)
		cctx, ccancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_ = c.CloseWithContext(cctx, websocket.StatusNormalClosure, "")
		ccancel()
	}
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	before := runtime.NumGoroutine()

	const iterations = 20
	for i := 0; i < iterations; i++ {
		c := dialTest(t, url)
		cctx, ccancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_ = c.CloseWithContext(cctx, websocket.StatusNormalClosure, "")
		ccancel()
	}

	// Allow stragglers to finish.
	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	after := runtime.NumGoroutine()
	// Budget: up to 10 lingering goroutines (http transport keepalives,
	// stalled-server goroutines from prior iterations, etc.). A real leak
	// would scale with iterations (20+).
	if after-before > 10 {
		t.Fatalf("goroutine leak after %d iterations: before=%d after=%d (delta %d)",
			iterations, before, after, after-before)
	}
}

// TestCloseWithContext_Stress_RapidCycles: sanity check that we're not
// paying the pre-fork 5s hardcoded tax per iteration.
func TestCloseWithContext_Stress_RapidCycles(t *testing.T) {
	t.Parallel()

	url, cleanup := unresponsivePeerServer(t)
	defer cleanup()

	const iterations = 30
	start := time.Now()
	for i := 0; i < iterations; i++ {
		c := dialTest(t, url)
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		_ = c.CloseWithContext(cctx, websocket.StatusNormalClosure, "")
		ccancel()
	}
	elapsed := time.Since(start)

	// Budget: 30 iterations * (30ms ctx + ~50ms unwind) = ~2.4s nominal.
	// Pre-fork would pay 5s per iteration = 150s+. We cap at 15s for CI headroom.
	if elapsed > 15*time.Second {
		t.Fatalf("stress run too slow: %v for %d iterations (would be <5s post-fork, >150s pre-fork)",
			elapsed, iterations)
	}
}
````

Also remove the sentinel line at the bottom of the file (`var _ = runtime.NumGoroutine`) — it's no longer needed because `runtime.NumGoroutine` is now used for real.

### Verification

```bash
# File is the only changed file
git status --porcelain | awk '{print $2}' | sort > /tmp/ws-fork-changed.txt
echo "close_with_context_test.go" > /tmp/ws-fork-expected.txt
diff /tmp/ws-fork-expected.txt /tmp/ws-fork-changed.txt

go vet ./...

# Race tests pass
go test -race -count=1 -timeout=120s -run '^TestCloseWithContext_Race_|^TestCloseWithContext_NoGoroutineLeaks|^TestCloseWithContext_Stress_' .

# 10 repeat runs for race detector stability
go test -race -count=10 -timeout=300s -run '^TestCloseWithContext_' .

# Full suite
go test -race -count=1 -timeout=120s ./...
```

### Acceptance

- All verification commands exit 0
- `-count=10` repeat shows every run PASS
- `TestCloseWithContext_NoGoroutineLeaks` consistently reports delta ≤ 10
- `TestCloseWithContext_Stress_RapidCycles` completes in under 15s

### Commit

```bash
git add close_with_context_test.go
git commit -m "test: add race and leak coverage for CloseWithContext

Concurrent reader, writer, and ping paths must all unwind when
CloseWithContext forces teardown. Goroutine-leak guard via manual
NumGoroutine comparison avoids adding a goleak dependency — keeps
the fork dependency-free.

Stress test verifies we're not paying the pre-fork 5s-per-iteration
timeout tax."
```

### Stop condition

- Goroutine delta > 10: investigate. Leak is likely in the teardown path or in the forced-close interaction with `closeRead` if any consumer was using it.
- Stress test >15s: the method is not actually bounding to the ctx budget. Re-check WO-3.
- `-count=10` flakes: real race. Do not commit. Gather failing output with `go test -race -count=10 -v` and surface.

---

## WO-6: Documentation

Purpose: Make the new method discoverable via godoc and README when we (or future-us) read the fork.

### Files touched

- `README.md`

### README diff

Insert a new Highlights bullet. Apply this diff:

```diff
--- a/README.md
+++ b/README.md
@@ -27,6 +27,7 @@ websocket is a minimal and idiomatic WebSocket library for Go.
 - [Close handshake](https://pkg.go.dev/github.com/coder/websocket#Conn.Close)
+- [CloseWithContext](https://pkg.go.dev/github.com/coder/websocket#Conn.CloseWithContext) for caller-controlled close-handshake timeouts with guaranteed transport teardown (Shunter fork)
 - [net.Conn](https://pkg.go.dev/github.com/coder/websocket#NetConn) wrapper
 - [Ping pong](https://pkg.go.dev/github.com/coder/websocket#Conn.Ping) API
 - [RFC 7692](https://tools.ietf.org/html/rfc7692) permessage-deflate compression
```

### Godoc verification

The godoc for `CloseWithContext` is written in WO-3. Confirm it renders:

```bash
go doc github.com/coder/websocket Conn.CloseWithContext
```

Expected output (first few lines):

```
func (c *Conn) CloseWithContext(ctx context.Context, code StatusCode, reason string) (err error)
    CloseWithContext performs the WebSocket close handshake under caller-controlled
    cancellation. ...
```

### Acceptance

- `go doc` output starts with the expected signature and doc comment
- `README.md` diff applies cleanly
- No CHANGELOG.md is created — fork does not maintain one

### Commit

```bash
git add README.md
git commit -m "docs: advertise CloseWithContext in README Highlights"
```

---

## WO-7: Shunter integration

Purpose: Wire the fork into Shunter, remove the documented limitation in `protocol/close.go`, and prove Story 6.3 hard-teardown in tests.

Agent note: Shunter-repo file contents are captured here accurately as of this spec's writing. Still read actual current file contents before applying — the repo may have drifted.

### Current Shunter shape (verified at spec time)

- `protocol/close.go`: `closeWithHandshake` is a **free function**, not a method. Signature: `closeWithHandshake(ws *websocket.Conn, code websocket.StatusCode, reason string, timeout time.Duration)`.
- Call sites use `go closeWithHandshake(...)` (fire-and-forget). Known call sites: `protocol/disconnect.go:49`, `protocol/dispatch.go:43`, `protocol/dispatch.go:146`, `protocol/keepalive.go:77`.
- `CloseHandshakeTimeout` lives on `ProtocolOptions` (`protocol/options.go:26`), default 250ms.
- Test file: `protocol/close_test.go`. Relevant test currently at `protocol/close_test.go:179` named with the timeout-limitation framing.

### Files touched (in Shunter repo, not the websocket fork)

- `go.mod` — add `replace` directive
- `protocol/close.go` — replace body of `closeWithHandshake` with a direct `CloseWithContext` call; remove the LIMITATION comment block; add `context` import
- `protocol/close_test.go` — tighten the timeout test to assert transport death, not just helper return
- `docs/decomposition/005-protocol/epic-6-backpressure-graceful-disconnect/story-6.3-clean-close-network-failure.md` — remove limitation note, link to this spec

### Step 1: Push the fork and tag

In the websocket fork repo:

```bash
# Must be on feat/close-with-context with all WOs through WO-6 committed
git push origin feat/close-with-context

# Recommended: tag a semver-compatible pre-release so Shunter's go.mod is clean
git tag -a v1.8.14-shunter.1 -m "CloseWithContext primitive for Shunter"
git push origin v1.8.14-shunter.1

# Capture the commit hash in case we pin by hash instead of tag
git rev-parse HEAD
```

### Step 2: Shunter go.mod

Add a `replace` directive. Pin to the tag:

```
// At the bottom of go.mod:
replace github.com/coder/websocket => github.com/<fork-owner>/websocket v1.8.14-shunter.1
```

Or by commit (if no tag):

```
replace github.com/coder/websocket => github.com/<fork-owner>/websocket v0.0.0-<yyyymmddhhmmss>-<short-hash>
```

Then:

```bash
cd <shunter-repo>
rtk go mod tidy
```

### Step 3: protocol/close.go rewrite

Replace the existing function and its LIMITATION comment block. The free-function shape and the `go`-wrapper at call sites are preserved — only the body changes.

```go
package protocol

import (
	"context"
	"time"

	"github.com/coder/websocket"
)

// Close codes used by the server (RFC 6455 + SPEC-005 §11.1).
const (
	CloseNormal   = websocket.StatusNormalClosure   // 1000: graceful shutdown
	CloseProtocol = websocket.StatusProtocolError   // 1002: unknown tag, malformed
	ClosePolicy   = websocket.StatusPolicyViolation // 1008: auth, buffer overflow, flood
	CloseInternal = websocket.StatusInternalError   // 1011: unexpected server error
)

// closeWithHandshake starts a WebSocket Close handshake under a bounded
// context. When the context deadline fires the underlying transport is
// forcibly torn down, so this function guarantees the ws is unusable on
// return — not merely that the helper has returned.
//
// Backed by the Shunter fork of coder/websocket (see SPEC-WS-FORK-001).
// Callers continue to invoke via `go` if they do not want to block.
func closeWithHandshake(ws *websocket.Conn, code websocket.StatusCode, reason string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = ws.CloseWithContext(ctx, code, truncateCloseReason(reason))
}
```

Verify `truncateCloseReason` still exists in the package; the current `close.go` references it implicitly via the old function. If it does not exist, STOP — that helper is elsewhere in the current codebase.

### Step 4: protocol/close_test.go — tighten the timeout test

Current test at `protocol/close_test.go:179` asserts the helper returns within `timeout`. With the fork it can assert something stronger: the transport is dead after the helper returns. Rename the test to reflect the new guarantee and add the hard-teardown assertion.

Sketch (read current test to merge; don't blindly overwrite):

```go
// Renamed from the old "does-not-guarantee" framing.
func TestCloseWithHandshake_HardTeardownOnTimeout(t *testing.T) {
	// ... existing setup using an unresponsive peer ...

	start := time.Now()
	closeWithHandshake(conn.ws, ClosePolicy, "test", timeout)
	elapsed := time.Since(start)

	if elapsed > timeout+100*time.Millisecond {
		t.Fatalf("closeWithHandshake took %v, expected ~%v + unwind budget", elapsed, timeout)
	}

	// New hard assertion: transport is dead.
	werr := conn.ws.Write(context.Background(), websocket.MessageText, []byte("x"))
	if werr == nil {
		t.Fatal("expected transport to be dead after closeWithHandshake timeout, Write succeeded")
	}
}
```

### Step 5: Story 6.3 decomposition doc

Edit `docs/decomposition/005-protocol/epic-6-backpressure-graceful-disconnect/story-6.3-clean-close-network-failure.md`. Remove the "library limitation" / "best-effort only" block. Replace with:

```markdown
### Implementation

Uses `github.com/coder/websocket.Conn.CloseWithContext` via the Shunter fork
of coder/websocket. The helper `closeWithHandshake` in `protocol/close.go`
wraps it with `CloseHandshakeTimeout`. Transport death is guaranteed within
`CloseHandshakeTimeout + small unwind budget`.

See [SPEC-WS-FORK-001](../../../../SPEC-WS-FORK-001-v2-close-with-context.md).
```

Adjust the relative path if the story doc is at a different depth.

### Verification

In Shunter repo:

```bash
rtk go build ./protocol/
rtk go test ./protocol/ -count=1 -race
rtk go test ./protocol/ -run 'TestCloseWithHandshake|TestDisconnectCloseHandshakeTimeout' -count=10 -race
```

### Acceptance

- `rtk go build` exit 0
- `rtk go test -race` exit 0
- Renamed hard-teardown test passes with transport-death latency < `timeout + 100ms`
- `-count=10` stable
- All existing `CloseHandshakeTimeout`-related tests still pass

### Commit (in Shunter repo)

```bash
git commit -am "feat(protocol): adopt CloseWithContext for hard close-timeout teardown

Replaces the bounded-wait workaround in closeWithHandshake with a direct
call to coder/websocket.Conn.CloseWithContext (Shunter fork). Story 6.3
now guarantees transport death within CloseHandshakeTimeout.

See SPEC-WS-FORK-001."
```

### Stop condition

- If the renamed test observes transport-alive after `timeout + 100ms`, the fork did not land correctly. Verify `go.mod` replace directive points at the right commit and `go mod tidy` was run.
- If `truncateCloseReason` is missing from the package, STOP — package layout has drifted since this spec was written.

---

## 8. Pitfalls reference (pre-resolved)

These are traps that would stop a less-prepared agent. All have pre-decided answers.

1. **Module path in go.mod**: Keep `github.com/coder/websocket`. Do not rename. Consumers use `replace`. Preserves the option to drop the fork cleanly later.

2. **Unresponsive peer testing**: Must use `httptest.NewServer`, not `wstest.Pipe`. `net.Pipe` is synchronous and would block the client-side Flush of the close frame.

3. **`writeClose` call sites in `write.go:428` and `read.go:359`**: Do NOT touch. WO-2's wrapper pattern preserves them.

4. **`writeControl`'s internal 5s timeout wrap**: Leave alone. `context.WithTimeout(ctx, 5s)` takes the *earlier* deadline, so a shorter caller ctx wins. A 5s cap on writing a tiny close frame is reasonable.

5. **`waitGoroutines`'s 15s hardcoded timeout**: Leave alone. It's a leak-detector safety net, not user-facing.

6. **`goleak` dependency**: Do not add. Manual `runtime.NumGoroutine` with GC settling is sufficient and keeps the fork dependency-free.

7. **CHANGELOG.md**: Does not exist at the pinned commit. Do not create one.

8. **Error swallowing**: `CloseWithContext`'s outer defer swallows `net.ErrClosed` (matches `Close()` convention). It does NOT swallow ctx errors — those surface so callers can detect forced teardown via `errors.Is(err, context.DeadlineExceeded)`.

9. **Gate interaction with write.go:365 self-close**: `CloseWithContext` takes `casClosing()` before any write, so the write-path branch at `write.go:365` (`closeReceived && !c.casClosing()`) evaluates to false-for-our-goroutine and does not fire `c.close()` from under us. This is by design.

10. **Test package**: `close_with_context_test.go` uses `package websocket_test` (black-box, public API only). Do not use `package websocket` — internals access is not needed.

11. **WO-4 error assertions are hard**: The WO-3 error-priority table is the contract. If a test fails an `errors.Is` check, the bug is in WO-3, not the test. Do not soften the assertion.

---

## 9. Master verification script

Save as `verify-fork.sh` in the fork root. Not committed; local tooling only.

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "==> Branch check"
branch=$(git rev-parse --abbrev-ref HEAD)
if [ "$branch" != "feat/close-with-context" ]; then
  echo "wrong branch: $branch"; exit 1
fi

echo "==> Build"
go build ./...

echo "==> Vet"
go vet ./...

echo "==> Targeted tests (50x repeat)"
go test -race -count=50 -timeout=600s -run '^TestCloseWithContext_' .

echo "==> Race stress on existing tests"
go test -race -count=1 -timeout=120s ./...

echo "==> Godoc renders"
go doc github.com/coder/websocket Conn.CloseWithContext >/dev/null

echo "==> README updated"
grep -q 'CloseWithContext' README.md

echo "==> Touched files match spec"
git diff --name-only upstream/master | sort > /tmp/touched.txt
cat > /tmp/expected.txt <<EOF
README.md
close.go
close_with_context_test.go
EOF
diff /tmp/expected.txt /tmp/touched.txt

echo "==> ALL CHECKS PASS"
```

Run it:

```bash
chmod +x verify-fork.sh
./verify-fork.sh
```

If every echo line prints and the final `ALL CHECKS PASS` appears, the fork is ready for Shunter integration (WO-7).

---

## 10. Execution order summary

Strict sequential. Do not parallelize.

1. Pre-flight: environment + branch setup
2. WO-1: Baseline
3. WO-2: Refactor → commit
4. WO-3: New method → commit
5. WO-4: Behavior tests → commit
6. WO-5: Race + leak tests → commit
7. WO-6: Docs → commit
8. Master verification script
9. Push branch + tag
10. WO-7: Shunter integration (separate repo) → commit

Total: 5 commits in the fork, 1 commit in Shunter.

---

## 11. Sign-off checklist (agent reports before handoff)

- [ ] Pre-flight environment captured
- [ ] WO-1 baseline PASS recorded in `.fork-notes.md`
- [ ] WO-2 commit `<hash>` — refactor, `close.go` only
- [ ] WO-3 commit `<hash>` — new method, `close.go` only
- [ ] WO-4 commit `<hash>` — behavior tests, new file only
- [ ] WO-5 commit `<hash>` — race/leak tests, append to existing file
- [ ] WO-6 commit `<hash>` — README only
- [ ] `./verify-fork.sh` exits 0 with `ALL CHECKS PASS`
- [ ] Branch pushed to origin
- [ ] Tag `v1.8.14-shunter.1` pushed
- [ ] WO-7 Shunter commit `<hash>` — `go.mod` replace, `protocol/close.go` simplified, test tightened, story 6.3 doc updated

Agent stops here and surfaces the checklist to Mitchell.
