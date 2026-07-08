---
name: production-release-review
description: Review this repo for commercial production readiness, scope honesty, POS-first architecture, missing tests, security gaps, and release boundary regressions.
---

# Production Release Review

Use this skill for code review, milestone review, or readiness checks.

## Mandatory Mapping Load

- Before triage, diagnosis, planning, implementation, review, or subagent
  handoff, read `docs/agents/codebase-map.md` after the root instructions and
  before scanning code.
- Use the map's feature, function, utility, helper, UI, and triage keyword
  routing to choose the first docs, files, skills, and subagents to inspect.
- If code evidence conflicts with the map, report `map conflict found`; code
  wins and the map must be corrected in the same approved scope.
- Any code change that affects mapped features, functions, utilities, helpers,
  UI surfaces, API routes, DTOs, migrations, tests, or ownership boundaries must
  update `docs/agents/codebase-map.md` before the task is complete.
- If no map update is needed, the final response must include
  `Mapping impact: none` with the reason.

## Codebase Truth Rule

- Be strictly honest about the current codebase and runtime configuration. Do not flatter, reassure, or infer readiness from docs, milestones, intended architecture, or previous claims.
- Before saying the production release is ready, a feature works, or a gap is only a test issue, verify the actual owning code path, config gates, persistence, API/UI behavior, and tests/status output when available.
- Separate confirmed current behavior from product intent, planned behavior, unverified assumptions, and missing implementation.
- If evidence conflicts, stop and name the conflict directly with file references instead of smoothing over it.

## Product-Grade Rule

- Review release readiness against commercial-grade production behavior, not MVP, demo, prototype, or happy-path scaffolding.
- Treat missing repeated-run/idempotency behavior, duplicate prevention, retry/rerun semantics, conflict handling, tenant/security/privacy boundaries, or safe failure states as real readiness risks.
- For export/import, sync, webhook, booking, provider, or AI training flows, require stable keys, upsert or dedupe semantics, schema/version impact, and regression coverage for repeated execution.
- Do not downgrade predictable duplicate/rubbish data, misleading owner/caller state, or unsupported production behavior to a cosmetic issue.

## Review Priorities

1. False booking confirmation risk.
2. Square/POS leakage outside adapter packages.
3. Tenant isolation by `salon_id`.
4. Raw or encrypted POS token exposure.
5. Duplicate/rubbish records from missing idempotency, retry, import, sync, webhook, or repeated-submit handling.
6. Missing POS error, sync log, booking attempt, audit, or notification records.
7. Frontend UI that implies unavailable production behavior.
8. Service-understanding drift: hardcoded salon keywords, aliases or category aliases not scoped by salon, service alias/category alias conflicts, category/fuzzy matches selecting services instead of clarifying, or missing transcript metadata for why a service or category was selected.
9. Party/group booking drift: supported party requests that bypass catalog-backed segment planning, booking-tool calls outside the booking service, missing fallback/exception handling, missing owner-review `party_booking_requests` where still required, or UI/caller wording that implies a confirmed appointment before POS success.
10. AI tone drift: style settings bypass backend-approved replies, alter booking/handoff/slot behavior, or are documented as provider config instead of salon settings.
11. Missing tests around critical booking/auth/POS/conversation-understanding paths.
12. Documentation drift around env vars, API routes, or milestone scope.

## Review Method

- Start with findings, ordered by severity.
- Cite exact files and line numbers.
- Separate confirmed issues from open questions.
- Do not list style nits unless they hide a real operational risk.
- If no issues are found, state residual risk and any tests not run.
- This skill is review-only and does not authorize edits. If the user asks for fixes, stop after findings and a proposed change plan, then wait for latest explicit approval for the exact scope before editing files.

## Validation Commands

```bash
cd backend
GOCACHE=/private/tmp/manleai-go-cache go test ./...
```

```bash
cd frontend
npm run typecheck
npm run build
```
