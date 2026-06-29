# Architecture

## Current Milestone

This codebase implements Milestone 1, Milestone 2, Milestone 3 booking safety, the Milestone 4 deterministic conversation simulator, the Milestone 5 live telephony webhook foundation, the Milestone 6 external AI voice provider layer, the Milestone 7A-7C salon knowledge/training slices, canonical POS ownership/provider-switch gates, configuration transfer, public catalog publishing, and call lifecycle retention for the AI Receptionist system.

The backend is organized as:

```txt
cmd/api              Fiber HTTP server
cmd/worker           POS sync job and call retention worker entrypoint
internal/config      environment config
internal/database    PostgreSQL connection bootstrap and startup migrations
internal/encryption  AES-GCM token encryption
internal/middleware  JWT middleware
modules/auth         login, refresh, roles
modules/salon        salon profile, settings, synced business hour periods
modules/pos          provider-neutral POS contracts and persistence
modules/pos_square   Square adapter and Square integration routes
modules/public_catalog public-safe salon catalog read API
modules/booking      booking attempts, appointments, and fallback pending safety
modules/customer     canonical customer CRUD, activity read model, and POS lookup facade
modules/integration_config encrypted salon-scoped provider app credentials and runtime settings
modules/config_transfer safe salon configuration export/import previews and applies
modules/conversation deterministic simulator sessions, transcripts, summaries, and handoffs
modules/training     salon-authored knowledge base and owner corrections
modules/voice        provider-neutral live voice runtime, status, routing, and webhook event audit
modules/voice_openai OpenAI STT, guarded LLM reply, TTS, and Realtime adapters
modules/voice_twilio Twilio signature verification, form parsing, TwiML responses, and Media Streams bridge
```

The frontend is organized as:

```txt
app/                 Next.js routes
components/ui        reusable UI primitives
components/layout    dashboard shell
features/auth        login flow
features/configuration-transfer export/import preview helpers and onboarding import UI
features/dashboard   dashboard home, appointments, calls, customers, services/staff controls, settings, billing gate, AI training
features/integrations Square integration page
features/onboarding salon profile creation
lib/api              typed API client
types                API response types
```

The public customer-facing web surface lives in `landing/`, separate from the
owner admin dashboard in `frontend/`. The landing app reads only public-safe
catalog data through unauthenticated `/api/public/*` endpoints and does not
create booking attempts or confirmed appointments.

## Core Boundary

ManleAI is the system of record for salon operational data: canonical services,
staff, customers, AI controls, owner workflow state, fallback pending requests,
logs, and training data. POS providers are external projections and booking
execution layers. The active POS provider owns real availability and provider
booking execution.

For the Square pilot, operating hours are owned in Square Appointments. ManleAI
imports the selected Square location's business hour periods into
`salon_business_hour_periods` and uses them as a local safety filter before
offering availability slots. The local dashboard displays those periods and can
trigger sync, but it does not edit the source hours.

Provider IDs are mappings, not primary product identity. Square Appointments is
the first real POS integration; future POS names are architecture targets until
implemented. `salons.active_pos_provider` records the provider that booking,
availability, service/staff management, and phone readiness should evaluate.
Current `services` and `staff` rows still carry legacy provider fields such as
`pos_provider`, `pos_service_id`, and `pos_staff_id`. Provider links in
`pos_entity_links` are the durable mapping model; old provider links are kept
for history and compatibility while preserving the POS-first booking boundary.

Local service/staff/customer writes may be projected to a POS provider only
through capability-gated `pos_sync_jobs`. Unsupported provider writes remain
local and must not be marked as synced. The worker processes supported jobs,
updates `pos_entity_links`, records `pos_sync_logs`, and logs provider failures
to `pos_errors`.

Provider switching is intentionally gated. The Integrations dashboard exposes
provider switch readiness for the active provider, a gated import wizard shell,
and persisted switch runs with service/staff/customer match summaries and owner
match review decisions. It also exposes a dry-run readiness checklist for
switch runs, but the import wizard and dry-run checklist remain blocked until a
real alternate adapter and executable import/dry-run path exist. The pilot
cannot activate an alternate POS provider until a real adapter exists, records
are imported and matched, conflicts are resolved, a dry-run booking readiness
check passes, and required mappings are ready.

Correct dependency direction:

```txt
HTTP handler -> service -> repository/provider interface -> concrete adapter
```

The booking service depends on `modules/pos.POSProvider`. It must not import `modules/pos_square`.

