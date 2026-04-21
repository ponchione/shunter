# Shunter brain extensions for an LLM harness

Status: exploratory design note
Scope: additive design document describing what would need to be added on top of the current Shunter decomposition/spec set to make Shunter a strong replacement for an Obsidian/MCP-style agent brain.

This document does not change the existing six core specs. It describes the additional subsystem surface needed to turn the current Shunter engine shape into a practical, durable, agent-facing knowledge system.

---

## 1. Goal

The current Shunter specs describe a clean-room, SpacetimeDB-inspired realtime engine kernel:

- schema definition
- in-memory relational state
- reducer execution
- commit log + snapshots
- subscription evaluation
- websocket protocol

That kernel is a strong foundation for structured shared state, but an LLM harness “brain” needs more than a transactional realtime store.

A useful agent brain must support all of the following at once:

- durable long-term memory
- rich text/document storage
- note relationships and graph traversal
- high-quality search and retrieval
- event/session history
- stable append-only provenance where needed
- ergonomic APIs for agents and tools
- live updates for dashboards / ongoing agent sessions
- safe concurrent writes from multiple agents/tools
- predictable recall behavior

This document describes the additive capability set required to make Shunter a credible backend for that use case.

---

## 2. Product target

### 2.1 What “brain” means here

A brain for an LLM harness is not just a database. It is a runtime knowledge substrate used by:

- autonomous agents
- chat/session orchestrators
- memory tools
- planning systems
- retrieval tools
- dashboards/UIs
- background jobs / ingestion pipelines

It must store and serve at least these categories of data:

1. Notes / documents
2. Extracted entities
3. Relationships between entities and notes
4. Session transcripts and summaries
5. Tasks / plans / checklists
6. Observations / facts / evidence
7. Embeddings / retrieval indexes
8. Agent actions and provenance logs
9. User preferences and stable profile facts
10. Derived artifacts such as summaries, tags, backlinks, and snapshots

### 2.2 What the current specs already provide

The current Shunter specs already provide:

- transactional writes through reducers
- structured tables
- commit-time changesets
- snapshots and recovery
- subscriptions and push deltas
- client protocol for live consumers

That is a strong base for state correctness and multi-client consistency.

### 2.3 What is still missing

What is missing is the knowledge-system layer above the engine:

- document model
- retrieval model
- graph model
- memory semantics
- ingestion/indexing workflows
- read/query ergonomics for agent workloads
- APIs specialized for notes, recall, summarization, and provenance

---

## 3. High-level additions required

To support an LLM harness brain, Shunter would need six major additive areas:

1. Brain data model
2. Retrieval and search subsystem
3. Ingestion and indexing pipeline
4. Agent-facing API surface
5. Provenance and memory-lifecycle rules
6. Operational/UX support for brain workloads

Each is described below.

---

## 4. Brain data model additions

The current specs define a generic engine and schema system. A brain requires a canonical application-level schema pack on top.

### 4.1 Required first-class record types

At minimum, the brain layer should define these conceptual tables.

#### A. Documents / notes

Purpose:
- store durable knowledge artifacts similar to Obsidian notes
- support both human-authored and machine-authored content

Minimum fields:
- document_id
- title
- canonical_path or logical_slug
- body_text
- content_format (markdown/plaintext/json)
- created_at
- updated_at
- source_kind (human, import, transcript, tool, generated)
- source_ref (optional external source identifier)
- author/agent identity
- archived/deleted flag

Recommended additions:
- checksum/content hash
- summary
- tag set
- frontmatter/metadata map
- workspace / namespace / collection field

#### B. Document revisions

Purpose:
- preserve note history independently from current materialized document rows
- allow auditability and rollbacks without overwriting provenance

Minimum fields:
- revision_id
- document_id
- parent_revision_id
- full body or patch payload
- created_at
- created_by
- reason / operation type

Design note:
- current commit-log is not enough as a user-facing note-history model
- application-visible revision history should be queryable without commit-log forensics

#### C. Entities

Purpose:
- store normalized “things” extracted from notes/sessions
- examples: people, projects, repos, concepts, tasks, tools, decisions

Minimum fields:
- entity_id
- entity_type
- canonical_name
- aliases
- status
- metadata JSON/blob fields as needed
- created_at
- updated_at

#### D. Edges / links

Purpose:
- represent graph relationships between documents and entities
- replace informal wiki-link parsing with explicit graph structure

Minimum fields:
- edge_id
- src_kind / src_id
- dst_kind / dst_id
- relation_type
- weight/confidence
- provenance_ref
- created_at

