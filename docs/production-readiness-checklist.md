# Production Readiness Checklist

## Milestone 1 and 2 Checklist

- [x] Monorepo structure
- [x] Docker Compose for PostgreSQL, Redis, API, frontend
- [x] Standalone POS Calendar app with local port `3091` and production domain wiring for `pos.knasoftware.com`
- [x] Go/Fiber API
- [x] Startup foundation migration
- [x] Ent schema mirror
- [x] Auth module
- [x] Salon module
- [x] POSProvider interface
- [x] Square adapter package
- [x] Square OAuth URL and callback
- [x] Encrypted POS token storage
- [x] POS connection, sync log, and error tables
- [x] Admin login
- [x] Dashboard shell
- [x] Salon profile creation UI
- [x] Square integration page

## Before Milestone 3 Review

- [x] Add signed/persisted OAuth state nonce.
- [ ] Confirm Square Appointments API payload details against a real sandbox account.
- [x] Add integration tests with mocked Square HTTP responses.
- [x] Add service/staff list endpoints for dashboard tables.

## Milestone 3 Next Steps

- [x] Add booking_attempts and appointment tables.
- [x] Add booking service that depends only on `pos.POSProvider`.
- [x] Implement customer search/create in SquareAdapter.
- [x] Implement availability checks in SquareAdapter.
- [x] Implement create appointment in SquareAdapter.
- [x] Implement reschedule/cancel appointment in SquareAdapter.
- [x] Add booking test UI and AI booking enablement gate.
- [x] Add owner appointments dashboard with confirmed POS bookings and fallback pending requests.
- [x] Add POS calendar range/sync APIs and standalone calendar view with day/week/month/agenda modes.
- [x] Add service/staff AI booking controls for synced Square records.
- [x] Add Customers dashboard with internal customer activity and gated Square phone lookup.

## Milestone 4 Conversation Simulator

- [x] Add call session, transcript, and handoff persistence.
- [x] Add deterministic conversation engine module that does not import Square packages.
- [x] Route simulator booking attempts through the provider-neutral booking service.
- [x] Keep AI-disabled and human-request paths as owner handoffs, not confirmed appointments.
- [x] Add dashboard Calls page with simulator transcript, detected details, outcomes, and recent sessions.
- [x] Add backend tests for confirmed, fallback pending, AI-disabled, and handoff paths.

## Milestone 5 Live Voice Webhooks

- [x] Add `phone` call session channel and provider call metadata.
- [x] Add voice webhook event audit persistence.
- [x] Add provider-neutral voice runtime interfaces for telephony, STT, LLM, and TTS adapters.
- [x] Add Twilio incoming and speech-turn webhooks with `X-Twilio-Signature` verification.
- [x] Route Twilio phone sessions through the same conversation engine and booking service.
- [x] Keep phone booking confirmations impossible unless POS booking succeeds.
- [x] Add Calls dashboard live phone readiness, channel badges, and phone/simulator metrics.

## Milestone 6 External AI Voice Providers

- [x] Add provider adapters for external STT, LLM, and TTS behind `modules/voice` interfaces.
- [x] Keep Twilio-specific webhook validation and TwiML response logic outside the conversation engine.
- [x] Add AI reply guardrails for one-question-at-a-time turns, low confidence, human requests, complaints, refunds, payment disputes, and complex group bookings.
- [x] Add salon-scoped AI receptionist tone presets that flow from Settings through phone/LLM reply generation without changing booking guardrails.
- [x] Keep confirmed wording impossible unless `booking.Service` returns a POS-confirmed booking attempt, POS booking ID, and appointment.
- [x] Add fallback and owner-handoff behavior when STT, LLM, TTS, or POS calls fail.
- [x] Add Calls dashboard readiness for external STT, LLM, and TTS provider configuration.
- [x] Keep knowledge base CRUD, owner corrections, and training workflows out of M6; those remain Milestone 7 scope.

## Milestone 7A Knowledge And Owner Corrections

- [x] Add salon-scoped knowledge item persistence for FAQ, policy, services, hours, handoff, and operations notes.
- [x] Add owner correction persistence with pending, applied, and dismissed statuses.
- [x] Add owner-scoped knowledge and correction API routes.
- [x] Feed active knowledge into conversation reply generation as advisory context only.
- [x] Keep appointment confirmation impossible unless the booking service returns POS-confirmed booking state.
- [x] Replace the AI Training placeholder with an operational dashboard for knowledge CRUD and corrections.

