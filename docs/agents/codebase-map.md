# Codebase Map

This map is mandatory routing context for agents and subagents. Read it before
triage, diagnosis, planning, implementation, review, or subagent handoff.

The map accelerates search and root-cause work. It does not replace code
inspection. When this file conflicts with code, the code wins and this file must
be updated in the same approved scope.

## Mapping Drift Guard

Update this file when a change adds, updates, deletes, renames, or moves:

- Feature behavior, workflow ownership, or source-of-truth rules.
- Backend routes, handlers, services, repositories, provider adapters, DTOs,
  migrations, Ent schemas, or tests.
- Frontend routes, pages, feature components, API helpers, UI states, types, or
  reusable UI/layout components.
- Landing routes, public API contracts, or public catalog presentation.
- Utilities, helpers, runtime config, validation, middleware, encryption,
  logging, or response helpers.
- Agent rules, skills, subagents, documentation routing, validation workflow, or
  triage keywords.

Every code-changing final response must include mapping impact:

- `Mapping impact: updated docs/agents/codebase-map.md` when the map changed.
- `Mapping impact: none` plus the reason when the map did not need a change.

Subagent outputs must classify mapping impact as `map update required`, `map
already accurate`, or `map conflict found`.

Keywords in this file are for triage/search only. Do not copy them into runtime
business logic, conversation rules, approval gates, service matching, or
production feature behavior.

## Fast Triage Rules

1. Start with the triage keyword table below.
2. Read the listed source docs and owner files before making behavior claims.
3. Follow the owning path: UI/page or API route -> handler -> service ->
   repository/provider -> schema/migration -> tests.
4. Keep POS-first booking safety explicit: confirmed appointments require a
   successful active POS booking ID.
5. If the request mentions UI placement, identify actor workflow, operational
   object, source of truth, and neighboring controls before choosing placement.
6. If the request mentions voice/conversation, map caller goal, known fields,
   missing fields, service/category ambiguity, availability gate, booking gate,
   and handoff gate.
7. For provider setup or active-runtime diagnosis, treat dashboard-managed
   `salon_integration_configs` as the source of truth. Start with
   `/dashboard/integrations`, `GET /api/salons/:id/integration-configs`, the
   relevant readiness/debug endpoint, persisted provider state, and the runtime
   resolver. Never use env files, Compose defaults, GitHub secrets, or process
   environment values as evidence of active salon configuration. Inspect
   legacy environment fallback only when the task explicitly targets that path
   and evidence proves the salon has no stored provider config.

## Function And Helper Lookup Contract

This map routes agents to the owning feature, module, UI surface, and helper
cluster. Before making a claim about a specific function/helper or editing it,
inspect the concrete symbols in code:

- Go functions/types: `rg -n "^(func|type) " backend/modules backend/internal backend/cmd`.
- Frontend components/helpers: `rg -n "^(export (async )?function|export function|export const|function |const [A-Z])" frontend landing pos-calendar -g '!**/node_modules/**' -g '!**/.next/**'`.
- API calls from UI: `rg -n "apiRequest<|apiRequest\\(" frontend pos-calendar -g '!frontend/node_modules/**' -g '!frontend/.next/**' -g '!pos-calendar/node_modules/**' -g '!pos-calendar/.next/**'`.
- Routes: `rg -n "func RegisterRoutes|api\\.|app\\." backend/modules backend/cmd/api/main.go`.
- DTOs: `rg -n "type .* struct|export type|export interface" backend/modules frontend/types landing/lib pos-calendar/types`.

When a change creates, renames, deletes, or changes the ownership or triage
meaning of a function/helper/component, update the relevant map section and
triage keyword table.

## Source Docs

- Product terms and invariants: `CONTEXT.md`.
- Architecture and module ownership: `docs/architecture.md`.
- POS provider boundary: `docs/pos-adapter-layer.md`.
- Square integration: `docs/square-integration.md`.
- API surface: `docs/api.md`.
- Deployment/runtime config: `docs/deployment.md`.
- Production readiness scope: `docs/production-readiness-checklist.md`.
- UI/UX contract: `DESIGN.md`.
- Live phone demo setup: `docs/agents/phone-booking-demo-config-memo.md`.

## Backend Entrypoints

- API server: `backend/cmd/api/main.go`.
- Worker: `backend/cmd/worker/main.go`.
- Twilio local simulator: `backend/cmd/twilio-sim/main.go`.
- Startup migrations: `backend/internal/database/migrate.go` and
  `backend/migrations/*.sql`.
