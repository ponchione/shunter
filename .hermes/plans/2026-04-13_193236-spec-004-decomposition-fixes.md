# SPEC-004 decomposition cleanup plan

Goal: resolve all audit findings in docs/decomposition/004-subscriptions by making the source spec, epic docs, and story docs internally consistent and aligned with SPEC-001/002/003/005.

Planned work:
1. Patch SPEC-004 source spec where cross-spec interfaces are inaccurate or ambiguous:
   - align fan-out caller behavior with SPEC-005
   - replace protocol-owned `ClientConn` usage with sender/protocol abstraction language
   - clarify durability wait ownership so it does not claim unsupported SPEC-002 interface surface
2. Patch top-level EPICS.md:
   - fix open-decision ownership gaps (§12.1, §12.2)
   - fix error ownership consistency
   - add/clarify cross-spec notes and scaling/benchmark ownership
3. Patch story headers and content for coverage/ownership gaps:
   - §3.1, §9.3, §10.2, §10.3, §11.2, §12.1, §12.2 ownership
   - compile-plan ownership and register ordering
   - unregister final-delta deferral note
   - CommittedReadView lifecycle notes
4. Patch dependency reciprocity/orphan issues in story frontmatter.
5. Patch cross-spec mismatches in fan-out stories:
   - use SPEC-005 sender contract instead of `ClientConn`
   - align caller reducer-result routing with SPEC-005
   - remove unsupported direct dependency on SPEC-002 `TxDurable` export while preserving the wait-channel concept as executor-integrated metadata
6. Patch benchmark/property/unit-test AC coverage so §9.1 and §13.1–§13.3 are explicitly mapped.
7. Re-run structured checks for:
   - section coverage
   - dependency reciprocity and cycles
   - convention compliance
   - cross-spec references
8. If any gap remains, patch again until clean.
