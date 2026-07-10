# Generated artifact drift

- Initial state: all three paths below matched `HEAD`; the initial status is in [preflight.md](preflight.md).
- Classification: deterministic gate-created generated drift.
- Disposition: restored after preserving this diagnosis; no pre-existing user change was restored or rewritten.

## TypeScript client build

`rtk npm --prefix typescript/client run build` changed only:

- `typescript/client/dist/index.js.map`
- `typescript/client/dist/index.d.ts.map`

The emitted `index.js` and `index.d.ts` remained unchanged. The current command resolved TypeScript `7.0.2`; this checkout has no TypeScript client lockfile or installed local compiler, while `package.json` declares `typescript: ^5.9.0` and invokes `npm exec --yes --package typescript -- tsc`. The source-map `mappings` lengths changed as follows:

| Artifact | HEAD | Generated |
| --- | ---: | ---: |
| `index.js.map` | 122160 | 122295 |
| `index.d.ts.map` | 29018 | 29100 |

The build, dry-run pack, and package smoke commands passed with the regenerated maps, but the source-map-only drift means the checked-in `dist` tree is not cleanly reproducible under the compiler currently resolved by the gate.

## Static hosted-binary gate

`rtk bash scripts/static-hosted-binary-gate.sh` appended one blank line to `examples/hosted-chat/frontend/src/generated/hosted_chat.ts`. No semantic generated-code change was present, and the gate passed.

## Restoration

Only the three paths proven clean by initial status and changed by this qualification were restored:

`rtk git restore -- examples/hosted-chat/frontend/src/generated/hosted_chat.ts typescript/client/dist/index.d.ts.map typescript/client/dist/index.js.map`

Post-restoration status contains only the pre-existing documentation slice plus this qualification evidence.

