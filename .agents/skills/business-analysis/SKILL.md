---
name: business-analysis
description: Turn ambiguous product, operations, or customer requests for this AI Receptionist repo into clear business requirements, workflow maps, acceptance criteria, open questions, scope boundaries, and implementation guardrails. Use when Codex is asked to analyze requirements, write PRD-style notes, define user stories, clarify owner/caller/staff workflows, service-understanding behavior, or prepare a plan before backend, frontend, POS, voice, booking, or dashboard changes.
---

# Business Analysis

Use this skill before implementation when the request is broad, ambiguous, workflow-heavy, or product-facing.

## Codebase Truth Rule

- Be strictly honest about the current codebase and runtime configuration. Do not flatter, reassure, or infer readiness from docs, milestones, intended architecture, or previous claims.
- Before saying a workflow exists, works, is ready, or only needs testing, verify the actual owning code path, config gates, persistence, API/UI behavior, and tests/status output when available.
- Separate confirmed current behavior from product intent, planned behavior, unverified assumptions, and missing implementation.
- If evidence conflicts, stop and name the conflict directly with file references instead of smoothing over it.

## Product-Grade Rule

- Analyze every request as commercial-grade production behavior, not MVP, demo, prototype, or happy-path scaffolding.
- Before recommending a plan, identify repeated-use behavior, idempotency, duplicate prevention, retry/rerun semantics, conflict handling, tenant/security/privacy boundaries, and empty/error/disabled states.
- For export/import, sync, webhook, booking, provider, or AI training flows, define stable keys, upsert or dedupe semantics, schema/version impact, and how repeated execution avoids duplicate or rubbish records.
- Acceptance criteria must include edge cases and regression risks, not only the happy path.

## Product Proposal Gate

Before proposing a product, workflow, UI, or architecture plan:

1. Define the actor workflow: actor, trigger, operational object, user goal, and source of truth.
2. Identify the existing owner surface: current page, module, adjacent controls, and neighboring workflow that already manages the same operational object.
3. Identify the primary parent object and data grain before choosing placement. If the requested field/action is a child attribute, child action, learned phrase, mapping, exception, setting, or status of one parent object, place it in that parent object's existing row, card, detail panel, or edit flow by default.
4. Do not create a standalone panel, page, or management table for child data unless the real workflow is cross-object bulk review, audit, import/export, reporting, deduplication, or conflict resolution.
5. Remove redundant target selectors when parent context already supplies the target. Do not make the owner choose the current service, staff member, customer, category, or appointment again unless the workflow intentionally moves the child record.
6. Identify backend/API support separately from user workflow. Backend module names, route grouping, docs section names, and implementation accidents are evidence about implementation, not proof of product placement.
7. Compare plausible placements or ownership boundaries when the answer is not obvious. Reject the wrong placement explicitly and state the confusion or source-of-truth problem it would create.
8. Run the failure check before presenting the plan: would this confuse structured operational data with free-text knowledge, provider setup, review/audit state, or unsupported production behavior? If yes, revise the plan first.
9. Use examples only as examples. Prefer general decision rules over latest-case-specific fixes.

## Start Here

1. Read `CONTEXT.md` for product language and invariants.
2. Read `docs/architecture.md` and `docs/production-readiness-checklist.md` for current release boundaries.
3. For UI-facing work, read `DESIGN.md`.
4. For API or integration work, read the smallest relevant source-of-truth docs such as `docs/api.md`, `docs/pos-adapter-layer.md`, or `docs/square-integration.md`.
5. Inspect code only as needed to verify current behavior and ownership.

## Analysis Workflow

Convert the request into a concrete business slice:

- Identify actors: owner, caller/customer, staff, AI receptionist, backend service, POS provider, support/admin.
- State the business goal in operational terms, not implementation terms.
- Separate current behavior, requested behavior, assumptions, and unknowns.
- Map the main workflow and exception workflows.
- Name the source of truth for each decision and state.
- Identify data, API, UI, notification, audit, and reporting needs.
- Identify repeated-run/idempotency behavior, duplicate prevention, conflicts, and safe failure states whenever the workflow can be retried, imported, synced, submitted, or replayed.
- Preserve milestone honesty: Square Appointments is the first real POS integration; future POS providers are adapter targets until implemented.
- Keep confirmed appointments impossible unless the active POS provider returns successful booking metadata.
- For conversation or AI training requests, separate free-text knowledge from structured service-understanding data. Service aliases are salon-scoped operational data with stable normalized keys; they should not be replaced by prompt wording or hardcoded per-salon keyword lists.

If rules overlap or conflict, invoke `business-logic-overlap-analysis` thinking before finalizing the plan.

## Write Approval Hard Stop

- This skill is analysis-first and does not authorize edits by itself.
- Treat questions, complaints, screenshots, appshots, status checks, "why" prompts, and partial instructions as read-only investigation requests, not implementation approval.
- For any file-changing task, stop after requirements, acceptance criteria, open questions, and plan. Wait for latest explicit approval for the exact scope before editing files.

## Output Shape

Return concise sections:

1. **Business Goal:** the owner/customer outcome being enabled.
2. **Actors And Jobs:** who acts, what they need, and when.
3. **Current Truth:** existing repo behavior, source-of-truth docs, and relevant files if inspected.
4. **Proposed Workflow:** happy path plus fallback, disabled, retry, and owner-review paths.
5. **Requirements:** must-have behavior grouped by backend, frontend, data, integration, voice, docs, and tests when relevant.
6. **Acceptance Criteria:** observable outcomes in Given/When/Then or bullet form.
7. **Open Questions:** only questions that materially affect scope or correctness.
8. **Out Of Scope:** what should not be built in this slice.
9. **Product-Grade Edge Cases:** idempotency, duplicate prevention, conflict handling, retry/rerun behavior, and safe failure states.
10. **Implementation Guardrails:** where logic should live and what invariants must not be violated.

For implementation requests, stop after the analysis and proposed plan. Wait for explicit user approval before editing files.

## Quality Bar

- Do not invent production behavior to make a workflow look complete.
- Do not hide missing backend support behind optimistic UI copy.
- Prefer narrow vertical slices that can be verified end to end.
- Tie every requirement to an actor, state, owner action, backend rule, or measurable production outcome.
- Call out when docs, DTOs, frontend types, tests, and UI states must move together.