- Ent schema mirror: `backend/ent/schema/*.go`.

## Production Runtime And Release

- Release workflow: `.github/workflows/ci-cd.yml`.
- Runtime stack: `docker-compose.prod.yml`; API owns startup migrations and the
  worker starts only after API health with `AUTO_MIGRATE=false`.
- Container targets: `backend/Dockerfile` targets `api` and `worker`.
- Local stack: `docker-compose.yml` must build the backend API with
  `target: api`; without an explicit target the multi-stage Dockerfile ends on
  the `worker` stage and the API healthcheck cannot succeed.
- Shared edge route and manifest templates:
  `deploy/manleai.caddy.template`, `deploy/manleai.edge-manifest.template`.
- VPS Caddy ownership: `project-edgectl validate/upsert manleai` manages only
  the ManleAI route under `/etc/caddy/projects`; never modify the Caddy root
  config from the release workflow.

## Backend Module Map

| Area | Owner files | Responsibilities | Tests |
| --- | --- | --- | --- |
| Auth and sessions | `backend/modules/auth/*`, `backend/internal/middleware/auth.go` | Login, refresh, bootstrap owner, JWT auth, user/salon claims | `backend/modules/auth/service_test.go` |
| Salon profile/settings | `backend/modules/salon/*` | Salon CRUD, settings, AI tone, public catalog settings, imported business hours | `backend/modules/salon/service_test.go` |
| POS provider-neutral layer | `backend/modules/pos/*` | `POSProvider` contracts, typed provider-write outcome/phase errors, optional appointment listing capability, POS entity links, service/staff/customer catalog, sync jobs/logs/errors, provider switching, category taxonomy, category aliases | `backend/modules/pos/service_test.go`, `backend/modules/pos/sync_processor_test.go` |
| Square adapter | `backend/modules/pos_square/*` | Square OAuth, locations, sync, Square payloads, salon-local availability ranges, booking list import, token refresh, provider write outcome/error mapping | `backend/modules/pos_square/*_test.go` |
| Booking | `backend/modules/booking/*` | Active-provider-scoped new booking/availability resolution, historical appointment-provider actions, durable booking operation claims/fingerprints/leases, confirmed appointments, fallback pending, unknown-result reconciliation gates, backend retry policy, calendar range/sync APIs, reschedule, cancel, POS idempotency, POS error writes | `backend/modules/booking/service_test.go`, `backend/modules/booking/repository_integration_test.go` |
| Customers | `backend/modules/customer/*` | Canonical customer CRUD, archive, search, activity read model, provider customer lookup facade | `backend/modules/customer/service_test.go` |
| Conversation runtime | `backend/modules/conversation/*` | Simulator/phone session state, intent, slot preservation, service understanding, answer routing, booking tool routing, party booking planning, handoff, transcript metadata, retention | `backend/modules/conversation/*_test.go` |
| Training | `backend/modules/training/*` | Knowledge items, owner corrections, correction apply/dismiss, service alias application, training evaluation | `backend/modules/training/service_test.go` |
| Voice provider-neutral | `backend/modules/voice/*` | Voice readiness/status, inbound routing, STT/LLM/TTS/realtime and streaming-speech interfaces, speech turns, audio output | `backend/modules/voice/*_test.go` |
| Twilio adapter | `backend/modules/voice_twilio/*` | Twilio signatures, form parsing, TwiML, recording mode, Media Streams bridge | `backend/modules/voice_twilio/*_test.go` |
| OpenAI voice adapter | `backend/modules/voice_openai/*` | OpenAI STT, guarded reply, whole-response TTS, dedicated streaming Speech-to-PCMU conversion, Realtime input session | `backend/modules/voice_openai/*_test.go` |
| Integration config | `backend/modules/integration_config/*` | Salon-scoped Square/Twilio/OpenAI runtime settings, encrypted secrets, dashboard-managed provider config | `backend/modules/integration_config/service_test.go` |
| Configuration transfer | `backend/modules/config_transfer/*` | Sanitized export/import preview/apply, stable request IDs, skip secrets/operational records | `backend/modules/config_transfer/*_test.go` |
| Public catalog API | `backend/modules/public_catalog/*` | Public-safe salon catalog read endpoints | `backend/modules/public_catalog/service_test.go` |

## Backend Helper And Utility Map

