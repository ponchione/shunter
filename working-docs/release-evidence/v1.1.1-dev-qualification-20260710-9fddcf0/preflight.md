# Qualification preflight

- Source version: `v1.1.1-dev`
- Shunter commit: `9fddcf0b842f72d5e24e399d558042394b337fbd`
- Initial Shunter worktree: dirty only in the pre-existing, user-owned documentation paths listed by `git status` below.
- TypeScript mirror check: pass (`VERSION` without leading `v` is `1.1.1-dev`, matching `typescript/client/package.json`).
- CLI source-version check: pass (`shunter v1.1.1-dev`).
- External canary commit: `e69bce73cb49fbd2334dd8b99eb664b07fc6e132`; initial worktree clean on local branch `master`.

## UTC time

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk date -u +%Y-%m-%dT%H:%M:%SZ`
- Exit code: 0

~~~text
2026-07-10T10:56:12Z
~~~

## Operator

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk whoami`
- Exit code: 0

~~~text
gernsback
~~~

## Platform

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk uname -a`
- Exit code: 0

~~~text
Linux gernsback 6.17.0-35-generic #35~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Tue May 26 19:30:42 UTC 2 x86_64 x86_64 x86_64 GNU/Linux
~~~

## Go version

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk go version`
- Exit code: 0

~~~text
go version go1.26.3 linux/amd64
~~~

## Go platform

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk go env GOOS GOARCH`
- Exit code: 0

~~~text
linux
amd64
~~~

## Shunter status

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk git status --short --branch`
- Exit code: 0

~~~text
* main...origin/main
 M working-docs/README.md
 M working-docs/deferred-functionality-backlog.md
 M working-docs/hosted-backend-roadmap.md
?? working-docs/nesl-operational-use-thought-experiment.md
?? working-docs/recommendations/
~~~

## Shunter HEAD

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk git rev-parse HEAD`
- Exit code: 0

~~~text
9fddcf0b842f72d5e24e399d558042394b337fbd
~~~

## Shunter history

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk git log -10 --date=iso-strict --pretty=fuller`
- Exit code: 0

~~~text
commit 9fddcf0b842f72d5e24e399d558042394b337fbd
Author:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
AuthorDate: 2026-06-10T13:50:16-04:00
Commit:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
CommitDate: 2026-06-10T13:50:16-04:00

    Reduce one-off query helper duplication

commit b03d4b329cf40be096d3d9bfbc71bf13962541a4
Author:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
AuthorDate: 2026-06-10T13:50:10-04:00
Commit:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
CommitDate: 2026-06-10T13:50:10-04:00

    Simplify multi-join split-or fixtures

commit ad81c2c8a1162553b1c1d4f37f1003f046457778
Author:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
AuthorDate: 2026-06-10T13:25:15-04:00
Commit:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
CommitDate: 2026-06-10T13:25:15-04:00

    code reduction

commit 8527a2251e90ac943d69e7c21517f436a4ddfa4c
Author:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
AuthorDate: 2026-06-10T13:15:29-04:00
Commit:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
CommitDate: 2026-06-10T13:15:29-04:00

    doc updates

commit d5c3bb3cf120544840187d71dd475d86b8259d60
Author:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
AuthorDate: 2026-06-10T12:59:29-04:00
Commit:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
CommitDate: 2026-06-10T12:59:29-04:00

    test: factor repeated runtime test setup

commit da2ca745f83ba480365fc476c80abc6c53d6e153
Author:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
AuthorDate: 2026-06-10T12:59:25-04:00
Commit:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
CommitDate: 2026-06-10T12:59:25-04:00

    test: share protocol admission error helpers

commit 3c1dc929981e573062dd8589a652268eb850c47c
Author:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
AuthorDate: 2026-06-10T12:59:21-04:00
Commit:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
CommitDate: 2026-06-10T12:59:21-04:00

    test: collapse linear multi-join fixtures

commit 59ad9bac0e55bc05a80a8db1835b4f2b4d43ba00
Author:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
AuthorDate: 2026-06-02T11:30:02-04:00
Commit:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
CommitDate: 2026-06-02T11:30:02-04:00

    Document RC hosted timing blocker