## Milestone 7B Transcript Review Corrections

- [x] Add correction capture actions to call transcript messages.
- [x] Persist transcript-linked corrections with call session and transcript message IDs.
- [x] Require transcript-linked corrections to include a call session source.
- [x] Add review-before-apply flow so owners can edit corrections into reusable knowledge.
- [x] Show correction source context in AI Training.

## Milestone 7C Training Evaluation Preview

- [x] Add read-only training evaluation endpoint for active knowledge previews.
- [x] Keep training evaluation from creating call sessions, transcripts, bookings, or POS calls.
- [x] Return matched knowledge, preview reply, outcome, booking action, and POS confirmation boundary.
- [x] Add AI Training dashboard panel for testing sample customer questions.
- [x] Keep unsafe confirmation wording guarded by Square Appointments confirmation requirements.

## Production Service Understanding

- [x] Interpret service utterances against the active salon catalog instead of hardcoded salon-specific keyword patches.
- [x] Add owner-reviewable service categories and category aliases for menu-level service understanding.
- [x] Keep category and category-alias matches as clarification candidates, not directly bookable services.
- [x] Add idempotent system taxonomy refresh for common nail salon groups without overriding owner-reviewed service assignments.
- [x] Treat generic and fuzzy service-family matches as clarification candidates, not selected services.
- [x] Persist salon-scoped service aliases from owner corrections with stable `(salon_id, normalized_alias)` upsert semantics.
- [x] Prevent one normalized phrase from being both an active service alias and an active category alias.
- [x] Feed active service aliases into simulator and phone conversation runtime.
- [x] Feed active service categories and category aliases into simulator and phone conversation runtime.
- [x] Re-check availability when a caller confirms a service correction after a time or slot was already collected.
- [x] Record service-understanding status, reason, confidence, candidates, selected service, alias source, alias ID, category ID, and category name in transcript metadata.

## Production Answer Routing

- [x] Load service, alias, category, staff, knowledge, and business-hour answer context once per salon turn window, but validate a database-owned provider/location/generation/readiness fence on every turn and double-read it around cache misses.
- [x] Answer service/menu questions from active-provider, POS-linked, synced, AI-bookable services only.
- [x] Answer open/close questions from imported business hour periods before owner-authored knowledge.
- [x] Answer staff questions from active-provider staff records without exposing inactive or non-bookable staff as confirmed booking options.
- [x] Route incomplete availability questions into booking-detail prompts instead of guessing availability or creating bookings.
- [x] Use owner-authored knowledge only after structured service, hours, staff, and availability sources do not answer the request.
- [x] Record answer source, reason, confidence, source record IDs, router intent, and answer-context cache state in transcript metadata.
- [x] Fail structured answer context closed during provider switching/syncing so no replica serves stale service, alias, staff, or business-hour data from a local cache.

## Product-Grade Semantic Turns And Final Review

