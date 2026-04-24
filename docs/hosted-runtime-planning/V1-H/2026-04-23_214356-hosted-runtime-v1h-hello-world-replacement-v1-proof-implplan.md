# Hosted Runtime V1-H Hello-World Replacement and V1 Proof Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task. This is planning only; do not implement code while writing or reviewing this plan.

**Goal:** Replace the normal manual-subsystem example path with a hosted-runtime hello-world proof that defines a module, builds/starts/serves a `shunter.Runtime`, connects a WebSocket client, observes reducer-driven subscription updates, and shuts down cleanly.

**Architecture:** V1-H is the proof slice for V1-A through V1-G. It should not create new runtime capabilities; it should consume the top-level `shunter` API planned by the earlier V1 slices and move the current example story away from direct `schema` / `commitlog` / `executor` / `subscription` / `protocol` assembly. The old low-level example may be retained only as an advanced/internal reference if it remains useful.

**Tech Stack:** Go, top-level `github.com/ponchione/shunter` package, `net/http` / `httptest`, `github.com/coder/websocket`, Shunter `protocol` test helpers for client message encode/decode, RTK-wrapped Go tooling.

---

## Validation summary from planning

### Planning context

This slice is the V1-H hello-world replacement and v1 proof plan.
- scope: example shape, end-to-end proof, old manual-example disposition, small README/example-doc pointers
- non-goals: new runtime APIs, local call APIs, export APIs, v1.5 declarations/codegen/contract, generated frontend, full tutorial site

### Live repo facts checked before writing this plan

Commands run during planning:

- `rtk go list .`
  - result now: `no Go files in /home/ponchione/source/shunter`
- `rtk go doc . Module`
  - result now: no root package yet
- `rtk go doc . Runtime`
  - result now: no root package yet
- `rtk go doc . Runtime.ListenAndServe`
  - result now: no root package yet
- `rtk go doc . Runtime.ExportSchema`
  - result now: no root package yet

This means the live code has not yet implemented the V1-A through V1-G top-level runtime surfaces that V1-H must consume. Therefore V1-H implementation must be gated on those code slices actually landing. The V1-A through V1-G planning artifacts are prerequisites, not proof that code exists.

Go-native package contracts inspected:

- `schema.Builder`
  - has `SchemaVersion`, `TableDef`, `Reducer`, `OnConnect`, `OnDisconnect`, and `Build`.
- `schema.Engine`
  - has `Registry`, `ExportSchema`, and `Start`.
- `protocol.Server`
  - `HandleSubscribe` is the HTTP-level WebSocket upgrade entrypoint for `/subscribe`.
  - production default path needs `JWT`, optional `Mint`, `Options`, `Executor`, `Conns`, `Schema`, and `State`.
- `protocol.ClientSender`
  - delivers direct responses and transaction updates to connected clients.
- `executor.ProtocolInboxAdapter`
  - bridges protocol requests to executor commands.
- `subscription.FanOutWorker`
  - delivers computed deltas through a `FanOutSender` on its own goroutine.
- `protocol.SubscribeSingleMsg`, `protocol.CallReducerMsg`, `protocol.TransactionUpdateLight`, `protocol.EncodeClientMessage`, and `protocol.DecodeServerMessage`
  - existing protocol helpers are sufficient for an end-to-end example test client.

### Existing example inspected as replacement target, not architecture source of truth

`cmd/shunter-example/main.go` currently:

- builds schema directly with `schema.NewBuilder()` and `schema.TableDef(...)`
- opens or bootstraps recovery through `commitlog.OpenAndRecoverDetailed(...)` and `commitlog.NewSnapshotWriter(...).CreateSnapshot(...)`
- starts a durability worker directly
- constructs `executor.ReducerRegistry` manually
- wires subscription manager and fan-out inbox manually
- creates executor/scheduler goroutines manually
- constructs `protocol.ConnManager`, protocol inbox adapter, client sender, fan-out sender, fan-out worker, and `protocol.Server` manually
- mounts `server.HandleSubscribe` on `/subscribe`
- owns shutdown ordering directly

`cmd/shunter-example/main_test.go` currently proves useful behavior that V1-H should preserve through the top-level API:

- cold boot then recovery against the same data directory
- anonymous WebSocket admission and identity-token handshake
- subscription to `SELECT * FROM greetings`
- reducer call to `say_hello`
- non-caller subscriber receives `TransactionUpdateLight` with inserts for `greetings`
- context cancellation shuts the example down cleanly

V1-H must not copy the manual architecture into a new normal example. It should preserve the behavioral proof while replacing manual wiring with the hosted-runtime surface.

---

## Prerequisites and hard gate

Do not start V1-H implementation until all of these are true in live code:

1. `rtk go list .` succeeds for the root package.
2. `rtk go doc . Module` shows the V1 module authoring surface from V1-A/V1-B.
3. `rtk go doc . Runtime` shows the V1 runtime owner surface.
4. `rtk go doc . Runtime.Start` and `rtk go doc . Runtime.Close` exist from V1-D.
5. `rtk go doc . Runtime.ListenAndServe` and `rtk go doc . Runtime.HTTPHandler` exist from V1-E.
6. Local calls from V1-F exist only as optional helper paths; V1-H's primary proof must still use WebSocket as the external client model.
7. Export/introspection from V1-G exists for optional diagnostics, but V1-H must not add new export APIs.
8. Focused tests for V1-A through V1-G pass.

If these are not true, stop and implement earlier slices first. Do not make V1-H invent missing API surface just to make the example compile.

---

## Scope

In scope:

- Add or rewrite a normal hosted-runtime hello-world example that uses the top-level `shunter` API.
- Define a `greetings` table through `Module` APIs.
- Define a `say_hello` reducer through `Module` APIs.
- Build and start/serve runtime through `shunter.Build` plus `Runtime.Start`/`HTTPHandler` or `Runtime.ListenAndServe`.
- Connect a WebSocket client through `/subscribe` using the V1-E network surface.
- Subscribe to `SELECT * FROM greetings` or the narrow supported equivalent.
- Call the reducer over the WebSocket protocol and observe live update delivery.
- Verify clean shutdown.
- Decide the old low-level manual example disposition:
  - preferred: move/demote it to an advanced/internal reference package or doc, not the normal example; or
  - if deleting is safe, delete it after preserving coverage; or
  - if retaining in place is temporarily necessary, update comments/docs so it is clearly not the normal app path.
- Update concise docs/README pointers so a new app author sees the hosted-runtime example first.

Out of scope:

- New `shunter.Module` methods.
- New `shunter.Runtime` lifecycle/network/local/export APIs.
- New protocol message types or serving routes.
- New auth product behavior.
- Local reducer/query APIs beyond consuming V1-F where helpful for tests.
- `shunter.contract.json` or canonical contract export.
- Codegen/client bindings.
- v1.5 query/view declarations.
- permissions/read-model metadata.
- migration metadata/diff tooling.
- multi-module hosting.
- control-plane/admin APIs.
- generated frontend app or full tutorial site.

---

## Locked decisions for V1-H

1. The normal example must be hosted-runtime-first.

   App-facing code should import `github.com/ponchione/shunter` and should not directly construct kernel subsystem graph pieces.

2. WebSocket remains the primary external proof path.

   The end-to-end proof should use a WebSocket client against `/subscribe`, not only local reducer calls.

3. Existing manual example is a replacement target only.

   `cmd/shunter-example` may be inspected to preserve user-visible behavior and tests, but it must not be treated as architecture source of truth.

4. V1-H must not backfill missing V1-A through V1-G APIs.

   If root `Module`, `Runtime`, `Build`, lifecycle, network, local, or export surfaces are missing, the implementation attempt is blocked by prerequisites.

5. Keep the module domain tiny.

   One `greetings` table and one `say_hello` reducer are enough. Do not add a larger demo app.

6. Keep docs concise.

   V1-H may add a short README/example pointer. It should not build a full tutorial site.

7. Preserve behavioral proof, not old structure.

   The proof is cold boot/recovery, client connect, subscription, reducer call, delta/live update, and shutdown. The old manual functions are not the proof.

---

## Candidate file layout

The exact path can be adjusted during implementation, but the recommended V1-H target is:

- Create or rewrite normal hosted example:
  - `cmd/shunter-hello/main.go`
  - `cmd/shunter-hello/main_test.go`

