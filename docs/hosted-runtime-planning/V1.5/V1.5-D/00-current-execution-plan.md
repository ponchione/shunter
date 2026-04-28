# Hosted Runtime V1.5-D Current Execution Plan

Goal: attach narrow permission/read-model metadata to declared read/write
surfaces.

Task sequence:
1. Reconfirm declaration, contract, and codegen surfaces.
2. Add failing tests for permission metadata attachment and export.
3. Implement narrow metadata on reducers, queries, and views.
4. Format and validate V1.5-D gates.

Task progress:
- Task 01 pending.
- Task 02 pending.
- Task 03 pending.
- Task 04 pending.

V1.5-D target:
- metadata attaches to reducers
- metadata attaches to named queries
- metadata attaches to named views/subscriptions
- metadata appears in the canonical contract
- generated clients/docs can inspect the metadata

V1.5-D must not become:
- a broad standalone policy framework
- a full multi-tenant auth product
- runtime-blocking access-control enforcement unless a later contract explicitly
  designs that behavior

Immediate next V1.5 slice after V1.5-D: V1.5-E migration metadata and contract
diff tooling.

