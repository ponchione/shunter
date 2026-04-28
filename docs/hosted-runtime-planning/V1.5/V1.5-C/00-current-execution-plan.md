# Hosted Runtime V1.5-C Current Execution Plan

Goal: generate useful client bindings from the canonical contract.

Task sequence:
1. Reconfirm V1.5-B contract export and existing codegen/spec guidance.
2. Add failing tests for the generator contract.
3. Implement the first client binding generator from the canonical contract.
4. Add secondary internal-client/downstream artifact support only where cheap and
   clearly separated.
5. Format and validate V1.5-C gates.

Task progress:
- Task 01 complete.
- Task 02 complete.
- Task 03 complete.
- Task 04 complete; no secondary artifact target was added beyond the first
  client binding package.
- Task 05 complete.

Primary target:
- frontend/client bindings generated from the V1.5-B contract

Initial language guidance:
- TypeScript is the existing documented first target for schema codegen
- do not add every language target in V1.5-C

Immediate next V1.5 slice after V1.5-C: V1.5-D permissions/read-model metadata.

Completion notes:
- `codegen.Generate` accepts detached `ModuleContract` values.
- `codegen.GenerateFromJSON` accepts canonical `ModuleContract` JSON.
- TypeScript is the only supported language target in this slice.
- generated TypeScript covers table row types, table subscription helpers,
  reducer raw-byte call helpers, declared query helpers, and declared view
  subscription helpers.
- lifecycle reducers are exposed separately from normal callable reducer helpers.
- unsupported language values and unusable contract JSON fail with clear errors.

Validation passed:
- `rtk go fmt ./codegen`
- `rtk go test ./... -run 'Test.*Codegen|Test.*Generator|Test.*TypeScript' -count=1`
- `rtk go test ./... -count=1`
- `rtk go vet ./codegen`
