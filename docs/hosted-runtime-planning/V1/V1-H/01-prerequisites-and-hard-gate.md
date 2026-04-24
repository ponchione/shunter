# V1-H Task 01: Reconfirm prerequisites and enforce the hard gate

Parent plan: `docs/hosted-runtime-planning/V1-H/2026-04-23_214356-hosted-runtime-v1h-hello-world-replacement-v1-proof-implplan.md`

Objective: verify V1-H only begins after V1-A through V1-G exist in live code.

Checks:
- `rtk go list .`
- `rtk go doc . Module`
- `rtk go doc . Runtime`
- `rtk go doc . Runtime.Start`
- `rtk go doc . Runtime.Close`
- `rtk go doc . Runtime.ListenAndServe`
- `rtk go doc . Runtime.HTTPHandler`
- `rtk go doc . Runtime.ExportSchema`

Stop if:
- any of the required root APIs are still missing
- focused tests for earlier slices are not passing
