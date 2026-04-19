# Phase 2 Slice 1b — SQL front door (OneOffQuery + SubscribeSingle + SubscribeMulti)

## Objective

Close the last Phase 2 protocol-surface parity divergence: the three client envelopes that currently carry structured predicate data (`OneOffQueryMsg`, `SubscribeSingleMsg`, `SubscribeMultiMsg`) must carry SQL strings on the wire, matching `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs` (`OneOffQuery.query_string`, `SubscribeSingle.query`, `SubscribeMulti.query_strings`).

Two deferral pins flip from deferral to positive:
- `protocol/parity_message_family_test.go::TestPhase1DeferralOneOffQueryStructuredNotSQL`
- `protocol/parity_message_family_test.go::TestPhase2DeferralSubscribeMultiQueriesStructured`

## What is explicitly deferred to a later slice (Slice 1c)

`OneOffQueryMsg.RequestID uint32 → MessageID []byte` (reference `message_id: Box<[u8]>`). Orthogonal wire-shape divergence with its own pin. Recorded in `TECH-DEBT.md`.

## Reference shapes

```
OneOffQuery      { message_id: Box<[u8]>, query_string: Box<str> }              // v1.rs:247
SubscribeSingle  { query: Box<str>, request_id: u32, query_id: QueryId }         // v1.rs:189
SubscribeMulti   { query_strings: Box<[Box<str>]>, request_id: u32, query_id }  // v1.rs:203
```

## Approach

1. New package `query/sql`. Minimum-viable recursive-descent parser.
2. Grammar (exactly what existing `Query{TableName, Predicates}` can represent):

   ```
   stmt       = "SELECT" "*" "FROM" ident [ where ]
   where      = "WHERE" eq ( "AND" eq )*
   eq         = ident "=" literal
   literal    = integer | bool | string
   ident      = [A-Za-z_][A-Za-z0-9_]*
   ```

   - Case-insensitive keywords (`select`, `from`, `where`, `and`, `true`, `false`).
   - Integer literal: `-?[0-9]+` → `types.ValueU64` or `types.ValueI64` — match the column type at plan time in the caller.
   - Bool literal: `true`/`false` → `types.ValueBool`.
   - String literal: `'...'` (single-quoted, simple — no escapes beyond `''`) → `types.ValueString`.
   - Anything else (`*` other than projection, JOIN, OR, ORDER BY, etc.) returns `ErrUnsupportedSQL`.

3. Output: `sql.Parse(string) (protocol.Query, error)` returning the existing structured representation. Handlers stay on the existing evaluator; only the wire shape + parse step changes.

4. Wire changes in `protocol/client_messages.go`:
   - `OneOffQueryMsg{RequestID, TableName, Predicates}` → `OneOffQueryMsg{RequestID, QueryString}`.
   - `SubscribeSingleMsg.Query Query` → `SubscribeSingleMsg.QueryString string`.
   - `SubscribeMultiMsg.Queries []Query` → `SubscribeMultiMsg.QueryStrings []string`.
   - Encoders write `writeString` / `writeStringList`; decoders mirror.

5. Handlers parse once on entry, then feed the existing executor path:
   - `handle_oneoff.go`: parse → existing snapshot scan + predicate match.
   - `handle_subscribe_single.go`: parse → existing `RegisterSet` with single predicate.
   - `handle_subscribe_multi.go`: parse each → existing `RegisterSet` with predicate list.
   - Parse failure: emit existing error response (`OneOffQueryResult{Status:1, Error:...}` or `SubscriptionError`).

6. Flip the two deferral pins and add a positive shape pin per envelope:
   - `TestPhase1DeferralOneOffQueryStructuredNotSQL` → `TestPhase2Slice1OneOffQuerySQLShape` — asserts fields `[RequestID, QueryString]`.
   - `TestPhase2DeferralSubscribeMultiQueriesStructured` → `TestPhase2Slice1SubscribeMultiSQLShape` — asserts `QueryStrings` is `[]string`.
   - Add `TestPhase2Slice1SubscribeSingleSQLShape` — asserts `QueryString` is `string`.