Examples:
- document mentions entity
- entity belongs_to project
- session produced note
- note supersedes note
- task relates_to repo

#### E. Sessions

Purpose:
- store conversations / harness runs / agent sessions as first-class objects

Minimum fields:
- session_id
- user_id / actor_id
- title
- started_at
- ended_at
- status
- summary
- parent_session_id / thread linkage

#### F. Session messages / events

Purpose:
- store transcript/event history cleanly instead of as opaque markdown dumps

Minimum fields:
- event_id
- session_id
- sequence_no
- role / event_type
- content_text
- structured_payload
- created_at
- tool_name / action_kind (optional)

Design note:
- a brain often needs both message history and higher-level extracted memory
- these should be separate tables, not conflated

#### G. Tasks / plans

Purpose:
- represent todos, plans, checklists, deferred work, and execution traces

Minimum fields:
- task_id
- title
- body
- status
- priority
- owner
- created_at
- updated_at
- due_at
- parent_task_id
- related_session_id / related_document_id

#### H. Facts / memory items

Purpose:
- store distilled long-term memory separate from source docs
- examples: user preferences, environment facts, project conventions

Minimum fields:
- memory_id
- memory_scope (user, project, system, agent)
- statement
- confidence
- source_refs
- freshness / review_at
- created_at
- updated_at

#### I. Embeddings

Purpose:
- enable semantic retrieval over notes, summaries, chunks, and entities

Minimum fields:
- embedding_id
- subject_kind
- subject_id
- chunk_id if chunked
- model_name
- vector
- text_span / normalized_text_hash
- created_at

#### J. Chunks

Purpose:
- support semantic retrieval at chunk granularity instead of whole-document granularity

Minimum fields:
- chunk_id
- document_id
- ordinal
- text
- token_count
- heading_path / section label
- created_at

### 4.2 Namespaces / multi-tenant separation

A brain backend will often need scoped memory domains:

- per user
- per project
- per repo
- per environment
- per agent role
- per workspace / vault

This likely needs a first-class namespace model rather than relying on ad hoc columns everywhere.

Minimum concept:
- namespace_id
- namespace_kind
- namespace_name
- access policy reference

Then most core tables should include namespace_id.

### 4.3 Soft-delete / archival policy

Brains accumulate stale knowledge. Hard delete is often the wrong default.

Needed additions:
- archived_at
- deleted_at
- tombstone reason
- visibility filters that default to live records only

### 4.4 Provenance fields on all derived records

Every extracted or generated object should be traceable back to source.

Recommended provenance fields:
- source_document_id
- source_session_id
- source_event_id
- created_by_agent
- derivation_kind
- derivation_version

Without this, the brain becomes untrustworthy quickly.

---

## 5. Retrieval and search additions

This is the biggest gap relative to an Obsidian/MCP brain.

Current Shunter specs are optimized around subscriptions and a constrained standing-query model. A brain needs richer read and retrieval behavior.

### 5.1 Full-text search

Need:
- lexical search over note bodies, titles, summaries, tags, entity names, and transcript text

Requirements:
- tokenization / normalization
- phrase search
- prefix search
- boolean operators or at least AND/OR subset
- ranking/scoring
- snippet extraction / highlights

Why needed:
- semantic search alone is not enough
- exact lookup, phrase recall, and metadata filtering matter constantly for agent brains

### 5.2 Semantic/vector retrieval

Need:
- embedding-based nearest-neighbor search over chunks/documents/entities

Requirements:
- vector storage
- ANN index or pluggable vector backend
- cosine/dot-product search
- top-k with optional metadata filters
- re-embedding lifecycle when text changes

Why needed:
- this is one of the biggest practical advantages over markdown-file brains
- enables concept-level recall even when exact terms differ

### 5.3 Graph traversal / backlink queries

Need:
- “what links to this?”
- “what entities are mentioned by this note?”
- “what decisions relate to this project?”
- “show all notes connected to this entity within 2 hops”

Requirements:
- explicit edge table(s)
- efficient adjacency queries
- likely graph-style helper reducers or query APIs

### 5.4 Hybrid retrieval

Best practical recall usually combines:
- lexical signals
- semantic similarity
- graph proximity
- recency
- explicit user/project scoping

A serious brain should define hybrid retrieval as a first-class service, not force every agent to manually merge results.

Possible API shape:
- query text
- namespace filters
- target types
- weights for lexical/semantic/recency/graph
- top-k

### 5.5 Read ergonomics beyond subscription model

The current specs are very standing-query / delta oriented. Brain workloads also need:

