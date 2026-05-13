# Functionality Audit Agent Prompt

Use this prompt to continue the package-by-package clean-room functionality
audit.

```text
You are working in /home/gernsback/source/shunter.

First read:
1. RTK.md
2. AGENTS.md
3. working-docs/functionality-gap-log.md

Task:
Continue the package-by-package functionality audit. Select one bounded
Shunter package or module, inspect its live Go code and narrow docs/specs, then
compare it with the closest crate/module in the ignored read-only reference
tree. Do not copy, port, paraphrase, or mechanically translate source from the
reference tree.

Audit rules:
- Treat the reference tree only as product/functionality evidence.
- Do not treat wire compatibility, byte compatibility, client compatibility,
  source compatibility, naming compatibility, or business-model compatibility
  as goals.
- Look for capabilities Shunter may want to clone independently.
- Prefer live Shunter code and tests over specs when classifying current
  behavior.
- Stay inside the selected slice.
- Log findings in working-docs/functionality-gap-log.md.
- Keep findings concise, implementation-facing, and prioritized when possible.

Suggested workflow:
1. Run `rtk go doc` and `rtk go list -json` for the selected Shunter package.
2. Read the package files and focused tests.
3. Inspect the closest reference crate/module only enough to identify behavior
   or product capabilities.
4. Decide which differences are intentional scope/non-goals and which are
   relevant functionality gaps.
5. Append one dated section to working-docs/functionality-gap-log.md with:
   - compared Shunter package and reference crate/module
   - current Shunter behavior
   - relevant findings
   - potential follow-up
6. Run focused tests for the Shunter package if code behavior was interpreted
   or if docs were changed nearby.
7. Report back with the selected slice, logged findings, files changed, and
   verification performed.
```
