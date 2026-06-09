# AI Receptionist for Nail Salons

POS-first production-like pilot foundation for an AI phone receptionist serving US nail salons, starting with Vietnamese-owned salons that use Square Appointments.

This repository currently implements Milestone 1 and Milestone 2 only:

- Go/Fiber API scaffold with PostgreSQL, Redis, startup SQL migrations, and Docker Compose.
- Auth, JWT access tokens, persisted refresh tokens, owner-scoped salon APIs.
- POS adapter boundary with provider-neutral DTOs.
- Square OAuth/connect/status/location/sync foundation.
- Encrypted POS token storage.
- Next.js admin shell with login, dashboard, onboarding profile creation, and Square integration status.
- Repo-local Codex guidance through `AGENTS.md`, `.agents/skills`, and `.codex/agents`.

Milestone 3 booking operations are intentionally not implemented yet. The API exposes the future routes with `501` responses so the frontend can keep the workflow visible without faking production behavior.

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

Open `http://localhost:3000/login`.

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
SQUARE_REDIRECT_URL=http://localhost:18080/api/integrations/square/callback
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
- POS sync and error logs
- Admin shell, login, dashboard, onboarding profile creation, integrations page

Still stubbed until later milestones:

- Square booking, reschedule, cancel, test booking, and cancel test booking
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