- ad hoc retrieval
- ranking
- filtered list queries
- graph traversal
- top-k nearest neighbor
- time-window browsing
- version history browsing

This likely requires either:
- a much richer one-off query surface
or
- a dedicated brain-query API above the base protocol

For a brain, the latter is probably cleaner.

---

## 6. Ingestion and indexing pipeline additions

A brain is only as good as its ingestion path.

### 6.1 Document ingest workflow

Need reducers / pipelines for:
- create document
- update document
- import external note/file
- attach transcript/log/tool output
- batch import

Each write should trigger downstream indexing work.

### 6.2 Chunking pipeline

When document text changes:
- split into chunks
- generate chunk rows
- invalidate stale embeddings
- regenerate embeddings
- update FTS indexes
- update extracted entities and links if enabled

This is application logic above the current specs.

### 6.3 Entity extraction pipeline

Need optional extraction of:
- people
- repos
- projects
- tools
- topics
- decisions
- deadlines
- tasks

This can be implemented as:
- async reducer-triggered job
- external worker consuming commit events
- background process subscribed to document changes

### 6.4 Link extraction / wiki-link compatibility

If replacing Obsidian-style notes, the system should understand:
- wiki links
- tags
- headings/sections
- backlinks

That means either:
- parse markdown at ingest time
- store explicit link edges
- preserve source markdown while materializing structured links

### 6.5 Derived-summary pipeline

Brain usefulness improves dramatically if the system stores:
- note summaries
- session summaries
- entity summaries
- rolling project summaries

That requires:
- derivation jobs
- provenance fields
- invalidation/update policies

### 6.6 Background job system

The current specs do include scheduling concepts, but a brain likely needs a richer background-job model for:
- embedding generation
- reindexing
- summarization
- extraction
- compaction/cleanup
- stale-memory review passes

A practical job subsystem needs:
- job tables
- retry policy
- failure reason tracking
- worker leases or ownership
- idempotency keys

---

## 7. Agent-facing API additions

The current protocol is low-level and engine-centric. A brain needs a higher-level API.

### 7.1 Memory API

Need operations like:
- create memory
- update memory
- supersede memory
- pin memory
- deprecate memory
- merge duplicates
- search memories
- retrieve relevant memories for context assembly

### 7.2 Document API

Need operations like:
- upsert note/document
- append to note
- create revision
- fetch note by path/slug/id
- list backlinks
- list related notes/entities
- get diff/history

### 7.3 Session API

Need operations like:
- open session
- append event/message
- summarize session
- link session to project/tasks/docs
- fetch recent relevant sessions

### 7.4 Retrieval API

Need higher-level operations like:
- retrieve_context(query, namespace, top_k)
- retrieve_related(subject)
- retrieve_recent_relevant(session)
- retrieve_for_planning(project, time_window)

The important point is that agents should not need to manually compose low-level table reads and ranking logic every time.

### 7.5 MCP / tool bridge layer

If replacing an Obsidian MCP brain, Shunter should likely expose a tool-oriented bridge with operations similar to:
- create_note
- update_note
- search_notes
- get_note
- get_backlinks
- remember_fact
- recall_facts
- record_session_event
- search_history
- get_related_entities

This can sit above the core websocket protocol if needed.

---

## 8. Memory lifecycle and governance additions

This is a major difference between “database” and “brain.”

### 8.1 Distinguish raw source from distilled memory

The system should model at least three layers:

1. Raw source artifacts
   - transcripts
   - imported docs
   - tool outputs

2. Structured extracted artifacts
   - entities
   - links
   - tasks
   - chunks

3. Distilled memory
   - stable facts
   - user preferences
   - project conventions
   - summaries

Without this separation, the brain becomes noisy and hard to trust.

### 8.2 Confidence and freshness

Memory items should have lifecycle metadata:
- confidence
- last_confirmed_at
- stale_after
- review_required
- contradicted_by refs

This is especially important if multiple agents can write to the brain.

### 8.3 Contradiction and supersession handling

Need explicit ways to represent:
- this fact is obsolete
- this note supersedes that note
- these two memories conflict
- this summary replaced a prior summary

This should not be left to application convention only.

### 8.4 User/profile vs project/system memory separation

A practical brain for an agent harness should separate:
- user profile memory
- project memory
- environment memory
- transient session memory

This maps well onto namespaces + memory scopes.

---

## 9. Protocol and query-surface additions for brain use

### 9.1 Richer one-off query capabilities

For brain usage, one-off queries likely need:
- pagination
- ordering by score / time / title
- filter expressions
- text search terms
- graph expansions
- vector top-k

