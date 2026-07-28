# Test Plan

## Milestone 1

- Verify API startup migrations create the local PostgreSQL schema.
- Run `make restart`; verify it migrates and applies the explicit `sample_test`
  profile automatically, then run it again and verify the volume is preserved
  with `sample_migration_replayed=true`.
- Verify the profile creates one Lotus tenant, seven services, four staff, 28
  eligibility links, one Platform Admin, one exact-salon Platform Ops with
  seven non-PII capabilities, one tenant owner, zero PII grants, and zero
  provider configs.
- Verify normal migrations alone leave `users=0`, `salons=0`, do not create
  `sample_data_migrations`, and leave owner bootstrap available.
- Verify deploy preflight accepts an exact migration ledger and returns the
  dedicated incompatible status for any persisted name/checksum mismatch.
- Verify only `sample_test` plus an exact release-tag reset approval can replace
  an incompatible pre-live PostgreSQL volume after backup; `live` fails before
  deletion.
- Verify the post-migration `live` guard rejects sample users, salons, or the
  sample fixture ledger, while a clean normal-migration database passes.
- Verify login succeeds and returns access and refresh tokens.
- Verify `GET /api/auth/me` returns the owner and primary salon ID.
- Verify owner can create, read, and update salons.
- Verify owner can read salon settings and Square-synced business hour periods.
- Verify local business hour updates return `BUSINESS_HOURS_POS_MANAGED`.
- Verify frontend login stores tokens and opens the dashboard.
- Verify empty dashboard state links to salon onboarding.

## Milestone 2

- Verify `GET /api/platform/tenants/:tenant_id/technical/square/connect-url` returns a Square OAuth URL only for an authorized Platform Technical actor, and still supports env fallback only for local bootstrap.
- Verify Square callback stores encrypted tokens in `pos_connections`.
- Verify token values are not returned by status endpoints.
- Verify `GET /api/platform/tenants/:tenant_id/technical/square/status` returns connection state and sync logs without owner impersonation.
- Verify `GET /api/platform/tenants/:tenant_id/technical/square/locations` calls Square through `SquareAdapter`.
- Verify `POST /api/platform/tenants/:tenant_id/technical/square/select-location` stores `location_id` and consumes the tenant provider-write quota.
- Verify `POST /api/platform/tenants/:tenant_id/technical/square/sync` writes normalized `services`, `staff`, business hour periods, and customers.
- Verify Square errors create `pos_errors` rows and failed sync logs.

## Milestone 3 Backend Foundation