| Helper area | Files | Use when triaging |
| --- | --- | --- |
| Server/bootstrap defaults | `backend/internal/config/config.go` | Server ports, CORS, JWT, and provider model normalization; active salon provider settings still resolve from dashboard-backed integration config |
| Database/migrations | `backend/internal/database/database.go`, `backend/internal/database/migrate.go`, `backend/migrations/*.sql` | Schema drift, startup migrator, migration checksums, version ordering |
| Encryption | `backend/internal/encryption/encryption.go` | POS/provider secret encryption, AES-GCM token handling |
| Auth middleware | `backend/internal/middleware/auth.go` | Tenant/user claims, protected routes, salon ownership context |
| Response helpers | `backend/internal/respond/respond.go` | API response/error shape |
| Validation helpers | `backend/internal/validation/validation.go` | Required fields and phone normalization |
| Logger | `backend/internal/logger/logger.go` | Structured logger initialization |

## Conversation Function Map

Use this file cluster when a defect mentions caller wording, service selection,
name collection, availability replies, party bookings, or transcript output.

- Main service orchestration: `backend/modules/conversation/service.go`.
- Availability replies and offered slots:
  `backend/modules/conversation/service_availability.go`.
  Availability invalidation compares complete ordered segment requests and
  deep-cloned before/after session state; offered-slot selection validates
  service and specific-staff assignments. Deterministic availability-question
  evidence overrides a conflicting semantic current-booking summary so an
  in-progress request either repeats valid offered slots or rechecks the active
  provider for the requested date.
- Booking tool flow and terminal booking wording:
  `backend/modules/conversation/service_booking_flow.go`.
- Intent, reschedule, cancel, and appointment-target selection:
  `backend/modules/conversation/service_intent.go`.
- Service extraction, matching, parsing, and understanding:
  `backend/modules/conversation/service_extraction.go`,
  `backend/modules/conversation/service_matching_parsing.go`,
  `backend/modules/conversation/service_understanding.go`,
  `backend/modules/conversation/conversation_act.go`,
  `backend/modules/conversation/service_datetime_confirmation.go`,
  `backend/modules/conversation/turn_reducer.go`, and
  `backend/modules/conversation/next_action_planner.go`.
  Configured production turns enter a multi-act/question semantic contract
  without a keyword gate. Catalog validation owns referenced service/staff IDs;
  the reducer owns draft mutation and dependency invalidation; the planner owns
  missing-field, review, and booking readiness. Pending candidates remain
  contextual. Review authorization and a resolved same-category guest-scope
  clarification are deterministic state-owned controls that cannot be overwritten
  by semantic interpretation; correction/negation evidence prevents review
  authorization. Phone customer-name collection confirms bare ASR candidates
  before persisting them unless the caller explicitly introduces a non-risky name
  or spells it. Provider failure preserves the draft with safe clarification.
  A new-booking date/time correction made after slots were offered is persisted
  as a typed pending confirmation; rejection preserves the original slots, and
  confirmation invalidates them and performs a fresh provider availability
  check.
- Versioned active dialog state and cloning:
  `backend/modules/conversation/conversation_dialog_state.go`,
  `backend/modules/conversation/draft_revision.go`,
  `backend/modules/conversation/types.go`,
  `backend/modules/conversation/repository.go`, and migration
  `backend/migrations/V36__conversation_dialog_state.sql`, and
  `backend/migrations/V37__conversation_draft_revision.sql`. `dialog_state`
  persists pending clarification, bounded mutation history, no-progress count,
  and draft/review/authorization revisions. Transcript metadata remains audit
  evidence, not active-state source of truth.
- Service consultation and safety handoff:
  `backend/modules/conversation/service_consultation.go`. Consultation reads
  only active-provider bookable `ServiceOption` facts, persists pending
  candidate IDs in transcript metadata, rejects ambiguous affirmatives, and
  routes health-suitability questions to owner handoff. Consultation candidate
  lifecycle uses explicit pending/cleared metadata; cancel, reschedule,
  handoff, and active party-plan actions retain routing precedence.
- Caller name, phone, email, and name-slot repair:
  `backend/modules/conversation/service_customer_name.go`.
- Party/group booking detection and planning:
  `backend/modules/conversation/service_party.go`,
  `backend/modules/conversation/service_party_intent.go`,
  `backend/modules/conversation/service_party_signal.go`.
- Spoken prompts, menu/service clarification, and catalog count/list questions:
  `backend/modules/conversation/service_prompts.go`.
- Answer-source routing and cached structured context:
  `backend/modules/conversation/answer_context.go`,
  `backend/modules/conversation/answer_router.go`.
