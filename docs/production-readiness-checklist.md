# Production Readiness Checklist

Completed historical items below describe the current Square-backed
`external_provider` runtime unless they explicitly say otherwise. They remain
valid implementation evidence but do not imply parity across scheduling
authorities. The Owner-first milestone at the end separately records the
implemented Phase 2 `owner_manual` request workflow and Phase 3
`manleai_calendar` configuration/readiness foundation plus Phase 4A staff-only
and Phase 4B structured multi-guest/multi-service pooled all-or-none
availability/atomic create, plus Phase 4C whole-root internal
reschedule/cancel and V51 lifecycle integrity. V52-V55 and the Settings UI add
the explicit owner-reviewed, readiness/version-fenced authority switch and
audited inverse-run workflow. This code readiness does not complete live
external-provider configuration, witnessed load/restore drills, or approved
RPO/RTO evidence.

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
- [x] Harden auth code so invalid and disabled login attempts share generic `401 INVALID_CREDENTIALS`, with found disabled-account status checked only after bcrypt comparison; refresh tokens rotate atomically once for active users, consume disabled-user tokens without a successor, and persist only hashed successors. Handler/unit tests and PostgreSQL repository integration tests pass under the fresh release gate; this remains code evidence, not deployed-runtime evidence.
- [x] Make shared `RequireAuth` use signed JWT identity only as the lookup key,
  then resolve the current active principal through `auth.Repository` for every
  protected request. Server-owned `users.status`, current salon ownership, and
  current roles replace stale JWT salon/role claims; disabled/deleted users,
  missing resolver, mismatched identity, and repository errors return generic
  `401 UNAUTHENTICATED`. `backend/internal/middleware/auth_test.go` covers claim
  replacement and fail-closed unit cases, while
  `backend/modules/auth/repository_integration_test.go` covers current role
  reload and immediate access/refresh revocation after disablement. The release
  security manifest owns both tests and fresh PostgreSQL execution passes under
  the final database gate.
- [x] Add the V64 expand-only SaaS authorization foundation without changing
  legacy Business-route ownership: backfill/synchronize exact salon-owner
  memberships; add Tenant Business Manager, Platform Admin/Ops, per-salon Ops
  capabilities, bounded 24-hour PII grants, optimistic versions, stable action
  fingerprints/replay, and immutable audit; retain `salons.owner_user_id` and
  scheduling owner triggers as the owner source of truth.
- [x] Build a server-owned `ActorContext` from the current active database
  principal on every protected request; V74 reloads immutable scope-compatible
  role state and permits a primary active membership only for Tenant identities
  while ignoring stale JWT/header scope, tenant, or role claims. Keep Tenant and
  Platform access surfaces route-owned and fail invalid
  surface/capability/PII combinations closed.
- [x] Add authenticated Platform access-management APIs and the one-time
  operator-only first-Platform-Admin command. Prevent owner membership
  revocation, last-admin removal, cross-salon Ops capability use, Platform PII
  access without an exact active grant, and arbitrary prose/customer data in
  the grant's bounded opaque change reference or immutable event details.
  Serialize Platform role changes and atomically revoke active salon/PII
  delegation so concurrent demotions cannot remove the last admin and role
  reactivation cannot revive stale access.
- [x] Require explicit `salon_id` on authenticated Square control/write routes
  and remove primary-salon middleware fallback before enabling multi-salon
  Platform actors.
- [x] Cut over the complete shared Business route set and `/api/salons` in one
  coherent SaaS Phase 3 slice with capability-aware Tenant/Platform DTO
  projections. V65 owns optimistic resource versions, exact action replay,
  actual-actor safe audit, provider/compatible-image insert coverage, and the
  salon-rooted staff/service eligibility relationship. Provider fields and
  external hours are read-only, Platform staff contacts are omitted, Platform
  customers require an exact PII grant, and canonical public-readiness and
  consultation validation owners are reused rather than copied.
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
- [x] Replace UUID-only buffered-TTS audio access with a database-expiry-bounded
  HMAC-SHA256 capability bound to audio/salon/provider/call/session identity;
  resolve only the stored salon Twilio token, fail closed without an unsigned
  URL, return uniform non-enumerating failures, and preserve repeated Twilio
  fetches within the maximum 15-minute TTL while token rotation revokes old
  URLs.
- [x] Keep semantic validation failures in auxiliary consultation extraction from erasing a valid primary act, question, or guidance action; retain bounded drop diagnostics.
- [x] Validate direct-model conversation quality through the production Conversation Service output-call decision, versioned checkpoints, backend transition facts, bounded selected canaries, delimiter-safe dynamic-identifier leak rejection, separate typed local-hour/minute input with backend-owned minute conversion, offered-slot-versus-time-constraint validation, exact consultation extraction checks, same-turn mutation/snapshot consistency, protocol-unknown normalization, operational-mutation ownership over bare consultation goals, structured consultation-question fallback after unsafe model copy, and evidence-grounded review batches that penalize invented consultation state, silent mutations, and over-broad operational answers instead of a forced universal reply rewrite.
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
- [x] Store versioned common US nail categories, service concepts, and category/service aliases in database-owned taxonomy tables; materialize them idempotently without creating services/POS links or overriding owner/imported classifications.
- [x] Treat generic and fuzzy service-family matches as clarification candidates, not selected services.
- [x] Keep exact and owner-defined alias selection behavior unchanged; when one fuzzy catalog candidate is proposed, persist a scoped `fuzzy_service_confirmation` pending state and require explicit state-bound confirmation before draft mutation, availability invalidation, or booking. Reject stale catalog/source provenance and replay duplicate events idempotently without turning bounded confirmation words into a general intent classifier.
- [x] Persist salon-scoped service aliases from owner corrections with stable `(salon_id, normalized_alias)` upsert semantics.
- [x] Prevent one normalized phrase from being both an active service alias and an active category alias.
- [x] Feed active service aliases into simulator and phone conversation runtime.
- [x] Feed active service categories and category aliases into simulator and phone conversation runtime.
- [x] Re-check availability when a caller confirms a service correction after a time or slot was already collected.
- [x] Record service-understanding status, reason, confidence, candidates, selected service, alias source, alias ID, category ID, and category name in transcript metadata.

## Production Answer Routing

- [x] Load service, alias, category, consultation-profile, staff, knowledge, and business-hour answer context once per salon turn window, but validate a database-owned fence on every turn and double-read it around cache misses; persisted common collection revisions invalidate service/staff/alias/category/profile/knowledge changes before the 45-second TTL, owner-managed local hours use the existing `business_hours` resource version, internal hours use config/activation capability versions, external hours use provider/location/generation/sync readiness, and authority-irrelevant provider/local-hours fields are normalized out.
- [x] Answer service/menu questions from active-provider, POS-linked, synced, AI-bookable services only.
- [x] Answer open/close questions from the selected authority's source before owner-authored knowledge: owner-managed `local_override` for `owner_manual`, activated/capability-ready `local_override` for `manleai_calendar`, and current provider/location imported periods for `external_provider`; never treat informational hours as availability.
- [x] Answer staff questions from active-provider staff records without exposing inactive or non-bookable staff as confirmed booking options.
- [x] Route incomplete availability questions into booking-detail prompts instead of guessing availability or creating bookings.
- [x] Use owner-authored knowledge only after structured service, hours, staff, and availability sources do not answer the request.
- [x] Record answer source, reason, confidence, source record IDs, router intent, and answer-context cache state in transcript metadata.
- [x] Separate canonical active-provider guidance services/aliases/categories from current-snapshot booking eligibility; during provider switching/syncing keep catalog-backed menu and consultation available while clearing booking-ready flags, staff, imported hours, availability, and booking access. Local hours never override the external-provider hours source.

