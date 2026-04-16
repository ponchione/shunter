# TD-025: Durability Worker Open/Resume Instead of Truncate

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `NewDurabilityWorker` opens and resumes an existing active segment instead of truncating it. New segments still created when none exists or on rotation.

**Architecture:** Add `OpenSegmentForAppend` to `segment.go` that opens an existing segment file, scans forward through valid records to find the write position and last TxID, then returns a `SegmentWriter` ready for appending. `NewDurabilityWorker` tries open-for-append first, falls back to create-new. Durable/lastEnq state initialized from the reopened segment.

**Tech Stack:** Go, existing commitlog segment infrastructure

---

## File Map

- Modify: `commitlog/segment.go` — add `OpenSegmentForAppend`
- Modify: `commitlog/durability.go` — use open-or-create in constructor, init durable state
- Modify: `commitlog/phase4_acceptance_test.go` — regression tests for reopen and create-new
- Modify: `TECH-DEBT.md` — mark TD-025 resolved
