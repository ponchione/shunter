# Spec Scope

These docs describe the clean-room Shunter core engine/runtime that is
architecturally inspired by SpacetimeDB.

They are intended to cover the comparable engine kernel:

- schema definition and export
- in-memory relational state
- reducer execution
- commit log + snapshot recovery
- subscription evaluation and fan-out
- client protocol / websocket delivery

They do **not** attempt full SpacetimeDB product compatibility.

Out of scope unless a spec says otherwise:

- hosted/cloud control-plane behavior
- standalone host/database routing concerns
- multi-language server-module runtimes (WASM / JS bundle hosting)
- full SpacetimeDB product/API surface beyond Shunter's v1 engine scope

Use `README.md` as the higher-level product framing, and use the numbered
`SPEC-*` files in this folder as the implementation-grade clean-room contracts
for the narrowed Shunter engine.
