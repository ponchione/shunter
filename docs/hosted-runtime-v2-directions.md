# Hosted runtime v2 directions

Status: draft
Scope: later-version structural/runtime directions after the v1 hosted runtime and v1.5 follow-ons are alive.

This document captures the bigger runtime/platform evolutions that should not distort the v1 hosted-runtime contract or the v1.5 usability follow-ons.

Current framing:
- v1 makes Shunter a coherent hosted runtime/server
- v1.5 makes that runtime more usable through declarations, exports, codegen, permissions metadata, and descriptive migration metadata
- v2+ is where Shunter can revisit larger runtime shape, operational control, module isolation, and migration execution

Related docs:
- `docs/hosted-runtime-v1-contract.md` defines the base runtime shape v2 should not distort retroactively
- `docs/hosted-runtime-v1.5-follow-ons.md` defines the transitional usability/platform surfaces v2 may later clean up
- `docs/hosted-runtime-implementation-roadmap.md` keeps v2 items parked until v1/v1.5 usage proves the need

The goal is not to commit to every v2 feature now.
The goal is to keep later structural pressure out of v1/v1.5 while preserving the likely direction.

---

## 1. v2 thesis

v2 should be the first place Shunter intentionally widens beyond the simple v1 hosted-runtime shape.

The v1 shape stays intentionally narrow:
- one runtime/server process
- one statically linked Go module
- one canonical WebSocket-first client surface
- top-level `shunter` API as the normal app/runtime surface

v2 may explore:
- multi-module hosting
- stronger runtime↔module boundaries
- out-of-process module execution
- richer admin/control-plane surfaces
- executable migration systems
- more mature operational tooling

Those should be treated as structural/runtime evolution, not as requirements for making v1 useful.

---

## 2. Multi-module hosting

Multi-module hosting is a v2+ exploration, not a v1 requirement.

The question for v2 is whether one Shunter runtime/server process should be able to host multiple modules at once.

Potential value:
- shared runtime/server process for related modules
- clearer composition for product suites
- common admin/introspection surface
- possible shared client/tooling entrypoint

Risks:
- module identity and routing become more complex
- auth/permissions need module scoping
- contract export/codegen needs multi-module namespacing
- recovery, migrations, and operational failure modes become harder
- lifecycle boundaries can blur if modules are not isolated clearly

Recommended v2 posture:
- do not assume multi-module hosting is automatically required
- prototype only after one-module v1 is proven with real apps
- require explicit module identity and namespacing before supporting it
- keep one-module hosting as a valid simple mode even if multi-module support lands

Likely cleanup question from v1/v1.5:
- does `shunter.contract.json` remain one artifact per module?
- or does v2 introduce a top-level runtime contract that references multiple module contracts?

Default answer for now:
- keep per-module contracts as the source of truth
- consider a runtime-level aggregate contract only if multi-module hosting becomes real

---

## 3. Stronger runtime↔module boundary

v1 uses a statically linked Go module model on purpose.
That is the fastest path to a usable hosted runtime.

v2 should revisit whether Shunter needs a stronger boundary between:
- the host/runtime
- the app/module definition
- module execution
- module packaging/export

Potential value:
- clearer isolation between runtime internals and app code
- safer lifecycle and resource ownership
- better module introspection/export discipline
- a future path toward dynamic loading or process isolation
- closer role alignment with SpacetimeDB's host/module/runtime model

Recommended v2 posture:
- move toward a more explicit host/module boundary where it improves Shunter's hosted-runtime design
- do not copy SpacetimeDB's implementation shape blindly
- keep Go-native authoring unless there is a strong reason to introduce another module runtime
- preserve the v1 top-level API lessons: app authors should not manually assemble kernel subsystems

The key v2 question is not "embedded or hosted?"
That is already locked: hosted-first.

The key v2 question is:
- how explicit should the host↔module seam become after the v1 in-process model has proven the API?

---

## 4. Out-of-process module execution

Out-of-process module execution is a possible v2+ direction, not a committed v2 requirement.

Potential value:
- fault isolation
- resource isolation
- stronger runtime/module separation
- future multi-language or sandboxed module paths
- cleaner operational supervision

Costs:
- protocol between runtime and module must be designed
- reducer/query invocation crosses a process boundary
- recovery and deterministic execution semantics become more complex
- local testing and hello-world ergonomics can get worse
- deployment becomes more operationally heavy

Recommended v2 posture:
- only pursue out-of-process execution if real app usage shows the in-process model is unsafe or too limiting
- design the runtime↔module boundary first, before choosing process isolation
- keep statically linked in-process modules as a supported simple path unless there is a concrete reason to remove them

In other words:
- stronger boundary first
- out-of-process execution only if the boundary proves it needs process isolation

---

## 5. Richer admin, CLI, and control surfaces

v1 should not grow a broad control plane.
v1.5 should focus on declarations, exports, codegen, permissions metadata, and descriptive migration metadata.

v2 is the right bucket for broader operational surfaces.

Possible v2 admin/control features:
- runtime/module inspection commands
- contract export/diff commands as first-class CLI workflows
- local admin commands for reducer calls and queries
- runtime health/readiness/status inspection
- backup/restore workflows
- migration planning and review commands
- module lifecycle commands if multi-module hosting exists
- dev server workflows around hot restart or watch mode

