---
name: salon-dashboard-ui
description: Investigate, review, plan, and only after explicit approval implement the Next.js salon owner dashboard UI. Use when frontend dashboard pages, onboarding, integrations, settings, service/staff tables, booking test UI, AI simulator, configuration transfer, public catalog settings, billing gates, or landing page handoffs may change. Do not edit frontend files until the latest user message explicitly approves the exact UI scope.
---

# Salon Dashboard UI

Use this skill for owner-dashboard work in `frontend/`. For the separate
customer-facing `landing/` app, still preserve the POS-first confirmation
boundary and public-safe catalog rules from `docs/architecture.md` and
`docs/api.md`.

## Codebase Truth Rule

- Be strictly honest about the current codebase and runtime configuration. Do not flatter, reassure, or infer readiness from docs, milestones, intended architecture, or previous claims.
- Before saying dashboard behavior works, is ready, or only needs testing, verify the actual component, API helper, backend contract, config gates, and tests/build/status output when available.
- Separate confirmed current behavior from product intent, planned behavior, unverified assumptions, and missing implementation.
- If evidence conflicts, stop and name the conflict directly with file references instead of smoothing over it.

## Product-Grade Rule

- Build dashboard behavior as commercial-grade production UI, not MVP, demo, prototype, or happy-path scaffolding.
- Before proposing or implementing, check double-click/repeated-submit behavior, idempotency, duplicate prevention, retry/rerun semantics, stale data, conflict handling, tenant/security/privacy boundaries, and loading/empty/error/success/disabled states.
- For export/import, sync, booking, integration, provider switching, public catalog, billing, or AI training controls, surface safe statuses and backend contract assumptions instead of implying unsupported behavior.
- Do not mark UI work complete until predictable edge cases are handled, safely gated, explicitly out of scope with a reason, or called out as a blocker.

## Product Experience

The app is an operational SaaS dashboard for nail salon owners. Keep the UI quiet, dense, professional, and fast to scan. Avoid marketing-page composition inside the app shell.

Read `DESIGN.md` before proposing, implementing, or reviewing dashboard UI. Treat it as the design, UI, and UX contract for tokens, layout, components, page states, copy, responsive behavior, and review criteria.

## UI Placement Gate

Before proposing where a dashboard feature belongs:

1. Name the owner workflow, trigger, operational object, and source of truth.
2. Inspect existing dashboard pages and name the adjacent controls that already manage related objects.
3. Identify the primary parent object and data grain. If the feature is a child field, child action, learned phrase, mapping, exception, status, or setting of one parent object, place it in that parent object's row, card, detail panel, or edit flow by default.
4. Do not create a standalone panel, page, or management table for child data unless the primary owner workflow is cross-object bulk review, audit, import/export, reporting, deduplication, or conflict resolution.
5. Remove redundant target selectors when the parent object already supplies the target context. Do not ask the owner to choose the current service, staff member, customer, category, or appointment again unless the workflow intentionally moves the child record.
6. Separate backend/API support from UI placement. Do not place a UI control on a page only because its backend route, module, or docs section is grouped there.
7. Compare plausible pages and component placements when placement is not obvious. State the chosen placement and at least one rejected placement with the reason it would confuse workflow or ownership.
8. Place structured operational data near the operational management surface it affects; place free-text policy/FAQ/notes near knowledge/training; place provider setup near integrations; keep review/audit workflows distinct from primary management surfaces.
9. Before presenting the Mockup as Text, ask whether the placement could make the owner confuse structured operational data with free-text knowledge, setup/configuration, review state, or unsupported production behavior. If yes, revise the placement first.

## Required Approval Before UI Edits

This skill does not authorize edits by itself.

Before editing any UI or user-facing layout:

1. Inspect the current component/page and relevant API contract.
2. Inspect `DESIGN.md` and map the change to the existing design contract.
3. Produce a Mockup as Text for user review.
4. Wait for explicit approval before changing frontend code.

The Mockup as Text must include layout blocks, visible copy, controls, states, responsive behavior, and backend/API assumptions.

- Treat screenshots, appshots, complaints, "why" prompts, status checks, and partial instructions as read-only investigation requests, not UI implementation approval.
- Earlier approval expires when the UI scope changes, after the assistant gives a final answer, or when the latest user message is a question or complaint instead of an approval.

## Required UI States

Every production page needs:

- Loading state
- Empty state
- Error state
- Success/data state
- Disabled or gated state when a workflow step is unavailable

## Frontend Rules

- Reuse `frontend/components/ui` and `frontend/components/layout`.
- Keep API calls in `frontend/lib/api` or feature-local helpers.
- Do not hard-code operational data as the main behavior.
- Do not claim AI booking is enabled until backend gate checks exist and pass.
- Use exact POS-first wording: "Square Appointments" for current integration, "adapter architecture" for future providers.
- AI tone belongs in Settings > AI receptionist as the controlled `ai_tone` speaking-style setting with backend-sourced value, save states, and preview copy. Do not place tone under Integrations > OpenAI or present it as model, voice, or provider setup.
- Public catalog UI must never expose staff contact details, POS IDs, sync errors,
  owner identifiers, provider tokens, or web booking confirmation claims.

## Validation

```bash
cd frontend
npm run typecheck
npm run build
```

If visual behavior changes, run a local dev server and inspect the relevant route.
