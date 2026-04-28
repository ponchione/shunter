# Hosted Runtime V1.5-D Current Execution Plan

Goal: attach narrow permission/read-model metadata to declared read/write
surfaces.

Task sequence:
1. Reconfirm declaration, contract, and codegen surfaces.
2. Add failing tests for permission metadata attachment and export.
3. Implement narrow metadata on reducers, queries, and views.
4. Format and validate V1.5-D gates.

Task progress:
- Task 01 complete. Prerequisite checks confirmed reducers already export
  through schema/contract metadata, queries and views export through V1.5-A/B
  declarations, metadata can live beside each declaration, and codegen can
  inspect metadata without runtime enforcement.
- Task 02 complete. Focused tests pin reducer permissions, query/view
  permissions, query/view read-model metadata, contract export, detached
  snapshots, and TypeScript metadata visibility.
- Task 03 complete. `PermissionMetadata`, `ReadModelMetadata`, reducer
  options, query/view metadata fields, contract declarations, and generated
  TypeScript metadata constants are implemented.
- Task 04 complete. Root/package validation passed; the broad `./...` run is
  blocked by the existing `store` rapid test failure noted in the hosted
  runtime handoff.

V1.5-D landed proof:
- reducers can be annotated with `WithReducerPermissions(...)`
- queries and views carry `Permissions` and `ReadModel` metadata
- `Runtime.ExportContract()` exports passive permission/read-model metadata in
  the canonical contract
- `ModuleContract.MarshalCanonicalJSON()` normalizes absent metadata to stable
  empty arrays
- generated TypeScript exposes `permissions` and `readModels` constants for
  inspection only
- no runtime access-control enforcement, policy engine, migration metadata, or
  runtime shape change was added

Validation:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*Permission|Test.*ReadModel|Test.*Contract' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go test ./... -run 'Test.*Codegen|Test.*Generator|Test.*TypeScript' -count=1`
- `rtk go test ./codegen -count=1`
- `rtk go vet ./codegen`

Known external blocker:
- `rtk go test ./... -count=1` fails in
  `store.TestRapidStoreCommitMatchesModel`; the same test fails in isolation
  with `rtk go test ./store -run TestRapidStoreCommitMatchesModel -count=1`.
  V1.5-D did not touch `store/`.

V1.5-D target:
- metadata attaches to reducers
- metadata attaches to named queries
- metadata attaches to named views/subscriptions
- metadata appears in the canonical contract
- generated clients/docs can inspect the metadata

V1.5-D must not become:
- a broad standalone policy framework
- a full multi-tenant auth product
- runtime-blocking access-control enforcement unless a later contract explicitly
  designs that behavior

Immediate next V1.5 slice after V1.5-D: V1.5-E migration metadata and contract
diff tooling.