- Optional internal/reference retention of old manual example:
  - Option A: move old manual example to `cmd/shunter-kernel-example/main.go` and `cmd/shunter-kernel-example/main_test.go`
  - Option B: keep `cmd/shunter-example` but rewrite it as the hosted-runtime example, moving old manual wiring into `docs/hosted-runtime-bootstrap.md` as historical/reference material only
  - Option C: delete old manual example after equivalent hosted-runtime tests cover the behavior

Preferred choice: use `cmd/shunter-example` as the normal hosted-runtime example if compatibility with docs/scripts matters, and move the old low-level manual example to `cmd/shunter-kernel-example` only if maintainers still want a runnable advanced reference. If old manual example remains in `cmd/shunter-example`, the slice has failed its discoverability goal.

Likely docs to update:

- `README.md`
  - replace “no polished runnable example server” status once V1-H implementation lands
  - point to the hosted example command
- `docs/hosted-runtime-bootstrap.md`
  - demote manual bootstrap wording to historical/internal reference, or replace it with hosted-runtime quickstart wording
- `docs/hosted-runtime-implementation-roadmap.md`
  - optional status note only if the repo is tracking completion in roadmap docs

Likely tests:

- `cmd/shunter-example/main_test.go` or `cmd/shunter-hello/main_test.go`
- possibly root package tests only if example integration exposes root API issues; avoid adding new API behavior in V1-H

---

## Target hosted example shape

The final normal example should read approximately like this conceptually. Exact method names must match V1-A through V1-G live code.

```go
package main

import (
    "context"
    "errors"
    "flag"
    "log"
    "os/signal"
    "syscall"

    "github.com/ponchione/shunter"
    "github.com/ponchione/shunter/schema"
    "github.com/ponchione/shunter/types"
)

type Greeting struct {
    ID      uint64
    Message string
}

func main() {
    var (
        addr    = flag.String("addr", ":8080", "HTTP listen address")
        dataDir = flag.String("data", "./shunter-data", "Shunter data directory")
    )
    flag.Parse()

    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    mod := shunter.NewModule("hello")
    registerHello(mod)

    rt, err := shunter.Build(mod, shunter.Config{
        DataDir:    *dataDir,
        ListenAddr: *addr,
        AuthMode:   shunter.AuthModeDev,
    })
    if err != nil {
        log.Fatalf("build: %v", err)
    }

    if err := rt.ListenAndServe(ctx); err != nil && !errors.Is(err, context.Canceled) {
        log.Fatalf("serve: %v", err)
    }
}

func registerHello(mod *shunter.Module) {
    mod.SchemaVersion(1)
    mod.TableDef(schema.TableDefinition{
        Name: "greetings",
        Columns: []schema.ColumnDefinition{
            {Name: "id", Type: types.KindUint64, PrimaryKey: true, AutoIncrement: true},
            {Name: "message", Type: types.KindString},
        },
    })
    mod.Reducer("say_hello", sayHello)
}

func sayHello(ctx *types.ReducerContext, args []byte) ([]byte, error) {
    msg := string(args)
    if msg == "" {
        msg = "hello, world"
    }
    const greetingsTableID uint32 = 0
    _, err := ctx.DB.Insert(greetingsTableID, types.ProductValue{
        types.NewUint64(0),
        types.NewString(msg),
    })
    return nil, err
}
```

Important notes for implementers:

- The reducer signature and context type must match the live V1-B wrapper contract. Do not force the example to use lower-level `executor.RegisteredReducer` unless that is what V1-B explicitly exposes through `Module.Reducer`.
- If V1-B added a cleaner explicit table-definition wrapper, use that instead of importing `schema.TableDefinition` directly.
- If V1-E's `ListenAndServe` signature differs, use the actual live signature.
- If `ListenAndServe(ctx)` automatically starts and closes the runtime as planned, the example should use that easy path.
- If the implementation wants a composable test server, tests can use `Runtime.Start(ctx)` plus `Runtime.HTTPHandler()` with `httptest.NewServer`.

---

## Test plan and TDD tasks

### Task 0: Reconfirm prerequisites before touching code

Objective: prove V1-H is not starting before the top-level runtime exists.

Files: none.

Run:

```bash
rtk go list .
rtk go doc . Module
rtk go doc . Runtime
rtk go doc . Runtime.Start
rtk go doc . Runtime.Close
rtk go doc . Runtime.ListenAndServe
rtk go doc . Runtime.HTTPHandler
rtk go test . -count=1
```

Expected:

- root package exists
- docs show V1-A through V1-E methods
- focused root tests pass

If any command fails because the root package or methods do not exist, stop. V1-H is blocked.

### Task 1: Add a failing hosted example build/recovery test

Objective: prove the hosted example can cold boot and recover without manual subsystem assembly in app code.

Files:

- Create or modify: `cmd/shunter-example/main_test.go` or `cmd/shunter-hello/main_test.go`
- Create or modify: `cmd/shunter-example/main.go` or `cmd/shunter-hello/main.go`

Test shape:

```go
func TestHostedHello_BootstrapThenRecover(t *testing.T) {
    dir := t.TempDir()
    ctx := context.Background()

    rt, err := buildHelloRuntime(dir, "127.0.0.1:0")
    if err != nil {
        t.Fatalf("first buildHelloRuntime: %v", err)
    }
    if err := rt.Start(ctx); err != nil {
        t.Fatalf("first start: %v", err)
    }
    if err := rt.Close(); err != nil {
        t.Fatalf("first close: %v", err)
    }

    rt2, err := buildHelloRuntime(dir, "127.0.0.1:0")
    if err != nil {
        t.Fatalf("second buildHelloRuntime: %v", err)
    }
    if err := rt2.Start(ctx); err != nil {
        t.Fatalf("second start: %v", err)
    }
    if err := rt2.Close(); err != nil {
        t.Fatalf("second close: %v", err)
    }
}
```

Expected initial failure:

- fails because `buildHelloRuntime` / hosted example wiring is not implemented yet, or because current example still returns low-level `engineGraph`.

### Task 2: Implement hosted module builder helper for the example

Objective: create a small helper that defines the module and calls `shunter.Build`.

Files:

- Modify: `cmd/shunter-example/main.go` or `cmd/shunter-hello/main.go`

Implementation guidance:

- Add `newHelloModule() *shunter.Module` or `registerHello(mod *shunter.Module)`.
- Add `buildHelloRuntime(dataDir, addr string) (*shunter.Runtime, error)`.
- Use top-level `shunter.Build`, not direct kernel constructors.
- Keep the table/reducer definitions identical in behavior to the old example: table `greetings`, columns `id` and `message`, reducer `say_hello` inserting the caller-supplied message or `hello, world` fallback.

Run:

```bash
rtk go test ./cmd/shunter-example -run TestHostedHello_BootstrapThenRecover -count=1
```

or, if using a new path:

```bash
rtk go test ./cmd/shunter-hello -run TestHostedHello_BootstrapThenRecover -count=1
```

Expected:

- test passes
- app-facing code in the example does not instantiate `commitlog.DurabilityWorker`, `executor.Executor`, `subscription.Manager`, `protocol.Server`, or manual adapter structs.

### Task 3: Add a failing hosted WebSocket admission test

Objective: prove the hosted runtime exposes the V1-E network surface and admits an anonymous/dev connection.

Files:

- Modify example test file.

Test shape:

```go
func TestHostedHello_AdmitsDevConnection(t *testing.T) {
    rt := mustBuildAndStartHelloRuntime(t)
    defer rt.Close()

    srv := httptest.NewServer(rt.HTTPHandler())
    defer srv.Close()

    wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/subscribe"
    conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
        Subprotocols: []string{"v1.bsatn.spacetimedb"},
    })
    if err != nil {
        t.Fatalf("dial: %v (resp=%v)", err, resp)
    }
    defer conn.Close(websocket.StatusNormalClosure, "")

    if resp.StatusCode != http.StatusSwitchingProtocols {
        t.Fatalf("upgrade status = %d, want 101", resp.StatusCode)
    }

    _, _, err = conn.Read(readCtx)
    if err != nil {
        t.Fatalf("read IdentityToken: %v", err)
    }
}
```

Expected initial failure:

- fails until the example uses the runtime's `HTTPHandler()` correctly or until auth/config defaults are set correctly.

### Task 4: Make hosted WebSocket admission pass

Objective: configure the hosted example for local/dev anonymous auth and mount the runtime handler.

