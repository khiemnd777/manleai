# Architecture

## Current Milestone

This codebase implements Milestone 1, Milestone 2, Milestone 3 booking safety, the Milestone 4 deterministic conversation simulator, the Milestone 5 live telephony webhook foundation, the Milestone 6 external AI voice provider layer, the Milestone 7A-7C salon knowledge/training slices, canonical POS ownership/provider-switch gates, configuration transfer, public catalog publishing, and call lifecycle retention for the AI Receptionist system.

The backend is organized as:

```txt
cmd/api              Fiber HTTP server
cmd/worker           Independently scheduled POS sync, booking lease, quote cleanup, Square webhook/repair, and call retention worker entrypoint
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
modules/conversation state-driven simulator/phone sessions, semantic turn reduction, service consultation, transcripts, summaries, and handoffs
modules/training     salon-authored knowledge base, owner corrections, and service aliases
modules/voice        provider-neutral live voice runtime, status, semantic-contract verification, routing, and webhook event audit
modules/voice_openai OpenAI STT, strict structured turns, guarded LLM reply, TTS, and Realtime adapters
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

Calendar sync captures one owner-scoped active-provider fence before the first
remote page, passes that same selected location and snapshot generation through
every provider request, rejects any returned booking that identifies a
different location, and revalidates the fence inside the mirror transaction
before any write. A location switch, resync, or provider change therefore makes
the import stale with zero appointment, attempt, notification, or
reconciliation writes instead of mixing provider locations.

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

The booking operation ledger is the concurrency and recovery boundary. Each
logical mutation has a salon-scoped operation key, normalized request
fingerprint, provider idempotency key, processing lease, provider outcome,
retry policy, and optional retry lineage. Every create/reschedule provider
write, including owner HTTP, Square test, simulator, and phone-conversation
paths, consumes one short-lived availability quote whose slot, ordered segment
snapshot, start/end, provider, salon, location, and snapshot generation must
match exactly. HTTP callers submit quote proof; conversation callers retain
the selected backend proof and refresh the exact slot immediately before
dispatch. Party booking refreshes all child proofs before its first POS write. A
safe retry reproduces the original normalized request and atomically supersedes
one eligible fallback attempt. Lease recovery is phase-sensitive and shared by
owner reads and the background worker. It first acquires the same salon calendar
lock and checks current provider mirror truth: an exact authoritative mirror can
terminalize an in-flight create/reschedule/cancel attempt without emitting a
fallback. Otherwise an expired `not_started` row proves dispatch never began and
becomes a retry-safe definitive fallback, while an expired `in_flight` row
remains unknown, retry-blocked, and reconciliation required. Both fallback
branches atomically write one deduplicated notification/outbox result and POS
error; only the unknown branch creates reconciliation work.
The worker runs lease recovery in its own non-overlapping recurring loop, so a
slow Square webhook/calendar repair batch cannot delay lease-expiry recovery.
An identical operation-key replay may safely resume an expired `not_started`
claim before the recovery transaction wins; afterward, retry uses the normal
explicit safe-lineage supersession contract.
Before mutable catalog or appointment validation, an owner-scoped operation-key
lookup recovers an already claimed operation. The service compares the stored
logical intent rather than the now-mutated appointment state: an exact replay
returns the existing pending, fallback, or successful result without another
provider call, while any changed customer, target, time, notes, or ordered
service/staff request conflicts. Refreshed quote IDs and slot-proof fingerprints
are ephemeral dispatch evidence, not replay identity, so a response-loss retry
can recover the prior result after conversation preflight refreshes the same
logical slot. Operation keys are mandatory at both HTTP and booking-service
boundaries.
Unknown or provider-pending outcomes create a
reconciliation task and remain blocked; resolution can attach only an exact
provider-synced candidate revalidated under lock, record verified definitive
non-creation, or escalate. V39 automatically supersedes a duplicate historical
attempt only when `provider_outcome=not_started` proves that provider dispatch
never began. It prefers any dispatched/unknown attempt as canonical and aborts
the migration when one fingerprint group contains more than one attempt that
cannot prove pre-dispatch safety; those outcomes must remain visible for POS
reconciliation. Safely superseded attempts point to the canonical attempt,
have their reconciliation bookkeeping closed with `status=resolved` and
`resolution=superseded`, are excluded from reconciliation paths, and cannot be
lease-swept, reacquired, started, or finalized by a stale worker. Reschedule and cancel attempts retain the target
appointment's provider version from before dispatch; a synchronized mutation
is accepted only when its provider version is newer than that baseline and its
operation-specific state still matches the requested time and ordered
service/staff assignment. The existing target booking ID alone is not evidence
that a reschedule or cancellation succeeded. Direct reschedule/cancel success
must return that exact target ID and a version newer than the captured baseline.
All direct-success, fallback, and lease-recovery persistence paths acquire the
salon calendar lock before appointment/attempt rows. If calendar sync has already
stored the same provider outcome, create requires a known provider booking ID
plus exact range and ordered canonical/raw segment proof; reschedule requires a
current accepted/rescheduled version at least as new as the provider response,
newer than the baseline, and exact requested range and segment proof; cancel
requires a current cancelled version at least as new as the response and newer
than the baseline. Those exact cases converge on the authoritative mirror and
preserve its newer version. Any mismatch remains unconfirmed fallback with
reconciliation instead of overwriting provider truth.
The target appointment also retains its immutable originating provider
location through the creating booking attempt. Owner lookup and action claims
require that origin to match the currently active, synchronized location;
reschedule additionally requires the fresh quote location to match, and cancel
passes the current exact provider fence into the adapter. A location switch or
legacy missing origin therefore fails closed before provider dispatch, while a
newer snapshot generation at the same location remains usable only when every
raw service/staff mapping and target version is still current.
Its database write shares the calendar/reconciliation lock and cannot replace a
newer stored provider version; an equal or newer mirror is accepted only as the
operation-specific exact authoritative match described above, while a
conflicting mirror produces an unknown fallback for reconciliation.
Equal-version calendar import can enrich missing
customer/service/staff mappings or resolve an action only after the locked
persisted status, range, version, and ordered raw service ID/version/duration
snapshot exactly matches the incoming provider snapshot. Manual reconciliation and
provider calendar import acquire the same salon-scoped transaction advisory
lock before row locks, so a zero-candidate decision cannot race an in-flight
mirror insert and the two paths cannot invert attempt/appointment lock order.
Owner notifications are durable outbox/in-product
records here; no external delivery consumer is currently implemented.

Availability quotes are short-lived operational evidence, not an unbounded
audit log. Every five minutes the worker drains up to eight lock-skipping
batches of 250 quotes (at most 2,000 per run and 24,000 per hour), retaining
unconsumed quotes for 24 hours after expiry and orphaned consumed quotes for 30
days after consumption. Each run stops early after a short batch so normal
traffic does not hold cleanup resources unnecessarily. Any quote still referenced by
`booking_attempts.availability_quote_id` or by its recorded consuming attempt
is excluded. Eligible quote deletion cascades only to quote-slot children and
does not mutate booking attempts.

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
An equal provider version remains immutable for status, time, and version, but
may fill previously missing customer fields and internal service/staff mappings
after catalog/customer sync makes those links available. This enrichment-only
path can then re-evaluate blocked exact reconciliation without waiting for
Square to increment the booking version.

Square full catalog imports use `pos_connections.snapshot_generation` as a
monotonic fence. A sync reserves its generation before provider reads and the
atomic snapshot transaction rechecks both generation and selected location
under a salon/provider advisory lock. Location changes invalidate in-flight
snapshots and completed-sync readiness; concurrent syncs cannot finish out of
order and overwrite newer catalog, staff, business-hour, or customer truth.
The same provider fence follows catalog service/staff resolution through the
business-hours schedule, provider availability request, persisted quote,
booking attempt, and adapter dispatch. Any connection, location, or generation
change invalidates the quote before a new POS write. Snapshot generation is not
part of the logical operation fingerprint: an exact safe retry may use a fresh
current-generation quote only when the location, ordered provider service and
staff identities/versions, and appointment target/version baseline still
match. Legacy attempts without stored provider-fence evidence are not marked
retryable. V43 deliberately clears existing Square completed-sync readiness so
one full post-deploy catalog sync establishes the first trusted fence.

Square booking webhooks are an asynchronous mirror trigger, not booking
confirmation authority. The public receiver resolves one unambiguous
salon-scoped merchant/location mapping, verifies the raw body against the
dashboard-stored notification URL and encrypted signature key, and durably
dedupes before enqueue acknowledgement. Root and nested booking location IDs
must agree when both are present. A claim-token-fenced worker retrieves current
provider truth and applies version-aware calendar updates; scheduled repair uses
its own claim token so stale completion cannot overwrite a newer lease. Repair
is a separate backstop whose health does not mutate OAuth/catalog connection
readiness. Webhook payload status cannot confirm an appointment by itself.

The Milestone 4 conversation simulator and Milestone 5 live phone webhook path call the booking service through a provider-neutral booking tool. They do not import Square packages, read POS tokens, build Square payloads, or use Square location IDs directly. The runtime owns booking slot state, including date-only requests before a time is known, so model wording cannot erase already-collected service, date, time, customer, or staff details. Deterministic date, time, staff, and customer evidence is applied before accepting a model-only summary path, so a semantic summary classification cannot discard a concrete correction such as a new requested day. Mutable session booking segments, offered-slot segments, party plans, and versioned `dialog_state` are deep-cloned per turn. `dialog_state` owns pending typed clarifications, bounded mutation history, no-progress recovery, `draft_revision`, `reviewed_revision`, and `authorized_revision` instead of reconstructing active control state from transcript metadata.

Every configured-production freeform turn that reaches conversation orchestration first enters the state-driven Turn Kernel. The kernel derives the expected input from versioned dialog state, measures deterministic coverage, and assigns one explicit lane: fast, answer, action, recovery, or semantic. Unambiguous state-scoped confirmations, offered-slot choices, structured questions, operational actions, and expected-field evidence avoid a model round trip. An initial catalog-backed service or category request is missing-field collection rather than a service-edit operation: an exact service advances to the next missing field, an ambiguous category asks for one concrete catalog option, and an add-or-replace operation choice is valid only after at least one service is selected. The semantic lane selects a schema from operational state rather than caller wording. Initial caller-goal or service-guidance turns with no booking progress use a compact `guidance_turn` contract containing a typed `guidance_action`, bounded explicit `guidance_party_size`, extraction-only consultation needs, confidence, reason, and global safety; it has no separately model-authored goal, booking acts, or questions. The model receives the complete stable recognition vocabulary for new booking, named service, catalog, consultation, salon question, reschedule, cancel, and human handoff. Recognition is therefore independent of whether the current salon catalog and owner-approved consultation profiles can fulfill the request. The backend derives the general turn goal and protocol-owned companion fields from the accepted action, rejects malformed party size, and separately resolves the runtime capability from canonical active-provider, provider-linked services, the salon consultation toggle, and ready profiles. Current provider connection/snapshot readiness is a separate booking fence and cannot erase this guidance catalog. Capability statuses are `recommendation_ready`, `catalog_only`, `consultation_disabled`, and `catalog_unavailable`. A valid party size is translated into the reducer's existing typed guest-count act. A recognized consultation request remains consultation in every capability state: ready profiles enter one-question-at-a-time recommendation; catalog-only uses current category or service names and asks one useful preference; disabled or unavailable states explain the technical limitation and offer owner help without inventing meaning or blaming the caller. The shorter state-owned choices spoken during dependency recovery remain separate from the model's recognition vocabulary. A semantic service-catalog action renders only from the active guidance service catalog, so knowledge copy cannot become a parallel menu source. Corrections, multi-intent turns, partial coverage, pending/review/party state, and any existing booking progress use the full `TurnUnderstanding` contract with ordered acts and questions. Every semantic model input strips consultation profiles and exposes only the catalog identities needed for extraction; owner-approved ready profiles remain backend-only inputs to consultation question planning and ranking. Party inputs expose guest identity and count but keep existing per-guest service assignments backend-owned so draft layout cannot become invented replacement evidence. During an active consultation, model-authored booking/service acts are discarded before validation so needs extraction cannot select or mutate a service. Questions carrying a structured time constraint while state awaits date/time are normalized to availability before reduction, so a mislabeled staff/current-booking subject cannot discard the scheduling constraint. Both contracts inherit the active simulator/phone request context with no conversation-owned 2.5-second deadline; the OpenAI transport retains its 30-second ceiling and separate salon/config/schema circuit keys. Provider-event dedupe, exact bounded review confirmations, and resolved same-category guest scope remain deterministic state-owned controls that semantic interpretation cannot overwrite. Freeform review approvals, corrections, and questions use semantic interpretation; a correction is reduced before its information question is answered, and the answer resumes a freshly revised review instead of authorizing stale details. A service correction against a completed `party_plan` cannot fall back to the generic single-draft add-or-replace prompt: typed pending state collects the concrete target, guest/group reference, add-or-replace operation choice, and replacement source one question at a time. For an existing party plan, exact `guest_ref` is the only assignment owner and redundant model-authored `guest_scope` is cleared before reduction. A replacement is accepted only when its source is grounded in what the caller explicitly identified in the current turn; otherwise the draft is preserved and the runtime asks for clarification. Short follow-up replies use the deterministic fast lane, the party reducer mutates only the selected group, repeated add execution is duplicate-safe, and a party group may retain more service segments than people only after an explicit add operation. Offered slots and review authorization remain unchanged during unresolved clarification and are cleared only after the correction resolves; unresolved party-correction pending state blocks availability and booking even if the semantic interpreter becomes unavailable. Backend validation rejects low-confidence goals, invented service/staff/category IDs, invalid entity/operation combinations, unsafe party flattening, and malformed counts before the unified reducer runs. Deterministic catalog names, aliases, categories, staff records, date/time parsing, phone normalization, and state-scoped bounded confirmations remain authoritative evidence. Phone customer-name candidates awaiting confirmation live in typed `dialog_state.pending`; transcript metadata is audit/legacy-read evidence only. Arbitrary speech cannot replace that value, semantic corrections are identity-validated against the current salon/service/staff catalog, and an unnamed request for another technician derives alternatives from the current staff catalog rather than selecting a hardcoded name. The reducer owns service/staff/date-time/customer/guest mutation and dependency invalidation; the next-action planner owns missing-field, review, and booking readiness decisions. Informational questions preserve pending work and resume the next useful booking question or fresh review. When caller goal or service remains unresolved, a separate typed `dialog_state.guidance` owner consumes only `TurnUnderstanding`, Turn Kernel answer/action decisions, and catalog/category evidence; it never classifies general freeform meaning from a recovery phrase list. Its pure reducer records dynamically offered capabilities, resets both counters after catalog/category/workflow progress, increments provider failures without blaming the caller, and uses distinct bounded handoffs for provider unavailability versus caller no-progress. Provider-failure copy offers only actions present in that state, including human handoff; `awaiting_action_choice` permits a bounded parser only for the immediately following choice and clears on progress or terminal handoff. Ready consultation profiles enable one-question-at-a-time narrowing, while an absent guidance catalog is never presented as an available menu. Legacy guidance prompt/counter fields are promoted to the version 5 nested state when sessions are read, so the JSONB transition requires no SQL rewrite. Any non-accepted semantic outcome preserves the draft and may still consume independently validated catalog and captured-field evidence before the next missing-field prompt; without such evidence the runtime clarifies or hands off instead of guessing.

Production turn execution is serialized by salon and conversation session before the first session read. `Message` and typed unintelligible-voice recovery use the conversation repository's session-scoped PostgreSQL advisory lock on a dedicated connection and hold it across planning, availability/POS side effects, bounded conflict retries, and `SaveTurn`. The repository bounds simultaneous lock connections from the configured SQL pool size so ordinary callback queries retain pool headroom; a one-connection pool is rejected because it cannot safely support this execution model. Cancellation-independent cleanup releases the lock before returning the connection. Provider-event dedupe and the persisted `state_revision` compare-and-swap remain defense-in-depth checks rather than the primary pre-side-effect concurrency boundary. Both customer and AI transcript rows carry the provider event key. A retry of an older event returns that event's exact historical AI reply for voice playback while leaving the newer current session state unchanged.

AI Consultation is a state-owned lane inside the same Conversation Supervisor, not a second tool-calling agent. The semantic interpreter may extract controlled current-system, desired-outcome, length, priority, desired-finish, compared-service, booking-intent, completion, field-level consultation mutations, and a global safety assessment, but deterministic backend code owns all state changes and recommendation ranking. Validated field-level mutations are the sole persistence authority for consultation need fields; a free-standing model snapshot is evidence only and cannot overwrite scalar or list state. Mutations use validated `set`, `replace`, `add`, `remove`, and `clear` semantics so corrections do not become sticky unions. Ranking reads only active-provider, POS-linked, AI-bookable services whose `service_consultation_profiles.status=ready` and whose profile contains both a recommended outcome and compatible current system. The next-question planner derives unresolved fields and discriminating option values from those ready profiles and current state; a guarded language-model call phrases exactly that one profile-backed question without owning field selection or adding facts. Generation failure retries safely and then hands off instead of falling back to a fixed field-prompt map. Provider failures and caller no-progress are counted separately. The runtime records profile revisions, question options, interpreter outcome, match reasons, and progress fingerprints in `dialog_state`, and never invokes availability or POS booking tools from consultation. Selecting a recommendation alone moves to `awaiting_booking`; an additional explicit booking request is required before the service is applied to the appointment draft. Consultation detours preserve an existing draft and resume its prior phase. Deterministic health-suitability evidence is checked before normal routing, and validated structured safety evidence is handled before any mutation or tool action; safety and bounded unresolved conversations hand off to the owner.

Primary acts, questions, and guidance actions are validated separately from auxiliary consultation extraction. `unknown` is absence rather than a persisted mutation value; malformed, low-confidence, or state-no-op consultation fields are dropped with bounded diagnostics without erasing valid primary meaning. A question containing a deterministically extractable expected field remains partial coverage when it can carry another semantic constraint, so a date plus a requested time window is interpreted as one complete turn. Once a guidance action is accepted, the runtime continues that workflow: booking asks for its next missing field and salon questions return their structured answer; the generic guidance menu remains recovery copy for genuine no-progress or provider failure.

Structured answer context is cacheable only behind a database-owned active
provider/location/generation/readiness fence. Every turn reads that fence from
PostgreSQL even on a local cache hit. Cache misses load structured records and
then re-read the fence; a concurrent switch or sync retries the load. Canonical
active-provider linked services, service aliases, and category aliases remain
available for menu answers and consultation when the snapshot is incomplete,
but every service is marked not booking-ready. Staff, imported business hours,
availability, and booking remain fail-closed; salon-authored knowledge remains
available.

The runtime checks provider-neutral availability, offers slots, and only calls booking creation after required fields are present and `draft_revision == reviewed_revision == authorized_revision`. Any service, staff, date/time, guest, party, or customer correction advances the draft revision; dependency-bearing changes clear offered slots and all corrections invalidate stale review authorization. Selected segments and `staff_selection_mode=anyone` survive unrelated turns. Booking confirmations remain impossible unless the booking service returns a POS-confirmed booking attempt, appointment, and provider booking ID. If AI booking is disabled, semantic interpretation cannot proceed safely, a customer requests a human, clarification cannot make progress, or POS cannot confirm, the runtime creates a handoff or fallback pending flow and avoids confirmed wording.

The live voice layer is split into `modules/voice`, `modules/voice_twilio`, and provider-specific AI adapter modules such as `modules/voice_openai`. `modules/voice` owns provider-neutral DTOs and runtime interfaces, including whole-response TTS for recording mode, chunked streaming speech for realtime mode, the owner-scoped semantic-contract check, and the bounded read-only semantic-evaluation route. `modules/voice_twilio` owns Twilio request verification, TwiML, Media Streams framing, typed reply scheduling, bounded/paced PCMU playout, playback marks, barge-in clear/cancel, caller-input gating, and stale-generation rejection. `modules/voice_openai` owns OpenAI payloads, strict full and guidance structured-turn schema validation, per-schema salon/config nonretryable contract circuits, Realtime input sessions, dedicated Speech streaming, raw PCM 24 kHz ingestion, stateful anti-aliased resampling, and PCMU encoding. The adapter validates each schema recursively before dispatch, surfaces only bounded type/code/parameter/request/fingerprint diagnostics, suppresses repeated live invalid requests while that schema circuit is open, and lets the synthetic `POST /api/salons/:id/voice/semantic-check` probe validate both contracts and close their matching circuits after successful requests. The separate `POST /api/salons/:id/voice/semantic-evaluate` path accepts catalog-bound scenarios for repeatable scoring without conversation, availability, or POS mutation.

The local `conversation-eval -mode direct-model` path is deliberately separate
from both runtime and the authenticated semantic-evaluation endpoint. It runs a
selected canary or the complete 50-execution, directly authored fixture corpus
through the production OpenAI turn schema and the production Conversation
Service using an in-memory store and side-effect-blocking booking adapter. The
service itself decides whether a turn needs the production guarded style reply,
a production consultation question, or no output-model call; the evaluator does
not force a second rewrite over every backend reply. Review batches contain at
most five retained outputs (ten rounds for the complete 50), use explicit
backend transition/booking/handoff evidence, and require at least 4/5 in every
dimension. OpenAI base URL, model, and secret resolve strictly
from the encrypted salon-scoped `salon_integration_configs` record; the salon ID
does not become logical scenario ownership. Checkpoint reservation before each
model request, a caller-provided hard call ceiling, versioned evaluation/review
contracts in the run key, fail-closed uncertain-call recovery, and retained
usage prevent an interrupted pilot from silently repeating or mixing paid work.
The production-flow validator derives forbidden identifiers from each scenario's
structured catalog, staff, category, party-group, and model-act data instead of
maintaining a phrase list. Evidence review also treats exposed protocol labels,
silent draft mutations, and over-broad hours replies as quality failures.
OpenAI structured-turn output carries separate salon-local 24-hour `hour` and
`minute` components, and the provider-neutral conversion computes canonical
minutes after midnight before the Conversation Service filters availability.
This removes model-side clock arithmetic and ambiguous hour/HHMM encodings.
Evaluation evidence retains the
salon timezone and every offered slot's local minute, so both deterministic
validation and the reviewer compare actual offers with `before`, `after`, or
`exact` rather than inferring compliance from reply wording. Positive
consultation mutations must also be represented in the same-turn structured
snapshot before the provider-neutral conversion retains them, while initial
guidance-to-consultation entry drops active-consultation booking/completion
flags. The evaluation contract independently requires an exact expected
consultation snapshot and fails invented transition flags or mutations.
Protocol `unknown` values normalize to absence. If model-generated consultation
question copy fails guardrails, the voice boundary uses a single safe spoken
question derived only from the already selected structured field and
profile-backed option values; provider transport failures still follow the
bounded retry/handoff path.
An accepted operational draft mutation also overrides a bare model
`goal=consultation` when the same turn contains no consultation needs or
consultation mutations, preventing an incorrect auxiliary goal from diverting
staff/service/date/customer edits into the recommendation flow. Dynamic-ID leak
checks use delimited identifier matching, so protocol values cannot match
ordinary longer words.
This harness can return synthetic availability and mutate its isolated in-memory
session, but it cannot persist production conversation state or invoke booking,
reschedule, cancellation, or POS operations.

The separate `conversation-eval-real` harness owns the independently authored
100-journey, multi-turn salon-operations suite. It does not expand the
single-turn semantic corpus and does not count review records or paraphrases as
customer journeys. Each journey keeps one isolated production Conversation
Service session for three to twelve caller turns and carries its own catalog
fixture, aliases, consultation profiles, staff, initial state, reply
obligations, forbidden behavior, state expectations, and no-side-effect rule.
The deterministic mode runs all 100 journeys with retained scripted semantic
fixtures to test production state transitions, complete transcripts, question
progression, handoff, tool-attempt, and POS-confirmation invariants without a
model call. Scripted semantic execution is never labeled a model pass. Live
evaluation is a separately selected set of exactly ten multi-turn canaries,
resolves OpenAI configuration strictly from the encrypted database-backed
integration record, checkpoints every paid call, stops after the first failed
journey, and enforces a total ceiling of 60 recognition, reply, consultation,
and review calls. A live result passes only after all ten journeys execute with
the model and both five-journey transcript review rounds score at least 4/5 in
every dimension. The isolated booking adapter may expose deterministic
availability but blocks create, reschedule, and cancellation side effects, so
no evaluation journey can confirm an appointment or call POS.

OpenAI Realtime is the input/VAD/transcription channel. GA input requests transcription log probabilities and applies profile-aware mean, low-tail, and VAD-coherence admission before conversation state can change. The default dashboard-managed `streaming_tts` output mode sends backend-approved text to the dedicated Speech endpoint, converts raw PCM 24 kHz to PCMU 8 kHz through a stateful anti-aliasing resampler, and sends a bounded 200 ms startup block. Subsequent 20 ms frames drain from a bounded backpressure queue on a monotonic clock, independent of provider HTTP chunk timing. Short replies flush at provider completion; longer replies may finish provider generation before local playout drains. `tts_stream_done` is provider lifecycle evidence, while reply advancement and terminal marks wait for `tts_playout_done`. The bridge assigns each accepted caller turn an input generation and schedules typed output (`terminal`, backend turn, initial, input recovery, progress) by state and priority. Backend output supersedes stale recovery/progress output; superseded generations are suppressed; no new reply starts while caller speech or its transcript is active. Streaming barge-in cancels immediately even inside the legacy playback guard. Low-priority recovery speech has a four-second first-provider-byte budget, and a terminal decision latches once so later transcript events cannot restart the backend flow. The legacy dashboard option `buffered_realtime` retains response identity binding and complete output-transcript validation before audio release for rollback compatibility. Both modes retain bounded output, delayed single progress reply, terminal fallback, PII-free timing diagnostics, and the same backend conversation/POS path. OpenAI cannot call Square or independently confirm a booking.

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

Service understanding is a backend domain layer, not prompt-only behavior. The conversation runtime interprets customer service utterances against the active salon service catalog, salon-scoped `service_aliases`, active `service_categories`, and active `service_category_aliases`. Exact catalog service names win over aliases; aliases can select one real service; category names and category aliases create catalog-backed clarification candidates; and fuzzy family matches only produce clarification candidates instead of selecting a service. Catalog ambiguity is authoritative over the structured semantic interpreter: a model-proposed add or replacement cannot narrow a category candidate set to one concrete service unless the caller supplied concrete service evidence. A bare concrete service utterance that would switch an in-progress draft uses the catalog-backed confirmation flow before mutation. Owner corrections can be applied into service aliases with a stable `(salon_id, normalized_alias)` key, while category aliases use their own stable `(salon_id, normalized_alias)` key and conflict with active service aliases so one phrase cannot mean both a single service and a category. Common US nail concepts are versioned in `service_taxonomy_releases` and its category/concept/alias child tables. Refresh materializes active taxonomy data into salon system categories and exact, unique-target service aliases; it never owns or creates local service/POS records and never overwrites owner/imported classifications. The forward-only V45 fill attaches taxonomy category aliases and exact-name suggestions to the active category record with the matching slug, including owner/imported category records, while preserving owner/import aliases and reviewed/manual assignments. Service-understanding decisions are written to transcript metadata with status, reason, confidence, candidates, selected service, alias source, alias ID, category ID, and category name so call reviews can explain why a service was selected or why the AI asked for clarification.

Conversation answers use an `AIAnswerContext` loaded from the active provider scope and cached briefly per salon. The answer router chooses structured sources before owner-authored knowledge: active-provider POS-linked AI-bookable services for service/menu answers, imported `salon_business_hour_periods` for open/close questions, active-provider staff records for staff questions, booking availability prompts for incomplete availability questions, and active knowledge only after those structured sources do not answer the request. A business-hours question without a requested day answers from the salon-local current day instead of reciting the full weekly schedule; an explicit day remains authoritative. Transcript metadata records `answer_source`, `answer_source_reason`, `answer_source_confidence`, `router_intent`, source record IDs when available, and whether the context came from cache. This keeps answers explainable while preventing stale knowledge from overriding synced services, staff, business hours, or POS-first booking rules.

Service consultation is part of the conversation runtime, not free-text knowledge. It may compare only active-provider, POS-linked, synced, AI-bookable services and may state only structured name/category/duration/price plus the ready profile's owner-approved summary, with `services.ai_description` and then `description` retained only as legacy summary fallbacks. Candidate IDs come from validated compared-service mutations rather than raw current-system wording; they and their profile revisions are persisted in typed dialog state and audit metadata. A bare affirmative after multiple options does not select a service; a catalog-selected reply while the session is in consultation enters the existing booking/service-edit state machine, which invalidates stale slots and rechecks availability. Cancel, reschedule, human handoff, and active party-plan actions take precedence over consultation. Pain, injury, infection, allergy, or other health-suitability questions create an owner handoff without medical advice or booking confirmation.

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
be matched to existing target services, portable service consultation profiles,
and owner-authored knowledge. Schema v7 bundles declare `included_sections`, so
curated data packs can import taxonomy, aliases, and consultation profiles
without overwriting salon profile, provider configuration, or AI runtime
settings. Import
previews and applies use stable request IDs, skip secrets and operational
records, and must not recreate services, staff, customers, appointments, POS
tokens, call sessions, transcripts, provider connections, synced business hour
periods, provider switch runs, party booking requests, voice webhook events, or
provider-side state. Service category imports use stable slug and
normalized-alias keys and reject category aliases that conflict with active
service aliases. Service alias imports use stable normalized-alias keys and
skip aliases whose target service cannot be resolved on the target salon.
Consultation profile imports resolve an existing target service by normalized
name plus duration, block missing or ambiguous targets, require `ready` profiles
to resolve to active-provider, POS-linked, synced, AI-bookable services, and
upsert by stable `(salon_id, service_id)` without changing revisions for
identical data. Services and provider mappings are never created by transfer.

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