## Product-Grade Semantic Turns And Final Review

- [x] Interpret add, replace, remove, undo, current-draft summary, and final-review authorization as typed conversation acts instead of scattered phrase-owned mutations.
- [x] Keep replacement source, replacement target, mutation scope, and guest scope separate and validate all service IDs against the active guidance catalog; revalidate booking eligibility separately before provider calls.
- [x] Persist versioned `dialog_state` with pending clarification, reversible last mutation, bounded no-progress recovery, and final-review state.
- [x] Allow explicit out-of-set catalog targets to supersede stale pending candidates while retaining pending context across informational detours.
- [x] Distinguish current-draft service counts from salon catalog counts, including common non-native English wording.
- [x] Clarify whether an added same-category service is for the caller or another guest before mutating the draft when guest scope is missing.
- [x] Require explicit final-review authorization before production call-session booking writes; review acceptance itself is never confirmed wording.
- [x] Add a strict structured semantic-turn interpreter that is data-minimized, catalog-validated, confidence-gated, and unable to call POS or mutate state directly.
- [x] Make validated field-level consultation mutations the sole persistence authority for need fields; free-standing semantic snapshots cannot overwrite scalar or list state.
- [x] Add golden transcript and invariant tests for directional switching, stale pending escape, review corrections, duplicate-safe turn handling, and bounded clarification handoff.
- [x] Route every configured-production freeform orchestration turn through the state-driven Turn Kernel; use deterministic fast/answer/action/recovery lanes when coverage is complete and a multi-act semantic lane for correction, ambiguity, or partial coverage without a keyword-only gate.
- [x] Remove the conversation-owned 2.5-second semantic deadline; inherit the active simulator/phone request context while retaining the OpenAI adapter's 30-second transport ceiling, select compact typed-action `guidance_turn` versus full multi-act/question schema from operational state, omit full catalogs/profiles from initial guidance, scope service/staff context to the active turn, and expose PII-free route, allowed/action, expected-input, semantic-contract, context-size, schema-fingerprint, duration, and interpreter-outcome diagnostics.
- [x] Keep the compact initial guidance contract complete for named services, new booking, explicit party size, reschedule, cancel, catalog, consultation, salon questions, and handoff; expose the full stable recognition vocabulary to the model, resolve fulfillment separately from runtime catalog/toggle/profile capability, and keep spoken provider-failure choices short and state-scoped.
- [x] Preserve recognized consultation/service-discovery intent when recommendation is `catalog_only`, `consultation_disabled`, or `catalog_unavailable`; use current catalog categories/services when available and otherwise give a truthful technical limitation plus owner handoff without semantic guessing or caller-blaming copy.
- [x] Make voice readiness expose separate `guidance_service_count` and strict booking `service_count`/staff counts; derive typed service-guidance readiness from canonical linked guidance data while retaining the provider/location/generation fence for booking readiness.
- [x] Route freeform service-menu requests recognized by `guidance_action=service_catalog` only through the active guidance catalog, and constrain no-provider recovery parsing to the immediately active state-owned choices instead of general keyword intent matching.
- [x] Validate the OpenAI structured-turn schema recursively before dispatch, exclude unsupported schema keywords, expose only bounded provider type/code/parameter/request/fingerprint diagnostics, and open a salon/config/schema-scoped circuit after nonretryable contract rejection.
- [x] Add an authenticated owner-scoped semantic-contract probe that validates both full and guidance schemas with the live model without caller PII, conversation writes, availability, or POS tools; successful requests close the matching per-schema local contract circuits.
- [x] Add a bounded owner-scoped semantic-evaluation endpoint plus a reproducible schema-v2 1,000-scenario single-turn contract corpus using the same guidance/full split as runtime, a 100-record deterministic source/state/hardcoding audit (not 100 customer conversations), an offline validator that reports zero model executions/passes, and a report-required live scorer hard-capped at 12 canaries that cannot create sessions, invoke POS tools, or confirm a booking.
- [x] Add a separate 50-execution direct-model feasibility pilot from 45 directly authored real nail-salon situations, including five phone/simulator pairs; resolve the active OpenAI model and encrypted key strictly from `salon_integration_configs`, run a zero-model-call database guidance preflight before paid work, label execution context as isolated fixture and runtime readiness as unverified, reuse production recognition/conversation/reply guardrails over those fixtures, block conversation/POS writes, checkpoint every paid request, enforce an explicit model-call ceiling, retain token usage and final replies, and run 10 actual five-output review rounds whose five dimensions must each score at least 4/5. This checklist item covers harness readiness only; no repository test or corpus generation claims that the stored model has passed the paid pilot.
- [x] Add an independent, directly authored 100-journey real nail-salon suite with exact service-advice, consultation, catalog/operations, single booking, party booking, reschedule/cancel, correction/multi-intent, safety, and dependency-failure coverage. Every journey retains one isolated multi-turn Conversation Service session, uses catalog/category/alias/profile/staff data instead of runtime phrase rules, asserts every reply and the terminal reply, and blocks booking/POS side effects. Structural evidence must remain `passed=false` until model execution and review occur. Deterministic runtime executes all 100 with scripted semantic fixtures; live evidence is limited to ten marked canaries, a 60-call total ceiling, stop-on-first-failure, checkpointed requests, and two five-transcript reviews. This checklist item proves harness and corpus readiness only; it does not claim the configured model passed the live canaries.
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
- [x] Add a production owner-notification delivery consumer and operations policy: V56 provider-neutral claims/leases/dispatch markers, immutable attempt/event/action ledgers, strict salon-scoped Twilio Messaging configuration, signed monotonic callbacks, bounded pre-dispatch retry, unknown-outcome dead-letter protection, owner-scoped masked delivery/requeue operations, the Appointments delivery surface, and worker/operations-health evidence documented in `operations/owner-notification-delivery.md`.
- [x] Add default-off customer appointment SMS with V59 policy, explicit
  per-destination consent and immutable evidence, signed STOP/START/HELP
  handling without body parsing or duplicate replies, transactional
  request/appointment outbox hooks, quiet-hours scheduling, exact
  consent/policy/source-version pre-dispatch fencing, canonical unknown-outcome
  dead letters, one bounded owner requeue, masked appointment/request child
  surfaces, and separate worker/operations-health evidence. Preserve external
  authority version zero and never treat provider acceptance/sent as delivered.
- [x] Add authenticated owner-scoped Square webhook operations: safe bounded
  event list/detail, status filters and pagination, backlog/dead-letter/recent-
  success metrics, calendar-repair health, backend-owned `can_requeue`, bounded
  action-key-idempotent requeue with exact replay header, raw/provider/PII
  redaction guards, and a responsive child panel inside the connected Square
  integration card. A configured verifier or received webhook still is not
  appointment-confirmation or live-subscription proof.
