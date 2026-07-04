# AI Receptionist for Nail Salons

POS-first commercial production foundation for an AI phone receptionist serving US nail salons, starting with Vietnamese-owned salons that use Square Appointments.

This repository currently implements Milestone 1, Milestone 2, Milestone 3 booking safety, the Milestone 4 deterministic conversation simulator, the Milestone 5 live telephony webhook foundation, the Milestone 6 external AI voice provider layer, the Milestone 7A-7C salon knowledge/training slices, canonical POS ownership/provider-switch gates, configuration transfer, public catalog publishing, and call lifecycle retention:

- Go/Fiber API scaffold with PostgreSQL, Redis, startup SQL migrations, and Docker Compose.
- Auth, JWT access tokens, persisted refresh tokens, owner-scoped salon APIs.
- POS adapter boundary with provider-neutral DTOs.
- Square OAuth/connect/status/location/sync foundation.
- Encrypted POS token storage.
- Persisted Square OAuth state nonce and pinned Square API version.
- Provider-neutral booking attempt and appointment persistence with fallback pending safety.
- Square customer search/create, availability search, create-booking, reschedule, cancel, and test-booking gate paths.
- Deterministic AI conversation simulator with persisted call sessions, transcript messages, summaries, owner handoffs, and booking attempt linkage.
- Twilio live voice webhook foundation with signed webhook verification, TwiML responses, phone call session routing, voice webhook audit events, and provider-neutral voice runtime interfaces.
- OpenAI-backed external STT, LLM reply, and TTS adapters behind the voice runtime interfaces, with guarded replies, recording-mode turns, expiring TTS audio playback, and safe fallback to text/Twilio behavior when providers fail.
- OpenAI Realtime and Twilio Media Streams bridge behind provider adapters, with completed transcripts routed back through the same conversation and booking services.
- Salon-authored knowledge base and owner corrections for FAQ, policy, hours, handoff, and operating notes.
- Transcript review correction capture from Calls into AI Training.
- Read-only AI Training evaluation previews for sample customer questions.
- Canonical service, staff, and customer CRUD with POS link/sync status and active-provider booking gates.
- Dashboard-managed Square Appointments, Twilio, and OpenAI runtime configuration stored per salon with encrypted secrets.
- Provider switch readiness, match review, dry-run readiness, and import wizard gates that stay disabled until a real alternate adapter exists.
- Safe configuration export/import preview and apply flows that exclude secrets and operational records.
- Backend service/staff list endpoints for synced and local canonical data.
- Next.js admin shell with login, create-account, dashboard, onboarding profile creation, Square integration status, appointments, calls, customers, services, staff, settings, billing gate, and AI Training.
- Public salon catalog app in `landing/` for customer-facing services, staff, hours, and call-to-book pages.
- Repo-local Codex guidance through `AGENTS.md`, `.agents/skills`, and `.codex/agents`.

Square Appointments create, reschedule, cancel, and dashboard test-booking operations are implemented through `POSProvider`. AI booking can only be enabled after Square is connected, a location is selected, services/staff/business hour periods are synced, and at least one service and staff member are booking-ready for the active provider. Square test booking create/cancel is an optional POS write smoke test, not an AI enablement gate. Until Square returns a successful POS booking ID and booking version, failed provider calls create fallback pending requests instead of confirmed appointments or internal appointment state changes.

The Milestone 4 simulator and Milestone 5 phone webhook path are provider-neutral at the conversation layer. They record sessions and transcripts, call the booking service only after required booking details are collected, and never confirm an appointment unless the booking service returns a POS-confirmed attempt and appointment.

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

The public landing app runs separately at `http://localhost:3090` when using
Docker Compose. For non-Docker landing development:

```bash
cd landing
npm install
npm run dev
```

Local seed login:

```txt
owner@lotusnails.example
password123
```

## Square Setup

Configure Square Appointments in the Integrations dashboard for the salon. The backend stores the Square client secret encrypted and resolves dashboard-saved configuration before environment fallbacks.

These environment variables are optional local bootstrap/fallback values when no dashboard Square configuration exists:

```bash
SQUARE_ENVIRONMENT=sandbox
SQUARE_CLIENT_ID=...
SQUARE_CLIENT_SECRET=...
SQUARE_REDIRECT_URL=http://localhost:18089/api/integrations/square/callback
SQUARE_API_VERSION=2026-05-20
```

The Square callback stores encrypted access and refresh tokens in `pos_connections`. Tokens are never returned to the frontend.