- Verify Square OAuth state is signed, persisted, expires, and cannot be replayed.
- Verify Square API calls send the configured `Square-Version` header.
- Verify synced services keep POS service version metadata for future booking payloads.
- Verify `GET /api/salons/:id/services` and `GET /api/salons/:id/staff` return only owner-scoped synced records.
- Verify booking service tests only use `pos.POSProvider`, not `pos_square`.
- Verify booking service creates a backend `pos_pending` attempt and backend-owned POS idempotency key before outbound POS booking writes.
- Verify POS success stores a confirmed appointment and POS booking ID.
- Verify POS failure stores `fallback_pending`, logs `pos_errors`, creates an owner notification, and does not create a confirmed appointment.
- Verify Square customer search/create payloads use Customers API fields and preserve customer IDs.
- Verify Square availability payloads include location, service variation, optional team member filter, and parse available slots.
- Verify Square create-booking payloads include location, start time, team member, service variation ID, service variation version, duration, and idempotency key.
- Verify confirmed appointments persist the POS booking version returned by Square.
- Verify Square booking, reschedule, and cancel payloads use the backend-provided idempotency key rather than generating one inside the adapter.
- Verify Square reschedule payloads include booking version, location, start time, team member, service variation ID, service variation version, duration, and idempotency key.
- Verify Square cancel payloads include booking version and idempotency key.
- Verify reschedule/cancel service success updates internal appointment state only after the POS provider succeeds.
- Verify reschedule/cancel service failure stores `fallback_pending`, logs `pos_errors`, creates an owner notification, and leaves the existing appointment unchanged.
- Verify Square readiness blocks test booking until Square is connected, a location is selected, services/staff are synced, and at least one service/staff record is AI-bookable.
- Verify Square test booking creates a real booking through `booking.Service` with source `square_test_booking`.
- Verify cancel test booking cancels the latest Square test appointment through `booking.Service`.
- Verify AI booking enablement is not blocked by optional Square test booking create/cancel state once Square connection, location, sync, bookable service/staff, and business hours readiness pass.
- Verify the dashboard Integrations page handles loading, empty, error, success, disabled/gated, and mobile states for Square readiness.
- Verify the dashboard Appointments page distinguishes authority-native internal/external confirmation, owner-review requests, and external `fallback_pending` reconciliation work without using a POS ID as the authority discriminator.
- Verify mixed-origin appointment pagination returns stable `limit`, `offset`, and `has_more` metadata without changing calendar-day or metric data to the current page slice.
- Verify customer record pagination returns stable `limit`, `offset`, and `has_more` metadata without changing customer summary metrics to the current page slice.
- Verify the dashboard Services and Staff pages let owners allow/block AI booking for active synced records without editing Square data.
- Verify inactive synced services/staff cannot be enabled for AI booking and remain unavailable in the dashboard.

## Milestone 4 Conversation Simulator

- Verify startup migrations create `call_sessions`, `call_transcript_messages`, and `handoff_requests`.
- Verify simulator sessions are owner-scoped by `salon_id`.
- Verify the conversation module does not import `modules/pos_square`.
- Verify simulator booking attempts call `booking.Service` with source `ai_conversation_simulator`.
- Verify simulator confirmed wording appears only after the booking service returns a confirmed attempt, POS booking ID, and appointment.
- Verify simulator and phone customer-facing booking replies do not mention Square, POS, provider names, or internal provider details; those stay in tool/audit logs only.
- Verify POS fallback from the booking service produces pending request wording and no confirmed appointment language.
- Verify disabled AI booking creates an owner handoff and does not call the booking service.
- Verify human requests, complaints, refunds, payment disputes, and unsupported or low-confidence group-booking language create owner handoffs.
- Verify the Calls dashboard handles loading, empty, error, success, disabled/gated, and mobile states.
- Verify simulator transcripts show customer, AI, and booking tool messages in sequence.
- Verify initial voice and simulator greetings identify the salon, include recording disclosure when configured/defaulted, and collect intent with an open-ended prompt before asking for a booking service.
- Verify greeting-only or connection-check turns such as "Hello" and "Can you hear me?" acknowledge the caller without replaying the full welcome, forcing booking intent, or calling booking tools when no booking details have been collected.
- Verify booking conversations preserve already-collected service/date/time/customer/staff slots and do not ask for a known day again after date-only turns such as "Thursday this week" or "Thứ Tư này tuần này".
- Verify offered-slot turns can select a unique offered time from spoken responses such as "one p.m." or a "Yes" response to a single-slot confirmation prompt, and do not rerun availability for unclear time fragments while the same offered slots remain valid.
- Verify exact service/date/time requests such as "gel manicure for 1 p.m. this Thursday" parse dotted meridiem, auto-select a matching POS availability slot, name the assigned technician, and ask the next missing customer detail instead of asking the caller to confirm the same time again.
- Verify service understanding uses the active salon catalog and active `service_aliases`, exact catalog service names beat aliases, aliases can select one service, and generic or fuzzy service-family matches ask for catalog-backed clarification instead of selecting a service.
- Verify noisy service utterances such as "Menikur", "Manecu", and mixed-language service family phrases do not book a guessed service; they must either select an exact/alias match or ask clarification with real catalog candidates.
- Verify exact service/date/time requests with multiple available technicians use fair rotation from confirmed/rescheduled salon assignments for that local day, not list order or alphabetic staff names, and record assignment policy metadata for audit.
- Verify customer-requested technicians are honored when available, and when unavailable at the requested time the AI does not auto-switch; it names the unavailable technician, offers same-time another-technician or another-time requested-technician options, and waits for the caller to choose.
- Verify customer-name collection accepts plausible bare names after service/date/time are known, rejects affirmative replies, connection checks, service phrases, active service aliases, and time/date phrases as names, and creates an owner handoff with non-confirmed wording when repeated non-answers or caller goodbye prevent collecting the name.
- Verify when a caller corrects the service after a time or offered slot was already collected, the engine changes the service, clears stale offered slots, re-checks availability, and does not book until customer details are complete.
- Verify supported multi-person requests such as "for four people, two manicures and two pedicures" resolve to catalog-backed party booking segments, call availability and booking only through the provider-neutral booking service, and use confirmed wording only after POS success; ambiguous service families must ask clarification before availability.
- Verify unclear repair turns such as "Sorry?", "Hello?", or partial STT fragments repeat or rephrase the current prompt without clearing the known booking date or calling booking tools.
- Verify voice provider retries with a stable event key do not append duplicate transcript turns or create duplicate booking attempts.