- Summaries, handoffs, retention:
  `backend/modules/conversation/service_summary.go`,
  `backend/modules/conversation/retention_processor.go`.
- Typed voice-input recovery handoff and no-callback completion:
  `backend/modules/conversation/service_voice_recovery.go`. Confidence-rejected
  speech never becomes a synthetic customer turn; the fourth consecutive
  rejection may create one `voice_input_unintelligible` handoff only after a
  callable caller number is available. A stable per-session event key dedupes
  repeated or concurrent recovery execution.
- Persistence: `backend/modules/conversation/repository.go`.
- DTOs/state: `backend/modules/conversation/types.go`.
- Structured semantic-turn interpretation: `backend/modules/voice/act_interpreter.go`,
  `backend/modules/voice/types.go`, and
  `backend/modules/voice_openai/adapter.go`. Model-proposed acts are
  multi-act/question, confidence-gated, PII-reduced, and active-catalog
  validated inside conversation ownership; catalog category ambiguity remains
  authoritative over a model-proposed concrete target, bare in-progress service
  switches return to catalog-backed confirmation, and deterministic slot
  corrections cannot be discarded by a model-only summary. The interpreter
  cannot call booking tools.

## Frontend Route And UI Map

| Route | Page file | Main component | Data/API helpers |
| --- | --- | --- | --- |
| `/login` | `frontend/app/login/page.tsx` | `frontend/features/auth/login-form.tsx` | `frontend/lib/api/client.ts` |
| `/create-account` | `frontend/app/create-account/page.tsx` | `frontend/features/auth/create-account-form.tsx` | `frontend/lib/api/client.ts` |
| `/onboarding` | `frontend/app/onboarding/page.tsx` | `frontend/features/onboarding/salon-profile-form.tsx` | `frontend/lib/api/client.ts`, `frontend/lib/api/configuration-transfer.ts` |
| `/dashboard` | `frontend/app/dashboard/page.tsx` | `frontend/features/dashboard/dashboard-home.tsx` | status, voice, calls, appointments, attempts, services, staff APIs |
| `/dashboard/appointments` | `frontend/app/dashboard/appointments/page.tsx` | `frontend/features/dashboard/appointments-dashboard.tsx` | availability, booking attempts, reschedule, cancel |
| `/dashboard/calls` | `frontend/app/dashboard/calls/page.tsx` | `frontend/features/dashboard/calls-dashboard.tsx` | sessions, realtime events, party requests, owner corrections |
| `/dashboard/customers` | `frontend/app/dashboard/customers/page.tsx` | `frontend/features/dashboard/customers-dashboard.tsx` | customer CRUD, archive, search |
| `/dashboard/services` | `frontend/app/dashboard/services/page.tsx` | `frontend/features/dashboard/services-dashboard.tsx` | services, categories, category aliases, service aliases, AI bookable |
| `/dashboard/staff` | `frontend/app/dashboard/staff/page.tsx` | `frontend/features/dashboard/staff-dashboard.tsx` | staff CRUD, archive, AI bookable |
| `/dashboard/settings` | `frontend/app/dashboard/settings/page.tsx` | `frontend/features/dashboard/settings-dashboard.tsx` | salon profile, settings, AI tone, business hours, public catalog, config transfer |
| `/dashboard/training` | `frontend/app/dashboard/training/page.tsx` | `frontend/features/dashboard/training-dashboard.tsx` | knowledge items, owner corrections, evaluation |
| `/dashboard/integrations` | `frontend/app/dashboard/integrations/page.tsx` | `frontend/features/integrations/square-integration.tsx` | Square OAuth/status/sync/test booking, provider config, provider switching |
| `/dashboard/billing` | `frontend/app/dashboard/billing/page.tsx` | `frontend/features/dashboard/billing-dashboard.tsx` | static gated billing surface |

## POS Calendar Surface Map

| Route | Page file | Main component | Data/API helpers |
| --- | --- | --- | --- |
| `/login` | `pos-calendar/app/login/page.tsx` | `pos-calendar/features/auth/login-form.tsx` | `pos-calendar/lib/api/client.ts` |
| `/calendar` | `pos-calendar/app/calendar/page.tsx` | `pos-calendar/features/calendar/pos-calendar-client.tsx` | calendar range/sync, calendar event stream/toasts, availability, booking attempts, reschedule, cancel, Square status, services, staff |

