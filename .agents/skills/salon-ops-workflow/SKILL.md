---
name: salon-ops-workflow
description: Analyze real nail salon operating workflows for this AI Receptionist repo, including caller journeys, owner handoff, staff/service availability, service categories, service-alias/category-alias corrections, structured party booking requests, business hours, booking requests, fallback pending review, Square Appointments setup, bilingual support, and dashboard actions. Use when Codex needs to reason about salon operations before designing, implementing, or reviewing product behavior.
---

# Salon Ops Workflow

Use this skill when a request depends on how a nail salon owner, caller, or staff member would actually work during a busy salon day.

## Codebase Truth Rule

- Be strictly honest about the current codebase and runtime configuration. Do not flatter, reassure, or infer readiness from docs, milestones, intended architecture, or previous claims.
- Before saying an operational workflow works, is ready, or only needs testing, verify the actual code/API/config gates, persistence, dashboard states, and tests/status output when available.
- Separate confirmed current behavior from product intent, planned behavior, unverified assumptions, and missing implementation.
- If evidence conflicts, stop and name the conflict directly with file references instead of smoothing over it.

## Product-Grade Rule

- Analyze salon workflows as commercial-grade production behavior, not MVP, demo, prototype, or happy-path scaffolding.
- Before proposing or approving a workflow, check repeated-use behavior, idempotency, duplicate prevention, retry/rerun semantics, owner conflict handling, tenant/security/privacy boundaries, and loading/empty/error/disabled states.
- For export/import, sync, booking, fallback pending, owner-review, or AI training flows, define stable keys, upsert or dedupe semantics, and how repeated execution avoids duplicate or rubbish records.
- Do not call a workflow ready if predictable salon-day exceptions would mislead the owner, caller, staff, AI runtime, or POS provider.

## Start Here

1. Read `CONTEXT.md` for shared terms.
2. Read `docs/architecture.md` for current system boundaries.
3. Read `docs/production-readiness-checklist.md` for production readiness scope.
4. For dashboard workflows, read `DESIGN.md`.
5. Inspect relevant code or API docs only after mapping the operational workflow.

## Operational Lens

Analyze the request across these salon realities:

- Owners are often serving customers while answering exceptions.
- Callers may ask for prices, hours, walk-ins, reschedules, cancellations, group bookings, or human help before giving full details.
- Callers may pronounce service names unclearly or use local salon wording. The operational fix is catalog-backed clarification through real services, `service_categories`, owner-reviewed `service_aliases`, or `service_category_aliases`, not per-call guessing or hardcoded keyword patches.
- Supported group or party bookings may be completed by the AI when party size and guest service counts resolve to real catalog services and provider-neutral availability returns bookable slots. Ambiguous or unsupported party requests must ask clarification or fall back safely; confirmed wording is allowed only after POS booking success.
- Staff availability, service duration, business hours, and active POS mappings determine whether booking can even be attempted.
- Square Appointments owns real availability and booking execution in the current production release.
- POS failure, disconnected Square, missing staff/service mappings, or disabled AI booking must become pending request or handoff behavior, not confirmed appointments.
- Dashboard actions must help the owner decide the next step quickly.
- Vietnamese support is in scope as product language, but do not assume full bilingual runtime behavior unless the feature explicitly supports it.

## Workflow Checklist

For each proposed workflow, identify:

- Entry point: phone call, simulator, dashboard, onboarding, Square setup, training, settings, or support action.
- Trigger: what user or system event starts the workflow.
- Required data: salon, services, service categories, category aliases, staff, customer, business hours, POS connection, provider mapping, voice readiness, owner settings.
- Decision gates: AI enabled, Square connected, booking-ready records, availability result, customer details, POS success, owner approval.
- Success state: what the owner and caller can safely believe happened.
- Exception states: disabled, blocked, unavailable, fallback pending, handoff, POS error, sync failure, retry needed.
- Owner action: review, approve, call back, sync, connect Square, enable AI, update services/staff/categories, apply a correction to knowledge or a service alias, add category aliases, contact/resolve party requests, edit settings, or dismiss.
- Repeat behavior: whether the workflow can be retried, resubmitted, imported, synced, or replayed, and what prevents duplicate/rubbish records.
- Auditability: booking attempt, POS error, sync log, notification, transcript, correction, party request, or dashboard timestamp.

## Output Shape

Return:

1. **Workflow Summary:** one paragraph in salon-operations language.
2. **Actor Journey:** caller, owner, staff, AI receptionist, POS provider.
3. **Operational Gates:** what must be true before the workflow can proceed.
4. **Exception Handling:** blocked, fallback, handoff, retry, owner-review, and disabled states.
5. **Dashboard Needs:** labels, statuses, owner actions, empty/loading/error/success/gated states.
6. **Data And API Needs:** backend state, DTOs, events, logs, and timestamps required.
7. **Risk Notes:** where the workflow may mislead the owner or caller.
8. **Acceptance Criteria:** observable behavior for the workflow.

For implementation requests, stop after workflow analysis and proposed plan. Wait for explicit user approval before editing files.

## Write Approval Hard Stop

- This skill is analysis-first and does not authorize edits by itself.
- Treat questions, complaints, screenshots, appshots, status checks, "why" prompts, and partial instructions as read-only investigation requests, not implementation approval.
- For any file-changing task, stop after workflow analysis, operational gates, exception handling, dashboard needs, and plan. Wait for latest explicit approval for the exact scope before editing files.

## Non-Negotiables

- Never describe an appointment as confirmed unless POS booking succeeded.
- Keep Square-specific behavior inside Square/POS integration ownership.
- Gate unsupported workflows with real dependencies.
- Do not add decorative dashboard behavior that does not help an owner act.
- Keep owner-facing copy concise and scope-honest.
