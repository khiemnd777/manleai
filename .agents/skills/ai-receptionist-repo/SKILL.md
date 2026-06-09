---
name: ai-receptionist-repo
description: Orient and implement changes in this AI Receptionist monorepo. Use when work touches repo architecture, module placement, milestone scope, product terminology, or cross-backend/frontend/docs changes for the nail salon AI receptionist.
---

# AI Receptionist Repo

Use this skill before broad or ambiguous repo work.

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
3. Present the plan and wait for explicit user approval before editing code.
4. Edit only the coherent approved slice.
5. Run the nearest validation commands.
6. Report what changed, what is verified, and what remains out of scope.