The POS calendar app is a standalone authenticated Next.js app with no
dashboard sidebar. Local runtime port is `3091`; production domain is
`pos.knasoftware.com`. It reuses the same admin auth tokens as `frontend/`.
The Day view groups timed appointments into horizontally scrollable technician
lanes, keeps unassigned and referenced inactive technicians visible, and uses
ordered appointment segments when one booking has multiple technician
assignments. Calendar event titles across day, week, month, agenda, and the day
drawer use `time · customer · technician`; service names remain supporting
detail rather than part of the event title.

## Frontend Helper And Utility Map

- API client/session/refresh: `frontend/lib/api/client.ts`.
- Configuration transfer helpers:
  `frontend/lib/api/configuration-transfer.ts`.
- Environment config: `frontend/lib/config/env.ts`.
- Classname helper: `frontend/lib/utils/cn.ts`.
- Shared API DTOs: `frontend/types/api.ts`.
- Dashboard shell: `frontend/components/layout/app-shell.tsx`.
- Deferred loading wrapper: `frontend/components/layout/deferred-page.tsx`.
- UI primitives: `frontend/components/ui/*`.
- Booking display labels: `frontend/features/dashboard/booking-display.ts`.
- Legacy service/staff controls still present:
  `frontend/features/dashboard/service-staff-controls.tsx`.

## Landing Surface Map

- Landing home: `landing/app/page.tsx`.
- Public salon page: `landing/app/s/[slug]/page.tsx`.
- Public loading/not-found states: `landing/app/s/[slug]/loading.tsx`,
  `landing/app/not-found.tsx`.
- Public catalog API helper: `landing/lib/api.ts`.
- Public env config: `landing/lib/config.ts`.
- Public DTOs: `landing/lib/types.ts`.
- Visual asset: `landing/public/images/salon-hero.png`.

## Workflow Map

### POS And Square

- Read first: `docs/pos-adapter-layer.md`, `docs/square-integration.md`,
  `docs/api.md`.
- Backend owner: `backend/modules/pos`, `backend/modules/pos_square`,
  `backend/modules/integration_config`.
- Frontend owner: `frontend/features/integrations/square-integration.tsx`,
  `frontend/features/dashboard/services-dashboard.tsx`,
  `frontend/features/dashboard/staff-dashboard.tsx`,
  `pos-calendar/features/calendar/pos-calendar-client.tsx` for calendar sync.
- Data owner: `pos_connections`, `pos_entity_links`, `pos_sync_jobs`,
  `pos_sync_logs`, `pos_errors`, `salon_integration_configs`,
  `salons.active_pos_provider`.
- Tests: `backend/modules/pos/*_test.go`,
  `backend/modules/pos_square/*_test.go`,
  `backend/modules/integration_config/service_test.go`.
- Skill/subagent: `pos-adapter-slice`, `pos_backend_reviewer`,
  `security_privacy_reviewer`.

### Booking, Availability, Reschedule, Cancel

- Read first: `docs/architecture.md`, `docs/pos-adapter-layer.md`,
  `docs/api.md`, `CONTEXT.md`.
- Backend owner: `backend/modules/booking` with conversation callers in
  `backend/modules/conversation`.
- Frontend owner: `frontend/features/dashboard/appointments-dashboard.tsx`,
  `frontend/features/integrations/square-integration.tsx` for test booking,
	  `pos-calendar/features/calendar/pos-calendar-client.tsx` for standalone
	  calendar add/edit/delete and realtime booking toasts.
- Data owner: `booking_attempts` operation ledger (operation key, fingerprint,
  processing lease, provider outcome, retry policy, reconciliation status), `booking_attempt_segments`,
  `appointments`, `appointment_services`, `appointments.pos_sync_status`,
  `appointments.last_pos_synced_at`, `appointments.pos_sync_error`, `pos_errors`,
  `owner_notifications`.
- Tests: `backend/modules/booking/service_test.go`,
  `backend/modules/conversation/service_test.go`,
  `backend/modules/voice_twilio/phone_booking_flow_test.go`.
- Skill/subagent: `booking-safety-tdd`, `pos_backend_reviewer`,
  `production-release-review`.

### Voice, Twilio, OpenAI, Realtime

- Read first: `docs/api.md`,
  `docs/agents/phone-booking-demo-config-memo.md`,
  `docs/deployment.md`.
- Backend owner: `backend/modules/voice`, `backend/modules/voice_twilio`,
  `backend/modules/voice_openai`, `backend/modules/conversation`.
- Frontend owner: `frontend/features/dashboard/calls-dashboard.tsx`,
  `frontend/features/integrations/square-integration.tsx` for provider config.