Booking workflow state belongs to the backend. Create-booking, reschedule, cancel, and dashboard test-booking requests first create a `booking_attempts` row with `pos_pending` and a backend-owned POS idempotency key. The POS adapter is then called as an outbound writer. For customer identity, booking resolves or creates the ManleAI canonical customer, reuses an active `pos_entity_links` customer mapping when present, or asks the active `POSProvider` to search/create a provider customer and then stores the mapping. If customer lookup/linking or appointment creation fails, the same attempt is finalized as `fallback_pending`, a POS error and owner notification are recorded, and no confirmed appointment is created. If the provider returns a POS booking ID and booking version, the same attempt is finalized as confirmed/rescheduled/cancelled and the appointment state is written in the backend database. Reschedule, cancel, and test-booking cleanup requests must leave the internal appointment unchanged unless the provider succeeds.

The Milestone 4 conversation simulator and Milestone 5 live phone webhook path call the booking service through a provider-neutral booking tool. They do not import Square packages, read POS tokens, build Square payloads, or use Square location IDs directly. The runtime checks provider-neutral availability, offers available slots to the caller, stores offered slot segment assignments on the call session, and only calls booking creation after the caller selects a slot and required customer details are present. Selected segments and `staff_selection_mode=anyone` survive later turns so simulator and phone bookings can create the same provider-neutral multi-service request while avoiding customer-facing named-technician wording unless the customer chose a specific technician. Booking confirmations remain impossible unless the booking service returns a POS-confirmed booking attempt and appointment. If AI booking is disabled, a customer requests a human, or the booking path cannot confirm through POS, the runtime creates a handoff or fallback pending flow and avoids confirmed wording.

The live voice layer is split into `modules/voice`, `modules/voice_twilio`, and provider-specific AI adapter modules such as `modules/voice_openai`. `modules/voice` owns provider-neutral DTOs, runtime interfaces for telephony/STT/LLM/TTS/realtime providers, salon phone routing, dashboard readiness, short-lived TTS audio output persistence, and webhook event audit writes. `modules/voice_twilio` owns Twilio-specific request signature validation, webhook form mapping, recording fetches, TwiML responses, and Twilio Media Streams WebSocket framing. `modules/voice_openai` owns OpenAI API URLs, payloads, response parsing, model configuration, and Realtime WebSocket sessions. Twilio speech gathering, recording mode, realtime stream transport, TwiML speech output, and audio playback are treated as provider adapter behavior, not conversation-engine logic. Completed realtime transcripts still enter the same conversation engine and booking service; OpenAI Realtime does not call Square or confirm bookings on its own.

Provider app credentials and runtime settings for Square Appointments, Twilio,
and OpenAI are salon-scoped operational configuration stored through
`modules/integration_config`. Secrets are encrypted with the same AES-GCM token
cipher used for POS tokens, never returned to the frontend, and resolved by
adapters at call time. Environment variables remain bootstrap and local
fallback configuration for infrastructure, JWT, CORS, encryption, and legacy
developer setup; dashboard-saved provider configuration takes precedence for a
salon.

The Milestone 7 training layer stores owner-authored salon knowledge and corrections as salon-scoped data. Conversation runtime may read active knowledge as advisory context for FAQ and policy answers, transcript-linked corrections can be reviewed into reusable knowledge, and training evaluation previews can test active knowledge without creating call sessions or bookings. Knowledge never replaces the booking service or POS confirmation checks.

Call sessions are operational records, not an unbounded owner-facing inbox. The Calls dashboard defaults to active sessions and supports archived and redacted lifecycle filters. Active lifecycle sessions carry a 90-day retention timestamp; the worker redacts expired sessions by clearing customer PII, transcript bodies, handoff summaries, webhook payloads, and temporary voice audio while preserving booking, handoff, provider call, outcome, and timestamp audit links. Manual redaction is irreversible from the dashboard and is blocked while a session is active.

Public salon catalog pages are owner-published by slug. They expose only
bookable active-provider services/staff and salon contact details needed for a
customer to call for an appointment. They never expose staff contact details,
POS IDs, provider tokens, sync errors, or owner identifiers, and they must not
present a web booking as confirmed.

Configuration transfer exports sanitized setup data only. Import previews and
applies use stable request IDs, skip secrets and operational records, and must
not recreate services, staff, customers, appointments, POS tokens, call
sessions, transcripts, or provider-side state.

## Next Milestone

The next slices should deepen owner approval loops and production readiness without changing the POS-first confirmation boundary.

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