Recommended v2 posture:
- build admin/CLI around the actual v1/v1.5 runtime contracts
- do not invent a separate control-plane model before the hosted runtime exists
- prefer local/owner-operated workflows first
- avoid cloud/multi-tenant assumptions unless real deployments require them

The v2 control surface should grow from the runtime's real introspection/export APIs, not from a separate product fantasy.

---

## 6. Executable migration systems

v1.5 migration policy metadata is descriptive only.
It gives app authors, tooling, generated contracts, and CI a way to reason about schema/module evolution.
It does not execute data migrations.

v2+ may introduce executable migration systems if real apps need them.

Potential executable migration features:
- ordered migration plans
- migration functions tied to schema/contract versions
- preflight validation against current stored state
- forward-only execution policy
- optional rollback metadata or compensating migration notes
- dry-run/report mode
- migration lock/serialization behavior
- integration with backup/restore workflows

Risks:
- unsafe data rewrites can corrupt durable state
- failure semantics must be very clear
- rollback is often harder than it looks
- distributed/client compatibility questions appear quickly
- runtime startup should not accidentally become a migration orchestrator without explicit operator intent

Recommended v2 posture:
- keep executable migrations as an explicit operator/tooling workflow, not an implicit side effect of normal startup
- build on v1.5 contract snapshots and diff metadata
- prefer forward-only migrations initially unless rollback semantics are designed carefully
- require dry-run/reporting before destructive changes
- keep manual-review-needed as a valid long-term outcome; not every change should be automatically executable

Likely migration direction:
- v1.5: describe and diff
- v2: plan and validate
- later v2+/v3: execute only once safety semantics are proven

---

## 7. Contract artifact evolution

v1.5 uses one canonical JSON full module contract artifact.
That is the right simple default.

v2 may need to revisit artifact shape if the platform grows.

Possible split points:
- public client contract
- admin/module contract
- runtime aggregate contract for multi-module hosting
- migration/diff report artifact
- generated human-readable docs

Recommended v2 posture:
- keep the full module contract as the source of truth until real complexity forces a split
- if split artifacts appear, define which artifact is canonical for each consumer
- do not let generated human-readable docs become a competing source of truth
- preserve stable contract diffing for CI/review workflows

This is one of the known "both" cleanup areas from v1.5.
The purpose of v2 is to make those overlaps intentional: keep what is useful, deprecate what is not.

---

## 8. Policy/auth evolution

v1.5 permissions/read-model declarations stay narrow and attach to reducers, queries, and views.

v2 may explore a broader policy system if real applications need it.

Possible v2 policy directions:
- reusable policy declarations
- role/group/tenant-aware policy helpers
- richer identity claims model
- policy export for generated clients and docs
- admin tooling to inspect effective access

Recommended v2 posture:
- keep policies close to declared read/write surfaces until repeated patterns justify abstraction
- do not introduce a broad standalone policy framework prematurely
- preserve dev-friendly local defaults
- make stricter production policy explicit rather than surprising

Policy should evolve from reducer/query/view usage, not from abstract platform ambitions.

---

## 9. Version and compatibility policy

v1.5 distinguishes:
- module version: app/backend package release
- schema/contract version: exported data/client compatibility surface

v1.5 defers full semantic-versioning policy.

v2 may formalize version policy after Shunter has real contract evolution history.

Possible v2 rules:
- what requires a schema/contract version bump
- what counts as compatible vs breaking
- how deprecations are represented
- how generated clients declare supported contract versions
- how CI compares current and previous snapshots
- how runtime/client compatibility is reported

Recommended v2 posture:
- base policy on real diffs observed from v1.5 contract snapshots
- avoid elaborate semver law before there are enough examples
- make compatibility rules machine-checkable where practical
- keep unknown/manual-review-needed available for changes that tools cannot classify safely

---

## 10. v2 non-goals until proven necessary

v2 should not automatically mean:
- cloud platform
- multi-tenant fleet control plane
- mandatory multi-module hosting
- mandatory out-of-process execution
- mandatory dynamic plugins
- cross-language module authoring
- replacing Go-native module authoring
- fully automatic data migrations for every schema change

These can become real directions only if v1/v1.5 usage shows they are needed.

---

## 11. Suggested v2 exploration order

A sane exploration order is:

1. Prove v1 with at least one real app module.
2. Prove v1.5 contract export/codegen/diff flow on that app.
3. Identify which pain is real:
   - module composition?
   - operational control?
   - migration execution?
   - policy complexity?
   - isolation/resource safety?
4. Design the smallest v2 seam that solves the real pain.
5. Preserve v1 simple mode unless it is actively harmful.

If multiple v2 pressures appear at once, prioritize in this order:
1. runtime↔module boundary clarity
2. admin/CLI workflows around existing contracts
3. migration planning/validation
4. multi-module hosting
5. out-of-process execution

Reasoning:
- a clearer runtime↔module boundary helps nearly every other v2 direction
- admin/CLI workflows can be built from existing contracts
- migration planning should precede migration execution
- multi-module hosting and process isolation are expensive and should be justified by real usage

---

## 12. Practical bottom line

v2 is where Shunter can become more structurally ambitious.

But the rule should stay simple:
- v1: make the hosted runtime real
- v1.5: make it usable and exportable
- v2+: make it more isolated, operable, composable, and migration-aware where real apps prove the need

The v2 direction should be guided by pressure from working Shunter-hosted apps, not by speculative platform completeness.