- Data owner: `call_sessions`, `call_transcript_messages`,
  `voice_webhook_events`, `voice_audio_outputs`,
  `salon_integration_configs`.
- Realtime sequencing owner: `backend/modules/voice_twilio/handler.go`; provider-neutral
  contracts: `backend/modules/voice/types.go`; OpenAI input and output adapters:
  `backend/modules/voice_openai/realtime.go` and `speech_stream.go`; bounded
  confidence-rejection recovery policy: `backend/modules/voice_twilio/realtime_recovery.go`.
  The default
  dashboard mode `streaming_tts` uses Realtime only for input, sends backend-approved
  text to dedicated Speech TTS, converts WAV incrementally to PCMU 8 kHz, and forwards
  each chunk to Twilio before stream completion. Replies use a bounded FIFO,
  application request IDs, explicit cancellation, Twilio clear/mark, and stale-generation
  rejection. `buffered_realtime` is the legacy fallback that still binds provider
  response IDs and validates the complete output transcript before release. GA input
  requires transcription logprobs and applies
  profile-aware mean, low-tail, and VAD-coherence admission; confidence-rejected
  transcripts do not enter conversation state. Rejected transcripts use bounded
  in-stream recovery and do not trigger recording/gather fallback: short retry,
  scoped retry with the last approved question, noise coaching, then a typed
  `voice_input_unintelligible` owner callback handoff on the fourth consecutive
  rejection when the caller number is callable. Missing caller ID completes with
  call-again guidance without promising a callback. Accepted transcripts reset
  the streak. Noisy/balanced input also uses near-field noise reduction. Accepted
  and rejected transcript timing events retain PII-free decision/reason, profile,
  item ID, mean/min logprob, token count, VAD duration, rejection streak, and
  recovery action diagnostics when available. Speech output timing/failures retain correlation
  IDs, counts, and salted canonical hashes without transcript/audio bodies. The
  owner-scoped Calls timeline exposes these whitelisted diagnostics. Transcription steering is
  a concise salon/catalog/alias keyword list rather than conversational example
  sentences. Backend progress copy is delayed and limited to once per call.
  Terminal realtime failure resumes the last approved prompt through
  recording/gather while the session remains active; legacy preview sessions
  use a separate compatibility branch.
- Tests: `backend/modules/voice/*_test.go`,
  `backend/modules/voice_twilio/*_test.go`,
  `backend/modules/voice_openai/*_test.go`.
- Skill/subagent: `voice-ai-runtime`, `security_privacy_reviewer`,
  `repo_mapper`.

### Conversation, Service Understanding, AI Training

- Read first: `CONTEXT.md`, `docs/architecture.md`, `docs/api.md`.
- Backend owner: `backend/modules/conversation`,
  `backend/modules/training`, service/category APIs in `backend/modules/pos`.
- Frontend owner: `frontend/features/dashboard/training-dashboard.tsx`,
  `frontend/features/dashboard/services-dashboard.tsx`,
  `frontend/features/dashboard/calls-dashboard.tsx`.
- Consultation owner UI: service copy stays inside the existing Service edit
  form; per-call consultation candidates/state/result are shown in Calls >
  Detected details from transcript metadata.
- Data owner: `knowledge_items`, `owner_corrections`, `service_aliases`,
  `service_categories`, `service_category_aliases`,
  service consultation copy in `services.ai_description`, persisted
  `call_sessions.dialog_state`, and diagnostic metadata on
  `call_transcript_messages`.
- Tests: `backend/modules/conversation/service_understanding_test.go`,
  `backend/modules/conversation/service_understanding_eval_test.go`,
  `backend/modules/conversation/conversation_act_test.go`,
  `backend/modules/conversation/service_test.go`,
  `backend/modules/training/service_test.go`,
  `backend/modules/voice/act_interpreter_test.go`, and
  `backend/modules/voice_openai/adapter_test.go`.
- Skill/subagent: `voice-ai-runtime`, `salon-ops-workflow`,
  `business-logic-overlap-analysis`, `repo_mapper`.

### Services, Staff, Categories, Customers

- Read first: `docs/architecture.md`, `docs/api.md`,
  `docs/canonical-pos-ownership-checklist.md`.
- Backend owner: `backend/modules/pos` for services/staff/categories and
  `backend/modules/customer` for customers.
- Frontend owner: `frontend/features/dashboard/services-dashboard.tsx`,
  `frontend/features/dashboard/staff-dashboard.tsx`,
  `frontend/features/dashboard/customers-dashboard.tsx`.
