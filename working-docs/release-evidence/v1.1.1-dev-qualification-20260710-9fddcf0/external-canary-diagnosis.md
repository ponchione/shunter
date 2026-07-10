# External canary diagnosis

- Canary commit: `e69bce73cb49fbd2334dd8b99eb664b07fc6e132`
- Initial and final canary worktree: clean, local branch `master`
- Shunter commit under test: `9fddcf0b842f72d5e24e399d558042394b337fbd`
- Classification: stale/incompatible external-canary expectations, not a reproduced Shunter regression.
- Canary changes made: none.

Both `canary-quick` and `canary-full` fail during their initial `go test ./... -count=1` phase. The full gate therefore does not reach its SDK smoke, seeded, race, contract/codegen reproducibility, in-process protocol smoke, or served protocol smoke stages.

The contract mismatch is narrow and deterministic. A non-mutating current export differs from the committed canary contract by ten added fields:

- `schema.tables[0..7].sdk.visibility = "public"`
- `schema.tables[8..9].sdk.visibility = "system"`

The generated TypeScript test also reports the committed binding stale under current Shunter codegen.

The auth failure is an expectation mismatch. The canary test treats a successful WebSocket upgrade for a missing token as failure. Current Shunter intentionally completes an upgrade when a supported Shunter subprotocol is offered, then closes with WebSocket policy-violation code 1008 and reason `auth-token rejected by admission`; Shunter pins that contract in `TestShunterStrictAuthRejectionCloseContract`.

Raw failures:

- [canary-quick.log](canary-quick.log)
- [canary-full.log](canary-full.log)

