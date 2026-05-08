# Contract Export And Codegen

Status: rough draft
Scope: exporting module contracts, reviewing compatibility, and generating
client bindings.

Contracts are the handoff artifact between a Shunter-backed Go app and clients,
review tools, or generated bindings.

A contract includes app-facing schema, reducers, declared queries, declared
views, visibility filters, permissions, read-model metadata, migration
metadata, and codegen metadata.

## Export From An App Binary

The generic Shunter CLI does not dynamically load app modules. Export contracts
from a binary or test that links the app module and builds the runtime.

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
rtk go run ./cmd/shunter contract diff --previous old.json --current shunter.contract.json
rtk go run ./cmd/shunter contract policy --previous old.json --current shunter.contract.json --strict
rtk go run ./cmd/shunter contract plan --previous old.json --current shunter.contract.json --validate
```

Use the output to decide whether a change is additive, breaking, or requires a
backup/migration plan.

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

Generated TypeScript currently includes row interfaces, table metadata,
declared-read constants and helpers, reducer helper functions, permission
metadata, read-model metadata, and protocol metadata.

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
