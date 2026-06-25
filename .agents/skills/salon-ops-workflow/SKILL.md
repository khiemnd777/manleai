---
name: salon-ops-workflow
description: Analyze real nail salon operating workflows for this AI Receptionist repo, including caller journeys, owner handoff, staff/service availability, business hours, booking requests, fallback pending review, Square Appointments setup, bilingual support, and dashboard actions. Use when Codex needs to reason about salon operations before designing, implementing, or reviewing product behavior.
---

# Salon Ops Workflow

Use this skill when a request depends on how a nail salon owner, caller, or staff member would actually work during a busy salon day.

## Start Here

1. Read `CONTEXT.md` for shared terms.
2. Read `docs/architecture.md` for current system boundaries.
3. Read `docs/pilot-checklist.md` for milestone scope.
4. For dashboard workflows, read `DESIGN.md`.
5. Inspect relevant code or API docs only after mapping the operational workflow.

## Operational Lens

Analyze the request across these salon realities:

- Owners are often serving customers while answering exceptions.
- Callers may ask for prices, hours, walk-ins, reschedules, cancellations, group bookings, or human help before giving full details.
- Staff availability, service duration, business hours, and active POS mappings determine whether booking can even be attempted.
- Square Appointments owns real availability and booking execution in the pilot.
- POS failure, disconnected Square, missing staff/service mappings, or disabled AI booking must become pending request or handoff behavior, not confirmed appointments.
- Dashboard actions must help the owner decide the next step quickly.
- Vietnamese support is in scope as product language, but do not assume full bilingual runtime behavior unless the feature explicitly supports it.

## Workflow Checklist

For each proposed workflow, identify:

- Entry point: phone call, simulator, dashboard, onboarding, Square setup, training, settings, or support action.
- Trigger: what user or system event starts the workflow.
- Required data: salon, services, staff, customer, business hours, POS connection, provider mapping, voice readiness, owner settings.
- Decision gates: AI enabled, Square connected, booking-ready records, availability result, customer details, POS success, owner approval.
- Success state: what the owner and caller can safely believe happened.
- Exception states: disabled, blocked, unavailable, fallback pending, handoff, POS error, sync failure, retry needed.
- Owner action: review, approve, call back, sync, connect Square, enable AI, update services/staff, edit settings, or dismiss.
- Auditability: booking attempt, POS error, sync log, notification, transcript, correction, or dashboard timestamp.

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

## Non-Negotiables

- Never describe an appointment as confirmed unless POS booking succeeded.
- Keep Square-specific behavior inside Square/POS integration ownership.
- Gate unsupported workflows with real dependencies.
- Do not add decorative dashboard behavior that does not help an owner act.
- Keep owner-facing copy concise and scope-honest.