Files:

- Modify example implementation and tests.

Implementation guidance:

- Use V1-E's real `HTTPHandler()` output directly with `httptest.NewServer`.
- Do not manually route `protocol.Server.HandleSubscribe` from example code.
- Do not instantiate `protocol.Server` in the example.
- Configure `shunter.Config` with the V1-E dev auth mode and listen address fields.

Run:

```bash
rtk go test ./cmd/shunter-example -run TestHostedHello_AdmitsDevConnection -count=1
```

or new package path equivalent.

Expected:

- WebSocket upgrade succeeds.
- identity-token frame arrives.

### Task 5: Add a failing hosted live-update proof test

Objective: preserve the old end-to-end proof that a subscriber observes a reducer insert, but route through top-level runtime APIs.

Files:

- Modify example test file.

Test shape:

```go
func TestHostedHello_SubscriberReceivesReducerInsert(t *testing.T) {
    rt := mustBuildAndStartHelloRuntime(t)
    defer rt.Close()

    srv := httptest.NewServer(rt.HTTPHandler())
    defer srv.Close()

    subscriber := dialHostedHello(t, srv.URL)
    defer subscriber.Close(websocket.StatusNormalClosure, "")
    caller := dialHostedHello(t, srv.URL)
    defer caller.Close(websocket.StatusNormalClosure, "")

    writeClientMessage(t, subscriber, protocol.SubscribeSingleMsg{
        RequestID:   1,
        QueryID:     1,
        QueryString: "SELECT * FROM greetings",
    })
    applied := readUntilTag(t, subscriber, protocol.TagSubscribeSingleApplied)
    if applied.(protocol.SubscribeSingleApplied).TableName != "greetings" {
        t.Fatalf("subscribe applied to wrong table")
    }

    writeClientMessage(t, caller, protocol.CallReducerMsg{
        RequestID:   2,
        ReducerName: "say_hello",
        Args:        []byte("hola"),
        Flags:       protocol.CallReducerFlagsFullUpdate,
    })

    msg := readUntilTag(t, subscriber, protocol.TagTransactionUpdateLight)
    light := msg.(protocol.TransactionUpdateLight)
    if len(light.Update) != 1 {
        t.Fatalf("light.Update len = %d, want 1", len(light.Update))
    }
    if light.Update[0].TableName != "greetings" {
        t.Fatalf("light update table = %q, want greetings", light.Update[0].TableName)
    }
    if len(light.Update[0].Inserts) == 0 {
        t.Fatal("light update inserts empty, want encoded row")
    }
}
```

Expected initial failure:

- fails until hosted runtime serving, fan-out sender wiring, and reducer registration from previous slices are correctly consumed by the example.

### Task 6: Make live-update proof pass without manual subsystem wiring

Objective: fix only example-level use of the already-existing runtime API.

Files:

- Modify example implementation and tests.

Rules:

- If reducer registration does not reach the executor, fix the earlier V1-B/V1-C implementation, not V1-H by manually constructing `executor.ReducerRegistry` in the example.
- If protocol-backed fan-out is missing, fix V1-E implementation, not V1-H by manually constructing `protocol.ClientSender` in the example.
- If lifecycle startup is incomplete, fix V1-D implementation, not V1-H by starting executor/scheduler/fan-out goroutines in the example.

Run:

```bash
rtk go test ./cmd/shunter-example -run TestHostedHello_SubscriberReceivesReducerInsert -count=1
```

or new package path equivalent.

Expected:

- subscriber receives `TransactionUpdateLight` with non-empty inserts.
- example code still imports only top-level runtime plus small schema/types/protocol helper packages needed for module definition and test client.

### Task 7: Add a failing clean shutdown test for the hosted serving path

Objective: prove the easy serving path shuts down cleanly on context cancellation.

Files:

- Modify example test file.

Test shape:

```go
func TestHostedHello_RunShutsDownCleanlyOnContextCancel(t *testing.T) {
    dir := t.TempDir()
    ctx, cancel := context.WithCancel(context.Background())

    errCh := make(chan error, 1)
    go func() { errCh <- run(ctx, "127.0.0.1:0", dir) }()

    time.Sleep(50 * time.Millisecond)
    cancel()

    select {
    case err := <-errCh:
        if err != nil {
            t.Fatalf("run returned %v, want nil", err)
        }
    case <-time.After(5 * time.Second):
        t.Fatal("run did not return within 5s after cancel")
    }
}
```

