---
name: salon-dashboard-ui
description: Build or review the Next.js salon owner dashboard UI. Use when editing frontend dashboard pages, onboarding, integrations, settings, service/staff tables, booking test UI, AI simulator, or landing page.
---

# Salon Dashboard UI

Use this skill for frontend work in `frontend/`.

## Product Experience

The app is an operational SaaS dashboard for nail salon owners. Keep the UI quiet, dense, professional, and fast to scan. Avoid marketing-page composition inside the app shell.

Read `DESIGN.md` before proposing, implementing, or reviewing dashboard UI. Treat it as the design, UI, and UX contract for tokens, layout, components, page states, copy, responsive behavior, and review criteria.

## Required Approval Before UI Edits

Before editing any UI or user-facing layout:

1. Inspect the current component/page and relevant API contract.
2. Inspect `DESIGN.md` and map the change to the existing design contract.
3. Produce a Mockup as Text for user review.
4. Wait for explicit approval before changing frontend code.

The Mockup as Text must include layout blocks, visible copy, controls, states, responsive behavior, and backend/API assumptions.

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

## Validation

```bash
cd frontend
npm run typecheck
npm run build
```

If visual behavior changes, run a local dev server and inspect the relevant route.
