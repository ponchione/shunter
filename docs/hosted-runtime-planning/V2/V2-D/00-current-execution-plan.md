# Hosted Runtime V2-D Current Execution Plan

Goal: reconcile v1.5 named query/view declarations with the live protocol SQL
read path before either surface grows larger.

V2-D target:
- define how declared queries/views relate to executable protocol reads
- preserve existing OneOffQuery and Subscribe SQL behavior
- avoid permanent divergence between named declarations, generated clients, and
  raw SQL protocol usage
- keep SQL grammar expansion out of scope unless required by the chosen
  declaration model

Task sequence:
1. Reconfirm query/view declaration, protocol SQL, and subscription evaluator
   contracts.
2. Add failing tests that expose the current divergence between declarations
   and executable reads.
3. Implement the smallest declaration-to-execution relationship chosen for
   v2.
4. Update contract/codegen output only if the executable read model adds stable
   exported metadata.
5. Format and validate V2-D gates.

Scope boundaries:
- In scope: named read/view semantics, raw SQL coexistence rules, generated
  client expectations, protocol compatibility.
- Out of scope: broad SQL expansion, cross-language query runtime, policy
  enforcement, multi-module routing, process isolation.

Immediate next V2 slice after V2-D: V2-E policy/auth enforcement foundation.