## Milestone 5 Live Voice Webhooks

- Verify startup migrations add `phone` channel support, provider call metadata, and `voice_webhook_events`.
- Verify `GET /api/salons/:id/voice/status` is owner-scoped and never exposes Twilio auth token values.
- Verify Twilio incoming and turn webhooks reject missing or invalid `X-Twilio-Signature` values.
- Verify Twilio incoming webhook matches `To` against the salon phone and creates a `phone` conversation session with provider call metadata.
- Verify Twilio turn webhook appends speech turns to the existing `phone` transcript and returns TwiML.
- Verify no-speech turns reprompt without creating a fake booking.
- Verify phone booking attempts call `booking.Service` with source `ai_voice_call`.
- Verify phone confirmed wording appears only after the booking service returns a confirmed attempt, POS booking ID, and appointment.
- Verify POS fallback from a phone call produces pending request wording and no confirmed appointment language.
- Verify phone human-request and AI-disabled paths create owner handoffs and do not call booking.
- Verify the Calls dashboard shows live phone readiness, channel badges, phone/simulator metrics, and transcript details on desktop and mobile.

## Milestone 6 External AI Voice Providers

- Verify external STT, LLM, and TTS adapters are configured behind `modules/voice` interfaces, not inside the conversation engine.
- Verify voice provider readiness reports STT, LLM, and TTS configuration without exposing API keys or secrets.
- Verify STT failures create safe reprompt, fallback, or owner-handoff behavior instead of fabricating customer intent.
- Verify LLM replies follow conversation rules: ask one question at a time, keep responses short, do not invent prices, and use owner handoff for low confidence or sensitive requests.
- Verify TTS failures do not mark a call or booking as successful and produce a safe fallback path.
- Verify AI-generated booking turns still call `booking.Service` only after required booking details are collected.
- Verify AI-generated confirmed wording appears only after the selected authority returns its exact durable success evidence; owner-manual requests never use confirmed wording.
- Verify POS failure after an AI-generated phone turn produces pending request wording and no confirmed appointment language.
- Verify prompt and model tests cover human requests, complaints, refunds, payment disputes, complex group bookings, missing fields, low confidence, AI-disabled state, and POS fallback.
- Verify AI tone presets change only spoken reply style and do not weaken one-question, known-slot preservation, handoff, or authority-native confirmation guardrails.
- Verify the Calls dashboard handles loading, empty, error, success, and gated states for external AI voice provider readiness.

## Milestone 7A Knowledge And Owner Corrections

