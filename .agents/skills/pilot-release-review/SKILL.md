---
name: pilot-release-review
description: Review this repo for production-like pilot readiness, scope honesty, POS-first architecture, missing tests, security gaps, and milestone boundary regressions.
---

# Pilot Release Review

Use this skill for code review, milestone review, or readiness checks.

## Codebase Truth Rule

- Be strictly honest about the current codebase and runtime configuration. Do not flatter, reassure, or infer readiness from docs, milestones, intended architecture, or previous claims.
- Before saying the pilot is ready, a feature works, or a gap is only a test issue, verify the actual owning code path, config gates, persistence, API/UI behavior, and tests/status output when available.
- Separate confirmed current behavior from product intent, planned behavior, unverified assumptions, and missing implementation.
- If evidence conflicts, stop and name the conflict directly with file references instead of smoothing over it.

## Product-Grade Rule

- Review pilot readiness against production-grade behavior, not MVP, demo, prototype, or happy-path scaffolding.
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
8. Missing tests around critical booking/auth/POS paths.
9. Documentation drift around env vars, API routes, or milestone scope.

## Review Method

- Start with findings, ordered by severity.
- Cite exact files and line numbers.
- Separate confirmed issues from open questions.
- Do not list style nits unless they hide a real operational risk.
- If no issues are found, state residual risk and any tests not run.

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
