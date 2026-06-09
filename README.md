# AI Receptionist for Nail Salons

POS-first production-like pilot foundation for an AI phone receptionist serving US nail salons, starting with Vietnamese-owned salons that use Square Appointments.

This repository currently implements Milestone 1, Milestone 2, and backend booking slices for Milestone 3:

- Go/Fiber API scaffold with PostgreSQL, Redis, startup SQL migrations, and Docker Compose.
- Auth, JWT access tokens, persisted refresh tokens, owner-scoped salon APIs.
- POS adapter boundary with provider-neutral DTOs.
- Square OAuth/connect/status/location/sync foundation.
- Encrypted POS token storage.
- Persisted Square OAuth state nonce and pinned Square API version.
- Provider-neutral booking attempt and appointment persistence with fallback pending safety.
- Square customer search/create, availability search, create-booking, reschedule, cancel, and test-booking gate paths.
- Backend service/staff list endpoints for synced POS data.
- Next.js admin shell with login, dashboard, onboarding profile creation, and Square integration status.
- Repo-local Codex guidance through `AGENTS.md`, `.agents/skills`, and `.codex/agents`.

Square Appointments create, reschedule, cancel, and dashboard test-booking operations are implemented through `POSProvider`. AI booking can only be enabled after Square is connected, services/staff are synced, and the latest Square test booking was created and cancelled successfully. Until Square returns a successful POS booking ID and booking version, failed provider calls create fallback pending requests instead of confirmed appointments or internal appointment state changes.

## Local Start

```bash
cp .env.example .env
docker compose up -d --build
```

The API applies database migrations automatically on startup. To seed the local demo owner and salon:

```bash
cd backend
DATABASE_URL=postgres://ai_receptionist:ai_receptionist@localhost:55432/ai_receptionist?sslmode=disable \
make seed-local
```

For non-Docker frontend development:

```bash
cp frontend/.env.example frontend/.env.local
cd frontend
npm install
npm run dev
```

Open `http://localhost:3088/login`.

Local seed login:

```txt
owner@lotusnails.example
password123
```

## Square Setup

Set these values before testing OAuth:

```bash
SQUARE_ENVIRONMENT=sandbox
SQUARE_CLIENT_ID=...
SQUARE_CLIENT_SECRET=...
SQUARE_REDIRECT_URL=http://localhost:18089/api/integrations/square/callback
SQUARE_API_VERSION=2026-05-20
```

The Square callback stores encrypted access and refresh tokens in `pos_connections`. Tokens are never returned to the frontend.

## Scope Boundary

Fully implemented now:

- Monorepo structure
- Auth and salon foundation
- POSProvider interface and provider-neutral DTOs
- Square OAuth skeleton and connect flow
- Square location listing
- Square service/staff sync foundation
- Signed and persisted Square OAuth state nonce
- Provider-neutral service/staff read endpoints
- Booking attempt, appointment, appointment service, and fallback notification tables
- Booking service that depends on `POSProvider`
- Square customer search/create, availability search, create-booking, reschedule, cancel, and test-booking gate paths
- Dashboard booking readiness UI for Square test booking and AI booking enablement
- POS sync and error logs
- Admin shell, login, dashboard, onboarding profile creation, integrations page

Still stubbed until later Milestone 3 slices:

- AI conversation engine
- Telephony, SMS, reminders, call logs, and knowledge base
- Stripe billing

## Agent Setup

This repo includes local Codex instructions and workflows:

- `AGENTS.md` for repo-wide agent guidance.
- `.agents/skills` for reusable repo-local skills.
- `.codex/agents` for custom subagents.
- `docs/agents` for usage notes.

See `docs/agents/usage.md` before asking Codex to split milestone work across subagents.