- Verify startup migrations create `knowledge_items`, `owner_corrections`, and `service_aliases`.
- Verify knowledge and correction routes are owner-scoped by `salon_id`.
- Verify active knowledge can answer FAQ and policy questions without calling the booking service.
- Verify owner corrections can be captured, applied to active knowledge, and dismissed.
- Verify owner corrections can be applied to a structured service alias when the correction is about service recognition, and repeated applies update one `(salon_id, normalized_alias)` row rather than creating duplicates.
- Verify knowledge context does not allow confirmed appointment wording unless the neutral scheduling boundary returns authority-native durable confirmation evidence.
- Verify the AI Training dashboard handles loading, empty, error, success, disabled/gated, and mobile states.

## Customer Appointment SMS

- Verify policy is disabled by default, owner/version scoped, rejects equal or
  invalid quiet hours, and computes cross-midnight, DST-gap, and DST-overlap
  boundaries in the salon timezone.
- Verify a caller ID alone creates no consent. Ask only on phone after the exact
  final-review authorization; yes records explicit consent, no/unclear/provider
  failure does not block scheduling, and simulator/disabled policy skips the
  prompt. Cover different wording/data across all three scheduling authorities.
- Verify owner attestation requires an unchecked-to-checked explicit action;
  false is invalid. Local yes/owner cannot lift STOP. Signed, policy-ready
  Twilio START can create or transition missing/pending/declined/opted-out
  state; HELP is event-only. Never parse webhook Body or send a second reply.
- Verify callback signatures cover exact URL and all form parameters, and bind
  Account SID plus Messaging Service SID or exact sender To. A correctly signed
  shared-token callback routed to another salon must perform zero mutation.
- Verify request_received commits with the owner-manual request and says it is
  not confirmed. Verify internal/external confirmed, rescheduled, and cancelled
  outbox rows commit with the matching appointment evidence; imported POS
  calendar mirrors do not enqueue. Rollback produces zero delivery rows.
- Verify external authority appointment version zero commits and produces one
  delivery with `source_version=0`; do not invent version one.
- Verify dispatch and requeue revalidate exact consent, policy, destination,
  request/appointment status and source version. Race STOP, policy disable, and
  lifecycle changes against final dispatch: either the dispatch marker wins
  first or the provider is never called; stale copy suppresses.
- Verify safe pre-dispatch retry is bounded, final lease exhaustion dead-letters,
  ambiguous custom provider errors become durable unknown-outcome evidence,
  and unknown outcomes cannot be requeued. Exactly one bounded owner requeue is
  allowed; exact replay succeeds and concurrent/new over-limit actions fail.
- Verify callbacks are monotonic: delivered followed by late sent records audit
  without regressing delivery or attempt state.
- Verify appointment and owner-review request child surfaces cover loading,
  empty, error/retry, consent, opted-out, quiet-hours, accepted, sent-pending,
  delivered, suppressed, failed, redacted, requeueable, blocked, success, and
  responsive mobile states without full destination, body, hash, provider ID,
  or credential exposure.

## Milestone 7B Transcript Review Corrections

- Verify the Calls dashboard can capture a correction from an AI, customer, or tool transcript message.
- Verify transcript-linked correction requests include both `call_session_id` and `transcript_message_id`.
- Verify correction creation rejects a transcript message source without a call session source.
- Verify the AI Training dashboard displays whether a correction came from a call transcript or manual entry.
- Verify `Review apply` pre-fills the knowledge form and only marks the correction applied after the owner saves.
- Verify service-recognition corrections can be reviewed into service aliases without editing POS services and without implying the alias is active until the backend confirms the apply request.

## Milestone 7C Training Evaluation Preview

- Verify `POST /api/salons/:id/training/evaluate` is owner-scoped and requires a message.
- Verify evaluation returns a preview answer for matching active knowledge.
- Verify evaluation returns a no-match fallback when no active knowledge matches.
- Verify unsafe confirmation knowledge returns authority-safe wording without inventing a request, internal commit, or provider confirmation.
- Verify evaluation never creates a call session, transcript, booking attempt, appointment, or POS call.
- Verify the AI Training dashboard handles evaluating, no-match, matched, error, and mobile states.

