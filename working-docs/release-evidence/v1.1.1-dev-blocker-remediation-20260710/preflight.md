# v1.1.1-dev blocker-remediation preflight

This is mutable-worktree remediation evidence, not a clean-ref qualification
record. No commit, tag, publication, push, or qualification decision is
created by this directory.

Captured before this remediation directory or any implementation file was
created or modified.

## Shunter

Command: `rtk git status --short --branch`

```text
* main...origin/main
 M working-docs/README.md
 M working-docs/deferred-functionality-backlog.md
 M working-docs/hosted-backend-roadmap.md
 M working-docs/release-qualification.md
?? working-docs/nesl-operational-use-thought-experiment.md
?? working-docs/recommendations/
?? working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/
```

Command: `rtk git rev-parse HEAD`

```text
9fddcf0b842f72d5e24e399d558042394b337fbd
```

Command: `rtk git diff --stat`

```text
 working-docs/README.md                         |  5 ++
 working-docs/deferred-functionality-backlog.md | 45 +++++++----------
 working-docs/hosted-backend-roadmap.md         | 34 +++++++++++++
 working-docs/release-qualification.md          | 67 ++++++++++++++++++++++++++
 4 files changed, 123 insertions(+), 28 deletions(-)
```

Command: `rtk git diff --name-status`

```text
M working-docs/README.md
M working-docs/deferred-functionality-backlog.md
M working-docs/hosted-backend-roadmap.md
M working-docs/release-qualification.md
```

Command: `rtk go version`

```text
go version go1.26.3 linux/amd64
```

Command: `rtk go run ./cmd/shunter version`

```text
shunter v1.1.1-dev
go go1.26.3
```

`VERSION` contained `v1.1.1-dev`. `typescript/client/package.json` contained
package version `1.1.1-dev`, declared `typescript: ^5.9.0`, and invoked
`npm exec --yes --package typescript -- tsc` for compilation. There was no
`typescript/client/package-lock.json` and no executable
`typescript/client/node_modules/.bin/tsc`.

Command matching the current script's compiler selection:
`rtk npm --prefix typescript/client exec --yes --package typescript -- tsc --version`

```text
Version 7.0.2
```

## opsboard-canary

No repository-local `AGENTS.md` was present, so the Shunter RTK and safety
rules apply.

Command: `rtk git status --short --branch`

```text
## master
```

Command: `rtk git rev-parse HEAD`

```text
e69bce73cb49fbd2334dd8b99eb664b07fc6e132
```

Command: `rtk git diff --stat`

```text
```

`go.mod` contains:

```text
replace github.com/ponchione/shunter => ../shunter
```

Command: `rtk go list -m -json github.com/ponchione/shunter`

```json
{
	"Path": "github.com/ponchione/shunter",
	"Version": "v0.0.0-00010101000000-000000000000",
	"Replace": {
		"Path": "../shunter",
		"Dir": "/home/gernsback/source/shunter",
		"GoMod": "/home/gernsback/source/shunter/go.mod",
		"GoVersion": "1.25.5"
	},
	"Dir": "/home/gernsback/source/shunter",
	"GoMod": "/home/gernsback/source/shunter/go.mod",
	"GoVersion": "1.25.5"
}
```

`scripts/shunter-canary` uses `SHUNTER_CHECKOUT` only in
`log_revision_context`; it does not edit `go.mod`, set a Go workspace, or
otherwise redirect module resolution. The maintained `Makefile` contract and
codegen commands are `go run ./cmd/export-contract` and
`go run ./cmd/codegen`.
