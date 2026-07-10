# Harden Operational Authorization

Status: recommended when an enterprise identity provider and multi-role app are
in scope

Promotion trigger: a real application needs site, region, department, or role
scoping that cannot be expressed safely through current token permissions and
declared views alone.

Owners: auth, root module declarations, protocol admission, visibility,
contracts, TypeScript client

## Why

Operational applications commonly distinguish dispatchers, plant personnel,
maintenance, sales, field teams, supervisors, and administrators. Shunter can
validate enterprise OIDC tokens and enforce passive permission tags, but it
does not currently provide an app-defined claim-to-permission mapper and its
visibility filters remain row-local.

## Outcome

A reviewed authorization model connecting enterprise identity claims to
Shunter actions and data scopes without embedding provider-specific assumptions
throughout reducers and views.

## Work

1. Model real actors, roles, sites, regions, and separation-of-duty rules before
   proposing API changes.
2. Use declared reads over private tables and current permission metadata for
   the first implementation where they are sufficient.
3. Decide whether claim normalization belongs in the identity provider, app
   shell, or a bounded Shunter mapping hook.
4. If Shunter gains a mapper, require explicit claim allowlists, output bounds,
   deterministic mapping, copy isolation, and startup validation.
5. Represent site/region membership as app state when it changes independently
   of token issuance.
6. Revisit cross-table visibility only with a concrete query that cannot be
   expressed as a safe declared view.
7. Export enough authorization metadata for contract review without exporting
   sensitive identity-provider configuration.

## Non-Goals

- a generic IAM product
- provider-specific role semantics in the runtime
- accepting arbitrary token claims directly in visibility SQL
- recursive cross-table RLS solely for reference-runtime parity
- treating generated public profiles as authorization boundaries

## Completion Evidence

- threat model and role/data-scope matrix for the promoted app
- allowed and denied tests for reducers, procedures, declared reads, raw reads,
  subscriptions, and reconnect
- key rotation and claim-change behavior documented
- no private-row or existence leak through joins, projections, or aggregates
- admin bypass remains explicit and absent from normal client tokens
