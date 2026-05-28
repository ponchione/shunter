# Contract Export And Codegen

Contracts are the handoff artifact between a Shunter-backed Go app and clients,
review tools, or generated bindings.

A contract includes app-facing schema, reducers, procedures, declared queries,
declared views, visibility filters, permissions, read-model metadata,
migration metadata, and codegen metadata.

## Contract JSON Compatibility

Stable v1 producers emit `contract_version: 1`. The stable top-level fields
are:

- `module`: app module name, version, and string metadata
- `schema`: schema version, tables, columns, indexes, read policy, reducers,
  procedures, and optional reducer/procedure argument/result product schemas
- `queries` and `views`: declaration names, optional executable SQL, optional
  parameter schemas, row schema metadata, and result-shape metadata
- `visibility_filters`: validated SQL, returned table metadata, and
  caller-identity usage
- `permissions`: reducer, procedure, query, and view permission metadata
- `read_model`: query and view read-model metadata
- `migrations`: descriptive module, table, query, and view migration metadata
- `codegen`: contract format, contract version, and default snapshot filename

`ModuleContract.MarshalCanonicalJSON` is the canonical emitted JSON format.
`ValidateModuleContract` validates known v1 fields, reducer/procedure product
schemas, declared-read parameter schemas, and SQL/read metadata.

V1 readers must ignore unknown JSON fields so additive metadata can be
introduced without breaking older consumers. V1 producers must not change the
type or meaning of an existing known field without a new contract version.
`Module.Version(...)` populates `module.version`; it is app-owned metadata, not
the Shunter runtime/tool version.

## Export From An App Binary

The generic Shunter CLI does not dynamically load app modules. Export contracts
from a binary or test that links the app module and builds the runtime.
Starting the runtime is not required for contract export.

```go
rt, err := shunter.Build(app.Module(), shunter.Config{
	DataDir: "./data/chat",
})
if err != nil {
	return err
}
defer rt.Close()

if err := contractworkflow.ExportRuntimeFile(rt, "shunter.contract.json"); err != nil {
	return err
}
```

You can also use the root runtime APIs directly:

```go
contract := rt.ExportContract()
jsonBytes, err := rt.ExportContractJSON()
_ = contract
_ = jsonBytes
_ = err
```

## Review A Contract Change

Keep a previous contract artifact for review. Then run the generic CLI against
old and new JSON files.

```bash
rtk go run ./cmd/shunter describe --contract shunter.contract.json
rtk go run ./cmd/shunter describe --contract shunter.contract.json --section reducers --format json
rtk go run ./cmd/shunter contract validate --contract shunter.contract.json --format json
rtk go run ./cmd/shunter contract assert --contract shunter.contract.json --module chat --module-version v0.1.0 --contract-version 1 --tables 1 --reducers 1 --queries 1 --format json
rtk go run ./cmd/shunter health --contract shunter.contract.json --format json
rtk go run ./cmd/shunter contract diff --previous old.json --current shunter.contract.json
rtk go run ./cmd/shunter contract policy --previous old.json --current shunter.contract.json --strict
rtk go run ./cmd/shunter contract plan --previous old.json --current shunter.contract.json --validate
```

Use `describe` for a quick local inventory of module name, schema version,
tables, reducers, procedures, declared reads, and visibility filters.
`--section` narrows detail output for review scripts, and JSON output includes a
`counts` object for review scripts. Use `contract assert` when a release gate
needs explicit module, module-version, contract-version, schema-version, or
count expectations for tables, columns, indexes, reducers, procedures, queries,
views, or visibility filters.
JSON assertion entries use `value_type` plus typed
`expected_string`/`actual_string` or `expected_number`/`actual_number` fields,
so review scripts do not need to infer assertion value types.
JSON output also includes `assertion_count` and `failure_count` fields for
gates that only need the aggregate result.
Use `contract validate` when a
gate needs an explicit local contract-validity status. Use `health --contract`
when the same gate wants a contract-local health-shaped status; it does not
check a running server. Use the diff, policy, and plan output to decide whether
a change is additive, breaking, or requires a backup/migration plan.

## Generate TypeScript

Use contract workflow helpers from Go:

```go
if err := contractworkflow.GenerateRuntimeFile(
	rt,
	"client.ts",
	codegen.Options{Language: codegen.LanguageTypeScript},
); err != nil {
	return err
}
```

Or generate from an existing contract JSON file:

```bash
rtk go run ./cmd/shunter contract codegen --contract shunter.contract.json --language typescript --out client.ts
```

Profile selection is explicit with `--profile internal`, `--profile full`, or
`--profile public`. Blank, `internal`, `full`, and `public` currently emit the
same TypeScript; public filtering waits for table visibility metadata. Go
callers use `codegen.Options.Profile`.

Generated TypeScript imports the Shunter SDK runtime package name
`@shunter/client` by default. Use `codegen.Options.TypeScriptRuntimeImport` or
`--runtime-import` only when an app vendors or renames the runtime package. The
generated import path must match the dependency name resolved by the app's
package manager:

```bash
rtk go run ./cmd/shunter contract codegen --contract shunter.contract.json --language typescript --runtime-import @app/shunter-runtime --out client.ts
```

Generated TypeScript currently includes protocol and contract metadata, table
row interfaces, `TableRows` and `tableRowDecoders`, table subscription helpers,
read-policy and visibility metadata, reducer and procedure constants and
helpers, schema-aware argument encoders and result decoders when product schemas
are exported, declared-query/view constants and helper functions, typed
declared-read parameter interfaces and encoders when parameter schemas are
exported, decoded declared-query/view row helpers when read metadata is
exported, permissions, and read-model metadata.

Generated helpers are contract-driven client bindings. Raw `Uint8Array`
helpers remain available for every reducer and procedure. When product schemas
are not declared, keep the application's argument/result encoding documented
near the reducer or procedure and use the same encoding across local calls,
protocol clients, and tests.

See [Use generated TypeScript clients](typescript-client.md) for local
`@shunter/client` installs, `createShunterClient`, stale-binding checks, typed
reducers and procedures, decoded declared reads, managed subscriptions, and
reconnect.
The hosted end-to-end example under
[`examples/hosted-chat`](../../examples/hosted-chat/README.md) shows these
commands against a runnable app server and frontend-shaped TypeScript client.

## What Contracts Are Good For

- client generation
- app/client review before deployment
- compatibility checks across releases
- documenting permissions and read models
- preserving the schema associated with a backup
- verifying that docs and examples describe the current app surface

## What Contracts Do Not Do

- load a module dynamically into the generic CLI
- migrate data by themselves
- replace runtime schema validation
- describe private implementation package internals
- report the Shunter runtime/tool version through `Module.Version(...)`

## Recommended Workflow

1. Export a contract from the current app binary.
2. Commit or archive the contract as a release artifact.
3. When app-facing declarations change, export a new contract.
4. Run diff, policy, and plan commands.
5. Generate client bindings from the reviewed contract.
6. Store the contract next to backups and deployment artifacts.

## Version Notes

Keep these fields separate:

- `contract_version` is the module contract JSON format version.
- `module.version` is the app module version from `Module.Version(...)`.
- `shunter.CurrentBuildInfo()` reports the Shunter runtime/tool build metadata.
