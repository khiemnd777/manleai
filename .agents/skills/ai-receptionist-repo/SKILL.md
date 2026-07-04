---
name: ai-receptionist-repo
description: Orient, investigate, and plan changes in this AI Receptionist monorepo. Use when work touches repo architecture, module placement, milestone scope, product terminology, agent rules, skills, or cross-backend/frontend/docs changes for the nail salon AI receptionist. Do not edit files from this skill until the latest user message explicitly approves the exact change scope.
---

# AI Receptionist Repo

Use this skill before broad or ambiguous repo work.

## Codebase Truth Rule

- Be strictly honest about the current codebase and runtime configuration. Do not flatter, reassure, or infer readiness from docs, milestones, intended architecture, or previous claims.
- Before saying a feature works, is ready, or only needs testing, verify the actual owning code path, config gates, persistence, and tests/status output when available.
- Separate confirmed current behavior from product intent, planned behavior, unverified assumptions, and missing implementation.
- If evidence conflicts, stop and name the conflict directly with file references instead of smoothing over it.

## Product-Grade Rule

- Treat every approved slice as commercial-grade production software, not MVP, demo, prototype, or happy-path scaffolding.
- Before proposing or implementing, check repeated-use behavior, idempotency, duplicate prevention, retry/rerun semantics, conflict handling, tenant/security/privacy boundaries, and empty/error/disabled states.
- For export/import, sync, webhook, booking, provider, or AI training flows, define stable keys, upsert or dedupe semantics, schema/version impact, and how repeated execution avoids duplicate or rubbish records.
- Do not mark work complete until predictable edge cases are handled, safely gated, explicitly out of scope with a reason, or called out as a blocker.

## Start Here

1. Read `CONTEXT.md` for shared product language.
2. Read `docs/architecture.md` and `docs/production-readiness-checklist.md` for current release boundaries.
3. Identify whether the change belongs to `backend`, `frontend`, `docs`, `.agents`, `.codex`, or multiple layers.
4. Keep product claims honest: Square is the first real POS integration; future POS names are architecture targets.

## User Confirmation Gate

- This skill does not authorize edits by itself.
- For feature, bugfix, refactor, or code-changing tasks, inspect first, then present the implementation plan and wait for explicit user approval before editing code.
- For UI changes, include a Mockup as Text before implementation and wait for explicit approval.
- Documentation-only or agent-rule changes may be applied directly when the user explicitly asks for those files to change.
- Treat questions, complaints, screenshots, appshots, status checks, "why" prompts, and partial instructions as read-only investigation requests, not implementation approval.
- Earlier approval expires when the scope changes, after the assistant gives a final answer, or when the latest user message is a question or complaint instead of an approval.
- Before any write, confirm the latest user message contains action-oriented approval for the exact files or scope, such as "approved", "cho phep ghi file", "trien khai theo plan nay", "sua cac file nay", or "lam di".

## Architecture Rules

- Backend changes follow handler -> service -> repository/provider.
- Booking and AI code call `modules/pos.POSProvider` only.
- Square-specific code stays in `backend/modules/pos_square`.
- Conversation service understanding is backend domain logic, not prompt-only behavior. Use active-provider catalog services, salon-scoped `service_aliases`, active `service_categories`, and active `service_category_aliases`; exact catalog services win, service aliases can select one service, category/category-alias matches ask for a real service in that group, and fuzzy family matches clarify instead of selecting. Do not patch salon-specific service keywords into prompts or generic matchers.
- Owner corrections can become either knowledge items or structured service aliases. Preserve idempotent `(salon_id, normalized_alias)` upsert semantics, prevent active service aliases from conflicting with active service category aliases, and keep transcript metadata explaining alias source, confidence, candidates, selected service, and category when relevant.
- Group or party booking requests are owner-review handoffs with structured `party_booking_requests`; they must not call availability or booking tools and must not use confirmed appointment wording.
- AI receptionist tone is salon-scoped runtime configuration on `salon_settings.ai_tone`. Keep presets controlled, flow tone through `conversation.RuntimeConfig` -> `voice.ModelRequest` -> provider adapters, and do not let style settings change slot collection, handoff routing, service understanding, or POS-first confirmation wording.
- Square Appointments, Twilio, and OpenAI runtime settings are dashboard-managed provider configuration stored in `salon_integration_configs`; `.env`, `project.env`, and deployment secrets are bootstrap or fallback values only. Check the Integrations dashboard/API before telling the user to edit env files for provider credentials, redirect URLs, webhook URLs, model names, or provider base URLs.
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