- [x] Fail runtime Square/Twilio/OpenAI configuration resolution closed on repository, malformed persisted settings, or decryption errors; keep stored enabled state and credentials authoritative; make Twilio/OpenAI runtime and reads database-only; prevent dashboard secret-source fallback for stored rows; and permit the legacy bootstrap fallback only for an exact missing Square salon configuration.
- [x] Enforce Phase 1 provider-diagnostic isolation: serialize integration
  secrets/SIDs/destinations as write-only state, normalize Square/POS failures
  to stable codes and fixed messages before persistence/API response, allowlist
  Twilio/OpenAI Realtime diagnostics, and apply V63 to redact historical raw
  provider text and payloads.

## Backup, Restore, And Migration Rollback

- [x] Add a bounded pre-deploy PostgreSQL backup script that requires an explicit source database and encrypted-private storage attestation, writes a custom-format `pg_dump` with private permissions, validates its catalog through `pg_restore --list`, creates an SHA-256 sidecar, and refuses artifact overwrite.
- [x] Require every tagged deploy to carry an exact release-tag previous-image/database compatibility declaration and approver; fail before database mutation when it is missing, stale, malformed, or false.
- [x] Take the pre-deploy backup before the candidate API startup migrator can run; require protected approval of the exact encrypted-private backup path, fail closed when the existing PostgreSQL container/source is ambiguous, and permit a no-backup record only for a truly empty initial deployment with neither container nor project volume.
- [x] Keep image rollback distinct from database recovery: automated failure handling may restart prior images only after forward-schema compatibility was declared, never rewinds PostgreSQL, and records that no database restore occurred.
- [x] Add an isolated restore drill that refuses the same source/target, reserved databases, non-prefixed targets, existing targets, checksum mismatch, malformed archives, migration name/checksum drift, missing required schema objects, or tenant-smoke violations; it never drops, truncates, renames, or overwrites a database.
- [x] Validate exact `app_schema_migrations` parity against the release SQL set, critical scheduling/integration/conversation/internal-calendar tables, tenant/staff/lifecycle constraints, and bounded cross-salon/orphan smoke queries before producing the API startup handoff.
- [x] Add a protected manual non-production GitHub drill workflow with explicit source/target/artifact/approval/RPO/RTO inputs; keep the dump, checksum, restored target, and private env on the drill host and upload only the sanitized 30-day report.
- [x] Record drill timestamp, artifact ID/checksum, release and approval references, approver, RPO target/observed age, RTO target/restore-validation duration, total duration, bounded failure stage, and API startup handoff without secrets, PII, row contents, or provider configuration.
- [x] Document forward-only expand/migrate/contract releases, previous-image compatibility review, preflight/backup/abort/restore decisions, post-snapshot write/provider reconciliation, and separate DBA approval before any production recovery/cutover.
- [ ] Configure the production backup path on an approved encrypted private volume, set a retention/key-rotation policy, and verify its capacity/monitoring before claiming production backup readiness.
- [ ] Configure the protected `postgres-restore-drill` environment on a dedicated non-production host, approve the exact encrypted-private backup path, install its exact non-production attestation file, and complete a witnessed drill; retain the sanitized report plus isolated API `/healthz` and bounded read-handoff evidence before claiming restore readiness.
- [ ] Define and approve business RPO/RTO objectives and run the drill on the required recurring cadence; the repository provides the manual workflow but does not claim a scheduled drill or an automatic production restore.

## Scheduling Load And Concurrency Verification

- [x] Add a reproducible bounded synthetic harness for `owner_manual` request replay/changed-payload conflict/transition CAS, `manleai_calendar` aggregate party replay and pooled-resource conflict, and authority-switch preview/commit replay plus concurrent commit fencing.
- [x] Refuse execution without the exact isolated non-production attestation, dedicated database name/user/prefix, release-migration checksum parity, a unique run UUID, and compiled concurrency/operation/duration bounds; keep the harness free of drop/truncate/delete cleanup and real POS/Twilio/OpenAI runtimes.
- [x] Emit a PII-free schema-versioned report with release/run/seed/database/migration evidence, p50/p95/p99/max latency, throughput, expected conflicts, unexpected errors, duplicate/orphan/provider-evidence/provider-call/tenant/idempotency/safety violations, and SQL pool statistics; pass only with zero unexpected errors and zero invariant violations.
- [x] Cover guardrails, report gates, destructive-SQL/provider-import exclusions, race detection, and opt-in fresh-PostgreSQL repeated-run/collision behavior. The fresh local integration verifies implementation only and does not claim production capacity.
- [ ] Complete an approved witnessed run in a representative isolated environment, retain the sanitized report and checksum with release approval evidence, and define workload-specific latency/throughput acceptance targets before making any production capacity claim.

## Public Catalog And Landing App

- [x] Add owner-managed public catalog settings and publish readiness.
- [x] Add unauthenticated public-safe catalog APIs for first published salon and slug lookup.
- [x] Add separate `landing/` Next.js app for public salon pages.
- [x] Keep public pages call-to-request only; no web booking attempt is presented as confirmed.
- [x] Avoid exposing staff contact details, POS IDs, sync errors, owner identifiers, or provider tokens.
- [x] Make publish/read eligibility selected-authority-aware: canonical services
  without POS/staff for `owner_manual`, current activated local hours/catalog
  for `manleai_calendar`, and current synced/linked projection for
  `external_provider`.
- [x] Fence publishing with the scheduling-authority advisory lock/version,
  fail stale public pages closed, and use call-to-request copy only.

## Configuration Transfer

- [x] Mount configuration transfer only under the Platform tenant detail
  `Transfer` tab; do not restore Tenant Settings or onboarding transfer routes.
- [x] Support direct tenant-to-tenant sources and schema-v10 JSON upload with
  schema-v9 and schema-v8 upload compatibility and explicit section scope.
- [x] Accept only explicitly scoped content-only v7 packs, canonicalize them to
  v8 before fingerprint/audit/apply, and reject v7 runtime/provider scope plus
  all v1-v6 uploads on the mounted Platform surface.
- [x] Derive JSON-upload section controls from the file's
  `included_sections`, disable absent sections, and visibly explain v7
  canonicalization before preview.
- [x] Require every selected source read and destination write capability for
  the actual Platform actor; do not add a broad transfer bypass capability.
- [x] Persist only safe reviewed-run fingerprints, fences, summaries, and
  immutable events; never persist the raw bundle or secrets.
- [x] Recheck source fingerprint, Business/Technical versions, and scheduling
  authority/version under one transaction; stale apply writes no destination
  domain data.
- [x] Preserve scheduling authority, active provider selection, provider
  connection state, provider-imported hours, credentials, and operational
  history. Transfer only `local_override` hours when both source and
  destination are locally managed.
- [x] Read provider settings only from salon-scoped persisted Platform state;
  missing source providers are no-ops and never inherit legacy environment
  fallback.
- [x] Write canonical domain rows and existing Business/Technical version and
  actual-actor audit ledgers atomically; include transfer events in Platform
  Audit.
- [x] Provide exact action replay, changed-action conflict, preview conflicts,
  responsive UI states, selected-scope JSON export, and recent safe run history.

- [x] Add sanitized owner-scoped configuration export with stable schema version.
- [x] Exclude services, staff, customers, appointments, call sessions, transcripts, POS OAuth tokens, API keys, client secrets, encrypted secrets, and operational records.
- [x] Include service category taxonomy, service category aliases, and service aliases in schema v5, add the salon consultation runtime toggle in schema v6, add portable service consultation profiles and scoped `included_sections` data packs in schema v7, and make schema v8 Owner-first while retaining v7 compatibility and excluding provider connection state.
- [x] Resolve v7 consultation profiles only against existing target services by normalized name plus duration; block missing, ambiguous, or ineligible ready targets without creating services or POS mappings.
- [x] Keep consultation profile import idempotent by `(salon_id, service_id)` and leave profile revision unchanged when imported data is identical.
- [x] Use stable category slugs and normalized alias keys so repeated imports update existing taxonomy instead of duplicating it.
- [x] Resolve imported service aliases only against existing target-salon services and skip unresolved alias targets without creating services.
- [x] Retain the earlier import preview/apply implementation for compatibility;
  after SaaS cutover its Tenant/onboarding routes are not registered.
