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
modules/conversation deterministic simulator sessions, service understanding, transcripts, summaries, and handoffs
modules/training     salon-authored knowledge base, owner corrections, and service aliases
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

The POS calendar operator surface lives in `pos-calendar/`, separate from the
owner admin dashboard shell. It uses the same authenticated owner API and admin
session tokens, but does not include the dashboard sidebar. Its first workflow
is `/calendar`, with day/week/month/agenda views, `Today` and `Tomorrow`
shortcuts, active-provider calendar sync, and POS-backed add, edit, and delete
actions. Add still creates a booking attempt through the booking service; edit
currently uses the existing POS-backed reschedule contract for time/staff/notes;
delete uses the existing POS-backed cancel contract and never hard-deletes
appointment history. The page also opens an authenticated calendar event stream
so new customer booking attempts can update the visible range and show a
top-right toast without treating pending POS work as confirmed.

## Core Boundary

ManleAI is the system of record for salon operational data: canonical services,
staff, customers, AI controls, owner workflow state, fallback pending requests,
logs, and training data. POS providers are external projections and booking
execution layers. The active POS provider owns real availability and provider
booking execution.

For the current Square Appointments production release, operating hours are owned in Square Appointments. ManleAI
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
real alternate adapter and executable import/dry-run path exist. The production release
cannot activate an alternate POS provider until a real adapter exists, records
are imported and matched, conflicts are resolved, a dry-run booking readiness
check passes, and required mappings are ready.

Correct dependency direction:

```txt
HTTP handler -> service -> repository/provider interface -> concrete adapter
```

The booking service depends on `modules/pos.POSProvider`. It must not import `modules/pos_square`.

Booking workflow state belongs to the backend. Create-booking, reschedule, cancel, and dashboard test-booking requests first create a `booking_attempts` row with `pos_pending` and a backend-owned POS idempotency key. The POS adapter is then called as an outbound writer. For customer identity, booking resolves or creates the ManleAI canonical customer, reuses an active `pos_entity_links` customer mapping when present, or asks the active `POSProvider` to search/create a provider customer and then stores the mapping. If customer lookup/linking or appointment creation fails, the same attempt is finalized as `fallback_pending`, a POS error and owner notification are recorded, and no confirmed appointment is created. If the provider returns a POS booking ID and booking version, the same attempt is finalized as confirmed/rescheduled/cancelled and the appointment state is written in the backend database. Reschedule, cancel, and test-booking cleanup requests must leave the internal appointment unchanged unless the provider succeeds.

New booking and availability resolution is explicitly scoped to
`salons.active_pos_provider` at both service and repository boundaries.
Historical appointment actions continue through the provider recorded on the
appointment. Provider mutation errors expose definitive versus unknown outcome
and operation phase; untyped or post-dispatch ambiguous errors default to
unknown and block retry until reconciliation. Availability date ranges use the
salon timezone and local calendar-day boundaries rather than UTC midnight.

Calendar range reads are local mirror reads over POS-confirmed appointments,
`pos_pending` booking attempts, and fallback pending booking attempts. Calendar
sync is an active-provider import path: the provider-neutral POS layer may
expose `ListAppointments`, and the
Square adapter maps Square Bookings list results into backend appointment
mirror rows keyed by `(salon_id, pos_provider, pos_appointment_id)`. Imported
appointments carry `pos_sync_status`, `last_pos_synced_at`, and
`pos_sync_error` so the calendar can show warnings on each appointment when a
record is not synced, pending verification, or failed its latest POS sync.

The Milestone 4 conversation simulator and Milestone 5 live phone webhook path call the booking service through a provider-neutral booking tool. They do not import Square packages, read POS tokens, build Square payloads, or use Square location IDs directly. The runtime owns booking slot state, including date-only requests before a time is known, so model wording cannot erase already-collected service, date, time, customer, or staff details. Deterministic date, time, staff, and customer evidence is applied before accepting a model-only summary path, so a semantic summary classification cannot discard a concrete correction such as a new requested day. Mutable session booking segments, offered-slot segments, party plans, and versioned `dialog_state` are deep-cloned per turn. `dialog_state` owns pending typed clarifications, bounded mutation history, no-progress recovery, `draft_revision`, `reviewed_revision`, and `authorized_revision` instead of reconstructing active control state from transcript metadata.