- Data owner: `services`, `staff`, `customers`, `service_categories`,
  `service_category_aliases`, `service_aliases`, `pos_entity_links`.
- Tests: `backend/modules/pos/service_test.go`,
  `backend/modules/customer/service_test.go`.
- Skill/subagent: `salon-dashboard-ui`, `salon-ops-workflow`,
  `pos_backend_reviewer`, `frontend_product_reviewer`.

### Party Booking Requests

- Read first: `CONTEXT.md`, `docs/architecture.md`, `docs/api.md`.
- Backend owner: `backend/modules/conversation` party files plus
  `backend/modules/booking` for supported POS-backed booking.
- Frontend owner: `frontend/features/dashboard/calls-dashboard.tsx`.
- Data owner: `party_booking_requests`, session metadata, offered slots,
  booking attempts/appointments when POS-backed.
- Tests: `backend/modules/conversation/service_test.go`,
  `backend/modules/voice_twilio/phone_booking_flow_test.go`.
- Skill/subagent: `voice-ai-runtime`, `salon-ops-workflow`,
  `pos_backend_reviewer`.

### Configuration Transfer

- Read first: `docs/architecture.md`, `docs/api.md`,
  `docs/deployment.md`.
- Backend owner: `backend/modules/config_transfer`.
- Frontend owner:
  `frontend/features/configuration-transfer/import-preview.tsx`,
  `frontend/features/dashboard/settings-dashboard.tsx`,
  `frontend/features/onboarding/salon-profile-form.tsx`.
- Data owner: sanitized import/export DTOs; no secrets or operational records.
- Tests: `backend/modules/config_transfer/*_test.go`.
- Skill/subagent: `business-analysis`, `production_release_planner`,
  `security_privacy_reviewer`.

### Public Catalog

- Read first: `docs/architecture.md`, `docs/api.md`, `DESIGN.md`.
- Backend owner: `backend/modules/public_catalog`,
  public-catalog settings in `backend/modules/salon`.
- Frontend owner: `frontend/features/dashboard/settings-dashboard.tsx`.
- Landing owner: `landing/app/s/[slug]/page.tsx`, `landing/lib/api.ts`.
- Data owner: public-safe salon profile, published services/staff/hours.
- Tests: `backend/modules/public_catalog/service_test.go`.
- Skill/subagent: `salon-dashboard-ui`, `frontend_product_reviewer`,
  `security_privacy_reviewer`.

### Call Lifecycle, Archive, Redaction, Retention

- Read first: `docs/architecture.md`, `docs/api.md`,
  `docs/deployment.md`.
- Backend owner: `backend/modules/conversation/repository.go`,
  `backend/modules/conversation/retention_processor.go`,
  `backend/cmd/worker/main.go`.
- Frontend owner: `frontend/features/dashboard/calls-dashboard.tsx`.
- Data owner: `call_sessions`, `call_transcript_messages`,
  `handoff_requests`, `party_booking_requests`, `voice_webhook_events`,
  `voice_audio_outputs`.
- Tests: `backend/modules/conversation/service_test.go`,
  `backend/modules/voice/service_test.go`.
- Skill/subagent: `security_privacy_reviewer`,
  `production-release-review`.

## Triage Keyword Index

