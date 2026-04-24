# V1-H Task 02: Create the hosted-runtime hello-world example skeleton

Parent plan: `docs/hosted-runtime-planning/V1-H/2026-04-23_214356-hosted-runtime-v1h-hello-world-replacement-v1-proof-implplan.md`

Objective: create the normal app-facing example around the top-level `shunter` API.

Files:
- Create or rewrite the normal hosted example, preferably `cmd/shunter-hello/main.go` or the chosen replacement path
- Create or update its test file

Implementation requirements:
- define one `greetings` table through `Module`
- define one `say_hello` reducer through `Module`
- build/start/serve through `shunter.Build` plus `Runtime.Start`/`HTTPHandler` or `Runtime.ListenAndServe`
- keep the domain intentionally tiny
- do not directly instantiate kernel subsystem graph pieces in the normal example
