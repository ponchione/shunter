# Deepen The Type System Before Broad SQL

Status: recommended long-term developer-experience investment

Promotion trigger: two or more real domain models require structured values
that are currently flattened, encoded as JSON, or split awkwardly across
columns.

Owners: types, bsatn, schema, store, commitlog, protocol, contracts, codegen,
TypeScript client

## Why

Shunter's flat kinds cover a wide primitive domain, but operational apps often
need enums, nested identifiers, structured status details, and general arrays.
Type depth improves module declarations, reducer arguments/results, procedures,
contracts, protocol rows, and generated clients together. It is likely more
valuable to Shunter's product boundary than broad SQL compatibility.

## Candidate Order

Do not implement all candidates in one change. Select one vertical slice after
domain evidence:

1. first-class identity and connection-ID value kinds
2. named sums/enums for lifecycle and decision states
3. nested product values for bounded structured data
4. homogeneous arrays beyond `arrayString`
5. explicit option/result algebra where nullable columns are insufficient

## Required Design Work

- recursive or bounded value representation and ownership rules
- canonical equality, comparison, hashing, and memory accounting
- index eligibility and ordering semantics
- BSATN and JSON encoding with depth and size limits
- schema and contract versioning
- commit-log and snapshot compatibility or explicit migration boundary
- TypeScript generated types and runtime codecs
- SQL coercion and unsupported-operation diagnostics
- fuzz/property corpus expansion

## Non-Goals

- reproducing another runtime's algebraic format byte for byte
- unbounded recursive values without admission limits
- indexing every structured value shape
- using JSON as the canonical typed representation
- combining type-system expansion with broad SQL or dynamic modules

## Completion Evidence

- one selected kind works end to end through declaration, validation, storage,
  durability, recovery, protocol, contract, codegen, and TypeScript execution
- compatibility and migration behavior is explicit
- malformed/deep/oversized inputs are bounded
- equality, hash, copy isolation, and round-trip properties are tested
- the real product model is simpler than its prior flattened/JSON shape
