# V2.5 Task 09: Contract Diff And Migration Policy

Parent plan: `docs/features/V2.5/00-current-execution-plan.md`

Depends on:
- Task 02 schema table read policy
- Task 06 protocol and codegen declared reads
- Task 07 visibility filter declarations

Objective: ensure contracts, contract validation, contract diff, and migration
planning classify read policy changes accurately.

## Required Context

Read:
- `docs/features/V2/READ-AUTHORIZATION-DESIGN.md`
- task completion notes from tasks 02, 06, and 07

Inspect:

```sh
rtk go doc . ModuleContract
rtk go doc ./contractdiff.Plan
rtk go doc ./contractworkflow
rtk rg -n "PermissionContract|ReadModelContract|Migration|Classification|Compare|Contract|Validate" . contractdiff contractworkflow cmd/shunter
rtk rg -n "querySQL|viewSQL|permissions|contract" codegen
```

## Target Behavior

Contracts must expose policy metadata needed by generated clients and review
tools:

- table access mode
- table read permission tags
- declared query/view permission tags
- declared query/view SQL metadata
- declared read execution metadata if introduced by task 06
- visibility filter metadata or safe summary

Contract validation must reject malformed policy data.

Contract diff must classify:

- table access changes
- table read permission changes
- declaration permission changes
- declaration SQL changes
- declared read execution surface changes
- visibility filter additions
- visibility filter removals
- visibility filter SQL/return-table changes

Migration/compatibility classification should be conservative:

- making data more visible is compatibility-sensitive and security-sensitive
- making data less visible can break clients and is compatibility-sensitive
- removing declared reads or visibility filters is breaking unless explicitly
  classified otherwise by existing migration policy conventions
- adding stricter permissions is breaking for clients without those permissions
- loosening permissions is security-sensitive even if client-compatible

## Tests To Add First

Add focused failing tests for:

- contract JSON includes table read policy
- contract JSON includes visibility filter metadata
- contract validation rejects invalid policy metadata
- diff detects table private/public/permissioned changes
- diff detects read permission additions/removals/changes
- diff detects visibility filter add/remove/change
- migration plan classifies stricter read policy as breaking
- migration plan classifies looser read policy as security-sensitive metadata
  or the nearest existing classification
- codegen preserves any metadata it must expose
- CLI contract commands still round-trip updated contracts

## Validation

Run at least:

```sh
rtk go fmt . ./contractdiff ./contractworkflow ./cmd/shunter ./codegen
rtk go test ./contractdiff ./contractworkflow ./cmd/shunter ./codegen -count=1
rtk go test . -run 'Test.*(Contract|Migration|Permission|Visibility|Codegen)' -count=1
rtk go vet . ./contractdiff ./contractworkflow ./cmd/shunter ./codegen
```

Run `rtk go test ./... -count=1` because contract shape changes can affect
many packages.

## Completion Notes

When complete, update this file with:

- final contract fields
- diff classifications
- migration compatibility decisions
- validation commands run

