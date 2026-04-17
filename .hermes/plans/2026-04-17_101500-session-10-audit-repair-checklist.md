# Session 10 audit-repair checklist

Violations to fix
1. AUDIT_HANDOFF Lane B intro still says live-code drift starts in Session 11+ even though Session 11 is now SPEC-006 residue and drift starts in Session 12+.
2. SPEC-005 divergence rows §3.1-§3.8 and §3.10-§3.11 in AUDIT_HANDOFF point at a nonexistent "divergence block" and use a weak procedural defer reason.
3. Session 11 advancement should remain internally consistent after fixes across the Lane B narrative, residue rows, and cadence text.

Concrete plan
1. Patch AUDIT_HANDOFF Lane B intro/rules text so every drift-start reference consistently says Session 12+.
2. Add a real SPEC-005 divergence section covering the currently deferred protocol divergences.
3. Update AUDIT_HANDOFF SPEC-005 rows §3.1-§3.8 and §3.10-§3.11 to reference the new SPEC-005 divergence section and mark them closed.
4. Re-read the touched sections, then run RTK git diff/stat/status checks to verify the violations are resolved.
5. Commit the docs-only repair if the worktree is clean except for expected untracked .hermes/plans files.