Expected initial failure:

- fails until `run` uses `Runtime.ListenAndServe(ctx)` or equivalent hosted serving path.

### Task 8: Rewrite `run` / `main` to use the easy hosted serving path

Objective: ensure the command reads like app code, not kernel assembly.

Files:

- Modify: `cmd/shunter-example/main.go` or `cmd/shunter-hello/main.go`

Implementation guidance:

- `main` should parse flags, make signal context, and call `run(ctx, addr, dataDir)`.
- `run` should build the hello module/runtime and call `rt.ListenAndServe(ctx)` or the actual V1-E easy-serving method.
- `run` should not create `http.Server` manually unless V1-E's intended easy path requires host-managed HTTP composition. If it does, the manual code should be only HTTP composition around `rt.HTTPHandler()`, not protocol subsystem assembly.
- Normalize `context.Canceled` / `http.ErrServerClosed` behavior according to V1-E's planned method contract.

Run:

```bash
rtk go test ./cmd/shunter-example -run TestHostedHello_RunShutsDownCleanlyOnContextCancel -count=1
```

or new package path equivalent.

Expected:

- cancellation returns nil or the documented benign cancellation result.
- runtime resources close cleanly.

### Task 9: Decide and execute old low-level example disposition

Objective: ensure the normal path is no longer the manual subsystem assembly story.

Files:

- Modify/move/delete: `cmd/shunter-example/main.go`
- Modify/move/delete: `cmd/shunter-example/main_test.go`
- Optionally create: `cmd/shunter-kernel-example/main.go`
- Optionally create: `cmd/shunter-kernel-example/main_test.go`

Decision checklist:

- If the old manual example is retained, rename it to make the level explicit, e.g. `cmd/shunter-kernel-example`.
- Add package comments stating it is an advanced/internal kernel-wiring reference, not the normal hosted-runtime app path.
- Ensure `README.md` and docs point first to the hosted example.
- Do not let two examples both claim to be the normal start-here path.

Run:

```bash
rtk go test ./cmd/... -count=1
```

Expected:

- all command package tests pass.
- no package path collision from moving/renaming command examples.

### Task 10: Update concise docs pointers

Objective: make the new normal example discoverable without creating a full tutorial site.

Files:

- Modify: `README.md`
- Modify: `docs/hosted-runtime-bootstrap.md` or replace with a hosted-runtime quickstart doc if appropriate
- Optional modify: `docs/hosted-runtime-implementation-roadmap.md`

Required doc changes:

- README should point to the hosted-runtime hello-world command as the start-here example.
- README should no longer say there is no polished runnable example once V1-H lands.
- Existing manual bootstrap docs should be marked advanced/internal/reference if kept.
- Do not add v1.5 contract/codegen promises as completed work.

Suggested concise README snippet:

````markdown
## Hosted-runtime hello world

The normal runnable example is `cmd/shunter-example`.
It defines a `greetings` table and `say_hello` reducer through the top-level
`shunter.Module` API, builds a `shunter.Runtime`, serves `/subscribe`, and proves
live updates over the WebSocket protocol.

Run:

```bash
rtk go run ./cmd/shunter-example -addr :8080 -data ./shunter-data
```
````

Adjust the command path if using `cmd/shunter-hello`.

### Task 11: Add import/source guard test or review checklist

Objective: prevent the normal example from quietly regressing to manual subsystem assembly.

Option A: Add a small test that reads the hosted example source and rejects forbidden direct subsystem constructor calls.

Files:

- Modify: example test file

Possible guard:

```go
func TestHostedHello_DoesNotManuallyAssembleKernelGraph(t *testing.T) {
    src, err := os.ReadFile("main.go")
    if err != nil {
        t.Fatal(err)
    }
    forbidden := []string{
        "commitlog.NewDurabilityWorker",
        "executor.NewExecutor",
        "executor.NewReducerRegistry",
        "subscription.NewManager",
        "subscription.NewFanOutWorker",
        "protocol.NewConnManager",
        "protocol.NewClientSender",
        "protocol.Server{",
    }
    for _, needle := range forbidden {
        if bytes.Contains(src, []byte(needle)) {
            t.Fatalf("normal hosted example must not manually assemble %s", needle)
        }
    }
}
```