- [x] Use request IDs for repeated import applies so retries do not create duplicate import runs.
- [x] Import `ai_enabled` as portable intent without a universal Square gate; evaluate confirmed booking, consultation eligibility, and public publishing against the destination scheduling authority, with `owner_manual` forced to pending approval and source/target/result booking mode plus a structured warning exposed on preview/apply and exact replay.
- [x] Exclude scheduling authority/version and switch history, scheduling requests/outbox state, internal-calendar configuration/execution evidence, provider secrets, and operational records; never move historical operations.
- [x] Fence apply with the shared scheduling-authority advisory lock and an exact authority/version recheck; concurrent authority changes fail before any import mutation.
- [x] Report source/destination/result `active_pos_provider`; preserve it as adapter intent only, and block external-executor adapter changes for explicit provider-switch review.
- [x] Cover schema v7 compatibility, repeated and concurrent request replay, changed-payload conflict, tenant isolation, and concurrent authority-switch fencing in config-transfer unit and PostgreSQL integration tests.

## Operations Health And Worker Heartbeats

- [x] Add V57 current-job and per-run ledgers with bounded technical fields,
  terminal history immutability, live lease claims, and exact
  job/run/worker-instance fencing.
- [x] Wrap every recurring worker job with start, heartbeat, cancellation,
  panic, and finish recording; keep job loops independent and non-overlapping.
- [x] Expose authenticated `GET /api/salons/:id/operations/status` with only
  tenant-scoped backlog/oldest/dead-letter aggregates and safe class/code data.
- [x] Omit irrelevant provider rows, fail closed for missing/stale evidence,
  and keep internal scheduling health separate from POS errors.
- [x] Add responsive Settings loading/error/healthy/degraded/stale/unknown states
  with refresh and owner-workflow links but no replay mutation.
- [x] Cover V57 ordering/constraints, scheduler cancellation/panic/lease-held
  behavior, stable classification, route authentication, and optional
  PostgreSQL tenant/fencing integration.
- [ ] Configure external polling/paging and an on-call response policy in each
  production environment. The in-product card is diagnostic evidence, not
  automatic alert delivery.

## Scheduling And Notification PII Retention

- [x] Add V61 policy-versioned, irreversible redaction fields and database
  guards for terminal scheduling requests/segments/events, owner/customer
  notification content and destinations, and expired voice-audio bytes.
- [x] Keep the baseline at 90 days and require both terminal business state and
  terminal delivery state before notification redaction. Preserve
  pending/contacted owner work, live leases, queued/retrying/unknown delivery,
  open reconciliation work, active consent/STOP routing keys, provider and
  operation IDs, versions, statuses, timestamps, and tenant/audit ownership.
- [x] Use explicit JSON audit-field allowlists instead of broad text matching,
  delete no audit row, and expose redaction markers through owner-scoped API/UI
  surfaces without permitting requeue or restoration.
- [x] Run `scheduling_pii_retention` every five minutes with a bounded default
  batch of 100, `FOR UPDATE SKIP LOCKED`, one-row transactions, idempotent
  concurrent processing, PII-free errors, and no provider calls.
- [x] Record prior fresh V1-through-V61 migrate-twice and V61 PostgreSQL
  retention evidence for exact 90-day boundaries, nonterminal/live-lease/
  consent/reconciliation fences, aggregate rollback, concurrency, tenant/API
  DTO behavior, irreversible redaction, and early manual audio redaction.
- [x] Rerun fresh V1-through-V62 migrate-twice plus V61/V62 PostgreSQL suites
  after the final bounded-expiry and composite-tenant-fence changes. The final
  isolated PostgreSQL release gate passed on 2026-07-26.
- [x] Add V62 fail-closed preflight plus a composite
  `(salon_id, call_session_id)` foreign key for party requests, tenant-filtered
  hydration, and PostgreSQL cross-salon insert/hydration/redaction regression
  coverage. Existing mismatches are never auto-repaired or reassigned.
- [ ] Obtain and record privacy/legal approval for production retention
  durations, jurisdictions, litigation holds, DSAR/deletion handling, backup
  expiry, and policy-version changes. Passing retention tests is code-readiness only,
  not compliance certification.

## Call Lifecycle Retention And Realtime Streams

- [x] Add call session lifecycle filters for active, archived, and redacted sessions.
- [x] Add idempotent archive and irreversible redaction actions with active-session conflict gates.
- [x] Add worker-driven 90-day retention redaction for expired active sessions.
- [x] Clear customer/contact PII, transcripts, handoff/party summaries and guest
  details, webhook payloads, temporary voice audio, quote/offered-slot proof,
  and every PII-capable conversation state container: `dialog_state`,
  `party_plan`, `booking_segments`, and `reschedule_candidates`, while
  preserving bounded booking/handoff/provider-call audit links.
- [x] Make re-redaction idempotent and capable of repairing residual JSONB on
  historically redacted sessions without changing the original `redacted_at`;
  clear future-dated audio immediately and keep later expiry-worker processing
  idempotent. `backend/modules/conversation/retention_integration_test.go`
  covers complete row/API clearing, cross-owner non-mutation, historical
  repair, repeated redaction, audio cleanup, and expiry processing. Its final
  fresh V1-through-V62 PostgreSQL execution passes under the checked rerun gate
  above.