Every configured-production freeform turn that reaches conversation orchestration first enters the state-driven Turn Kernel. The kernel derives the expected input from versioned dialog state, measures deterministic coverage, and assigns one explicit lane: fast, answer, action, recovery, or semantic. Unambiguous state-scoped confirmations, offered-slot choices, structured questions, operational actions, and expected-field evidence avoid a model round trip. An initial catalog-backed service or category request is missing-field collection rather than a service-edit operation: an exact service advances to the next missing field, an ambiguous category asks for one concrete catalog option, and an add-or-replace operation choice is valid only after at least one service is selected. Corrections, multi-intent turns, partial coverage, and unresolved ambiguity use the strict semantic `TurnUnderstanding` contract, which may contain multiple ordered acts and questions. Semantic input includes only the services and staff relevant to the current state unless service discovery or correction requires the active catalog; the call has a bounded 2.5-second timeout. Provider-event dedupe, contextual review authorization, and resolved same-category guest scope remain deterministic state-owned controls that semantic interpretation cannot overwrite. A service correction against a completed `party_plan` cannot fall back to the generic single-draft add-or-replace prompt: typed pending state collects the concrete target, guest/group reference, add-or-replace operation, and replacement source one question at a time. Short follow-up replies use the deterministic fast lane, the party reducer mutates only the selected group, repeated add execution is duplicate-safe, and a party group may retain more service segments than people only after an explicit add operation. Offered slots and review authorization remain unchanged during unresolved clarification and are cleared only after the correction resolves; unresolved party-correction pending state blocks availability and booking even if the semantic interpreter becomes unavailable. Authorization grammar accepts concise natural approval directives but rejects turns containing correction, negation, cancellation, reschedule, or service-mutation evidence. Backend validation rejects low-confidence goals, invented service/staff/category IDs, invalid entity/operation combinations, unsafe party flattening, and malformed counts before the unified reducer runs. Deterministic catalog names, aliases, categories, staff records, date/time parsing, phone normalization, and context-scoped affirmative handling remain authoritative evidence. Bare customer-name candidates collected on the phone require a confirmation turn unless the caller explicitly introduces a non-risky name or spells it. The reducer owns service/staff/date-time/customer/guest mutation and dependency invalidation; the next-action planner owns missing-field, review, and booking readiness decisions. Informational questions preserve pending work and resume the next useful booking question. Any non-accepted semantic outcome preserves the draft and may still consume independently validated catalog and captured-field evidence before the next missing-field prompt; without such evidence the runtime clarifies or hands off instead of guessing.

The runtime checks provider-neutral availability, offers slots, and only calls booking creation after required fields are present and `draft_revision == reviewed_revision == authorized_revision`. Any service, staff, date/time, guest, party, or customer correction advances the draft revision; dependency-bearing changes clear offered slots and all corrections invalidate stale review authorization. Selected segments and `staff_selection_mode=anyone` survive unrelated turns. Booking confirmations remain impossible unless the booking service returns a POS-confirmed booking attempt, appointment, and provider booking ID. If AI booking is disabled, semantic interpretation cannot proceed safely, a customer requests a human, clarification cannot make progress, or POS cannot confirm, the runtime creates a handoff or fallback pending flow and avoids confirmed wording.

The live voice layer is split into `modules/voice`, `modules/voice_twilio`, and provider-specific AI adapter modules such as `modules/voice_openai`. `modules/voice` owns provider-neutral DTOs and runtime interfaces, including whole-response TTS for recording mode and chunked streaming speech for realtime mode. `modules/voice_twilio` owns Twilio request verification, TwiML, Media Streams framing, reply FIFO, PCMU delivery, playback marks, barge-in clear/cancel, and stale-generation rejection. `modules/voice_openai` owns OpenAI payloads, Realtime input sessions, dedicated Speech streaming, incremental WAV parsing, and PCM-to-PCMU conversion.

OpenAI Realtime is the input/VAD/transcription channel. GA input requests transcription log probabilities and applies profile-aware mean, low-tail, and VAD-coherence admission before conversation state can change. The default dashboard-managed `streaming_tts` output mode sends backend-approved text to the dedicated Speech endpoint, converts the result to PCMU 8 kHz incrementally, and holds a bounded 200 ms startup buffer before the first Twilio media message so provider chunk jitter cannot expose an isolated first phoneme. Audio after startup continues streaming before full speech completion and does not wait for `response.done`; short replies flush when the Speech stream completes. Stream completion is lifecycle/audit evidence only. The legacy dashboard option `buffered_realtime` retains response identity binding and complete output-transcript validation before audio release for rollback compatibility. Both modes keep a bounded FIFO, delayed single progress reply, playback guard, terminal fallback, PII-free timing diagnostics, and the same backend conversation/POS path. OpenAI cannot call Square or independently confirm a booking.

Provider app credentials and runtime settings for Square Appointments, Twilio,
and OpenAI are salon-scoped operational configuration stored through
`modules/integration_config`. Secrets are encrypted with the same AES-GCM token
cipher used for POS tokens, never returned to the frontend, and resolved by
adapters at call time. Environment variables remain bootstrap and local
fallback configuration for infrastructure, JWT, CORS, encryption, and legacy
developer setup; dashboard-saved provider configuration takes precedence for a
salon.

