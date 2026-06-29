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

- `backend/`: Go/Fiber API, PostgreSQL persistence, startup SQL migrations, Ent schemas, domain modules.
- `frontend/`: Next.js TypeScript admin UI.
- `docs/`: architecture, integration, deployment, testing, and agent guidance.
- `.agents/skills/`: repo-local Codex skills for repeatable workflows.
- `.codex/agents/`: project-scoped custom subagents.

## Conversation Style

- The user may use the Vietnamese "mày-tao" register, but the assistant must not use it. Use a respectful neutral register such as "mình/bạn" or omit pronouns when possible.

## Evidence And Source Discipline

- Do not give vendor-console navigation, setup instructions, or operational diagnoses as bare assertions. Ground them in visible user-provided UI/log text, repository files, command output, or official vendor documentation.
- When interpreting logs or screenshots, quote the exact field names, values, timestamps, error codes, and request IDs that support the conclusion. Separate confirmed facts from inference.
- When recommending where to click in a third-party console, cite the source of that path: current screenshot/appshot, official documentation URL, or clearly label it as an unverified UI-memory guess and ask the user to confirm the screen.
- Prefer official vendor docs for external-service behavior. Include links when internet sources are used, and avoid half-sourced instructions such as "go to X -> Y" without origin or evidence.
- If the evidence is insufficient, say exactly what is missing and what needs to be inspected next instead of filling the gap with a confident-sounding answer.

## User Confirmation Gate

- For every feature request, bugfix, refactor, or task that changes code, stop after investigation and present the intended implementation plan for user review.
- Do not edit production code, migrations, tests, frontend files, or runtime configuration until the user explicitly confirms and allows implementation.
- Read-only exploration, repo inspection, and answering questions are allowed before confirmation.
- Documentation-only or agent-rule changes may be applied directly only when the user explicitly asks to change those files.
- Treat phrases such as "need", "cần", "should", "want", "add", "create", "fix", "build", or "implement" as a request to analyze and propose a plan, not as approval to write files.
- Do not create, scaffold, patch, format, generate, move, delete, or otherwise write any code, tests, docs, skills, agents, migrations, runtime configuration, or generated artifacts before explicit approval for that exact scope.
- Do not request escalated permissions for any write operation before the user has approved the scope and the specific need for escalation.
- Explicit approval must be clear and action-oriented, such as "approved", "cho phép ghi file", "triển khai theo plan này", or "sửa các file này"; otherwise continue in read-only planning mode.
- One explicit approval is sufficient when it directly answers an agent-proposed scope or plan. Do not ask for a second confirmation for the same scope; execute the approved scope.
- If the user says "implement", "fix", "build", or similar, still present the plan first and wait for explicit approval before changing code.
- For any request that changes the UI or user-facing layout, provide a **Mockup as Text** before implementation and wait for explicit approval.

## Product-Grade Standard

- Build and review every approved slice as production-grade pilot software, not MVP, demo, prototype, or happy-path scaffolding.
- Before proposing or implementing, check repeated-use behavior, idempotency, duplicate prevention, retry/rerun semantics, conflict handling, rollback or cleanup needs, disabled/error/empty states, tenant boundaries, and security/privacy exposure.
- For export/import, sync, webhook, booking, provider, AI training, or any workflow that can run more than once, define stable keys, upsert or dedupe behavior, and prove repeated execution will not create duplicate or rubbish records.
- If external systems will consume exported data, include contract stability, schema versioning, stable identifiers, and import semantics even when the importer is outside the current implementation slice.
- Narrow vertical slices are allowed only when the slice is internally complete and scope-honest. Do not call a slice done if predictable product-grade failure modes are unhandled, hidden, or left ambiguous.
- Do not ship fake, placeholder, or demo behavior as production behavior. Local seed/demo data is allowed only when isolated from production paths.
- Acceptance criteria and test plans must cover critical edge cases and regression risks, not just the happy path.

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
- Add SQL migrations under `backend/migrations` for schema changes and keep Ent schemas aligned.
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
docker compose up -d postgres redis
```

The API and worker run the embedded startup migrator when `AUTO_MIGRATE=true`.

## Change Discipline

- Prefer narrow, vertical slices that keep backend, frontend, docs, and tests coherent.
- Respect the User Confirmation Gate before editing code.
- Do not implement fake production paths. Local seed/demo data is allowed only when isolated from production behavior.
- If a requested change crosses API contracts, update DTOs, mappers, docs, and UI states together.
- Every implementation plan must include the product-grade edge cases relevant to the slice, especially repeated execution/idempotency, duplicate prevention, conflict handling, and safe failure states.
- For high-risk booking, auth, token, tenant, or POS behavior, add tests before or alongside the change.
