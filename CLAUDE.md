# Shunter

Read `docs/PROJECT-BRIEF.md` first. It defines what Shunter is, the architectural decomposition, and the spec plan.

## Current Phase: Spec Derivation

We are reading the SpacetimeDB codebase (in `reference/SpacetimeDB/`) to understand algorithms and architecture, then writing standalone specs in `docs/specs/SPEC-*.md`. No Rust code is copied or ported. Specs describe *what* and *why* — not *how SpacetimeDB does it*.

Specs must be implementable by someone who has never seen the SpacetimeDB source.

## Rules

- Output goes in `docs/`. Research notes go in `docs/research/notes/`.
- Never copy Rust code into specs. Describe the algorithm in plain language.
- Each spec is self-contained with defined interfaces to other subsystems.
- When a design decision has multiple valid approaches, present the tradeoffs and make a recommendation.

## Repo Layout

```
docs/               # Project brief and specs (our output)
reference/           # SpacetimeDB clone (read-only research input)
```

Implementation comes later via SodorYard. That's not this phase.

@RTK.md