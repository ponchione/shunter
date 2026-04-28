# V1.5-C Task 03: Implement The First Client Binding Generator

Parent plan: `docs/hosted-runtime-planning/V1.5/V1.5-C/00-current-execution-plan.md`

Objective: generate frontend/client bindings from the canonical contract, with
TypeScript as the first narrow target.

Implementation target:
- parse canonical contract JSON
- validate the contract format/version
- generate deterministic TypeScript output
- map Shunter column kinds to TypeScript types
- emit table row types
- emit reducer call helpers or reducer name constants
- emit query helper shapes from V1.5-A declarations
- emit view/subscription helper shapes from V1.5-A declarations
- keep raw-byte reducer argument surfaces if typed args are not present

CLI guidance:
- prefer the existing documented shape if a CLI is added:
  `shunter-codegen --lang typescript --schema shunter.contract.json --out ./generated/`
- accept `--schema` as the contract input path even if older docs said
  `schema.json`; update docs if the canonical contract replaces that input

Non-goals:
- every language target
- generated frontend app
- framework-specific scaffolding
- generated server/module implementation

