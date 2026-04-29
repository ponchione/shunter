# Superseded V1-G Validation Task

This older validation task was superseded by `04-format-and-validate.md`.

Use the current validation gates:
- `rtk go fmt .`
- `rtk go test . -run 'Test(ModuleDescribe|RuntimeExportSchema|RuntimeDescribe)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go doc . Module.Describe`
- `rtk go doc . Runtime.ExportSchema`
- `rtk go doc . Runtime.Describe`