Option B: If source-inspection tests are considered brittle, include the same forbidden list in the PR checklist and code review notes. Option A is preferred for this slice because the entire point is preventing example regression.

Run:

```bash
rtk go test ./cmd/shunter-example -run TestHostedHello_DoesNotManuallyAssembleKernelGraph -count=1
```

Expected:

- guard passes for the normal hosted example.
- if a retained advanced kernel example exists, this guard applies only to the hosted example path.

---

## Final validation commands for V1-H implementation

Use RTK for all commands.

Focused checks:

```bash
rtk go fmt ./cmd/shunter-example
rtk go test ./cmd/shunter-example -count=1
rtk go vet ./cmd/shunter-example
rtk go build ./cmd/shunter-example
```

If using `cmd/shunter-hello`, run the same commands for that path.

Command-package sweep:

```bash
rtk go test ./cmd/... -count=1
```

Root/runtime regression checks:

```bash
rtk go test . -count=1
```

Broader verification before calling V1 complete:

```bash
rtk go test ./... -count=1
rtk go build ./...
```

Search checks:

```bash
rtk grep "cmd/shunter-example" README.md docs
rtk grep "manual bootstrap\|subsystem assembly\|hosted-runtime-bootstrap" README.md docs cmd
```

Manual smoke command after tests pass:

```bash
rtk go run ./cmd/shunter-example -addr 127.0.0.1:8080 -data ./shunter-data
```

Then connect with a small test client or rely on the automated WebSocket test if manual CLI client tooling is not present.

---

## Risks and guardrails

### Risk: V1-H starts before V1-A through V1-G code exists

Guardrail:

- Run the prerequisite `go doc` commands first.
- If root package methods are missing, stop.
- Do not implement missing runtime APIs in this slice.

### Risk: example silently reimplements runtime assembly

Guardrail:

- Add the source guard or strict review checklist against direct subsystem constructors.
- Keep app-facing code at the `shunter.Module` / `shunter.Runtime` level.

### Risk: tests only prove local calls, not the hosted client model

Guardrail:

- The live-update proof must connect via WebSocket and use protocol messages.
- Local calls may support setup/diagnostics only.

### Risk: docs overstate V1.5 readiness

Guardrail:

- README/docs should say V1 hosted runtime example exists.
- Do not claim canonical contract export, codegen, permissions metadata, or migration tooling exists.

### Risk: moving the old example breaks useful low-level coverage

Guardrail:

- Preserve behavior through hosted tests first.
- If keeping low-level coverage is still useful, demote it to `cmd/shunter-kernel-example` and keep tests there separately.

### Risk: auth defaults confuse the hello-world path

Guardrail:

- Use the V1-E dev/anonymous auth mode explicitly.
- The example should be runnable locally without an external IdP.
- Strict auth remains supported by runtime config but is not the hello-world default.

---

## Completion criteria

V1-H is complete when:

1. The normal runnable example uses the top-level hosted-runtime API.
2. The example defines `greetings` through module/table registration.
3. The example defines `say_hello` through module/reducer registration.
4. The example builds and starts/serves a `shunter.Runtime` without manual kernel subsystem assembly.
5. A WebSocket client can connect to `/subscribe` through the runtime network surface.
6. A subscriber can subscribe to `SELECT * FROM greetings`.
7. A caller can invoke `say_hello` over the WebSocket protocol.
8. The subscriber receives a live update with an inserted `greetings` row.
9. The runtime shuts down cleanly on context cancellation / close.
10. The old low-level manual example is removed, renamed, or clearly documented as advanced/internal reference only.
11. README/docs point first to the hosted-runtime example.
12. Focused example tests, command-package tests, root/runtime tests, `go vet`, and broad `go test ./...` pass.

---

## Immediate next slice after V1-H

After V1-H lands and V1 is proven end-to-end, the next planning track is V1.5-A query/view declarations only if the repo owner wants to continue hosted-runtime usability work. Do not begin V1.5 until V1-H's example proof is green and the V1 runtime owner is real in code.