- [x] Add Twilio Media Streams and OpenAI Realtime adapter path with completed transcripts routed back through the same conversation engine and booking service.
- [x] Correlate realtime response creation, audio, cancellation, and completion by application request ID and provider response ID; schedule typed replies by input generation/workflow priority, suppress superseded output, and reject stale audio.
- [x] Fail closed on missing GA transcription confidence, apply profile-aware mean/low-tail/VAD-coherence admission, and keep rejected noise out of conversation state.
- [x] Retain the legacy buffered-Realtime rollback path that verifies canonical operational facts in the completed audio transcript before release.
- [x] Add dashboard-selectable low-latency streaming TTS that keeps Realtime input-only, converts raw provider PCM 24 kHz through a stateful anti-aliasing resampler to Twilio PCMU 8 kHz, sends a bounded 200 ms startup block, then drains a bounded backpressure queue at one 160-byte frame per 20 ms, flushes short replies safely, and retains buffered Realtime as a legacy rollback mode.
- [x] Gate new TTS while caller speech/transcription is active, cancel streaming speech immediately on barge-in, let backend output supersede stale recovery/progress, enforce a recovery first-byte budget, reject stale generations, and latch terminal replies through one playback mark, interruption close, or timeout.
- [x] Expose owner-scoped, PII-free realtime admission and output-validation diagnostics in the Calls timeline without transcript or audio bodies.
- [x] Keep operational facts deterministic and allow guarded LLM rewriting only for explicitly style-only replies.
- [x] Persist structured service consultation profiles with stable `(salon_id, service_id)` ownership, controlled values, owner-approved copy, no-op identical retries, revision increments only when data changes, and a fail-closed `ready` contract requiring both a recommended outcome and compatible current system.
- [x] Default salon consultation to disabled, reject enablement without at least one fully eligible profile, and rank only active-provider, POS-linked, AI-bookable services whose consultation profile satisfies the complete `ready` contract; record recommendation reasons and profile revisions for audit.
- [x] Keep semantic consultation output extraction-only and unable to recommend services, mutate booking state, or call availability/POS tools; validate field-level set/replace/add/remove/clear semantics before the backend reducer applies them.
- [x] Include complete ready consultation profiles and revisions only when active consultation or full-turn reasoning requires them; derive the next unresolved discriminating field/options from profiles and state, then use guarded generation only to phrase one question without a fixed field-prompt map.
- [x] Persist active consultation needs, candidates, recommendations, selection, resume phase, no-progress count, and exit reason in versioned `dialog_state`; transcript metadata remains audit evidence.
- [x] Persist consultation question options, provider outcome/diagnostics, separate provider-failure and caller-no-progress counters, and a stable progress fingerprint; retry safely then hand off instead of guessing when question generation fails.
- [x] Require both a concrete service selection and explicit booking intent before transitioning from consultation to booking, and preserve an existing booking draft across consultation detours.
- [x] Support consultation-only completion with `consultation_completed`, bounded unresolved handoff, and global deterministic plus structured safety handoff before any mutation or tool action, without medical advice.
- [x] Keep cancel, reschedule, handoff, and active party-plan actions ahead of consultation routing.
- [x] Expose per-service consultation profile controls in Services, the salon-wide toggle and coverage in Settings, and typed consultation audit state in Calls.

## Owner-First Production Release Gate

- [x] Require every `v*` image build and deploy to pass complete backend `go test ./...` and `go vet ./...`, a bounded high-risk race suite, and typecheck/build for `frontend`, `landing`, and `pos-calendar`.
- [x] Add a versioned package/test-file manifest and fail-closed orchestration script; do not select coverage by grepping Go test names or output.
- [x] Start the PostgreSQL contract from an empty dedicated database/role whose database name carries the `release_gate` marker, reject missing/unsafe identity and pre-existing public state, construct test URLs without logging them, prove migrate-twice/checksum behavior, and verify every repository migration from V46 through latest was applied exactly once.
- [x] Clone the verified migration-only baseline into a disposable database per package, then run the scheduling authority, owner-manual, internal-calendar, switch, booking/POS/Square and webhook operations, configuration/public, owner/customer notification, scheduling-PII retention, operations-health, and V58 alias integration packages serially with bounded timeouts and no live provider, paid API, or network dependency.
- [x] Run an explicit tenant/security contract covering route authentication, cross-salon rejection, whole-response integration token/secret redaction, provider-error/audit redaction, zero POS evidence for manual/internal work, public PII/provider-ID absence, notification masking, and callback signature enforcement.
- [x] Keep `build-images` and `deploy` dependent on the release gate and preserve the existing tag-only, pre-deploy backup, forward-migration compatibility, healthcheck, and rollback gates.
- [ ] Treat a passing gate as code-ready only; separately verify dashboard-managed provider configuration, live callback/delivery behavior, production backup storage/retention/capacity, witnessed restore/RPO/RTO evidence, alert routing, and on-call readiness before claiming operational production readiness.
- [x] Replace browser local-storage tokens with memory-only access tokens and a
  host-only HttpOnly/Secure-production/SameSite-Strict refresh cookie; preserve
  one exact concurrent rotation successor; add nonce CSP with no production
  wildcard/`unsafe-inline`/`unsafe-eval` across all three web apps; remove POS
  inline scheduler styles; and enforce HMAC-pseudonymous atomic Redis global
  plus route-class buckets with typed `429`/`503`, edge-owned client IP, health,
  live Redis integration, race, web policy, typecheck, and build coverage.
- [ ] Independently verify the emitted CSP/auth-cookie behavior at the deployed
  production origins, confirm Redis capacity/latency/alerting and tune the
  documented abuse thresholds from representative traffic before treating the
  release gate as a complete operational security approval.

## SaaS Tenant Refactor Phases 3-10

- [x] Phase 3: expose one canonical Business contract on fixed Tenant and
  Platform routes for salon profile, services, staff, staff-service
  eligibility, prices, hours, public settings, and customers; enforce exact
  membership/capability and actual-actor versioned audit.
- [x] Phase 4: split `/dashboard/*` Tenant UI from `/platform/*` Platform UI;
  add tenant directory/detail Business, Technical, Operations, and Audit tabs;
  remove technical provider configuration from Tenant navigation.
- [x] Phase 5: move Square, Twilio, OpenAI, ManleAI Calendar technical config,
  and scheduling-authority controls to fixed Platform tenant routes with exact
  delegated capabilities and no owner impersonation.
- [x] Phase 6: add Platform Operations recovery, masked delivery/Square webhook
  evidence, immutable audit, and V70 versioned/idempotent AI-runtime control.
- [x] Phase 7: require active tenant membership at runtime; keep POS Calendar
  tenant-scoped through safe Business/readiness projections; serve independent
  `/s/[slug]` landing pages.
- [x] Phase 8: run API/worker with a separate non-owner/non-`BYPASSRLS` role;
  apply/reset actor context per DB operation; enforce tenant RLS; give public
  scope zero base-table visibility; enforce exact Platform PII scope on all
  customer/call/appointment/notification tables.
- [x] Phase 9: persist per-tenant quotas/usage and fair worker claim caps;
  return bounded `429`/`Retry-After` for exhausted provider/voice/scheduling
  work without affecting other tenants.
- [x] Phase 10: add V63-V72 migration/replay/security ownership to the release
  gate, update Compose runtime/migration credential separation, runtime-role
  initialization, deployment/runbook docs, and codebase routing map.
- [x] Phase 11: add V73 schema-only `live|sample_test` classification, a
  separate checksum-ledger Lotus fixture runner, three marked sample accounts,
  one marked sample tenant, fail-closed live-data collision checks, visible
  dashboard labels/filters, and release-gate integration ownership. Normal
  startup migration remains fixture-free.
- [x] Phase 12: add V74 immutable `tenant|platform` principal scope; fail the
  migration on historical mixed identities; enforce Tenant ownership,
  memberships, and legacy roles versus Platform roles, delegation, and PII
  grants with database foreign keys; split Platform/Tenant account-search APIs;
  make auth/session routing scope-owned; and bootstrap a dedicated Platform
  identity without promoting a Tenant login.
- [x] Phase 13: add V75 Owner-reviewed Platform support authorization for exact
  Services, AI Training, and Calls capabilities. Require current base Platform
  Admin permission or exact-salon Ops assignment plus the active Owner approval;
  require request-linked Calls PII; enforce 30-day non-PII and 24-hour Calls
  bounds, immediate revoke/expiry/role-assignment invalidation, per-action
  actual-actor audit, feature RLS, and select-only call-linked scheduling
  evidence without general Appointments access or writes.
- [x] Phase 14: add V76 control-plane authority correction. Platform Admin now
  has direct role-capability access to every salon Platform tab including PII;
  Admin grants/revokes time-bounded feature/PII access to assigned Platform Ops;
  Tenant Owner approval endpoints/UI are removed; Admin can suspend/reactivate
  the Owner membership without changing `salons.owner_user_id`; every Tenant
  salon route checks current exact membership; Platform users support direct
  create/edit/password/role/status with session revocation, idempotency, audit,
  and last-active-Admin protection.
