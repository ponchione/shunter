# Hosted Runtime V2-E Current Execution Plan

Goal: turn passive v1.5 permission metadata into a narrow, testable
policy/auth enforcement foundation where real identity data supports it.

V2-E target:
- preserve dev-friendly local defaults
- define a small claims/permission model compatible with existing JWT identity
  validation
- enforce reducer permissions before widening to read permissions
- keep policy attached to reducers, queries, and views
- keep broad standalone policy frameworks out of scope

Task sequence:
1. Reconfirm auth, caller identity, reducer, declaration, and contract metadata
   surfaces.
2. Add failing tests for reducer permission enforcement and dev/strict behavior.
3. Implement the smallest permission claim extraction and enforcement path.
4. Extend enforcement to declared reads/raw SQL only after V2-D clarifies read
   semantics.
5. Format and validate V2-E gates.

Scope boundaries:
- In scope: narrow permission tags, JWT claim extraction if needed, local-call
  and protocol reducer checks, contract/codegen metadata consistency.
- Out of scope: tenant framework, role database, external IdP integration
  beyond current JWT validation, broad policy language, multi-module scoping.

Immediate next V2 slice after V2-E: V2-F multi-module hosting exploration.