- [x] Interpret add, replace, remove, undo, current-draft summary, and final-review authorization as typed conversation acts instead of scattered phrase-owned mutations.
- [x] Keep replacement source, replacement target, mutation scope, and guest scope separate and validate all service IDs against the active bookable catalog.
- [x] Persist versioned `dialog_state` with pending clarification, reversible last mutation, bounded no-progress recovery, and final-review state.
- [x] Allow explicit out-of-set catalog targets to supersede stale pending candidates while retaining pending context across informational detours.
- [x] Distinguish current-draft service counts from salon catalog counts, including common non-native English wording.
- [x] Clarify whether an added same-category service is for the caller or another guest before mutating the draft when guest scope is missing.
- [x] Require explicit final-review authorization before production call-session booking writes; review acceptance itself is never confirmed wording.
- [x] Add a strict structured semantic-turn interpreter that is data-minimized, catalog-validated, confidence-gated, and unable to call POS or mutate state directly.
- [x] Make validated field-level consultation mutations the sole persistence authority for need fields; free-standing semantic snapshots cannot overwrite scalar or list state.
- [x] Add golden transcript and invariant tests for directional switching, stale pending escape, review corrections, duplicate-safe turn handling, and bounded clarification handoff.
- [x] Route every configured-production freeform orchestration turn through the state-driven Turn Kernel; use deterministic fast/answer/action/recovery lanes when coverage is complete and a multi-act semantic lane for correction, ambiguity, or partial coverage without a keyword-only gate.
- [x] Bound semantic interpretation to 2.5 seconds, scope service/staff context to the active turn, and expose PII-free route, expected-input, context-size, and interpreter-outcome diagnostics.
- [x] Validate the OpenAI structured-turn schema recursively before dispatch, exclude unsupported schema keywords, expose only bounded provider type/code/parameter/request/fingerprint diagnostics, and open a salon/config/schema-scoped circuit after nonretryable contract rejection.
- [x] Add an authenticated owner-scoped semantic-contract probe that uses the live schema/model without caller PII, conversation writes, availability, or POS tools; a successful probe closes the matching local contract circuit.
- [x] Treat initial catalog service/category evidence as booking field collection, reject add-or-replace operation pending state without a selected service, and continue from independently validated captured fields when semantic interpretation is not accepted.
- [x] Route service, staff, date/time, customer, and guest corrections through one validated reducer boundary with dependency invalidation.
- [x] Bind final-review authorization to the exact draft revision and invalidate it after every draft correction.
- [x] Restrict deterministic review acceptance to exact bounded state confirmations; route freeform acceptance/correction/question turns through semantic interpretation, reduce corrections before answering, and resume a fresh review for the new draft revision.
- [x] Persist customer-name confirmation as typed pending state, treat transcript metadata as audit/legacy-read evidence only, reject arbitrary replacement and dynamic salon/service/staff identity collisions, and derive unnamed staff alternatives from the current catalog.
- [x] Keep a bounded mutation history for repeated undo without storing control state only in transcript metadata.
- [x] Preserve the draft and clarify or hand off when semantic interpretation is disabled, unavailable, low-confidence, malformed, or contains invented catalog IDs.
- [x] Keep completed party-plan service corrections out of generic single-draft fallback; collect target, guest/group, operation, and replacement source as typed pending state, resolve short replies without another model call, mutate only the selected group, prevent duplicate adds, block booking while unresolved, and invalidate slots/review only after resolution.
- [x] Serialize each production conversation session before its first state read and retain the lock across availability/POS side effects and the state/transcript commit; keep event-key dedupe and `state_revision` compare-and-swap as defense in depth.
- [x] Persist provider event keys on both customer and AI transcript rows so an older-event retry returns its exact historical AI reply without mutating newer session state or repeating booking side effects.

## Party Booking

- [x] Detect supported group and party booking requests and convert clear party size/service counts into catalog-backed booking segments.
- [x] Call provider-neutral availability and booking for supported party bookings, with confirmation allowed only after POS success.
- [x] Preserve fallback-pending wording when POS booking fails and avoid confirmed appointment wording.
- [x] Persist structured `party_booking_requests` with stable salon/session/event keys for owner-review exception records.
- [x] Add owner status transitions for pending, contacted, resolved, and dismissed party requests.
- [x] Redact party request representative PII and guest-service notes during call lifecycle redaction.

## Canonical POS Ownership And Provider Switch Gates

- [x] Add durable `pos_entity_links` and sync status fields for canonical services, staff, and customers.
- [x] Add local create/update/archive flows for services, staff, and customers while keeping local-only or unmapped records out of booking.
- [x] Add capability-gated `pos_sync_jobs` with worker processing, sync logs, POS errors, and duplicate open-job prevention.
- [x] Add `salons.active_pos_provider` as the active booking/readiness source of truth.
- [x] Add provider switch readiness, switch run persistence, service/staff/customer match review, dry-run readiness, and gated import wizard shell.
- [x] Keep provider switch import, dry-run, and activation disabled while Square Appointments is the only native POS adapter.
- [ ] Enable executable alternate-provider import, dry-run, and activation only after a real alternate adapter exists.

## Booking Integrity And Square Calendar Recovery