- [x] Phase 15 expand release: preserve the authenticated Platform identity
  throughout Calls status/simulator/scheduling access; make Twilio/OpenAI live
  runtime and technical reads strictly salon-database-backed; add V78
  `app.system_salon_id`, provider-only salon-ID locators, and bind resolved
  provider plus claimed worker item operations to one salon.
- [x] Phase 15 contract preparation: V79 moves global worker
  claim/recovery/cleanup/retention discovery behind narrow worker-only database
  functions, binds selected items to one salon before ordinary repository work,
  and enforces composite salon/session ownership for call children. V79 leaves
  provider/worker base RLS unchanged for rollback compatibility.
- [x] Phase 15 contract release implementation: V80 requires matching
  `app.system_salon_id` in provider/worker base RLS, rewrites the direct
  Calls/Support system branches, audits the final PostgreSQL policy catalog,
  and adds runtime-role negative tests for unbound and cross-tenant base-table
  access. V80 remains blocked from production cutover until the V79-aware image
  is deployed on every replica and all provider/worker paths are observed.
- [ ] Phase 15 contract production cutover: record the exact V79-aware image,
  drain every older API/worker replica, observe all provider callbacks and
  worker classes, then apply V80 and verify post-migration negative tests plus
  operations health. Rollback may use only a declared V79-aware image.
- [x] Restore and reuse the full existing `ServicesDashboard`, `CallsDashboard`,
  `SettingsDashboard`, and `TrainingDashboard` on their Tenant routes; reuse the
  same Services/Calls/Training components through explicit Platform surface
  adapters. Preserve category management, category aliases, service aliases,
  corrections, lifecycle actions, and rich page states instead of replacing
  them with reduced restructuring dashboards.
- [x] Keep service aliases and category aliases as canonical structured children
  managed inside Services. An explicitly reviewed Training correction may write
  a canonical service alias, but neither alias type becomes free-text knowledge.
- [x] Remove the reduced duplicate Services editor from Platform Business and
  make the full reused Services tab the canonical Platform workflow. Keep the
  legacy Business service/category API paths compatibility-safe behind the same
  Services gate and fail-closed per-action audit. Admin access is direct; Ops
  access is exact-salon and Admin-granted.
- [x] Make legacy POS service/category repository reads accept the authorized
  actual Platform actor without substituting the salon Owner ID; require exact
  Services write authorization at the AI-bookable mutation fence and preserve
  actual-actor attribution.
- [x] Keep provider configuration, provider identifiers, sync/webhook/model
  diagnostics, and test controls in Platform Technical. Reused Business
  dashboards consume only bounded business-safe readiness projections.
- [ ] Before production-live cutover, replace/reset the pre-live fixture
  database, set the protected deploy profile to `live`, remove the sample
  secret/reset approval, run only normal migrations, and require the CI/CD live
  guard to prove zero sample rows and no `sample_data_migrations` table; then
  verify owner bootstrap is available.
- [ ] Before production cutover, provision/rotate the existing database runtime
  role, validate API/worker startup with RLS enforced, run the tag release gate,
  witness per-tenant Square/Twilio/OpenAI readiness and callbacks, and complete
  the existing backup/restore/load/on-call approvals. Code readiness does not
  establish those external operational facts.

## Owner-First Scheduling Authority Milestone

Phase 0 documentation/additive V46 compatibility fields, the Phase 1 dispatch
boundary, the Phase 2 `owner_manual` request workflow, and the Phase 3
`manleai_calendar` configuration/readiness foundation are implemented. Phase
4A adds verified staff-only availability and atomic single-guest create, and
Phase 4B adds structured multi-guest/multi-service pooled all-or-none
availability/create, and Phase 4C adds target-origin whole-root internal
reschedule/cancel with V51 lifecycle guards and durable replay evidence.
Phase 2 adds request-only availability, durable owner-review book/reschedule/
cancel requests, additive neutral APIs, conversation linkage and safe wording,
and the Appointments owner-review queue. Phase 3 then implements the separate
`manleai_calendar` configuration/readiness foundation. Phase 4C keeps
capabilities operation-specific: all six may be true only when their exact
selected-authority, current activation/config, and service-policy predicates
pass, and aggregate `execution_ready` is true only when every capability is
true. That new-work readiness view is distinct from historical target-origin
dispatch after a later authority change. Authority switching is a separate
workflow. Owner-notification delivery is implemented as owner-operational SMS
with V56 migration compatibility, provider-neutral processing, Twilio
isolation, API/UI states, safe retry/unknown-outcome policy, and focused
validation. Customer appointment SMS is implemented separately by V59 and
`modules/customer_notification`: it remains disabled by default, requires
explicit per-destination consent, and uses its own source-fenced delivery
ledger rather than reusing owner-notification consent or state.