## Canonical POS Ownership And Provider Switch Gates

- Verify `pos_entity_links` backfills existing Square service/staff mappings and keeps provider IDs as mappings, not primary identity.
- Verify services, staff, and customers can be created, updated, and archived locally with owner-scoped APIs.
- Verify local-only, unmapped, archived, inactive, or sync-failed services/staff cannot be used for availability, booking, or AI bookable enablement.
- Verify repeated service/staff writes create at most one open `pos_sync_jobs` row per salon/provider/entity/operation.
- Verify unsupported provider writes stay gated, record visible sync status, and do not mark local records as synced.
- Verify provider switch readiness reports Square Appointments as the only installed provider and keeps alternate import, dry-run, and activation disabled.
- Verify provider switch run creation for an unavailable target persists a blocked run without creating provider links or activating a provider.
- Verify match review changes are owner-scoped, recompute review state, and never activate a provider.
- Verify dry-run readiness remains false until a real target adapter and executable dry-run path exist.

## Square Webhook Operations

- Verify the Square webhook list, detail, and requeue routes require
  authentication, reject cross-salon access as not found, and never expose
  merchant/location/booking identifiers, raw payloads, signatures, tokens,
  claim tokens, provider responses, customer data, or raw errors.
- Verify list filters accept only `pending`, `processing`, `failed`,
  `dead_letter`, and `succeeded`; the empty filter may return a safe read-only
  `ignored` event, while an explicit `ignored` filter is rejected.
- Verify list pagination returns stable `limit`, `offset`, and `has_more`, while
  backlog/dead-letter/recent-success metrics and calendar-repair health remain
  salon-wide rather than page-sliced.
- Verify `can_requeue` is false for pending, processing, succeeded, ignored,
  nonterminal failure, missing safe diagnostic evidence, and exhausted requeue
  limits; only a backend-authorized terminal failure/dead letter can be
  requeued.
- Verify the same `(salon_id, action_key, event)` returns the saved action with
  `X-Idempotent-Replay: true`, changed action-key reuse conflicts, double-click
  cannot create a second requeue, and an uncertain browser response retries the
  exact same action key.
- Verify requeue queues the existing durable event and clears only bounded
  processing failure state; it does not create or confirm an appointment and
  cannot mutate an internal-origin appointment through downstream repair.
- Verify `/platform/tenants/:tenant_id/operations` renders webhook operations only inside the
  active connected Square card, gates missing verifier configuration, and
  covers loading, empty, list/detail error, success, disabled, filtering,
  pagination, submitting, exact-replay, read-only ignored, desktop table, and
  mobile card states.
- Verify the dashboard renders only backend-safe event/timestamp/error-code
  fields, reads `X-Idempotent-Replay` when exposed, and treats a missing replay
  header as an ordinary successful response rather than inferring replay.

## Public Catalog And Landing App

- Verify public catalog settings are owner-scoped and slug uniqueness is enforced.
- Verify `GET /api/public/salons/:slug` returns only published, public-safe data through V71 while public DB scope sees zero base-table rows; verify the former first-tenant endpoint is not registered.
- Verify public responses exclude staff contact details, POS IDs, provider tokens, sync errors, and owner identifiers.
- Verify the landing app renders loading, not-found, unpublished, and published states without creating booking attempts.
- Verify public copy stays call-to-request and never claims a web appointment is confirmed.
- Verify `owner_manual` publishing needs a canonical eligible service but no
  staff or POS link, `manleai_calendar` uses current activation plus local
  hours, and `external_provider` uses current synced/linked projections.
- Verify publish waits on the shared scheduling fence, rejects a stale expected
  authority version, remains tenant-scoped, and public reads fail closed after
  readiness becomes stale.

