# TypeScript SDK Public Profile And Provenance

Status: actionable implementation slice
Primary backlog items: `deferred-functionality-backlog.md` items 22 and 23
Related deferred item: item 21 only for the current TypeScript output path

## Purpose

Make the current TypeScript client path ready for public app consumption while
keeping generation scoped to TypeScript. The first goal is not more languages
or a new generator architecture. The goal is a public-facing TypeScript SDK
profile that does not expose private/system tables as callable public helpers,
plus package/release provenance that makes generated bindings traceable to the
contract and Shunter runtime they target.

## Current Boundary

Current state:

- `typescript/client` is named `@shunter/client`.
- Generated TypeScript imports runtime helpers from `@shunter/client` by
  default.
- `typescript/client/package.json` is still `"private": true`.
- The package has build, typecheck, pack dry-run, and smoke package scripts.
- `codegen.Options` supports `Language` and `TypeScriptRuntimeImport`.
- `cmd/shunter contract codegen` supports `--language`, `--runtime-import`,
  `--contract`, and `--out`.
- The generator emits one TypeScript file.
- The generator currently emits table row types, decoders, metadata, and table
  subscription helpers for every table in the exported contract.
- `schema.TableExport` carries `ID`, `Name`, `IsEvent`, `Columns`, `Indexes`,
  and `ReadPolicy`, but no explicit system/private SDK visibility profile.
- Contracts already export read policy metadata, permissions metadata, read
  model metadata, protocol metadata, and generated contract metadata.

Implementation anchors:

- `codegen/codegen.go` defines `Options` and routes language selection; only
  TypeScript is currently implemented.
- `codegen/typescript.go` emits table row interfaces, decoders, contract
  metadata, reducer/procedure helpers, declared read helpers, and table
  subscription helpers.
- `contractworkflow` owns file/runtime generation and can compute file-level
  provenance if source contract hashing is added there.
- `cmd/shunter` implements `contract codegen` and currently exposes
  `--language`, `--runtime-import`, `--contract`, and `--out`.
- `schema.TableExport` is the table-level contract shape available to codegen;
  it lacks SDK visibility metadata today.
- `schema/system_tables.go` registers system tables, but the generated
  contract does not expose an explicit table visibility classification.
- `codegen/v1_compat_test.go` proves current contract readers tolerate unknown
  fields; any new required meaning still needs validation and compatibility
  tests.
- `typescript/client/package.json` has local build/test/package-smoke scripts
  but remains private.

Exact gaps:

- There is no codegen profile option in Go APIs or CLI.
- The generator cannot distinguish public app helpers from internal/system
  helper surfaces without guessing from names or read policy.
- Generated TypeScript does not record generation profile, source contract
  hash, or Shunter codegen/runtime version as machine-readable provenance.
- `@shunter/client` package metadata is not publish-ready and still marks the
  package private.
- The public profile cannot be implemented safely until contract metadata
  identifies which tables are public, private/internal, or system.

The supported app path is:

```text
ModuleContract JSON
  -> shunter contract codegen --language typescript
  -> generated app bindings
  -> @shunter/client runtime package
  -> frontend app
```

## Non-Goals

Do not use this slice to add:

- Rust, C#, C++, Unreal, WASM, Python, or other codegen targets
- multi-file generator output
- a Node server SDK
- framework adapters
- generated mutation paths that bypass reducers/procedures
- runtime authorization changes
- contract compatibility with any third-party protocol or client

Generated output is an ergonomic surface. Runtime auth, permissions, read
policy, and visibility filters remain the security boundary.

## Actionable Outcomes

1. Add an explicit TypeScript generation profile:
   - `internal` or `full`: current behavior, preserving compatibility
   - `public`: public app SDK facade
2. Keep current generated output as the default until an intentional breaking
   release changes it.
3. In the public profile, hide private/system tables from public facades.
4. Preserve enough metadata for compatibility checks and declared-read decoding
   without turning private tables into app-facing helpers.
5. Add package/release metadata and docs needed to publish `@shunter/client`.
6. Add provenance fields or generated comments that let a generated binding be
   tied back to:
   - Shunter version
   - contract format/version
   - module name/version
   - protocol min/current/default/supported values
   - runtime import target
   - generation profile

## Public Profile Semantics

The public profile should be conservative.

Recommended first-pass behavior:

| Surface | Internal profile | Public profile |
| --- | --- | --- |
| Reducer helpers | all non-lifecycle reducers | all non-lifecycle reducers, with permission metadata emitted |
| Procedure helpers | all procedures | all procedures, with permission metadata emitted |
| Declared query helpers | all executable declared queries | all executable declared queries |
| Declared view helpers | all executable declared views | all executable declared views |
| Table row interfaces | every contract table | only public/profile-visible table helpers, plus private row codecs only when required by declared-read result metadata |
| Table subscription helpers | every contract table | only profile-visible public table surfaces |
| Event helpers | every event table | only public/profile-visible event surfaces |
| Table read policy metadata | every table | profile-visible tables plus optional metadata-only redacted entries |
| Visibility filter metadata | every filter | metadata-only, unless it leaks private SQL that should be redacted |
| Lifecycle reducers | metadata only | metadata only |
| System tables | emitted today | hidden from public helpers |

Important distinction:

- A public profile may still need private table row decoding for a declared
  query/view whose public result shape is derived from private tables.
- That should be represented as declared-read row codecs, not as public table
  subscription helpers.
- Do not infer client-callability solely from table read policy if the
  contract lacks enough metadata. Add explicit contract metadata when needed.

## Visibility Metadata Gap

The backlog called out a real metadata gap: the contract needs a way to
distinguish metadata-only exports from callable SDK APIs.

Current `schema.TableExport` does not identify system tables directly. The
implementation must avoid making long-term public SDK decisions from table name
prefixes such as `sys_`.

Preferred direction:

1. Add explicit SDK visibility/profile metadata to contract exports.
2. Keep the current contract versioning rules honest. If the exported contract
   JSON shape changes, update validation, compatibility tests, fixtures, and
   `ModuleContractVersion` only when the project is ready for that contract
   change.
3. Let codegen consume the explicit metadata instead of guessing.

Possible metadata shape:

```go
type TableSDKMetadata struct {
	Visibility string `json:"visibility"` // public, internal, private, system
	Callable   bool   `json:"callable"`
}
```

This is only an example. The final shape should match existing contract style:
small, deterministic, and validated.

## Codegen API Shape

Likely additions:

```go
const (
	ProfileInternal = "internal"
	ProfilePublic   = "public"
)

type Options struct {
	Language                string
	TypeScriptRuntimeImport string
	Profile                 string
}

type TypeScriptOptions struct {
	RuntimeImport string
	Profile       string
}
```

CLI:

```bash
rtk go run ./cmd/shunter contract codegen \
  --contract shunter.contract.json \
  --language typescript \
  --profile public \
  --out src/shunter.gen.ts
```

Rules:

- blank profile means current internal/full behavior
- invalid profile returns an option validation error before file I/O
- generator output remains deterministic for a given contract/profile/import
- public profile snapshots should be golden-tested
- `public` affects generated facades only; runtime auth, permissions, and read
  policies remain enforced by the server
- generated internal codecs may still exist when required to decode a declared
  public result, but they should not create public table subscription helpers

## Runtime Package Work

`typescript/client/package.json` currently describes a private local package.
Before public npm publishing, decide and implement:

- `private: false` only when the scope/package ownership is ready
- package description suitable for public users
- license field, if the repository has one
- repository, bugs, homepage, and keywords metadata
- exact `files` allowlist
- provenance/release notes for packed artifacts
- npm publish command policy, including dry-run and smoke package gates
- version synchronization with root `VERSION` or an explicit documented rule
- whether `dist/` remains checked in or is release-built only

Keep package publish mechanics separate from codegen behavior where possible.
The public profile can be implemented before npm publishing, and npm
publishing can be enabled after the existing pack/smoke gates are stable.

Current package-readiness decision:

- Existing repo docs do not settle npm ownership or release authority for the
  `@shunter` scope.
- Keep `typescript/client/package.json` private and do not add npm publish
  mechanics until the registry owner, publish command policy, version
  synchronization rule, and `dist/` release-artifact policy are documented.
- The supported current consumption paths remain workspace, `file:`, and
  locally packed tarball installs that resolve as `@shunter/client`.

## Provenance

At minimum, generated TypeScript should continue to emit:

- source contract format and version
- module name and module version
- protocol metadata
- runtime import target

Add profile and generation provenance:

- `generationProfile`
- Shunter codegen/runtime version from `CurrentBuildInfo` if the generator path
  can access it without making codegen package depend on process globals in an
  awkward way
