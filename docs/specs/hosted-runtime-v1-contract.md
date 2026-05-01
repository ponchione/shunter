# Hosted runtime v1 contract

Status: initial v1 implementation landed; contract remains the v1 reference
Scope: the concrete v1 contract for Shunter's hosted runtime surface.

This document turns the current hosted-runtime decisions into a compact implementation-facing contract.
It is the v1 target shape for making Shunter feel like a real hosted runtime/server instead of a set of manually wired subsystems.

Current repo reality behind this contract:
- the repo already has working kernel packages (`schema`, `store`, `commitlog`, `executor`, `subscription`, `protocol`)
- the root `github.com/ponchione/shunter` package now exposes the initial v1 hosted-runtime surface
- there is no maintained bundled hello-world command; the prior throwaway example was removed after V1-H
- this document remains the v1 contract and reference point for polish/follow-on work

Related docs:
- `docs/specs/APP-RUNTIME-LAYER-AND-USAGE-SURFACE.md` explains the high-level hosted-runtime layer
- `docs/specs/hosted-runtime-v1.5-follow-ons.md` defines the near-follow-on usability/platform surfaces
- `docs/specs/hosted-runtime-v2-directions.md` parks larger structural/runtime evolution

---

## 1. v1 thesis

For v1, Shunter should be a hosted runtime/server first.
It is not an embedded-first system.

This contract should be read as the public/runtime layer above the current kernel.
The live repo now exposes the initial v1 surface, and the normal example no longer requires app authors to assemble the subsystem graph directly.

The normal model is:
- application authors define a Go module against Shunter
- Shunter owns runtime bring-up, recovery, lifecycle, protocol serving, and shutdown
- clients connect to the Shunter-hosted surface
- low-level subsystem assembly is no longer the default developer experience

This is a pragmatic simplification, not the final long-term runtime shape.
Later versions should move Shunter toward a more explicit host/module/runtime model closer in role to SpacetimeDB, while staying Go-native where that is the better fit.

---

## 2. Primary v1 runtime model

The primary v1 model is:
- one Shunter runtime/server process
- one loaded Go module
- one canonical client/tooling surface for that module

Why:
- keeps bootstrap and lifecycle simple
- gives Shunter one clear runtime identity
- avoids premature multi-module or control-plane complexity

Explicitly deferred beyond v1:
- multi-module hosting
- secondary runtime processes or sidecars
- cloud/control-plane concerns

---

## 3. Top-level public API

Shunter should expose one coherent top-level hosted-runtime API.
That top-level surface is the normal way to build against Shunter.

The top-level package should be the primary surface, not `schema.Engine` and not direct subsystem assembly.

Planned top-level shape:
- `shunter.Module`
- `shunter.Config`
- `shunter.Runtime`
- `shunter.Build(module, config)`

Meaning:
- `Module` is the authored backend definition
- `Config` is narrow runtime/server configuration
- `Runtime` is the running hosted runtime
- `Build(...)` turns a module definition plus config into a runtime instance

Low-level packages such as `schema`, `commitlog`, `executor`, `subscription`, and `protocol` may remain public in v1, but they are secondary/advanced surfaces.
Normal app development should not require assembling them directly.

---

## 4. Module-first authored model

The main authored object in v1 is `shunter.Module`.

The intended flow is:
1. define a module
2. register schema/reducers/lifecycle hooks
3. build a runtime from that module
4. start the runtime

Conceptually:

```go
mod := shunter.NewModule("kickbrass")
kickbrass.Register(mod)

rt, err := shunter.Build(mod, shunter.Config{
    DataDir:    "./data",
    ListenAddr: ":8080",
    AuthMode:   shunter.AuthModeDev,
})
if err != nil {
    return err
}

return rt.ListenAndServe(ctx)
```

This is intentionally close in role to the SpacetimeDB module model, but simpler in v1 implementation.

---

## 5. Module contract in v1

Module authoring should be explicit and imperative first.
Reflection and helper layers may exist, but they should not define the core identity of how a Shunter module is authored.

At minimum, a v1 module should contain:
- schema definitions
- reducer declarations
- lifecycle hook declarations
- version information
- module/app metadata

Schema definition should be explicit-first.
Reflection/tag-based helpers can exist as convenience layers, but should not be the primary contract the hosted runtime is built around.

Reducer declaration should also stay simple in v1:
- plain function registration first
- method/handler-object styles may be supported later as optional authoring styles

A good default package shape is:
- one app/module package that defines the hosted backend module
- domain packages that contribute schema/reducers through explicit `Register(...)` hooks
- one top-level runtime entrypoint that builds and runs Shunter with that module