## Voice Setup

Configure Twilio and OpenAI in the Integrations dashboard for the salon before pointing Twilio Programmable Voice webhooks at the API. Dashboard-saved provider configuration takes precedence over environment values.

These environment variables are optional local bootstrap/fallback values when no dashboard Twilio/OpenAI configuration exists:

```bash
VOICE_PROVIDER=twilio
VOICE_PUBLIC_BASE_URL=https://api.example.com
VOICE_TWILIO_AUTH_TOKEN=...
VOICE_TWILIO_INCOMING_PATH=/api/voice/twilio/incoming
VOICE_TWILIO_TURN_PATH=/api/voice/twilio/turn
VOICE_TWILIO_RECORDING_PATH=/api/voice/twilio/recording
VOICE_TWILIO_STREAM_PATH=/api/voice/twilio/stream
VOICE_TWILIO_VOICE_TRANSPORT=recording
VOICE_AI_PROVIDER=openai
VOICE_OPENAI_API_KEY=...
VOICE_OPENAI_BASE_URL=https://api.openai.com/v1
VOICE_OPENAI_TRANSCRIPTION_MODEL=gpt-4o-mini-transcribe
VOICE_OPENAI_REPLY_MODEL=gpt-4.1-mini
VOICE_OPENAI_SPEECH_MODEL=gpt-4o-mini-tts
VOICE_OPENAI_SPEECH_VOICE=alloy
VOICE_OPENAI_REALTIME_ENABLED=false
VOICE_OPENAI_REALTIME_MODEL=gpt-realtime-2
VOICE_OPENAI_REALTIME_VOICE=alloy
VOICE_OPENAI_REALTIME_INSTRUCTIONS=
```

The Twilio incoming, turn, recording, stream status, stream fallback, and Media Streams paths reject unsigned or incorrectly signed requests where applicable. Live phone sessions route by matching Twilio `To` against the configured salon phone number. When external STT is configured, Twilio can use recording-mode turns and the OpenAI transcription adapter; otherwise it keeps the speech gather path. For lower latency, configure realtime stream transport and OpenAI realtime settings in the dashboard. TTS audio is stored as short-lived runtime output and served through public unguessable audio URLs for Twilio `<Play>`.

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
- Conversation simulator session, transcript, summary, handoff, and booking-attempt linkage tables
- Deterministic conversation engine that asks one question at a time and routes booking through `booking.Service`
- Live phone call session metadata and voice webhook event audit table
- Twilio signed incoming/turn webhooks that create phone call sessions and continue transcripts through the same conversation engine
- Twilio recording-mode and Media Streams paths for external STT/realtime turns
- Provider-neutral voice interfaces for telephony, STT, LLM, TTS, and realtime runtime adapters
- OpenAI external voice adapters for STT, guarded LLM replies, TTS playback, and realtime transcripts
- Call session archive/redaction lifecycle with worker-enforced retention
- Dashboard-managed provider configuration for Square Appointments, Twilio, and OpenAI
- Canonical Services, Staff, and Customers CRUD with sync/link status
- Provider switch readiness, match review, dry-run readiness, and import wizard gated shell
- Configuration export/import preview and apply flows without secrets or operational data
- Dashboard booking readiness UI for optional Square test booking and AI booking enablement
- Dashboard Calls page with live webhook readiness, external AI provider readiness, simulator and phone transcripts, detected details, outcomes, and recent sessions
- Dashboard AI Training page with knowledge CRUD and owner correction workflows
- Dashboard Calls page correction capture for reviewed transcript messages
- Dashboard AI Training answer preview for active knowledge evaluation
- Owner-published public catalog pages in `landing/` backed by safe unauthenticated `/api/public/salon` and `/api/public/salons/:slug` reads
- POS sync and error logs
- Admin shell, login, create-account, dashboard, onboarding profile creation/import, integrations page, appointments, calls, customers, services, staff, settings, billing gate, and training pages

Still stubbed until later milestones:

- Additional owner approval loops beyond transcript correction review
- Executable alternate POS provider import, dry-run, and activation
- SMS and reminders
- Stripe billing

## Agent Setup

This repo includes local Codex instructions and workflows:

- `AGENTS.md` for repo-wide agent guidance.
- `.agents/skills` for reusable repo-local skills.
- `.codex/agents` for custom subagents.
- `docs/agents` for usage notes.

See `docs/agents/usage.md` before asking Codex to split milestone work across subagents.
