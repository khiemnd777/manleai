---
name: pos-adapter-slice
description: Investigate, review, plan, and only after explicit approval implement POS adapter work for this repo. Use when POSProvider, SquareAdapter, POS connections, sync logs, POS errors, OAuth, provider DTOs, service/staff sync, or future POS integration seams may change. Do not write files until the latest user message explicitly approves the exact change scope.
---

# POS Adapter Slice

Use this skill for all POS integration work.

## Mandatory Mapping Load

- Before triage, diagnosis, planning, implementation, review, or subagent
  handoff, read `docs/agents/codebase-map.md` after the root instructions and
  before scanning code.
- Use the map's POS, Square, provider config, mapping, sync, and booking
  keywords to choose the first docs, files, tests, and subagents to inspect.
- If code evidence conflicts with the map, code wins and the map must be
  corrected in the same approved scope.
- Any code change that affects mapped POS behavior, provider adapters, routes,
  DTOs, helpers, migrations, tests, or ownership boundaries must update
  `docs/agents/codebase-map.md` before the task is complete.
- If no map update is needed, the final response must include
  `Mapping impact: none` with the reason.

## Codebase Truth Rule

- Be strictly honest about the current codebase and runtime configuration. Do not flatter, reassure, or infer readiness from docs, milestones, intended architecture, or previous claims.
- Before saying POS behavior works, is ready, or only needs testing, verify the actual adapter/service path, provider config gates, persistence writes, and tests/status output when available.
- Separate confirmed current behavior from product intent, planned behavior, unverified assumptions, and missing implementation.
- If evidence conflicts, stop and name the conflict directly with file references instead of smoothing over it.

## Product-Grade Rule

- Treat POS adapter work as commercial-grade production integration behavior, not MVP, demo, prototype, or happy-path scaffolding.
- Before proposing or implementing, check OAuth retry/rerun behavior, webhook idempotency, sync dedupe, duplicate provider/local record prevention, token safety, tenant boundaries, provider conflicts, and safe failure states.
- Provider IDs, sync cursors, idempotency keys, upsert behavior, error normalization, and audit/sync logs must be explicit for flows that can repeat.
- Do not mark POS work complete until repeated execution cannot create duplicate local records, duplicate provider bookings, leaked token state, or misleading owner-facing status.

## Required Reading

- `docs/pos-adapter-layer.md`
- `docs/square-integration.md`
- `backend/modules/pos/types.go`
- The relevant adapter package, currently `backend/modules/pos_square`

## Non-Negotiables

- Booking Service depends on `pos.POSProvider`, not `pos_square`.
- Square payloads, endpoint URLs, OAuth behavior, token refresh, and provider error mapping stay inside `SquareAdapter`.
- POS tokens are encrypted at rest and never returned to the frontend.
- POS failures are logged to `pos_errors` with normalized codes.
- Sync operations write `pos_sync_logs`.

## Write Approval Hard Stop

- This skill does not authorize edits by itself. Follow the root `AGENTS.md` confirmation gate before any code, test, migration, doc, config, skill, agent, runtime, or generated artifact change.
- Treat questions, complaints, screenshots, appshots, status checks, "why" prompts, and partial instructions as read-only investigation requests, not implementation approval.
- Before editing, trace the real POS owner and root cause, state ambiguity or open questions, present the implementation and test plan, then wait for latest explicit approval for the exact scope.
- Earlier approval expires when the scope changes, after the assistant gives a final answer, or when the latest user message is a question or complaint instead of an approval.

## Implementation Flow

After explicit approval for the exact POS scope:

1. Confirm the provider-neutral DTO or method shape first.
2. Update migrations and Ent schemas when persistence changes.
3. Implement adapter mapping behind the interface.
4. Keep handler responses provider-neutral.
5. Add focused tests for mapping, error normalization, and token handling.
6. Validate with:

```bash
cd backend
GOCACHE=/private/tmp/manleai-go-cache go test ./...
```

## Future Provider Rule

Add future providers as new packages such as `modules/pos_vagaro`. Do not add fake implementations for providers not actually integrated.