---

## 6. Runtime contract in v1

`shunter.Runtime` should be the stable owner object for the running system.

At minimum it should support:
- start/stop lifecycle
- readiness/health inspection
- HTTP/WebSocket serving
- local reducer/query calls
- schema/module export and introspection

Concrete intended surface:
- `Start(ctx)` / `Close()`
- `ListenAndServe(...)` as the easy default serving path
- `HTTPHandler()` for composition into a larger host app
- `CallReducer(...)`
- `Query(...)` and/or `ReadView()`
- `ExportSchema()`

The exact method names can still move slightly.
The important contract is that the app/operator sees one stable runtime owner rather than a collection of subsystem handles.

---

## 7. Client surface in v1

The primary external client surface in v1 should be the realtime WebSocket protocol.
Shunter should not be framed as REST-first or MCP-first at the core runtime boundary.

That means the primary client model remains:
- connect
- authenticate
- subscribe
- call reducers
- receive pushed updates

REST, MCP, and similar surfaces are better treated as adapters layered on top later.

### Local runtime calls

Local reducer/query calls should also exist as legitimate secondary APIs.
They matter for:
- tests
- tooling
- admin/maintenance flows
- in-process integrations

These local calls are not the main external product contract, but they are not hacks either.
They are part of the runtime-owner model.

---

## 8. Network surface in v1

The top-level API should expose a small network surface.
It should not force users into only raw handlers, and it should not force one rigid serving path either.

In practice v1 should provide:
- a clean default like `ListenAndServe(...)`
- direct handler access like `HTTPHandler()` when the host app needs composition
- protocol/network options flowing through top-level runtime config rather than manual low-level `protocol` wiring

This keeps Shunter easy to run directly while still composable inside a larger host app.

---

## 9. Runtime config boundary in v1

`shunter.Config` should stay narrow and runtime-focused.
It should control runtime behavior, not become a bucket for app/product concerns.

At minimum it should cover:
- persistence path / data directory
- queue sizing
- protocol enablement/options
- auth mode
- listen settings
- logging/metrics hooks

Module/app metadata should live on the module definition, not in runtime config.
Broader feature toggles and richer product/app concerns should be deferred beyond v1.

### Auth posture

Strict auth should be supported as a real runtime mode.
But the default local/dev story should stay easy to boot and test.

That means Shunter should not require production-style external identity setup just to bring up a runtime locally.

---

## 10. v1 implementation model

For v1, the module model should be statically linked.

In plain English:
- a Shunter app binary is built with its Go module linked in
- v1 does not require dynamic plugin loading
- v1 does not require out-of-process module execution

This is a deliberate simplification to get the hosted runtime alive first.
Longer-term isolation and runtime↔module boundary work belongs in later version docs.

---

## 11. Hello-world standard

A correct Shunter hello-world should read like:
- define a table
- define a reducer
- build/start runtime
- connect a client
- observe live state

It should not primarily read like:
- open recovery plan
- start workers manually
- adapt tx IDs
- build sender adapters
- wire protocol/server internals by hand

If the normal example still reads like subsystem assembly, the hosted runtime contract is not finished.

---

## 12. Explicitly out of scope for v1

The following should not distort the v1 runtime contract:
- multi-module hosting
- secondary runtime processes / sidecars / control-plane layers
- dynamic plugin/module loading
- out-of-process module execution
- broad admin/control-plane surfaces
- full codegen workflow as a first-class shipped platform surface
- query/view declarations as part of the base v1 module contract
- permissions/read-model declarations as part of the base v1 module contract
- migration policy metadata as part of the base v1 module contract
- cloud-hosting or multi-tenant product concerns
- cross-language module packaging

These are important follow-ons, but they belong in later-version docs rather than the base v1 contract.

---

## 13. Relationship to v1.5 and v2+

v1 is the minimum coherent hosted runtime.
It should be intentionally narrow, but real.

Strong v1.5 follow-ons:
- code-first named query/view declarations
- canonical JSON module contract export, with `shunter.contract.json` as the recommended repo snapshot name
- binding export metadata and client codegen as first-class platform/tooling surfaces
- narrow permissions/read-model declarations attached to reducers, queries, and views
- descriptive migration policy metadata, not executable migration runners

Likely v2+ directions:
- richer admin/CLI/control surfaces
- multi-module hosting exploration
- stronger runtime↔module boundary
- possible out-of-process module execution

The rule is simple:
- v1 must be coherent and usable
- later versions can widen capability and move closer to a more explicit host/module runtime model
- but those later goals should not muddy the v1 contract