- [x] Persist booking operation fingerprints, provider idempotency keys, processing leases, provider outcomes, retry policies, retry lineage, and atomic supersession.
- [x] Auto-supersede historical duplicate fingerprints only when provider dispatch is proven not started; fail migration closed when multiple dispatched/unknown outcomes require POS reconciliation.
- [x] Recover exact authoritative calendar outcomes before lease fallback; otherwise treat expired pre-dispatch (`not_started`) leases as definitive retry-safe fallbacks, keep expired `in_flight` writes unknown/reconciliation-required, and dedupe repeated worker/read recovery outputs.
- [x] Schedule lease recovery independently from long-running Square webhook and calendar-repair batches while preventing overlap within each recurring worker job.
- [x] Require single-use, location/generation-fenced, exact ordered-segment availability quotes for every create/reschedule path; refresh conversation and all party-child proofs before the first applicable POS write and return explicit required/stale conflicts.
- [x] Bound availability-quote retention work with a five-minute cleanup cadence, at most eight batches of 250 quotes per run, a 24-hour post-expiry grace, 30-day orphan-consumed audit retention, and booking-attempt reference preservation.
- [x] Keep unknown and provider-pending outcomes unconfirmed, create tenant-scoped reconciliation tasks, and revalidate exact provider-synced candidates under lock before attachment.
- [x] Capture the pre-write provider version for reschedule/cancel and require a newer exact provider mirror before resolving the mutation as applied.
- [x] Bind reschedule/cancel to the target booking's immutable originating provider location, pass a current exact fence through cancellation, and fail location switches or legacy missing origins before provider dispatch.
- [x] Validate direct reschedule/cancel response identity and version, serialize persistence with calendar import, and never overwrite a newer stored provider version.
- [x] Converge direct success, fallback, and lease recovery with exact authoritative calendar mirrors under one advisory-first lock order; require full canonical/raw segment proof and preserve newer provider versions.
- [x] Allow equal-version calendar re-import to enrich only missing customer/service/staff mappings after exact locked status/time/version/raw-segment equality; conflicting snapshots cannot enrich mappings or resolve an action.
- [x] Serialize reconciliation resolution with calendar mirror imports per salon before row locking to prevent phantom `not_created` decisions and lock-order deadlocks.
- [x] Preserve operation keys only for identical logical intent (excluding refreshed ephemeral quote proof); lock safe-retry fields and require the original time/assignment to remain available.
- [x] Require non-empty operation keys at HTTP and service boundaries, and recover semantic create/reschedule/cancel replays before current catalog or mutated-target validation; refreshed ephemeral quote proof does not change replay identity, so successful response-loss retries never dispatch a second POS write.
- [x] Keep snapshot generation out of the logical operation fingerprint, require a fresh current-generation quote for a new dispatch, and allow retry only when stored location, ordered raw provider mappings/versions, and appointment target baseline still match.
- [x] Import only Square service variations available for booking with positive duration and staff present in both the selected-location active Team and bookable Booking Profiles; apply snapshots atomically without re-enabling owner-disabled AI eligibility.
- [x] Fence full provider snapshots by selected location and monotonic generation so location switches and out-of-order concurrent syncs cannot overwrite newer imported truth.
- [x] Fence catalog resolution, schedule, availability, quote persistence/consumption, booking attempt, and provider dispatch with the same selected location and snapshot generation; force one trusted Square resync when V43 first introduces the fence.
- [x] Fence range-scoped calendar pagination and mirror persistence with one captured provider location/generation, reject cross-location provider rows, and make a stale fence a zero-write failure.
- [x] Clear completed-sync freshness when a snapshot begins or fails, and restore `last_sync_at` only after an active successful generation completes.
- [x] Verify Square webhook signatures against dashboard-stored salon configuration, reject conflicting root/nested location IDs, dedupe event IDs, claim-token fence event and repair completion, and prevent stale provider versions from overwriting newer calendar truth.
- [x] Keep scheduled calendar repair health separate from OAuth/catalog connection readiness.
- [ ] Add a production external delivery consumer and operations policy for durable owner-notification outbox rows.
- [ ] Add authenticated webhook backlog/dead-letter/replay and recent-delivery/repair health observability before claiming end-to-end webhook operations readiness.
- [ ] Fail runtime Square/Twilio/OpenAI configuration resolution closed on repository or decryption errors; permit legacy bootstrap fallback only for an exact missing stored salon configuration.

## Public Catalog And Landing App

- [x] Add owner-managed public catalog settings and publish readiness.
- [x] Add unauthenticated public-safe catalog APIs for first published salon and slug lookup.
- [x] Add separate `landing/` Next.js app for public salon pages.
- [x] Keep public pages call-to-book only; no web booking attempt is presented as confirmed.
- [x] Avoid exposing staff contact details, POS IDs, sync errors, owner identifiers, or provider tokens.

## Configuration Transfer