commit a903383aab2b2de2f959be2449a21413acfe3888
Author:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
AuthorDate: 2026-06-02T11:18:13-04:00
Commit:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
CommitDate: 2026-06-02T11:18:13-04:00

    Add hosted subscription fanout timing evidence

commit 554e2da2974d6ebd9d4609693fe0b25d3f9ddbb9
Author:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
AuthorDate: 2026-06-02T11:01:33-04:00
Commit:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
CommitDate: 2026-06-02T11:01:33-04:00

    Add hosted subscription timing evidence
~~~

## Shunter tags

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk git tag --sort=-version:refname`
- Exit code: 0

~~~text
v1.1.0
v1.0.1
v1.0.0
v0.1.1
v0.1.0
~~~

## Pre-existing tracked diff summary

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk git diff --stat`
- Exit code: 0

~~~text
working-docs/README.md                         |  5 +++
 working-docs/deferred-functionality-backlog.md | 45 ++++++++++----------------
 working-docs/hosted-backend-roadmap.md         | 34 +++++++++++++++++++
 3 files changed, 56 insertions(+), 28 deletions(-)
~~~

## Pre-existing tracked changed paths

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk git diff --name-status`
- Exit code: 0

~~~text
M	working-docs/README.md
M	working-docs/deferred-functionality-backlog.md
M	working-docs/hosted-backend-roadmap.md

Changes:
~~~

## Source version

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk read VERSION`
- Exit code: 0

~~~text
v1.1.1-dev
~~~

## TypeScript package metadata

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk read typescript/client/package.json`
- Exit code: 0

~~~text
{
  "name": "@shunter/client",
  "version": "1.1.1-dev",
  "description": "Private local Shunter v1 TypeScript client runtime.",
  "private": true,
  "type": "module",
  "types": "./dist/index.d.ts",
  "sideEffects": false,
  "exports": {
    ".": {
      "types": "./dist/index.d.ts",
      "browser": "./dist/index.js",
      "import": "./dist/index.js",
      "default": "./dist/index.js"
    }
  },
  "files": [
    "dist",
    "README.md"
  ],
  "scripts": {
    "build": "npm exec --yes --package typescript -- tsc -p tsconfig.build.json",
    "pack:dry-run": "npm pack --dry-run",
    "smoke:package": "node test/package-smoke.mjs",
    "test": "npm run typecheck && npm exec --yes --package typescript -- tsc -p tsconfig.runtime-test.json && node test/runtime-behavior.test.mjs && node .tmp_runtime_test/test/generated-type-index-decoding.test.js && node test/hosted-type-index-canary.test.mjs",
    "typecheck": "npm exec --yes --package typescript -- tsc -p tsconfig.json --noEmit"
  },
  "devDependencies": {
    "typescript": "^5.9.0"
  }
}
~~~

## CLI version

- Working directory: `/home/gernsback/source/shunter`
- Command: `rtk go run ./cmd/shunter version`
- Exit code: 0

~~~text
shunter v1.1.1-dev
go go1.26.3
~~~

## Canary status

- Working directory: `/home/gernsback/source/opsboard-canary`
- Command: `rtk git status --short --branch`
- Exit code: 0

~~~text
## master
~~~

## Canary HEAD

- Working directory: `/home/gernsback/source/opsboard-canary`
- Command: `rtk git rev-parse HEAD`
- Exit code: 0

~~~text
e69bce73cb49fbd2334dd8b99eb664b07fc6e132
~~~

## Canary history

- Working directory: `/home/gernsback/source/opsboard-canary`
- Command: `rtk git log -1 --date=iso-strict --pretty=fuller`
- Exit code: 0

~~~text
commit e69bce73cb49fbd2334dd8b99eb664b07fc6e132
Author:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
AuthorDate: 2026-05-25T09:02:21-04:00
Commit:     Mitchell Ponchione <mitchell.ponchione@protonmail.com>
CommitDate: 2026-05-25T09:02:21-04:00

    Refresh canary for current Shunter protocol
~~~


