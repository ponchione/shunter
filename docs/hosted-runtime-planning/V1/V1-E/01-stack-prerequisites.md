# V1-E Task 01: Reconfirm prerequisites and protocol contracts

Parent plan: `docs/hosted-runtime-planning/V1-E/2026-04-23_212032-hosted-runtime-v1e-runtime-network-surface-implplan.md`

Objective: verify V1-E is stacked on V1-D and grounded in the protocol/auth contracts needed for serving.

Read first:
- `docs/hosted-runtime-planning/V1-D/2026-04-23_210537-hosted-runtime-v1d-runtime-lifecycle-ownership-implplan.md`

Checks:
- `rtk go list .`
- `rtk go doc ./protocol.Server`
- `rtk go doc ./executor.NewProtocolInboxAdapter`
- `rtk go doc ./protocol.NewClientSender`
- `rtk go doc ./protocol.NewFanOutSenderAdapter`
- `rtk go doc ./protocol.ConnManager.CloseAll`
- `rtk go doc ./auth.JWTConfig`
- `rtk go doc ./auth.MintConfig`
