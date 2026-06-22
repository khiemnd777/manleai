# Architecture

## Current Milestone

This codebase implements Milestone 1, Milestone 2, Milestone 3 booking safety, the Milestone 4 deterministic conversation simulator, the Milestone 5 live telephony webhook foundation, the Milestone 6 external AI voice provider layer, and the first Milestone 7A salon knowledge/training slice for the AI Receptionist system.

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
modules/customer     provider-neutral customer activity read model and POS lookup facade
modules/conversation deterministic simulator sessions, transcripts, summaries, and handoffs
modules/training     salon-authored knowledge base and owner corrections
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
features/dashboard   dashboard home, appointments, customers, services/staff controls, calls dashboard, AI training
features/integrations Square integration page
features/onboarding salon profile creation
lib/api              typed API client
types                API response types
```

## Core Boundary

ManleAI is the system of record for salon operational data: canonical services,
staff, customers, AI controls, owner workflow state, fallback pending requests,
logs, and training data. POS providers are external projections and booking
execution layers. The active POS provider owns real availability and provider
booking execution.

Provider IDs are mappings, not primary product identity. Square Appointments is
the first real POS integration; future POS names are architecture targets until
implemented. Current `services` and `staff` rows still carry Square-shaped
provider fields such as `pos_provider`, `pos_service_id`, and `pos_staff_id`.
The canonical ownership migration should move those mappings into provider link
records while preserving the POS-first booking boundary.

Correct dependency direction:

```txt
HTTP handler -> service -> repository/provider interface -> concrete adapter
```

The booking service depends on `modules/pos.POSProvider`. It must not import `modules/pos_square`.

Booking workflow state belongs to the backend. Create-booking, reschedule, cancel, and dashboard test-booking requests first create a `booking_attempts` row with `pos_pending` and a backend-owned POS idempotency key. The POS adapter is then called as an outbound writer. If the provider returns a POS booking ID and booking version, the same attempt is finalized as confirmed/rescheduled/cancelled and the appointment state is written in the backend database. If the provider fails, times out, or omits required booking metadata, the same attempt is finalized as `fallback_pending`, a POS error and owner notification are recorded, and no confirmed appointment is created. Reschedule, cancel, and test-booking cleanup requests must leave the internal appointment unchanged unless the provider succeeds.

The Milestone 4 conversation simulator and Milestone 5 live phone webhook path call the booking service through a provider-neutral booking tool. They do not import Square packages, read POS tokens, build Square payloads, or use Square location IDs directly. The runtime checks provider-neutral availability, offers available slots to the caller, stores offered slot segment assignments on the call session, and only calls booking creation after the caller selects a slot and required customer details are present. Selected segments and `staff_selection_mode=anyone` survive later turns so simulator and phone bookings can create the same provider-neutral multi-service request while avoiding customer-facing named-technician wording unless the customer chose a specific technician. Booking confirmations remain impossible unless the booking service returns a POS-confirmed booking attempt and appointment. If AI booking is disabled, a customer requests a human, or the booking path cannot confirm through POS, the runtime creates a handoff or fallback pending flow and avoids confirmed wording.

The live voice layer is split into `modules/voice`, `modules/voice_twilio`, and provider-specific AI adapter modules such as `modules/voice_openai`. `modules/voice` owns provider-neutral DTOs, runtime interfaces for telephony/STT/LLM/TTS providers, salon phone routing, dashboard readiness, short-lived TTS audio output persistence, and webhook event audit writes. `modules/voice_twilio` owns Twilio-specific request signature validation, webhook form mapping, recording fetches, and TwiML responses. `modules/voice_openai` owns OpenAI API URLs, payloads, response parsing, and model configuration. Twilio speech gathering, recording mode, TwiML speech output, and audio playback are treated as provider adapter behavior, not conversation-engine logic.

The Milestone 7 training layer stores owner-authored salon knowledge and corrections as salon-scoped data. Conversation runtime may read active knowledge as advisory context for FAQ and policy answers, and training evaluation previews can test active knowledge without creating call sessions or bookings. Knowledge never replaces the booking service or POS confirmation checks.

## Next Milestone

The next Milestone 7 slices should deepen owner approval loops without changing the POS-first confirmation boundary.

## Data Ownership

SQL files under `backend/migrations` are the current database source of truth.
The API startup migrator applies them once and records checksums in
`app_schema_migrations`. Ent schema files mirror the table structure so
generated clients can be introduced without changing the domain boundaries.

Domain ownership is separate from provider execution. ManleAI owns canonical
salon records and owner workflow state; the active POS owns provider-side
availability, booking IDs, booking versions, and the success or failure of
booking execution. A confirmed, rescheduled, or cancelled appointment must never
be recorded unless the active `POSProvider` returns the required successful
provider booking metadata.

Tenant isolation is enforced by owner-scoped queries in repositories and by
salon ownership checks before POS actions.
