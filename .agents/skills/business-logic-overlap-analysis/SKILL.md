---
name: business-logic-overlap-analysis
description: Analyze overlapping feature rules, duplicated ownership, conflicting business logic, and regression risks in this AI Receptionist repo. Use when a request may touch multiple workflows, modules, actors, states, API contracts, POS booking rules, dashboard gates, voice behavior, or salon operations and Codex needs to identify collisions before implementation.
---

# Business Logic Overlap Analysis

Use this skill before implementing or reviewing work that may collide with existing business rules.

## Codebase Truth Rule

- Be strictly honest about the current codebase and runtime configuration. Do not flatter, reassure, or infer readiness from docs, milestones, intended architecture, or previous claims.
- Before saying rules do not overlap, behavior works, or a path is ready, verify the actual owning code paths, config gates, persistence, API/UI behavior, and tests/status output when available.
- Separate confirmed current behavior from product intent, planned behavior, unverified assumptions, and missing implementation.
- If evidence conflicts, stop and name the conflict directly with file references instead of smoothing over it.

## Product-Grade Rule

- Review every overlap as commercial-grade production behavior, not MVP, demo, prototype, or happy-path scaffolding.
- Before clearing a plan, check repeated-use behavior, idempotency, duplicate prevention, retry/rerun semantics, race conditions, conflict handling, tenant/security/privacy boundaries, and safe empty/error/disabled states.
- For export/import, sync, webhook, booking, provider, or AI training flows, verify stable keys, upsert or dedupe semantics, schema/version impact, and repeated execution behavior.
- Do not accept a design where predictable duplicate/rubbish data, misleading state, or unsupported behavior is merely undocumented.

## Start Here

1. Read `CONTEXT.md` for domain language and invariants.
2. Read `docs/architecture.md` for module ownership and dependency direction.
3. Read the smallest relevant source-of-truth docs for the workflow, such as `docs/api.md`, `docs/pos-adapter-layer.md`, `docs/square-integration.md`, `docs/production-readiness-checklist.md`, or `DESIGN.md`.
4. Inspect the current owning code paths before judging overlap. Prefer `rg` to find handlers, services, repositories, DTOs, frontend types, and dashboard state.

## Overlap Questions

Ask these questions before proposing implementation:

- Which actor owns the decision: caller, owner, staff, backend service, POS provider, AI runtime, or dashboard?
- Which module is the source of truth for the rule?
- Is the same decision made in more than one place?
- Does the frontend gate match the backend gate?
- Does any path imply a confirmed appointment without active `POSProvider` success and a provider booking ID?
- Does a local canonical record conflict with provider-side booking execution or sync status?
- Does a voice/conversation path bypass booking service rules?
- Does service understanding happen in one backend-owned place, or do prompts, name-slot repair, dashboard copy, and training corrections each infer services differently?
- Do owner corrections update the right data type: reusable knowledge for FAQ/policy, or structured `service_aliases` for service recognition?
- Does AI tone stay a style-only salon setting flowing through `salon_settings.ai_tone`, `conversation.RuntimeConfig`, `voice.ModelRequest`, and provider adapters without changing booking, handoff, slot, or service-understanding decisions?
- Does an API response, DTO, or UI label use a different state name than persistence or docs?
- Does the requested behavior create a race between availability, booking attempt, appointment write, notification, audit log, or POS error logging?
- Can the workflow be run, retried, imported, synced, submitted, or replayed more than once, and if so what stable key prevents duplicate or rubbish records?
- Does the change cross tenant, auth, token, or salon ownership boundaries?

## Collision Types

Classify findings with one or more of these labels:

- `rule_ownership`: unclear or duplicated owner for a business rule.
- `state_conflict`: incompatible status names, transitions, or terminal states.
- `source_of_truth`: frontend, local DB, provider projection, or POS provider treated as authority in the wrong place.
- `booking_boundary`: possible confirmed/rescheduled/cancelled appointment without provider success.
- `workflow_gap`: happy path exists but fallback, disabled, retry, owner review, or handoff path is missing.
- `api_contract`: backend DTO, frontend type, docs, or mapper disagree.
- `understanding_drift`: service recognition, aliases, fuzzy candidates, or transcript metadata differ across runtime, dashboard, docs, and tests.
- `tone_guardrail`: AI tone is treated as provider setup or prompt-only behavior, or style changes can override conversation and booking safety rules.
- `tenant_security`: salon ownership, token secrecy, or cross-salon data isolation is unclear.
- `milestone_scope`: UI or copy implies unsupported production behavior.
- `idempotency_gap`: repeated execution, retry, import, sync, webhook, or double-submit can create duplicates or rubbish state.
- `test_gap`: high-risk overlap lacks regression coverage.

## Output Shape

For analysis-only tasks, return:

1. **Scope:** what workflow, modules, and files were inspected.
2. **Rule Map:** actor, source of truth, owning module, state transitions, and side effects.
3. **Overlap Findings:** concrete collisions or likely collisions, ordered by risk, with file references when code was inspected.
4. **Decision Needed:** product or architecture choices that must be resolved before implementation.
5. **Implementation Guardrails:** where logic should live, what must stay out of scope, and which docs/types/tests must move together.
6. **Test Plan:** focused regression tests for the overlap points.

For implementation requests, stop after the analysis and plan. Wait for explicit user approval before editing files.

## Write Approval Hard Stop

- This skill is analysis-first and does not authorize edits by itself.
- Treat questions, complaints, screenshots, appshots, status checks, "why" prompts, and partial instructions as read-only investigation requests, not implementation approval.
- For any file-changing task, stop after overlap findings, decisions needed, guardrails, and test plan. Wait for latest explicit approval for the exact scope before editing files.

## Repo Invariants

- Confirmed booking requires active POS provider success and a provider booking ID.
- Booking and AI modules depend on `modules/pos.POSProvider`, not `modules/pos_square`.
- Square Appointments is the first real POS integration; future providers are architecture targets until real adapters exist.
- ManleAI owns canonical salon operational data; the active POS provider owns availability and booking execution.
- Unsupported workflows must be disabled or gated with the real dependency, not faked.