The current constrained protocol shape is not enough by itself.

### 9.2 Subscription use cases for brain workloads

Subscriptions are still useful for:
- live note editor sync
- dashboard updates
- agent status views
- task boards
- event streams
- live “recent memory” panes

So the existing realtime model remains valuable; it just needs a broader read plane beside it.

### 9.3 Partial-document / chunk subscriptions

If notes are large, subscribing only to:
- document metadata
- recent revisions
- selected chunks
may be better than subscribing to whole bodies.

### 9.4 Access control

Even for personal projects, a brain backend should probably anticipate:
- namespace-level access rules
- API tokens / identities for agents
- read/write scopes
- reducer/tool restrictions by actor

This may stay lightweight in v1, but should be designed early.

---

## 10. Operational additions

### 10.1 Blob/attachment handling

Obsidian-like brains often include:
- images
- PDFs
- exports
- artifacts
- logs

A complete replacement may need:
- blob metadata table
- optional object/blob storage
- attachment-to-note linking
- content hashing

### 10.2 Backup/export/import

For a personal brain product, exportability matters a lot.

Need:
- export namespace/project
- export documents + links + memory rows
- import from markdown/vaults/json
- round-trip-friendly formats where possible

### 10.3 Debuggability / provenance UX

To trust a brain, users need to answer:
- where did this memory come from?
- when was it created?
- what source note/session supports it?
- what agent generated it?
- what changed?

So queryable provenance is not optional.

### 10.4 Data retention and compaction policy

Brains grow constantly.

Need policy for:
- pruning stale embeddings
- compacting old event streams
- archiving superseded revisions
- retaining source vs derived artifacts

---

## 11. Suggested additive spec families

If this direction is pursued, a practical way to extend the spec set would be to add new spec families above the current engine kernel.

Possible additions:

### SPEC-007 — Brain data model

Would define:
- documents
- revisions
- entities
- edges
- tasks
- sessions
- events
- memories
- namespaces
- provenance rules

### SPEC-008 — Retrieval and indexing

Would define:
- full-text indexing/search
- chunking
- embeddings
- vector search
- hybrid retrieval
- graph traversal contracts
- ranking/scoring inputs

### SPEC-009 — Brain ingestion and derivation

Would define:
- document ingest flows
- extraction pipelines
- summarization pipelines
- background jobs
- invalidation/reindex rules

### SPEC-010 — Agent brain API / tool surface

Would define:
- brain reducers
- memory/document/session API
- context retrieval API
- MCP/tool bridge contract
- access control scope

### SPEC-011 — Blob/export/import lifecycle

Would define:
- attachments
- export/import
- backup portability
- archival policies

These are only naming suggestions. The key point is that the current six specs are the engine kernel, while the brain product needs a second layer of product-oriented specs.

---

## 12. What can remain unchanged

The current core specs remain highly useful and likely should not be replaced.

The following current capabilities are still the right foundation:
- reducer execution model
- commit log + recovery
- snapshot support
- subscriptions and push fan-out
- schema system
- websocket protocol basis

The brain should be built on top of them, not instead of them.

This is a layering opportunity:

- Layer 1: Shunter core runtime
- Layer 2: Brain schema + retrieval + ingestion + agent APIs

---

## 13. Practical bottom line

If the goal is “replace an Obsidian doc-based MCP brain with something better,” the current Shunter specs are enough to justify the engine foundation, but not enough to define the whole product.

What they can already support well:
- structured durable state
- event/session storage
- live updates
- transactional correctness
- multi-agent consistency

What must still be added for a real brain replacement:
- first-class documents and revisions
- graph/link model
- lexical and semantic retrieval
- ingestion/indexing pipelines
- memory lifecycle semantics
- higher-level brain APIs
- export/import/blob handling

So the right mental model is:

Shunter, as currently specced, is the engine for the brain.
It is not yet the brain product itself.

---

## 14. Recommended success criteria for a brain-oriented extension

A future Shunter-based brain backend should be considered viable for personal-project agent use when it can do all of the following reliably:

1. Store notes/documents durably with revision history
2. Search those notes lexically and semantically
3. Maintain backlinks / entity links / graph relations
4. Record session and tool history in structured form
5. Distill stable memory items from raw sources
6. Retrieve relevant context for an agent query with predictable quality
7. Keep multiple consumers live-updated via subscriptions
8. Explain provenance for any recalled fact or generated summary
9. Import from existing markdown/vault sources
10. Export data back out without lock-in

Once those are true, Shunter stops being merely a realtime database kernel and becomes a serious candidate brain backend for an LLM harness.
