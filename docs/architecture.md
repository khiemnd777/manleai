# Architecture

## Current Milestone

This codebase implements Milestone 1, Milestone 2, Milestone 3 booking safety, the Milestone 4 deterministic conversation simulator, the Milestone 5 live telephony webhook foundation, and the Milestone 6 external AI voice provider layer for the AI Receptionist system.

The backend is organized as:

```txt
cmd/api              Fiber HTTP server
cmd/worker           worker entrypoint for later async jobs
internal/config      environment config
internal/database    PostgreSQL connection bootstrap and startup migrations
internal/encryption  AES-GCM token encryption
internal/middleware  JWT middleware
modules/auth         login, refresh, roles
modules/salon        salon profile, settings, business hours
modules/pos          provider-neutral POS contracts and persistence
modules/pos_square   Square adapter and Square integration routes
modules/booking      booking attempts, appointments, and fallback pending safety
modules/conversation deterministic simulator sessions, transcripts, summaries, and handoffs
modules/voice        provider-neutral live voice runtime, status, routing, and webhook event audit
modules/voice_openai OpenAI STT, guarded LLM reply, and TTS adapters
modules/voice_twilio Twilio signature verification, form parsing, and TwiML responses
```

The frontend is organized as:

```txt
app/                 Next.js routes
components/ui        reusable UI primitives
components/layout    dashboard shell
features/auth        login flow
features/dashboard   dashboard home, appointments, services/staff controls, calls dashboard
features/integrations Square integration page
features/onboarding salon profile creation
lib/api              typed API client
types                API response types
```

## Core Boundary

Correct dependency direction:

```txt
HTTP handler -> service -> repository/provider interface -> concrete adapter
```

The booking service depends on `modules/pos.POSProvider`. It must not import `modules/pos_square`.

Square create-booking, reschedule, cancel, and dashboard test-booking gate operations are implemented inside `modules/pos_square` and routed through the provider-neutral booking service where appointment state changes are required. Until a provider returns a POS booking ID and booking version, booking requests must be stored as fallback pending attempts and must not create confirmed appointments. Reschedule, cancel, and test-booking cleanup requests must leave the internal appointment unchanged unless the provider succeeds.

The Milestone 4 conversation simulator and Milestone 5 live phone webhook path call the booking service through a provider-neutral booking tool. They do not import Square packages, read POS tokens, build Square payloads, or use Square location IDs directly. Booking confirmations remain impossible unless the booking service returns a POS-confirmed booking attempt and appointment. If AI booking is disabled, a customer requests a human, or the booking path cannot confirm through POS, the runtime creates a handoff or fallback pending flow and avoids confirmed wording.

The live voice layer is split into `modules/voice`, `modules/voice_twilio`, and provider-specific AI adapter modules such as `modules/voice_openai`. `modules/voice` owns provider-neutral DTOs, runtime interfaces for telephony/STT/LLM/TTS providers, salon phone routing, dashboard readiness, short-lived TTS audio output persistence, and webhook event audit writes. `modules/voice_twilio` owns Twilio-specific request signature validation, webhook form mapping, recording fetches, and TwiML responses. `modules/voice_openai` owns OpenAI API URLs, payloads, response parsing, and model configuration. Twilio speech gathering, recording mode, TwiML speech output, and audio playback are treated as provider adapter behavior, not conversation-engine logic.

## Next Milestone

Milestone 7 is knowledge base, owner corrections, and AI training workflows. It should add salon-authored operating knowledge without changing the POS-first confirmation boundary.

## Data Ownership

SQL files under `backend/migrations` are the current database source of truth. The API startup migrator applies them once and records checksums in `app_schema_migrations`. Ent schema files mirror the table structure so generated clients can be introduced without changing the domain boundaries.

Tenant isolation is enforced by owner-scoped queries in repositories and by salon ownership checks before POS actions.
