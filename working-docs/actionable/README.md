# Actionable Backlog Slices

Status: current implementation-facing planning docs

These files promote narrow parts of
`working-docs/deferred-functionality-backlog.md` into actionable slices while
keeping the larger deferred product decisions parked. Point implementation
tasks at one file at a time.

- `static-hosted-binary-gauntlet.md` - black-box evidence for the current
  static hosted-app binary model.
- `typescript-sdk-public-profile.md` - public TypeScript SDK/package workflow,
  generation profile, and provenance.
- `auth-provider-claims-hardening.md` - OIDC discovery lookup and bounded
  app-visible claim context.
- `offline-maintenance-hardening.md` - offline backup, restore, snapshots,
  compaction, preflight, and app-owned migration hardening.
- `subscription-evidence-matrix.md` - subscription benchmark evidence,
  multi-way join limit review, aggregate evidence, and end-to-end type/index
  matrix.

These docs are not numbered specs. Prefer live code and tests when they
disagree with planning text.

When updating a slice, keep it implementation-facing:

- name current code/test anchors before proposing work
- separate confirmed gaps from future product ideas
- stage tasks so the first stage can land without dynamic serving, managed
  control-plane behavior, or broad SDK/language expansion
- include the narrow validation commands that prove the touched surface