The Milestone 7 training layer stores owner-authored salon knowledge, corrections, and service aliases as salon-scoped data. Conversation runtime may read active knowledge as advisory context for FAQ and policy answers, transcript-linked corrections can be reviewed into reusable knowledge or structured service aliases, and training evaluation previews can test active knowledge without creating call sessions or bookings. Knowledge never replaces structured POS-owned answers, the booking service, or POS confirmation checks.

AI receptionist tone is salon-scoped runtime configuration on `salon_settings`. Tone presets guide spoken reply style, but backend conversation guardrails still own slot collection, handoff routing, and POS-first confirmation wording.

Service understanding is a backend domain layer, not prompt-only behavior. The conversation runtime interprets customer service utterances against the active salon service catalog, salon-scoped `service_aliases`, active `service_categories`, and active `service_category_aliases`. Exact catalog service names win over aliases; aliases can select one real service; category names and category aliases create catalog-backed clarification candidates; and fuzzy family matches only produce clarification candidates instead of selecting a service. Catalog ambiguity is authoritative over the structured semantic interpreter: a model-proposed add or replacement cannot narrow a category candidate set to one concrete service unless the caller supplied concrete service evidence. A bare concrete service utterance that would switch an in-progress draft uses the catalog-backed confirmation flow before mutation. Owner corrections can be applied into service aliases with a stable `(salon_id, normalized_alias)` key, while category aliases use their own stable `(salon_id, normalized_alias)` key and conflict with active service aliases so one phrase cannot mean both a single service and a category. Service-understanding decisions are written to transcript metadata with status, reason, confidence, candidates, selected service, alias source, alias ID, category ID, and category name so call reviews can explain why a service was selected or why the AI asked for clarification.

Conversation answers use an `AIAnswerContext` loaded from the active provider scope and cached briefly per salon. The answer router chooses structured sources before owner-authored knowledge: active-provider POS-linked AI-bookable services for service/menu answers, imported `salon_business_hour_periods` for open/close questions, active-provider staff records for staff questions, booking availability prompts for incomplete availability questions, and active knowledge only after those structured sources do not answer the request. Transcript metadata records `answer_source`, `answer_source_reason`, `answer_source_confidence`, `router_intent`, source record IDs when available, and whether the context came from cache. This keeps answers explainable while preventing stale knowledge from overriding synced services, staff, business hours, or POS-first booking rules.

Service consultation is part of the conversation runtime, not free-text knowledge. It may compare only active-provider, POS-linked, synced, AI-bookable services and may state only structured name/category/duration/price plus the owner-approved `services.ai_description` (falling back to `description`). Candidate IDs are persisted in transcript metadata and cleared explicitly when the caller selects a concrete catalog service or the workflow leaves consultation. A bare affirmative after multiple options does not select a service; a catalog-selected reply while the session is in consultation enters the existing booking/service-edit state machine, which invalidates stale slots and rechecks availability. Cancel, reschedule, human handoff, and active party-plan actions take precedence over consultation. Pain, injury, infection, allergy, or other health-suitability questions create an owner handoff without medical advice or booking confirmation.

Supported group and party bookings use the same POS-first confirmation boundary as single-customer bookings. When the conversation can resolve party size and guest service counts into catalog-backed booking segments, it calls provider-neutral availability, offers bookable slots, and calls the booking service only after the caller selects a slot and required customer details are collected. The AI may confirm only after the active POS provider returns a successful booking ID. Ambiguous service families ask for catalog-backed clarification before availability, and POS failure returns fallback-pending wording instead of confirmed appointment wording. `party_booking_requests` remain an owner-review exception queue for records that cannot be safely completed through the POS-first flow.

Call sessions are operational records, not an unbounded owner-facing inbox. The Calls dashboard defaults to active sessions and supports archived and redacted lifecycle filters. Active lifecycle sessions carry a 90-day retention timestamp; the worker redacts expired sessions by clearing customer PII, transcript bodies, handoff summaries, party request representative details, webhook payloads, and temporary voice audio while preserving booking, handoff, provider call, outcome, and timestamp audit links. Manual redaction is irreversible from the dashboard and is blocked while a session is active.

Public salon catalog pages are owner-published by slug. They expose only
bookable active-provider services/staff and salon contact details needed for a
customer to call for an appointment. They never expose staff contact details,
POS IDs, provider tokens, sync errors, or owner identifiers, and they must not
present a web booking as confirmed.

Configuration transfer exports sanitized setup data only, including salon
profile, AI settings, public catalog settings, integration runtime settings,
service category taxonomy, service category aliases, service aliases that can
be matched to existing target services, and owner-authored knowledge. Import
previews and applies use stable request IDs, skip secrets and operational
records, and must not recreate services, staff, customers, appointments, POS
tokens, call sessions, transcripts, provider connections, synced business hour
periods, provider switch runs, party booking requests, voice webhook events, or
provider-side state. Service category imports use stable slug and
normalized-alias keys and reject category aliases that conflict with active
service aliases. Service alias imports use stable normalized-alias keys and
skip aliases whose target service cannot be resolved on the target salon.

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