- [x] Resolve owner-scoped current and originating scheduling authority: use `salon_settings.scheduling_authority` only for genuinely new availability/create, use persisted operation/retry/target origins for historical work, require cross-source equality for appointment mutations, dispatch availability/create/reschedule/cancel through `scheduling.Service`/`Executor`, and keep candidates plus create/cancel replay as provider-free history delegation.
- [x] Keep exact protocol support limited to `owner_manual`, `manleai_calendar`, and `external_provider`, with tenant-scoped validation and no inference from integrations, provider fields, UI copy, or caller wording.
- [x] Implement `owner_manual` as a deduplicated pending owner-review request workflow for book, reschedule, and cancel: persist the request/ordered segment/event aggregate, call-session link, and deduplicated queued owner-notification outbox row in one database transaction; make exact operation and transition replay idempotent through unique keys, row locks, fingerprint checks, expected-version compare-and-swap, and append-only events; expose request-only availability, neutral action/list/detail/transition APIs, safe conversation wording, and the responsive Appointments queue; never automatically confirm or mutate an appointment, invent provider/POS/reconciliation evidence, or claim external notification delivery.
- [x] Implement Phase 5H conversation booking modes independently of scheduling authority: `pending_approval` verifies selected internal/external availability but creates one non-reserving owner-review request; `confirmed_booking` retains the authority-native confirming executor; `disabled` performs zero origin-free scheduling calls; `owner_manual + confirmed_booking` remains invalid; exact persisted operations replay through their original authority before current-policy gating.
- [x] Fence final conversation review to both `booking_mode` and selected scheduling authority; require a new full review after either changes; preserve ordered party/service/staff/requested-time data for pending requests while stripping execution proof; persist the optional target authority through V55 without backfilling legacy NULL requests; expose booking mode in AI Settings and target/non-reservation evidence in Appointments owner review.
- [x] Implement the Phase 3 `manleai_calendar` configuration/readiness foundation: persist V48 root policy and monotonic fence, `local_override` hours, independent service/staff eligibility, staff schedules, service capacity policies, optional resource pools/requirements, scoped exceptions, version-fenced activation audit, and immutable events; enforce owner/tenant scope, action-key replay, expected-version conflict handling, and overlap constraints; expose owner-scoped aggregate/child APIs plus Settings/Staff/Services management and Appointments readiness UI; keep configuration/activation distinct from execution and authority switching.
- [x] Implement Phase 4A staff-only availability and atomic create: register the executor; expose granular staff-only capabilities while aggregate `execution_ready=false`; use canonical services/staff, V48 policy, local hours, schedules, buffers, exceptions, strict DST and half-open conflict planning; fail closed on incomplete external conflict evidence; persist V49 authority/config-fenced quote, internal attempt/appointment/service/event evidence with null POS fields; revalidate under the shared salon lock; require durable appointment and attempt IDs; and return exact committed replay after response loss.
- [x] Integrate Phase 4A with conversation and Appointments: use authoritative capability/readiness context, keep internal party/multi-segment/lifecycle actions gated, refresh only typed stale quote evidence without false confirmation, reject provider-shaped internal results, preserve external guidance behavior, and expose a responsive staff-only create flow with exact-operation retry and durable-ID confirmation.
- [x] Implement Phase 4B pooled-capacity evaluation and all-or-none party create: normalize ordered quantity-one guest/service units; plan same-guest services sequentially and different guests concurrently only when staff/resources fit; assign `anyone` deterministically; return one exact aggregate quote; revalidate authority/config/activation/catalog/schedule/staff/resource/capacity fences; and commit one root appointment plus every child service/resource row or nothing, without changing external-provider behavior.
- [x] Add V50 database guards over the V49 ledger: quote resource integrity, exact guest-reference/party-size invariant, sorted pool locks, base/override capacity probes, concurrent over-capacity prevention, exact bidirectional quote/attempt/appointment/event graph equality, and immutable consumed quote/committed-book history without a second reservation ledger.
- [x] Integrate Phase 4B with conversation and Appointments: use one aggregate party availability/action proof, reject partial confirmed children, reopen the complete draft after a typed resource/quote conflict with zero confirmation, preserve exact root replay, retain the external-provider per-child contract, and expose capability-gated structured guest/service/pooled-resource review in the responsive create flow.
- [x] Implement Phase 4C internal reschedule/cancel with target-origin routing after a current-authority change; exact target/version and open-cutoff validation; quote-backed whole-root replan that preserves party/service/guest shape; quote-free cancel with exact old-plan snapshot; atomic release and one-version same-root transition; immutable historical event replay; current-plan-only hydration; zero-active-child terminal cancel; durable status/version/active-child result validation; and no provider/POS evidence.
- [x] Add V51 database guards for release ownership, immutable root provenance and party identity, exact one-version transitions, unique contiguous versioned events, cancelled-terminal state, exact reschedule quote/attempt/new-plan equality, exact cancel old-plan snapshot, and zero active children after cancellation.
- [x] Integrate Phase 4C with conversation and Appointments: require explicit state-scoped lifecycle authorization, an exact reviewed reschedule quote or a cancellation reason with no quote, stale-target refresh/re-offer without success wording, cutoff-safe handoff, exact event replay after later mutations, complete internal-origin row evidence, fail-closed client cutoff handling, locked exact retries for uncertain submissions, and durable response graph/status/version checks before success copy.
- [x] Preserve the existing Square `external_provider` booking ID/version, quote, fence, idempotency, unknown-outcome, retry, reconciliation, webhook, and token-security safeguards unchanged behind the authority-neutral boundary; route Square test writes through the facade, expose/gate readiness by current authority, sanitize gate errors, and fence external lease/calendar mutation while retaining provider/connection-scoped webhook repair for historical external mirrors.
- [x] Keep party and multi-segment operations all-or-none: pending only for `owner_manual`, one atomic root/child commit for `manleai_calendar`, and complete provider success or safe rollback/reconciliation for `external_provider`.
- [x] Add the V52-V55 explicit owner-reviewed authority switch workflow with readiness preview, dry-run blockers, authority-version and concurrency fences, exact replay/conflict handling, immutable run/event audit, and an explicit inverse-run reference; integration connect/sync/import/webhook actions do not switch authority implicitly.
- [x] Preserve immutable originating authority and authority-native evidence for appointments, attempts, quotes, and segments so historical retry, reschedule, cancellation, reconciliation, and webhook work cannot cross authorities.
- [x] Add Phase 4A/V50/V51 internal execution evidence without overloading `active_pos_provider` or populating fake `pos_*` fields: authority/config-fenced normalized quotes, internal attempt/appointment/segment/resource/event ownership, staff overlap and resource-capacity guards, exact graph equality, released-child ownership, versioned lifecycle events, and exact historical replay identity.
- [x] Keep cross-authority non-success state authority-native: `owner_manual` persists versioned pending owner review without execution side effects; `manleai_calendar` fails or atomically rolls back with typed quote/capability/target conflicts and no provider evidence; `external_provider` retains fallback, unknown-outcome, safe-retry, rollback and reconciliation evidence; V52 switch runs persist preview-ready/blocked and committed state with immutable events and explicit inverse-run references instead of overloading any scheduling outcome.
- [x] Complete the Owner-first dashboard authority workflow in Settings with explicit target selection, readiness preview/review, blocked and stale-conflict handling, commit progress, exact replay, latest-run recovery, and explicit inverse-switch context; preserve the request queue, configuration surfaces, readiness, internal create, and Phase 4C lifecycle controls in their owning workflows.
- [x] Update conversation answer context and internal create/lifecycle flows to use selected-authority or target-origin canonical data and backend capability evaluation without broadening the external-provider catalog path.
- [x] Make voice readiness Owner-first: expose separate phone-answering, request-capture, and automated-booking dimensions; fence them to the current scheduling authority/version and booking mode; source capability blockers from the authority owner; keep `owner_manual` request-only; and treat `phone_booking_ready` as a compatibility alias without making Square a prerequisite for owner/internal authorities.
- [x] Update public catalog publishing and public reads to use selected-authority
  data without making Square or staff a universal prerequisite; fence publish
  against authority changes and fail stale pages closed.
- [x] Complete the AI-bookable consumer audit: conversation answer context,
  configuration transfer/onboarding, consultation eligibility, voice readiness,
  public catalog, Services/Staff controls, Settings consultation counts,
  dashboard summaries, Appointments, and POS Calendar now use the selected
  authority, persisted origin, authority-owned catalog/configuration, or an
  explicitly external-only workflow; canonical owner/internal records do not
  require fake POS links, while unknown authority fails closed.
- [x] Add the owner-scoped Calls `scheduling_result_evidence` projection:
  `owner_manual` exposes reviewed request/target provenance as nonconfirming;
  `manleai_calendar` requires its complete durable appointment/attempt/status/
  version/active-child graph; `external_provider` requires a succeeded attempt
  plus exact mirror/current graph; split results require every root; historical
  exact results remain noncurrent; and raw session IDs or outcome text never
  infer confirmation. Unit coverage exists, and
  `backend/modules/conversation/scheduling_result_evidence_integration_test.go`
  covers valid owner-scoped GET/list, partial-graph fail-closed behavior, and
  cross-owner non-leak. The integration test code exists, but its final fresh
  V1-through-V62 PostgreSQL execution remains pending under the already-
  unchecked rerun gate above.
- [x] Make frontend authority-evidence validation executable rather than
  typecheck-only: `frontend` runs appointment and Calls evidence helper tests,
  `pos-calendar` runs its scheduling-evidence helper tests, both expose
  `npm run test:evidence` through the shared
  `deploy/run-ts-evidence-tests.mjs` runner, and CI executes those suites for
  the two applicable web apps before typecheck/build.
