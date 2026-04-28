# Hosted Runtime V2-G Current Execution Plan

Goal: decide whether out-of-process module execution is justified, then gate
any prototype behind the runtime/module boundary proven by earlier V2 slices.

V2-G target:
- preserve statically linked in-process modules as the simple supported path
- identify the reducer/query/lifecycle protocol required for a process boundary
- prove whether failure/resource isolation benefits justify the added
  complexity
- avoid committing to a production process runner before transaction,
  subscription, and durability semantics are proven

Task sequence:
1. Reconfirm runtime, executor, schema, store, subscription, protocol, and
   contract seams that process isolation would cross.
2. Add failing contract tests for the process-boundary interface, not a full
   runner.
3. Prototype a narrow invocation boundary only if the tests justify it.
4. Record explicit keep/defer/remove decisions for process isolation.
5. Format and validate V2-G gates.

Scope boundaries:
- In scope: boundary protocol shape, invocation semantics, failure semantics,
  feasibility proof.
- Out of scope: production supervisor, dynamic plugin marketplace,
  cross-language SDK commitment, cloud deployment, replacing in-process
  modules.

V2-G is a gate. It may legitimately end with "defer out-of-process execution."
