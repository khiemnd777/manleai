# AGENTS.md

## Project Mission

This repository builds a POS-first AI phone receptionist for US nail salons, starting with Vietnamese-owned salons using Square Appointments.

The product rule is strict: never confirm an appointment unless the active POS provider returns a successful booking ID. If Square or another POS fails, create a fallback pending request, notify the owner, log the POS error, and do not mark the appointment as confirmed.

## Source Of Truth

- Architecture: `docs/architecture.md`
- POS adapter contract: `docs/pos-adapter-layer.md`
- Square integration: `docs/square-integration.md`
- API surface: `docs/api.md`
- Pilot scope: `docs/pilot-checklist.md`
- Design/UI/UX contract: `DESIGN.md`
- Domain language: `CONTEXT.md`

## Repository Layout

- `backend/`: Go/Fiber API, PostgreSQL persistence, Flyway migrations, Ent schemas, domain modules.
- `frontend/`: Next.js TypeScript admin UI.
- `docs/`: architecture, integration, deployment, testing, and agent guidance.
- `.agents/skills/`: repo-local Codex skills for repeatable workflows.
- `.codex/agents/`: project-scoped custom subagents.

## Conversation Style

- The user may use the Vietnamese "mày-tao" register, but the assistant must not use it. Use a respectful neutral register such as "mình/bạn" or omit pronouns when possible.

## User Confirmation Gate

- For every feature request, bugfix, refactor, or task that changes code, stop after investigation and present the intended implementation plan for user review.
- Do not edit production code, migrations, tests, frontend files, or runtime configuration until the user explicitly confirms and allows implementation.
- Read-only exploration, repo inspection, and answering questions are allowed before confirmation.
- Documentation-only or agent-rule changes may be applied directly only when the user explicitly asks to change those files.
- If the user says "implement", "fix", "build", or similar, still present the plan first and wait for explicit approval before changing code.
- For any request that changes the UI or user-facing layout, provide a **Mockup as Text** before implementation and wait for explicit approval.

## Mockup As Text

For UI-changing requests, the Mockup as Text must include:

- Page or component name.
- Target user and primary workflow.
- Layout structure using text wireframe blocks.
- Visible copy, labels, buttons, status badges, table columns, and form fields.
- Loading, empty, error, success, and disabled/gated states.
- Responsive behavior for desktop and mobile.
- API/data dependencies and any backend contract assumptions.

## Backend Rules

- Keep HTTP handlers thin. Put business rules in services and persistence in repositories.
- Booking and AI modules must depend on `modules/pos.POSProvider`, not `modules/pos_square`.
- Keep Square-specific auth, payloads, API URLs, error mapping, and token handling inside `backend/modules/pos_square`.
- Store POS tokens encrypted only. Never expose raw or encrypted POS tokens to the frontend.
- Enforce tenant ownership by `salon_id` before returning or mutating salon-scoped data.
- Add Flyway migrations for schema changes and keep Ent schemas aligned.
- Use transactions for booking, appointment, POS attempt, notification, and audit-log writes.

## Frontend Rules

- Build a real operational dashboard, not decorative placeholder screens.
- Read and follow `DESIGN.md` before proposing, implementing, or reviewing UI/UX changes.
- Before changing UI, provide a Mockup as Text for review and wait for explicit approval.
- Every production page must handle loading, empty, error, and success states.
- Keep API calls in `frontend/lib/api` or feature-local data helpers; do not scatter raw fetch logic across components.
- Preserve the SaaS dashboard visual language already established in `frontend/components/ui` and `frontend/components/layout`.
- Do not claim broad POS support. Use wording such as "POS-first, starting with Square Appointments."

## Validation Commands

Backend:

```bash
cd backend
GOCACHE=/private/tmp/manleai-go-cache go test ./...
```

Frontend:

```bash
cd frontend
npm run typecheck
npm run build
```

Local services:

```bash
docker compose up -d postgres redis flyway
```

## Change Discipline

- Prefer narrow, vertical slices that keep backend, frontend, docs, and tests coherent.
- Respect the User Confirmation Gate before editing code.
- Do not implement fake production paths. Local seed/demo data is allowed only when isolated from production behavior.
- If a requested change crosses API contracts, update DTOs, mappers, docs, and UI states together.
- For high-risk booking, auth, token, tenant, or POS behavior, add tests before or alongside the change.