- [x] Add sanitized owner-scoped configuration export with stable schema version.
- [x] Exclude services, staff, customers, appointments, call sessions, transcripts, POS OAuth tokens, API keys, client secrets, encrypted secrets, and operational records.
- [x] Include service category taxonomy, service category aliases, and service aliases in schema v5, add the salon consultation runtime toggle in schema v6, then add portable service consultation profiles and scoped `included_sections` data packs in schema v7 while continuing to exclude service records and provider state.
- [x] Resolve v7 consultation profiles only against existing target services by normalized name plus duration; block missing, ambiguous, or ineligible ready targets without creating services or POS mappings.
- [x] Keep consultation profile import idempotent by `(salon_id, service_id)` and leave profile revision unchanged when imported data is identical.
- [x] Use stable category slugs and normalized alias keys so repeated imports update existing taxonomy instead of duplicating it.
- [x] Resolve imported service aliases only against existing target-salon services and skip unresolved alias targets without creating services.
- [x] Add import preview and apply flows for existing salons and onboarding.
- [x] Use request IDs for repeated import applies so retries do not create duplicate import runs.
- [x] Gate live `ai_enabled`, confirmed booking mode, and public catalog publishing until Square Appointments readiness passes on the target salon.

## Call Lifecycle Retention And Realtime Streams

- [x] Add call session lifecycle filters for active, archived, and redacted sessions.
- [x] Add idempotent archive and irreversible redaction actions with active-session conflict gates.
- [x] Add worker-driven 90-day retention redaction for expired active sessions.
- [x] Clear customer PII, transcript bodies, handoff summaries, webhook payloads, and temporary voice audio while preserving booking/handoff/provider-call audit links.
- [x] Add Twilio Media Streams and OpenAI Realtime adapter path with completed transcripts routed back through the same conversation engine and booking service.
- [x] Correlate realtime response creation, audio, cancellation, and completion by application request ID and provider response ID; serialize replies through a bounded FIFO and reject stale audio.
- [x] Fail closed on missing GA transcription confidence, apply profile-aware mean/low-tail/VAD-coherence admission, and keep rejected noise out of conversation state.
- [x] Retain the legacy buffered-Realtime rollback path that verifies canonical operational facts in the completed audio transcript before release.
- [x] Add dashboard-selectable low-latency streaming TTS that keeps Realtime input-only, converts raw provider PCM 24 kHz through a stateful anti-aliasing resampler to Twilio PCMU 8 kHz, sends a bounded 200 ms startup block, then drains a bounded backpressure queue at one 160-byte frame per 20 ms, flushes short replies safely, and retains buffered Realtime as a legacy rollback mode.
- [x] Cancel streaming speech on barge-in, clear Twilio playback, reject stale generations, and close terminal replies only after a Twilio playback mark or timeout.
- [x] Expose owner-scoped, PII-free realtime admission and output-validation diagnostics in the Calls timeline without transcript or audio bodies.
- [x] Keep operational facts deterministic and allow guarded LLM rewriting only for explicitly style-only replies.
- [x] Persist structured service consultation profiles with stable `(salon_id, service_id)` ownership, controlled values, owner-approved copy, no-op identical retries, revision increments only when data changes, and a fail-closed `ready` contract requiring both a recommended outcome and compatible current system.
- [x] Default salon consultation to disabled, reject enablement without at least one fully eligible profile, and rank only active-provider, POS-linked, AI-bookable services whose consultation profile satisfies the complete `ready` contract; record recommendation reasons and profile revisions for audit.
- [x] Keep semantic consultation output extraction-only and unable to recommend services, mutate booking state, or call availability/POS tools; validate field-level set/replace/add/remove/clear semantics before the backend reducer applies them.
- [x] Include complete ready consultation profiles and revisions in semantic context; derive the next unresolved discriminating field/options from profiles and state, then use guarded generation only to phrase one question without a fixed field-prompt map.
- [x] Persist active consultation needs, candidates, recommendations, selection, resume phase, no-progress count, and exit reason in versioned `dialog_state`; transcript metadata remains audit evidence.
- [x] Persist consultation question options, provider outcome/diagnostics, separate provider-failure and caller-no-progress counters, and a stable progress fingerprint; retry safely then hand off instead of guessing when question generation fails.
- [x] Require both a concrete service selection and explicit booking intent before transitioning from consultation to booking, and preserve an existing booking draft across consultation detours.
- [x] Support consultation-only completion with `consultation_completed`, bounded unresolved handoff, and global deterministic plus structured safety handoff before any mutation or tool action, without medical advice.
- [x] Keep cancel, reschedule, handoff, and active party-plan actions ahead of consultation routing.
- [x] Expose per-service consultation profile controls in Services, the salon-wide toggle and coverage in Settings, and typed consultation audit state in Calls.