## Configuration Transfer

- Verify configuration export returns schema version, exported timestamp, excluded data, and secret re-entry requirements.
- Verify export never includes services, staff, customers, appointments, booking attempts, call sessions, transcripts, POS OAuth tokens, API keys, client secrets, encrypted secrets, provider switch runs, POS sync records, voice webhook/audio records, or operational records.
- Verify import preview is dry-run only and reports creates, updates, skips, conflicts, and gated live states without writing records.
- Verify Twilio stream transport and OpenAI realtime settings round-trip through export, preview, and apply without importing secrets.
- Verify service aliases import only when their target service resolves on the target salon, skip unresolved target services, and conflict with active category aliases.
- Verify v7 exports portable consultation profiles without salon/service UUIDs, profile IDs, owner IDs, or revision/audit metadata.
- Verify scoped `included_sections` packs change only their declared sections and cannot overwrite salon profile, provider configuration, or AI runtime settings.
- Verify consultation profile preview resolves normalized service name plus duration, blocks missing or ambiguous targets, and rejects `ready` targets that are not active-provider, POS-linked, synced, and AI-bookable.
- Verify repeated consultation profile imports do not create duplicates or increment revision when profile data is identical.
- Verify onboarding rejects partial packs and defers full-bundle consultation profiles until Square services exist after sync.
- Verify import apply uses `request_id` idempotency so repeated applies do not create duplicate import runs or duplicate knowledge items.
- Verify onboarding import can create a salon from a valid bundle but still skips secrets and readiness-gated live states.
- Verify import refuses schema versions or malformed bundles that would break contract stability.

## Call Lifecycle Retention And Realtime Streams

- Verify conversation list defaults to active sessions and supports `lifecycle_status=active|archived|redacted`.
- Verify conversation list pagination returns stable `limit`, `offset`, and `has_more` metadata for each lifecycle filter without duplicating rows across page navigation.
- Verify archive is idempotent and keeps transcript text available until retention redaction.
- Verify redaction rejects active sessions, is irreversible, clears customer PII/transcripts/handoff summaries/webhook payloads/audio, and preserves booking/handoff/provider-call audit links.
- Verify the worker redacts expired sessions according to `retention_expires_at` without touching non-expired active sessions.
- Verify realtime stream status/fallback events are recorded without leaking raw provider payloads or secrets.
- Verify `streaming_tts` requests raw PCM 24 kHz, preserves the voice passband while suppressing above-Nyquist aliases during 3:1 resampling, and emits known-correct PCMU bytes. Verify it withholds an isolated first frame, releases one ordered bounded 200 ms startup block, then drains 160-byte frames on a 20 ms monotonic cadence from a bounded backpressure queue without drops or reordering. Cover provider bursts and gaps, underrun diagnostics, short replies, provider failure, barge-in before startup and after provider completion, stale-generation chunks, FIFO advancement only after playout drain, and terminal close only after the matching Twilio mark or timeout.
- Verify legacy `buffered_realtime` still serializes OpenAI `response.create`, waits for matching `response.done` and output-transcript validation, suppresses interrupted audio, and rejects response identity conflicts.
- Verify completed realtime transcripts enter the same conversation engine and booking service and cannot confirm without POS success.

## Regression Guardrails

- Booking services must import `modules/pos`, not `modules/pos_square`.
- AI/conversation modules must not import Square packages.
- Service understanding must remain backend-owned and catalog/alias-backed; do not add production behavior through prompt-only service keyword patches.
- Voice modules must keep Twilio-specific request validation and TwiML response logic outside the conversation engine.
- External AI voice provider adapters must not read POS tokens or import `modules/pos_square`.
- No API response may expose encrypted or raw POS tokens.
- Appointment confirmation, reschedule, and cancellation must remain impossible unless the active POS provider returns a successful booking response.
