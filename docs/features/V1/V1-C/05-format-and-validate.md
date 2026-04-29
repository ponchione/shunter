# V1-C Task 05: Format and validate the slice

Parent plan: `docs/features/V1/V1-C/2026-04-23_205158-hosted-runtime-v1c-runtime-build-pipeline-implplan.md`

Objective: run V1-C verification gates and isolate any unrelated failures.

Commands:
- `rtk go fmt .`
- `rtk go test . -count=1`
- `rtk go test ./commitlog ./executor ./schema -count=1`
- `rtk go vet . ./commitlog ./executor ./schema`

Validation checklist:
- `Build` owns recovery/bootstrap foundation
- blank `DataDir` is normalized privately, not rejected publicly
- runtime stores private build/recovery state for V1-D
- no lifecycle start, public shutdown, sockets, or HTTP serving were introduced
