---
name: ai-receptionist-repo
description: Orient and implement changes in this AI Receptionist monorepo. Use when work touches repo architecture, module placement, milestone scope, product terminology, or cross-backend/frontend/docs changes for the nail salon AI receptionist.
---

# AI Receptionist Repo

Use this skill before broad or ambiguous repo work.

## Codebase Truth Rule

- Be strictly honest about the current codebase and runtime configuration. Do not flatter, reassure, or infer readiness from docs, milestones, intended architecture, or previous claims.
- Before saying a feature works, is ready, or only needs testing, verify the actual owning code path, config gates, persistence, and tests/status output when available.
- Separate confirmed current behavior from product intent, planned behavior, unverified assumptions, and missing implementation.
- If evidence conflicts, stop and name the conflict directly with file references instead of smoothing over it.

## Product-Grade Rule

- Treat every approved slice as production-grade pilot software, not MVP, demo, prototype, or happy-path scaffolding.
- Before proposing or implementing, check repeated-use behavior, idempotency, duplicate prevention, retry/rerun semantics, conflict handling, tenant/security/privacy boundaries, and empty/error/disabled states.
- For export/import, sync, webhook, booking, provider, or AI training flows, define stable keys, upsert or dedupe semantics, schema/version impact, and how repeated execution avoids duplicate or rubbish records.
- Do not mark work complete until predictable edge cases are handled, safely gated, explicitly out of scope with a reason, or called out as a blocker.

## Start Here

1. Read `CONTEXT.md` for shared product language.
2. Read `docs/architecture.md` and `docs/pilot-checklist.md` for current milestone boundaries.
3. Identify whether the change belongs to `backend`, `frontend`, `docs`, `.agents`, `.codex`, or multiple layers.
4. Keep product claims honest: Square is the first real POS integration; future POS names are architecture targets.

## User Confirmation Gate

- For feature, bugfix, refactor, or code-changing tasks, inspect first, then present the implementation plan and wait for explicit user approval before editing code.
- For UI changes, include a Mockup as Text before implementation and wait for explicit approval.
- Documentation-only or agent-rule changes may be applied directly when the user explicitly asks for those files to change.

## Architecture Rules

- Backend changes follow handler -> service -> repository/provider.
- Booking and AI code call `modules/pos.POSProvider` only.
- Square-specific code stays in `backend/modules/pos_square`.
- Frontend pages must be operational dashboards with loading, empty, error, and success states.
- Docs must be updated when architecture, API, env, setup, or milestone scope changes.

## Output Shape

For implementation tasks:

1. Inspect the smallest relevant owning files.
2. State the slice boundary if the task is broad.
3. Include product-grade edge cases, repeated-run/idempotency behavior, and safe failure handling in the plan.
4. Present the plan and wait for explicit user approval before editing code.
5. Edit only the coherent approved slice.
6. Run the nearest validation commands.
7. Report what changed, what is verified, and what remains out of scope.