- [x] Cover Phase 4A with V49 safety tests, real-PostgreSQL repository concurrency/replay/fence/conflict tests, executor/planner/handler/service tests, conversation golden/counterexample/replay/no-partial-write tests, frontend deterministic helper checks, typecheck/build, and Go race checks.
- [x] Cover Phase 4B with `backend/migrations/v50_safety_test.go`, aggregate planner/executor tests, the real-PostgreSQL aggregate party/pooled replay/conflict and guest-invariant cases in `execution_repository_integration_test.go`, and `backend/modules/conversation/manleai_calendar_party_scheduling_test.go` for aggregate allocation, partial-child rejection, resource-conflict zero confirmation, counterexamples, and exact replay; validate frontend aggregate payload/evidence helpers with typecheck/build.
- [x] Keep named Phase 4B regression anchors: `TestV50CapacityOverridesExactGraphRollbackAndHistory`, `TestV50ConcurrentReversePoolOrdersSerializeWithoutOvercapacity`, `TestPlanAggregateAvailabilityExpandsPartyUnitsAndAssignsAnyoneDeterministically`, `TestPlanAggregateAvailabilityPreservesSequentialServicesPerGuest`, `TestExecutionRepositoryPostgresAggregatePartyPooledEvidenceReplayAndAtomicConflict`, `TestManleAICalendarPartyRejectsPartialConfirmedChildren`, `TestManleAICalendarPartyResourceConflictReopensDraftWithZeroConfirmation`, and `TestManleAICalendarPartyExactReplayUsesSameRootOperation`.
- [x] Cover Phase 4C with V51 migration shape/application/atomic-history tests, lifecycle planner and cutoff unit tests, real-PostgreSQL whole-root switched-authority replay tests, conversation request/candidate/golden/reason/replay/stale/cutoff/no-provider-evidence tests, and frontend typecheck/build validation.
- [x] Cover Phase 5H with the six-row scheduling matrix, replay-after-mode-change and legacy-NULL counterexamples, disabled pre-planner zero-call coverage, structured pending-party fidelity, V55 migration/order safety, real-PostgreSQL request target/event/notification/replay evidence, Twilio pending-external golden flow, frontend typecheck/build, and focused Go race checks.
- [x] Keep named Phase 4C regression anchors: `TestV51DefinesTenantFencedLifecycleGraphWithoutProductHardcoding`, `TestV51AppliesAfterV50`, `TestV51LifecycleAtomicHistoryAndAuthoritySwitch`, `TestLifecycleCutoffNullDisabledAndEqualityClosed`, `TestReplacementPreservesPartyServiceUnitAndGuestMapping`, `TestHistoricalTargetPlannerDoesNotRequireCurrentAuthority`, `TestExecutionRepositoryPostgresWholeRootLifecycleSwitchedAuthorityHistoricalReplay`, `TestInternalLifecycleSchedulingActionRequestPreservesWholeRootVersionAndShape`, `TestInternalLifecycleCandidateMappingPreservesBackendSourceOfTruth`, `TestInternalLifecyclePhoneGoldenRescheduleAfterCurrentAuthoritySwitch`, `TestInternalLifecyclePhoneGoldenCancelReasonAndMinimalAction`, `TestInternalLifecycleCancellationReasonPassesFullTurnKernelWithoutPhraseMatching`, `TestInternalLifecycleExactRescheduleReplaySkipsAvailabilityRefreshAfterLaterCancel`, `TestInternalLifecycleStaleTargetReoffersAndCutoffNeverConfirms`, and `TestInternalLifecycleRejectsProviderOrPartialConfirmationEvidence`.
- [x] Keep named persisted fuzzy-confirmation regression anchors: `TestFuzzyServiceGoldenRequiresExplicitConfirmationAcrossSchedulingAuthorities`, `TestFuzzyServiceConfirmationWrongStateInputsNeverBook`, and `TestFuzzyServiceConfirmationEventReplayIsIdempotent`.
- [x] Prove the V52-V55 authority-switch slices with operation/action replay and changed-reuse conflicts, unique durable run/event evidence, owner/tenant fencing, stale source/readiness/CAS fences, dependency/readiness failure, concurrent commit and live-external-lease conflicts, explicit inverse-run audit, historical target/retry dispatch after a switch, and conversation/UI contracts that do not turn preview, blocked, conflict, or commit state into confirmed/rescheduled/cancelled appointment wording.
- [x] Complete provider-neutral recurring-job/queue observability and owner-notification delivery without labeling internal scheduling failures as POS errors or exposing provider secrets, message IDs, full destinations, message bodies, or raw errors in owner-facing records.

## Twilio Voice tenant-bound rollout

- [x] Add V83 expand-only route identity, canonical active inbound-number
  uniqueness, provider-only tenant locator, verified-route audit/fingerprint
  indexes, and safety plus PostgreSQL integration coverage.
- [x] Add tenant-bound incoming/turn/recording/stream/status/fallback routes;
  bind provider tenant context before config decryption or tenant mutation;
  require route, inbound number, Account SID, durable CallSid ownership where
  present, and official exact-URL/all-form signature verification; return one
  non-enumerating rejection for every mismatch.
- [x] Make exact first-webhook retry return one durable call session and one
  initial transcript, reject changed/cross-tenant CallSid reuse, and retain
  only bounded routing evidence without token, signature, or provider body.
- [x] Add Platform Technical child controls, computed read-only URLs, distinct
  `routing_configured` and current-fingerprint `live_verified` status, route
  blockers/conflict handling, and destination-preserving configuration
  transfer behavior.
- [ ] Run the documented preflight against the production migration target and
  resolve every invalid or duplicate active E.164 value without inferring from
  `salons.phone`.
- [ ] Configure every production Twilio number with its exact tenant-bound URL,
  place a live call, verify `call_sessions.salon_id`, and retain matching
  `last_verified_inbound_at` operator evidence for every tenant.
- [ ] After the operational migration and rollback window, ship the contract
  release that unmounts shared Twilio Voice routes and removes Voice ingress
  phone fallback. Do not claim the fleet-wide tenant-routing guarantee before
  both this item and the per-tenant live-evidence item are complete.

## OpenAI tenant-bound rollout

- [x] Add V84 credential uniqueness identity/revision, fixed destination
  profile, durable verification runs/capabilities/events, immutable safe audit,
  exact-tenant RLS, and bounded worker claim contract.
- [x] Require a tenant resolver for the production OpenAI adapter; reject blank
  and cross-tenant resolution before network; remove plaintext credentials from
  circuit identities; allow only official HTTPS/WSS destination with no
  redirects, environment proxy, or unsafe DNS/dial target.
- [x] Decouple Twilio ingress from OpenAI configuration so missing/unreadable
  OpenAI falls back to recording/Gather rather than blocking the tenant-bound
  webhook.
- [x] Add Platform Technical managed-destination, runtime-resolvable blockers,
  credential revision/uniqueness state, explicit async verification, and
  truthful saved-versus-live-verified copy.
- [x] Ship Platform Configuration Transfer schema v10 with v9 input
  compatibility; transfer only portable OpenAI model/voice settings and
  preserve/exclude target credential, destination, enabled state, and
  verification evidence.
- [ ] Apply the production preflight and save every active tenant OpenAI row so
  it has canonical destination and credential identity/revision.
- [ ] Resolve every `OPENAI_CREDENTIAL_TENANT_CONFLICT` by rotating one tenant
  key; never disable the uniqueness index or copy identity fields.
- [ ] Queue a live verification for every active tenant, retain fresh success
  for all required capabilities, then witness a bounded real call. CI and
  release self-tests must remain live-provider-free.
