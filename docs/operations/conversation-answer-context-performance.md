# Conversation AnswerContext Performance And Freshness Evidence

## Purpose And Scope

This document owns the P2 local implementation evidence for the per-turn
Conversation `AnswerContextFence`, the process-local 45-second answer-context
cache, and cross-replica freshness. It does not measure scheduling execution,
call transport, a live provider, or production capacity. The scheduling load
harness is deliberately not used because it does not execute this workflow.

The measured path is:

`Service.Message` -> `loadAnswerContextWithDiagnostics` ->
`GetAnswerContextFence` -> process-local cache -> authority-specific projections
on a miss -> second fence read -> cache install.

No migration, API shape, frontend surface, availability call, booking call, POS
call, transcript body, provider payload, or salon identifier was added for P2.

## Database Round-Trip Topology

The exact counts below are enforced by
`answer_context_performance_integration_test.go` through a test-only
`database/sql/driver` wrapper. It counts executed PostgreSQL query round trips,
not Go repository method calls.

| Scheduling authority | Stable hit | Cold or TTL miss | Stale persisted fence | Readiness mismatch | Notes |
| --- | ---: | ---: | ---: | ---: | --- |
| `owner_manual` | 1 | 8 | 8 | Not applicable | Readiness is request-only and does not have a second evidence query. A refresh is two fence reads plus six projections. |
| `external_provider` | 1 | 10 | 10 | Not applicable | Readiness is derived from the same lightweight provider fence. A refresh is two fence reads plus eight projections. |
| `manleai_calendar` | 1 | 22 | 22 | 35 for one mismatch followed by a stable attempt | A refresh is two fence reads, one 12-query authoritative calendar evidence load, and eight projections. The mismatch case is `2*12 + 3 fence + 8 projection` queries. |

An authority switch uses the destination authority's refresh topology. The
PostgreSQL test covers `owner_manual -> external_provider` at 10 queries; the
unit topology matrix covers all three authority-specific destinations.

The stable `manleai_calendar` hit is one `GetAnswerContextFence` query. It makes
zero calls to `GetManleAICalendarAnswerContextEvidence`, so it does not hydrate
hours, schedules, policies, eligibility, pools, overrides, or exceptions. A
miss still loads the authoritative aggregate and evaluates readiness before
installing a cache entry.

## Bounded EXPLAIN Baseline

The repository test runs the exact `answerContextFenceQuery` with
`EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`. Fixtures are synthetic and bounded:

| Fixture | Services | Staff | Planning ms | Execution ms | Root shared hits | Root shared reads |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| small | 5 | 3 | 1.220 | 0.089 | 12 | 0 |
| medium | 100 | 40 | 0.586 | 0.119 | 17 | 0 |
| high-bounded | 500 | 150 | 0.641 | 0.185 | 42 | 0 |

This baseline was captured on 2026-07-29 using the repository's local
PostgreSQL 16 Alpine service on Apple M3 Pro. Every root plan returned exactly
one row with one loop. Every plan used index-backed nodes, including
`salons_pkey`, `manleai_calendar_configs_pkey`, and
`idx_pos_connections_salon_provider`; the high-bounded plan also selected
`business_resource_versions_pkey`. PostgreSQL chose sequential access for some
tiny joined relations in the smaller fixtures. The test does not force planner
settings and fails on root row multiplication or a plan with no index-backed
node.

These figures are reproducible local implementation evidence, not an SLO and
not an approved witnessed representative-production run.

## Go Cache-Path Baseline

`BenchmarkAnswerContextStableCacheHitInMemory` isolates Go cache lookup,
context cloning, and the repository boundary with an in-memory test store. Three
local runs produced:

| Authority | Range ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| `owner_manual` | 331.8-333.6 | 832 | 8 |
| `manleai_calendar` | 330.8-334.9 | 832 | 8 |
| `external_provider` | 330.7-334.1 | 832 | 8 |

This benchmark intentionally excludes network and PostgreSQL latency. It proves
that authority selection does not introduce a different process-local cache
path; database cost is owned by the query-count and EXPLAIN evidence above.

## Multi-Replica And Concurrency Contract

The PostgreSQL suite creates independent `Service` instances with separate
in-memory caches and proves:

- a committed tenant-A mutation is rejected by the next read on replica B;
- tenant B retains its independent cache and data;
- a mutation committed between the first projection and the verification fence
  causes a retry and returns only the stable revision;
- three consecutive between-fence mutations exhaust the bounded retry loop,
  return no answer context, and install no cache entry;
- a ManleAI readiness/config mutation between the lightweight fence and the
  aggregate evidence causes a readiness-mismatch retry;
- no availability, booking, or provider adapter is part of any measured path.

## Operational Diagnostics And Privacy

The existing `answer_context_ms` timing and transcript
`answer_context_cache_hit` flag could not distinguish TTL expiry, persisted
fence invalidation, readiness mismatch, a concurrent retry, or load failure.
P2 therefore adds these low-cardinality timing fields to the existing
`backend_turn_done` diagnostic path:

| Field | Bounded values |
| --- | --- |
| `answer_context_authority` | the three scheduling protocol tokens |
| `answer_context_cache_status` | `hit`, `miss` |
| `answer_context_refresh_reason` | `none`, `cold`, `ttl_expired`, `fence_mismatch` |
| `answer_context_retry_reason` | `none`, `readiness_mismatch`, `fence_changed_during_load` |
| `answer_context_attempts` | `1`, `2`, `3` |
| `answer_context_outcome` | `cache_hit`, `refreshed`, `refreshed_fail_closed`, `load_error`, `retry_exhausted` |
| `answer_context_ready` | `true`, `false`; omitted before readiness is evaluated |

The voice snapshot and persisted realtime-event allowlists accept only these
fields. Tests reject salon IDs, transcript text, and raw error detail. The
change is additive and schema-free. If the fields must be rolled back, remove
the timing attributes and both allowlist entries; no persisted schema or API
migration is involved.

## Reproduction

Use an isolated migrated PostgreSQL database; never point these tests at a
production database:

```bash
cd backend
TEST_DATABASE_URL='<isolated-postgres-url>' go test ./modules/conversation \
  -run 'TestPostgresAnswerContext' -count=1 -v

go test ./modules/conversation -run '^$' \
  -bench '^BenchmarkAnswerContextStableCacheHitInMemory$' -benchmem -count=3
```

The full backend, PostgreSQL, race, vet, security, and web release checks remain
owned by `docs/operations/release-gate.md`.

## P2 Validation Record

The following local repository gates completed successfully on 2026-07-29:

- release-gate self-test;
- complete backend tests and vet plus the manifest-owned high-risk race suite;
- fresh PostgreSQL 17 migration/checksum/migrate-twice gate and every isolated
  PostgreSQL integration package;
- PostgreSQL 17 plus Redis 7 tenant/security/privacy contract, including the
  Conversation and Voice diagnostic allowlists;
- the dedicated query-count, bounded EXPLAIN, multi-replica, concurrent retry,
  retry-exhaustion, and in-memory benchmark commands documented above.

This record is code-readiness evidence for the tested revision. It is not a
production deployment observation or a production capacity claim.

## SLO And Remaining Operational Witness

No latency or throughput SLO is set by P2. A production target requires
business approval after an approved witnessed run captures a representative
turn mix, database/network placement, replica count, cache-hit distribution,
and tail latency. Local test success must not be presented as production
capacity.