| Keywords / symptoms | Start with | Then inspect |
| --- | --- | --- |
| confirm, confirmed, booking ID, fallback pending, POS failed, no booking id, duplicate appointment, operation key, request fingerprint, retry blocked, reconciliation required, POS timeout unknown, HTTP 5xx write, truncated provider response, post-write lookup | `booking-safety-tdd`, `backend/modules/booking/service.go` | `backend/modules/booking/repository.go`, `backend/modules/conversation/service_booking_flow.go`, `backend/modules/pos/types.go`, `backend/modules/pos_square/adapter.go`, migrations V34-V35, booking tests |
| availability, local day, timezone, DST, open slots, offered slots, stale segment, no common time, split, staggered, staff assignment | `backend/modules/booking`, `backend/modules/conversation/service_availability.go` | POS provider adapter, `service_matching_parsing.go`, `appointments-dashboard.tsx`, `pos-calendar/features/calendar/pos-calendar-client.tsx`, conversation tests |
| Square OAuth, token expired, refresh token, location, sync, catalog import, calendar sync | `pos-adapter-slice`, `backend/modules/pos_square` | `backend/modules/pos`, `integration_config`, integrations UI, POS calendar UI |
| POS mapping, provider link, active provider, AI bookable, local only, sync failed | `backend/modules/pos`, `docs/canonical-pos-ownership-checklist.md` | Services/Staff UI, `pos_entity_links`, sync tests |
| semantic turn, multi-intent, service alias, category alias, service understanding, wrong service, category narrowed to one service, bare service switch, stale date after summary, staff/date/customer correction, add/replace/remove/undo, question plus correction, non-native wording, ASR paraphrase | `backend/modules/conversation/conversation_act.go`, `turn_reducer.go`, `next_action_planner.go` | dialog state/repository, voice semantic interpreter, training aliases, transcript metadata, golden conversation tests |
| service menu, how many services, how many I book, what do I have, current booking summary, service count, repeated clarification, informational service question | `backend/modules/conversation/conversation_act.go`, `backend/modules/conversation/service_prompts.go`, `backend/modules/conversation/answer_router.go` | `backend/modules/conversation/service.go`, dialog state, party flow, golden tests |
| final review, stale review, draft revision, reviewed revision, authorized revision, book it, just book this for me, correction during review, repeated same-category guest question, no progress loop | `backend/modules/conversation/conversation_act.go`, `backend/modules/conversation/draft_revision.go`, `next_action_planner.go`, `service.go` | repository, V36/V37 migrations, booking flow, conversation and phone tests |
| AI training, owner correction, knowledge, FAQ answer, stale policy | `backend/modules/training`, answer router/context | training UI, knowledge tests |
| party booking, group booking, two people, split booking, party request | conversation party files | booking service, Calls UI party request panel |
| name captured wrong, background phrase captured as name, bare phone name, service instead of name, spelling, phone/email | `service_customer_name.go` | conversation golden tests, transcript metadata |
| reschedule, cancel, move appointment, appointment target, ordinal option, day view, week view, month view, agenda, Tomorrow button, appointment warning | `service_intent.go`, `backend/modules/booking/service.go` | Appointments UI, POS Calendar UI, booking tests |
| Twilio signature, webhook, TwiML, recording, media stream, stream fallback | `backend/modules/voice_twilio` | `backend/modules/voice`, phone demo memo |
| OpenAI STT, TTS, realtime, model, voice, guarded reply, background noise, false transcript, transcript logprob, repeated progress reply, spoken fact mismatch, realtime transport fallback | `backend/modules/voice_openai`, `backend/modules/voice`, `backend/modules/voice_twilio/handler.go` | integration config, conversation runtime, realtime event timeline, voice tests |
| AI tone, speaking style, concise/warm/professional | `backend/modules/salon`, `conversation.RuntimeConfig`, `voice.ModelRequest` | Settings UI, config transfer |
| integration config, provider secrets, dashboard settings, active provider config, env fallback | `backend/modules/integration_config`, `/dashboard/integrations`, authenticated integration/status APIs | runtime resolver code first; deployment docs only for an explicitly scoped legacy fallback task |
| public catalog, published slug, public services, landing page, staff privacy | `backend/modules/public_catalog`, `landing/app/s/[slug]/page.tsx` | Settings UI public catalog card |
| config transfer, export, import preview, request id, duplicate import | `backend/modules/config_transfer` | Settings/onboarding import UI |
| archive, redaction, retention, transcript PII, call audio cleanup | conversation repository/retention processor | Calls UI lifecycle filters, worker |
| auth, tenant leak, salon ownership, token exposure, cross salon | `security_privacy_reviewer`, middleware/repositories | handlers, frontend API surfaces |
| loading, empty, error, disabled, gated UI, copy, responsive | `salon-dashboard-ui`, `DESIGN.md` | page component and API helper |
| CI/CD deploy, release tag, VPS deploy, GHCR image publish/sequential pull, SSH key, image extraction, healthcheck, systemd Caddy, project-edgectl, worker image | `.github/workflows/ci-cd.yml` | `docs/deployment.md`, `docker-compose.prod.yml`, `backend/Dockerfile`, deploy templates, GitHub Actions deploy log |

## Map Maintenance Checklist

Before finishing any code-changing task:

1. Did a route, module, component, helper, utility, DTO, migration, or test owner
   move or change responsibility?
2. Did a new feature/workflow keyword appear in a bug report, UI label, API
   response, or docs?
3. Did root-cause routing become faster if a new keyword is added here?
4. Did the change modify source-of-truth ownership, POS boundary, tenant/security
   boundary, provider config, or UI placement?
5. If yes to any item, update this map in the same approved scope.
