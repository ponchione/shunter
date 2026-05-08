# Reference Notes

Status: rough draft
Scope: compact implementation-facing notes that complement Go doc.

Use Go doc for exact API signatures. These pages explain when and why to use
the exported app-facing surfaces.

- `config.md` - practical meaning of `shunter.Config` fields.
- `lifecycle.md` - order of operations for `Build`, `Start`, serving, health,
  and `Close`.
- `read-surface.md` - choosing among local reads, declared queries, declared
  views, and protocol reads.

For the formal current support matrix, see `../v1-compatibility.md`.
