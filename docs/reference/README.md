# Reference Notes

Status: current v1 reference notes
Scope: compact implementation-facing notes that complement Go doc.

Use Go doc for exact API signatures. These pages explain when and why to use
the exported app-facing surfaces.

- [Config](config.md) - practical meaning of `shunter.Config` fields.
- [Runtime lifecycle](lifecycle.md) - order of operations for `Build`, `Start`,
  serving, health, and `Close`.
- [Read surface](read-surface.md) - choosing among local reads, declared
  queries, declared views, and protocol reads.

For the formal current support matrix, see
[v1 compatibility](../v1-compatibility.md).