- optional source contract hash if contractworkflow owns the file path and can
  compute it at the workflow layer

Keep provenance machine-readable in the generated `shunterContract` or adjacent
metadata. A comment is useful for humans but not enough for release automation.

## Staging

Stage A: add profile plumbing with no output change.

- add `Profile` to `codegen.Options`
- validate blank, `internal` or `full`, and `public`
- thread the value through `contractworkflow` and CLI
- prove blank/internal output is byte-for-byte compatible with current golden
  expectations

Stage B: add explicit visibility metadata.

- add table SDK visibility metadata in the contract shape
- validate values during contract validation
- update compatibility tests and fixtures
- keep contract versioning honest if the exported JSON contract changes

Stage C: implement public filtering.

- hide system/private table helpers and table subscriptions from public output
- preserve declared query/view helpers and the codecs required by their result
  metadata
- golden-test private table, system table, declared-read-over-private-table,
  and permission/read-model metadata cases

Stage D: add provenance and package readiness.

- record generation profile and runtime import target in machine-readable
  generated metadata
- add Shunter version or source contract hash at the workflow layer if that
  avoids awkward dependencies inside `codegen`
- update package metadata only when npm ownership and release policy are known

## Risks

- Inferring visibility from `sys_` names or read policy will hard-code current
  accidents into the public SDK. Add explicit metadata first.
- Hiding helper facades is not a security mechanism. Server-side auth and read
  policy remain the boundary.
- Contract JSON changes can affect existing generated clients. Keep default
  profile behavior compatible unless a release explicitly opts into a break.
- Package publishing can become blocked on ownership or registry policy. Keep
  npm publishing separable from public-profile generation.

## Implementation Sequence

1. Add profile option validation in `codegen`.
2. Thread profile through `contractworkflow.GenerateFile`,
   `GenerateRuntime`, and CLI codegen.
3. Add tests proving blank/internal profile exactly preserves current output.
4. Add contract metadata for table SDK visibility if current contract data is
   insufficient.
5. Implement public-profile filtering for table helpers and table facades.
6. Preserve declared query/view result decoding in public profile.
7. Add golden tests for:
   - private table hidden from public table helpers
   - system table hidden from public table helpers
   - declared query over private table still has a decoded public helper when
     row metadata is exported
   - permission/read-model metadata remains available without creating a
     callable table helper
8. Update docs:
   - `typescript/client/README.md`
   - `docs/how-to/typescript-client.md`
   - `docs/how-to/contract-export-and-codegen.md`
9. Update package metadata and release qualification if publish workflow
   changes.

## Likely Touched Files

- `codegen/codegen.go`
- `codegen/typescript.go`
- `codegen/codegen_test.go`
- `codegen/v1_compat_test.go`
- `contract.go`
- `contract_validate.go`
- `contract_compat_test.go`
- `contract_test.go`
- `contractworkflow/*`
- `cmd/shunter/main.go`
- `typescript/client/package.json`
- `typescript/client/README.md`
- `docs/how-to/typescript-client.md`
- `docs/how-to/contract-export-and-codegen.md`
- `working-docs/release-qualification.md`

## Validation

Targeted:

```bash
rtk go test ./codegen ./contractworkflow ./cmd/shunter
rtk npm --prefix typescript/client test
rtk npm --prefix typescript/client run build
rtk npm --prefix typescript/client run pack:dry-run
rtk npm --prefix typescript/client run smoke:package
```

If contract JSON changes:

```bash
rtk go test . ./schema ./codegen ./contractworkflow ./contractdiff ./cmd/shunter
rtk go vet . ./schema ./codegen ./contractworkflow ./contractdiff ./cmd/shunter
```

Before publishing or changing the release workflow:

```bash
rtk go test ./...
rtk go vet ./...
rtk go tool staticcheck ./...
rtk npm --prefix typescript/client test
rtk npm --prefix typescript/client run build
rtk npm --prefix typescript/client run pack:dry-run
rtk npm --prefix typescript/client run smoke:package
rtk bash scripts/hosted-chat-gate.sh
```

## Completion Criteria

This slice is complete when:

- `--profile public` exists for TypeScript generation
- current default generated output remains compatible unless explicitly
  changed
- public profile hides private/system table helpers
- declared reads over private tables can still expose their declared public row
  shape
- package publish metadata is either ready or the remaining package ownership
  decision is documented narrowly
- generated output records the profile/provenance needed by release gates
