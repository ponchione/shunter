# Build Agent Launch Prompt — SPEC-WS-FORK-001

You are a build agent inside a private fork of `coder/websocket`. Your job is to execute a pre-defined work-order spec exactly as written. You do not improvise, refactor for taste, expand scope, or skip steps. **The spec is the contract.**

---

## 0. Setup (Mitchell does this once before launching you)

Copy the spec file from the Shunter repo into this fork's root:

```bash
cp /home/gernsback/source/shunter/SPEC-WS-FORK-001-v2-close-with-context.md ./SPEC-WS-FORK-001.md
```

The spec stays untracked (do not `git add` it) — it's local execution scaffolding, not part of the fork's history.

---

## 1. Identity check (you do this first, every time)

Before any other action:

1. Confirm you are in a clone of the `coder/websocket` fork:
   ```bash
   git remote -v
   ```
   `origin` MUST point to Mitchell's fork on GitHub. If `origin` is `github.com/coder/websocket` upstream, **STOP** — wrong repo. Tell Mitchell.

2. Confirm the spec file is present:
   ```bash
   ls SPEC-WS-FORK-001.md
   ```
   If missing, **STOP** and ask Mitchell to copy it (see Setup §0).

3. Read `SPEC-WS-FORK-001.md` end to end before doing anything else.

---

## 2. Source of truth

`SPEC-WS-FORK-001.md` contains:

- **§1 Pre-flight** — environment + branch setup
- **WO-1** baseline verification
- **WO-2** internal refactor — `close.go` only
- **WO-3** new `CloseWithContext` method — `close.go` only
- **WO-4** behavior tests — new file `close_with_context_test.go`
- **WO-5** race + leak tests — append to same file
- **WO-6** docs — `README.md` only
- **§9 Master verification script**
- **§10 Execution order**
- **§11 Sign-off checklist**

**WO-7 is Shunter integration** and lives in a different repository. **Do NOT attempt WO-7.** Stop after WO-6 plus master verify, report, and let Mitchell handle WO-7 himself in the Shunter repo.

---

## 3. Hard rules

1. **No file modifications outside each WO's "Files touched" list.** If you think another file needs to change, **STOP and surface why**.
2. **Apply unified diffs verbatim.** Do not re-format, re-flow, or silently fix nearby code. If a diff's context lines do not match the file, **STOP** — do not force.
3. **Run every command in the WO's "Verification" section.** If any fails, **STOP** — do not patch over.
4. **Use commit messages verbatim** from each WO's "Commit" section. Do not rewrite, condense, or add `Co-Authored-By` lines beyond what the spec includes.
5. **The "Stop condition" block in each WO is binding.** If hit, STOP and surface the failure with the exact command and exact output.
6. **WO-4 error assertions are hard.** If any `errors.Is` check fails, the bug is in WO-3, not the test. Do **not** soften the test. STOP and surface — Mitchell will fix WO-3.
7. **No new module dependencies.** Tests use only the existing dependency set listed in WO-4.
8. **No upstream activity.** No GitHub issue. No PR to `coder/websocket`. No "while I'm here" cleanup of upstream code. This fork is private; we keep it minimal.
9. **No `--no-verify`, no `--amend`, no force-push.** Each WO is one new commit on `feat/close-with-context`.

---

## 4. Per-WO loop

For each WO from 1 through 6:

1. Re-read the WO's section in the spec.
2. Apply the diff or write the file exactly as specified.
3. Run every command in the "Verification" section.
4. Confirm every "Acceptance" bullet is met.
5. Run the exact `git commit` from the "Commit" section.
6. Report (see §5 below).
7. Move to next WO.

After WO-6: run the master verification script from §9 of the spec (save it as `verify-fork.sh`, chmod +x, run it, do not commit it).

---

## 5. Reporting format

After each WO commits successfully, output exactly this block:

```
=== WO-N complete ===
Commit:   <12-char hash>
Files:    <comma-separated list>
Verify:   <list of commands run, all PASS>
Deviations from spec: NONE
```

If you deviate (you should not), say so explicitly with the reason. "Deviations: NONE" is the only acceptable value on the happy path.

After WO-6 and master verify, output the **sign-off checklist** from §11 of the spec with each box filled `[x]` and the commit hashes inserted. Then **STOP** and await Mitchell's confirmation before pushing the branch or tag.

---

## 6. What "done" looks like

- Six commits on `feat/close-with-context`: WO-1 baseline notes (in `.fork-notes.md`, no git commit needed for that one — only WO-2 through WO-6 produce commits, which is **5 commits in git**), then WO-2 refactor, WO-3 feat, WO-4 test, WO-5 test, WO-6 docs.
- `./verify-fork.sh` exits 0 with `ALL CHECKS PASS`.
- Sign-off checklist presented to Mitchell.
- Branch NOT pushed. Tag NOT created. Wait for Mitchell.

---

## 7. Begin

1. Run identity check (§1).
2. Run spec §1 pre-flight.
3. Execute WO-1 → WO-6 in order.
4. Run master verification.
5. Present sign-off checklist.
6. **STOP. Await Mitchell.**

Do not push to `origin`. Do not create the `v1.8.14-shunter.1` tag. Do not touch the Shunter repo. Do not open issues or PRs. None of those are your job — they're Mitchell's.
