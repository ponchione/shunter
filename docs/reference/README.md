# Reference Notes

Use Go doc for exact API signatures. These pages explain when and why to use
the exported app-facing surfaces.

- [Config](config.md) - practical meaning of `shunter.Config` fields.
- [Runtime lifecycle](lifecycle.md) - order of operations for `Build`, `Start`,
  serving, health, and `Close`.
- [Read surface](read-surface.md) - choosing among local reads, declared
  queries, declared views, and protocol reads.
- [Running-app admin CLI](admin-cli-running-app.md) - behavior and maintenance
  notes for `shunter call`, `shunter procedure`, `shunter query`, and live
  diagnostics commands.
