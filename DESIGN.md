# DESIGN.md

## Purpose

This file is the design, UI, and UX contract for ManleAI agents. Use it before proposing, implementing, or reviewing any user-facing frontend work.

The product is an operational SaaS dashboard for nail salon owners, not a marketing site. The UI should help an owner configure the salon, connect Square Appointments, monitor phone receptionist readiness, and understand failures quickly.

The public customer-facing catalog in `landing/` is a separate surface. It can
use a simpler customer-facing layout, but it must stay public-safe: call-to-book
only, no confirmed web booking claims, no staff contact details, no POS IDs, no
sync errors, no owner identifiers, and no provider tokens.

## Product Experience

- Keep the interface quiet, dense, professional, and fast to scan.
- Prioritize operational clarity over decorative visuals.
- Show real workflow status, next actions, and blockers. Do not fill production pages with fake activity.
- Keep all product claims POS-first and scope-honest: "Square Appointments" is the current integration; other POS providers are future adapter targets.
- Never imply an appointment is confirmed unless the active POS provider returned a successful booking ID.
- When a workflow is not available yet, show a disabled or gated state with the real dependency, not a pretend success path.

## Visual System

Use the established frontend tokens from `frontend/tailwind.config.ts`:

- `ink` `#111827` for primary text.
- `muted` `#6b7280` for secondary text.
- `line` `#d9dee8` for borders and separators.
- `panel` `#ffffff` for cards and panels.
- `shell` `#f5f7fb` for the app background.
- `brand` `#0f766e` for primary actions, active navigation, and key icons.
- `accent` `#b91c1c` for destructive actions.
- `shadow-soft` for elevated panels only.

Use Inter/system sans typography. Keep page headings compact: `text-2xl` is enough for dashboard pages; use `text-base`/`text-sm` inside cards and controls. Do not use viewport-scaled text or negative letter spacing.

Use `rounded-md` for buttons, nav items, alerts, inputs, and small controls. Use `rounded-lg` only for cards/panels. Do not create highly rounded pill-heavy layouts unless the existing component already does so, such as status badges.

## Layout System

Dashboard pages live inside `frontend/components/layout/app-shell.tsx`:

- Desktop uses the fixed left navigation and a top status/action bar.
- Main content is constrained with `max-w-7xl`, `px-5`, and `py-6`.
- Page content should use `space-y-6` and grid gaps of `gap-4` or `gap-6`.
- Prefer responsive grids like `md:grid-cols-2`, `xl:grid-cols-4`, or explicit dashboard ratios.
- Avoid nested cards. Use cards for individual repeated items, focused forms, tables, and operational panels.
- Do not build landing-page hero sections inside the dashboard shell.

For mobile:

- Collapse data into a single readable column.
- Keep primary actions reachable without horizontal scrolling.
- Tables must degrade to cards, compact rows, or horizontally scrollable regions with visible context.
- Text must fit inside buttons, badges, cards, and table cells without overlap.

## Components

Reuse primitives from `frontend/components/ui` before adding new styling:

- `Button` for actions. Use `primary` for the main next step, `secondary` for safe alternatives, `ghost` for low-emphasis shell actions, and `danger` for destructive actions.
- `Card`, `CardTitle`, and `CardDescription` for focused dashboard panels.
- `Badge` for statuses. Add status classes centrally when a new status is introduced.
- `Alert` for blocking errors or success confirmations.
- `Skeleton` for loading states.

Use lucide-react icons for navigation, buttons, and status affordances when an icon adds scan value. Icons should be `h-4 w-4` in buttons/nav and `h-5 w-5` in panel headers unless a local pattern requires otherwise.

If a new reusable primitive is needed, put it in `frontend/components/ui` and match the existing token, radius, typography, disabled, and focus-visible behavior.

## Standard Page Anatomy

Every production dashboard page should have:

1. Header area with page title, short operational description, and primary action/status when relevant.
2. Loading state using skeletons shaped like the final layout.
3. Error state using `Alert` with a clear retry or next action when available.
4. Empty state that explains what is missing and provides the next setup step.
5. Success/data state with real data, timestamps, statuses, and owner actions.
6. Disabled/gated state for workflows blocked by onboarding, Square connection, booking safety checks, or milestone scope.

Do not ship a page that only says a feature will come later unless it is explicitly a gated placeholder for a milestone, and even then it must tell the owner what dependency unlocks it.

## Copy And Terminology

Use concise operational copy:

- Prefer "Connect Square", "Sync services", "Review pending request", and "Open fallback requests" over vague marketing language.
- Say "Square Appointments" for the current POS integration.
- Say "POS-first, starting with Square Appointments" for broader positioning.
- Say "adapter architecture" when describing future POS provider support.
- Use "AI phone receptionist" or "AI receptionist" consistently.
- For booking failures, distinguish "pending request" from "confirmed appointment".

Avoid:

- Claims of broad POS support before providers exist.
- Claims that AI booking is live before backend gates pass.
- Decorative feature explanations inside the app UI.
- Placeholder operational metrics that look real.

## Forms And Tables

Forms:

- Group related fields with clear labels and helper text only when it reduces errors.
- Show validation errors near the field and a page-level error when submission fails.
- Disable submit while saving and keep the label action-specific, such as "Save salon profile" or "Connect Square".
- Preserve owner-entered data after failed submissions.

Tables and lists:

- Use columns that support owner decisions: name, status, source, last synced, owner action, and failure reason where relevant.
- Empty rows should become an empty state, not blank table chrome.
- Error/failure rows need visible status badges and next actions.
- Keep raw IDs out of the primary UI unless needed for support/debug workflows.

## State And Data Contract

Frontend state must reflect backend truth:

- Read API calls from `frontend/lib/api` or feature-local data helpers.
- Keep frontend types aligned with backend DTOs.
- Do not expose raw or encrypted POS tokens.
- Tenant-scoped UI must assume backend ownership checks by `salon_id` and must not cache cross-salon data casually.
- When API support is missing, gate the UI instead of fabricating production behavior.

## Agent Workflow

Before frontend/UI edits:

1. Read this file, `AGENTS.md`, and `.agents/skills/salon-dashboard-ui/SKILL.md`.
2. Inspect the existing page, nearest components, API helper, and backend/API docs that own the workflow.
3. Provide a Mockup as Text and wait for explicit user approval before editing UI code.
4. Implement the smallest coherent slice using existing components and tokens.
5. Include loading, empty, error, success, and disabled/gated states.
6. Run `cd frontend && npm run typecheck && npm run build`.
7. For visible layout changes, run the local app and inspect the route on desktop and mobile.

## Review Checklist

Use this checklist before finishing UI work:

- The page looks like an operational dashboard, not a landing page.
- The layout matches the existing app shell, spacing, cards, buttons, badges, and typography.
- The copy is scope-honest about Square Appointments and AI booking readiness.
- Every production page state is represented.
- Buttons and links have clear outcomes and disabled states where needed.
- Mobile layout has no overlap or horizontal overflow except intentional table scrolling.
- API data, DTOs, and frontend types agree.
- No fake confirmed appointment path exists without POS booking success.
