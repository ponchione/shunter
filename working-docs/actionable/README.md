# Actionable Backlog Slices

Status: explicitly authorized implementation-facing planning docs only

This directory is for narrow implementation slices explicitly promoted for a
concrete goal while larger deferred product decisions remain parked. A file's
presence does not authorize continued work or select the next feature.

- `subscription-evidence-matrix.md` - closed subscription evidence campaign,
  retained for historical context. It is not an instruction to keep adding
  synthetic benchmark stages.

A new actionable slice requires explicit promotion for a concrete
implementation goal. Do not create a slice or choose a feature merely to keep
this directory populated; no next implementation task is selected here.

These docs are not numbered specs. Prefer live code and tests when they
disagree with planning text.

When updating a slice, keep it implementation-facing:

- name current code/test anchors before proposing work
- separate confirmed gaps from future product ideas
- stage tasks so the first stage can land without dynamic serving, managed
  control-plane behavior, or broad SDK/language expansion
- include the narrow validation commands that prove the touched surface