7. Minimum parser acceptance tests (new `query/sql/parser_test.go`):
   - `SELECT * FROM users` → `Query{TableName:"users", Predicates:nil}`
   - `SELECT * FROM users WHERE id = 1` → single uint predicate
   - `SELECT * FROM users WHERE id = 1 AND name = 'alice'` → two predicates
   - `select * from Users where Id=1` → case-insensitive keywords, identifiers preserved
   - Rejection cases: `SELECT id FROM users` (non-star projection), `SELECT * FROM a JOIN b`, `SELECT * FROM users WHERE id > 1`, `SELECT * FROM users ORDER BY id`, trailing garbage, unterminated string, malformed integer, missing FROM, missing table name.

## Strict TDD order

1. Parser failing test → parser skeleton → pass. Iterate through grammar cases.
2. Flip `OneOffQueryMsg` shape. Start with wire round-trip test (`EncodeClientMessage` ↔ `DecodeClientMessage`) that expects the new shape. Adjust encoder/decoder. Fix parity pin.
3. Update `handle_oneoff.go`; fix `handle_oneoff_test.go`. Run focused.
4. Same loop for `SubscribeSingleMsg`.
5. Same loop for `SubscribeMultiMsg`. Flip deferral pin.
6. `rtk go test ./protocol ./subscription ./executor` focused.
7. `rtk go test ./...` broad.

## Files expected to change

Primary:
- NEW `query/sql/parser.go`
- NEW `query/sql/parser_test.go`
- `protocol/client_messages.go`
- `protocol/client_messages_test.go`
- `protocol/handle_oneoff.go`
- `protocol/handle_oneoff_test.go`
- `protocol/handle_subscribe_single.go`
- `protocol/handle_subscribe_multi.go`
- `protocol/parity_message_family_test.go`

Possible incidental:
- `protocol/dispatch.go` (only if handler signatures change)
- `protocol/client_messages_test.go` for updated round-trip cases
- Any subscription-side test that constructs a `SubscribeSingleMsg`/`SubscribeMultiMsg` literal in-test

Explicitly untouched:
- `executor/*` — executor path stays identical; parse happens in protocol layer only.
- `subscription/*` evaluator + fanout — predicates reach them in the same shape as before.
- Call-reducer seam (just locked in 2026-04-19_184219 handoff).

## Do not change

- Do not reshape `Query` / `Predicate` types — they remain the internal structured form.
- Do not move SQL parsing into `executor` or `subscription`. It is a protocol-layer wire-adaptation step.
- Do not widen the parser beyond the grammar above. Anything broader is a new slice.
- Do not flip `OneOffQueryMsg.RequestID` to `MessageID []byte` in this slice (Slice 1c).
- Do not touch `CallReducer` or the committed-reply seam.

## Acceptance criteria

Confirmed true at slice close:
- `SELECT * FROM T` and `SELECT * FROM T WHERE c = v [AND c = v]*` parse to the existing `Query` shape.
- Unsupported SQL rejected with a clear `parse: ...` error surfaced on the wire (OneOff: `OneOffQueryResult.Status=1`; Subscribe: `SubscriptionError`).
- All three envelopes round-trip `Encode → Decode` with SQL strings on the wire.
- `TestPhase1DeferralOneOffQueryStructuredNotSQL` and `TestPhase2DeferralSubscribeMultiQueriesStructured` removed or inverted to positive pins.
- `rtk go test ./...` passes across the 9 packages.

## Stop / escalate criteria

Stop and escalate if:
- Reference grammar requires features the minimal grammar cannot express for a given pinned scenario.
- Any existing subscription-set or caller-delivery test needs structural change (suggests the seam leaked into evaluator / fanout — out of scope here).
- SubscribeMulti SQL-list length / ordering semantics disagree with the reference in a way the structured path silently papered over.

## Follow-ups (Slice 1c and beyond)

- **Slice 1c**: flip `OneOffQueryMsg.RequestID uint32 → MessageID []byte` matching reference `message_id: Box<[u8]>`. New pin `TestPhase2Slice1COneOffQueryMessageIDBytes`. Record in `TECH-DEBT.md` as TD-14x.
- Broader SQL surface (projection other than `*`, comparison operators, OR, JOIN, ORDER BY, LIMIT) stays deferred until a pinned parity scenario demands it.

## Verification commands

Focused:
- `rtk go test ./query/sql`
- `rtk go test ./protocol -run 'Phase2Slice1|OneOffQuery|Subscribe(Single|Multi)'`

Broader:
- `rtk go test ./protocol ./subscription ./executor`
- `rtk go test ./...`
