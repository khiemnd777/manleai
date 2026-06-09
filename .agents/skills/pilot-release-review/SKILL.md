---
name: pilot-release-review
description: Review this repo for production-like pilot readiness, scope honesty, POS-first architecture, missing tests, security gaps, and milestone boundary regressions.
---

# Pilot Release Review

Use this skill for code review, milestone review, or readiness checks.

## Review Priorities

1. False booking confirmation risk.
2. Square/POS leakage outside adapter packages.
3. Tenant isolation by `salon_id`.
4. Raw or encrypted POS token exposure.
5. Missing POS error, sync log, booking attempt, audit, or notification records.
6. Frontend UI that implies unavailable production behavior.
7. Missing tests around critical booking/auth/POS paths.
8. Documentation drift around env vars, API routes, or milestone scope.

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

