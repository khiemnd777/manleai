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

## No-Hardcoding Routing Guard

Read and apply the root `AGENTS.md` **No-Hardcoding Contract** before proposing
or reviewing any implementation. Route dynamic behavior to its authoritative
owner in this order:

1. Persisted domain data and the owner-facing UI/API that manages it.
2. Provider-backed runtime state and salon-scoped configuration.
3. Domain schema, typed state machine, or shared module contract.
4. Presentation copy only after the decision has already been derived from the
   sources above.

If a profile, catalog, provider, schema, or state owner already exists, do not
create a parallel phrase list, matcher, prompt map, field order, menu, taxonomy,
or frontend option list. A missing source of truth must be solved as a data or
contract ownership problem, not hidden in code constants.

Every routed proposal, review, and subagent handoff must name `Source of truth`,
`Dynamic inputs`, `Fixed invariants`, and `Hardcoding audit`. Classify hardcode
risk as `none`, `existing`, or `introduced`; an introduced risk blocks approval.
When source-of-truth ownership changes, update the relevant feature/function map
and triage keywords in this file.

## Fast Triage Rules

1. Start with the triage keyword table below.
2. Read the listed source docs and owner files before making behavior claims.
3. Follow the owning path: UI/page or API route -> handler -> service ->
   repository/provider -> schema/migration -> tests.
4. Route authority-sensitive scheduling through
   `docs/scheduling-authority.md` and `backend/modules/scheduling`. New
   availability/create work may use the current setting; existing operation,
   retry, and target records use persisted origin and must agree before
   dispatch. Candidate/replay methods are provider-free history delegation.
   Operation origin is resolved across external `booking_attempts` and owner-
   manual `scheduling_requests`; quote origin is also resolved before action
   dispatch. Square-backed `external_provider` requires a successful provider
   booking ID; `owner_manual` is a ready request-only executor that persists
   pending owner review without POS calls; Phase 4C `manleai_calendar` is a
   registered executor for verified aggregate availability, atomic
   multi-guest/multi-service staff-only or pooled create, and whole-root
   reschedule/cancel. V49 owns the
   authority/config-fenced ledger, while V50 guards pooled capacity,
   concurrency, exact committed graphs, and guest-party invariants; V51 guards
   lifecycle release ownership, root/event versions, replacement-plan
   equality, exact cancellation snapshots, and terminal state.
   `configuration_ready` and activation alone never imply an operation
   capability; all six capabilities are evaluated separately and aggregate
   `execution_ready` is true only when every capability is true. Historical
   target-origin lifecycle dispatch is distinct from current new-work
   readiness. Authority switching is an explicit owner-reviewed preview/commit
   workflow; it never follows from integration state. Party operations are
   all-or-none, and
   historical external actions preserve originating authority.
5. If the request mentions UI placement, identify actor workflow, operational
   object, source of truth, and neighboring controls before choosing placement.
6. If the request mentions voice/conversation, map caller goal, known fields,
   missing fields, service/category ambiguity, availability gate, booking gate,
   and handoff gate.
7. For provider setup or active-runtime diagnosis, treat Platform-managed
   `salon_integration_configs` as the source of truth. Start with
   `/platform/tenants/:tenant_id/technical`,
   `GET /api/platform/tenants/:tenant_id/technical/integration-configs`, the
   relevant readiness/debug endpoint, persisted provider state, and the runtime
   resolver. Never use env files, Compose defaults, GitHub secrets, or process
   environment values as evidence of active salon configuration. Inspect
   legacy environment fallback only when the task explicitly targets that path
   and evidence proves the salon has no stored provider config.
   Runtime Square resolution enters the legacy bootstrap path only for exact
   repository `ErrNotFound`; runtime Twilio and OpenAI resolution is strictly
   database-backed and fails closed when the salon record is missing. All three
   propagate repository, malformed-settings, and decryption errors. A stored row
   never inherits environment credentials or enabled state. Route future changes through
   `backend/modules/integration_config` and account for the shared Square OAuth,
   adapter, and webhook resolver boundary.
8. Run the root No-Hardcoding Contract audit before presenting a fix or code.
   If a profile, catalog, provider, schema, or typed state owner exists, route
   behavior through it and reject duplicated phrase matching, field-specific
   prompt tables, fixed menus, and fixed question order.

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
- Scheduling authority, confirmation evidence, party atomicity, and explicit
  authority switching: `docs/scheduling-authority.md`.
- Architecture and module ownership: `docs/architecture.md`.
- POS provider boundary: `docs/pos-adapter-layer.md`.
- Square integration: `docs/square-integration.md`.
- API surface: `docs/api.md`.
- Deployment/runtime config: `docs/deployment.md`.
- PostgreSQL backup/isolated restore operations:
  `docs/operations/postgres-backup-restore.md`.
- Forward-only migration compatibility and rollback decisions:
  `docs/operations/migration-rollback.md`.
- Bounded Owner-first scheduling load/concurrency verification:
  `docs/operations/scheduling-load-harness.md`.
- Tagged Owner-first code, PostgreSQL, tenant, and security release gate:
  `docs/operations/release-gate.md`.
- Production readiness scope: `docs/production-readiness-checklist.md`.
- UI/UX contract: `DESIGN.md`.
- Live phone demo setup: `docs/agents/phone-booking-demo-config-memo.md`.

## Backend Entrypoints

- API server: `backend/cmd/api/main.go`.
- Worker: `backend/cmd/worker/main.go`.
- First Platform Admin bootstrap: `backend/cmd/platform-access/main.go`.
- Opt-in sample-test provisioning: `backend/cmd/sample-data/main.go`,
  `backend/sampledata/*`, and `backend/sampledata/migrations/*.sql`.
- Twilio local simulator: `backend/cmd/twilio-sim/main.go`.
- Scheduling load/concurrency harness:
  `backend/cmd/scheduling-load-harness/main.go`.
- Startup migrations: `backend/internal/database/migrate.go` and
  `backend/migrations/*.sql`.
- Ent schema mirror: `backend/ent/schema/*.go`.

## Production Runtime And Release

- Release workflow: `.github/workflows/ci-cd.yml`.
- Release env/edge cutover contract: the workflow stages candidate
  `project.env` inside the release directory, retains the exact previous env
  and Compose file for image rollback, renders the previous project edge route
  for post-upsert recovery, runs `deploy/production-domain-smoke.sh`, and only
  then promotes the active env and `current` release. The Owner-first
  `self-test` statically rejects eager env promotion or reordered cutover
  markers.
- Current V80 edge contract retains `ADMIN_DOMAIN`, `LANDING_DOMAIN`, and
  `POS_DOMAIN`; the public cutover smoke runs in `--legacy` mode. The separate
  `MARKETING_DOMAIN` and final Platform-domain cutover are not active in this
  release.
- Owner-first release contract: `deploy/owner-first-release-gate.sh` loads the
  versioned package/test-file ownership in
  `deploy/owner-first-release-gate.manifest`. The tagged workflow requires full
  backend test/vet, bounded high-risk race checks, and all three web
  typecheck/build jobs. Frontend/POS `test:evidence` additionally executes
  authority, memory-only browser-session, and nonce-CSP contracts; landing
  executes its nonce-CSP security contract; POS also proves scheduler placement
  uses nonce-authorized style rules rather than inline attributes. The shared
  `deploy/run-ts-evidence-tests.mjs` compiler/Node runner owns execution. The
  gate also requires fresh migrate-twice PostgreSQL integration coverage from
  V46 through latest, per-package database clones that prevent fixture
  contamination, a live Redis 7 atomic-limiter test, and an explicit tenant/
  security package contract covering customer notification, Square webhook
  operations, and scheduling-PII retention before image build or deploy.
  See `docs/operations/release-gate.md`; a passing gate is code-ready evidence
  and not live-provider, backup/restore, alerting, or on-call proof.
- Manual non-production restore drill workflow:
  `.github/workflows/postgres-restore-drill.yml`.
- Runtime stack: `docker-compose.prod.yml`; API owns startup migrations and the
  worker starts only after API health with `AUTO_MIGRATE=false`.
- Local orchestration: root `make restart` delegates to
  `deploy/local-restart.sh`, which creates missing private local config,
  builds the stack, runs `deploy/postgres-migration-preflight.sh`, resets only
  an incompatible Compose-owned local sample PostgreSQL volume, invokes the
  guarded fixture runner, verifies `sample_test` through
  `deploy/postgres-data-profile-guard.sh`, and waits for all services.
- Deployment data-profile safety: `.github/workflows/ci-cd.yml` reads the
  protected `DEPLOY_DATA_PROFILE`; `deploy/postgres-migration-preflight.sh`
  checks the persisted ledger before API startup,
  `deploy/postgres-sample-target-preflight.sh` classifies an existing pre-live
  target, and `deploy/postgres-data-profile-guard.sh` verifies the selected
  post-migration profile. Only `sample_test` plus an exact tag-bound reset
  approval can remove the Compose-owned PostgreSQL volume after backup. `live`
  never resets or runs fixtures and rejects remaining sample rows/ledger.
- Worker scheduler: `backend/cmd/worker/scheduler.go` runs one startup-immediate,
  non-overlapping recurring loop per job, so POS sync, booking lease recovery,
  quote cleanup, Square webhook/repair, owner/customer notification delivery,
  call retention, and scheduling-PII retention do not starve each other. V57
  and `backend/modules/operations_health` add the
  cross-replica claim/heartbeat/finish fence, bounded safe result ledger, and
  owner-scoped queue health; `backend/cmd/worker/scheduler_test.go` owns
  isolation, cancellation, panic, and lease-held regressions.
- Container targets: `backend/Dockerfile` targets `api` and `worker`.
- Local stack: `docker-compose.yml` must build the backend API with
  `target: api`; without an explicit target the multi-stage Dockerfile ends on
  the `worker` stage and the API healthcheck cannot succeed.
- Shared edge route and manifest templates:
  `deploy/manleai.caddy.template`, `deploy/manleai.edge-manifest.template`.
- VPS Caddy ownership: `project-edgectl validate/upsert manleai` manages only
  the ManleAI route under `/etc/caddy/projects`; never modify the Caddy root
  config from the release workflow.
- Database release safety: `deploy/postgres-backup.sh` creates and validates a
  private custom-format dump plus checksum before candidate API migration;
  `deploy/postgres-restore-drill.sh` permits only a new explicit
  `manleai_restore_drill_...` target; and
  `deploy/postgres-verify-restore.sh` owns release migration-checksum parity,
  required schema-object checks, tenant smoke queries, and the isolated API
  startup handoff. Automatic image rollback requires an exact tag-specific
  previous-image/database compatibility declaration and never restores the DB.

## Backend Module Map

| Area | Owner files | Responsibilities | Tests |
| --- | --- | --- | --- |
| Auth and sessions | `backend/modules/auth/*`, `backend/internal/middleware/auth.go`, `frontend/lib/api/browser-session.ts`, `frontend/lib/api/session-contract.ts`, `pos-calendar/lib/api/browser-session.ts` | Generic non-enumerating login for invalid and disabled accounts; immutable current `tenant|platform` scope in user/session/ActorContext; host-only HttpOnly/Secure-production/SameSite-Strict refresh cookie scoped to `/api/auth`; memory-only browser access tokens plus deletion of legacy local-storage keys; deterministic one-successor active-user rotation with bounded exact concurrent replay and hashed persistence; Tenant bootstrap owner; shared `RequireAuth` reloads scope-compatible active server-owned state and fails closed with generic `401 UNAUTHENTICATED` | auth service/handler/repository integration tests, auth middleware tests, `frontend/lib/api/session.test.ts`, and both browser-session evidence tests |
| SaaS access control | `backend/modules/access/*`, `backend/internal/middleware/auth.go`, `backend/cmd/platform-access/main.go`, `backend/migrations/V64__saas_access_control_foundation.sql`, `backend/migrations/V74__principal_scope_isolation.sql`, `backend/migrations/V75__owner_authorized_platform_support.sql`, `backend/migrations/V76__platform_admin_authority_and_ops_delegation.sql`, `frontend/features/platform/access-console.tsx`, `frontend/features/platform/tenant-access-console.tsx`, `frontend/features/platform/access-ui.tsx`, `frontend/lib/api/access-contract.ts`, `frontend/lib/api/access.ts` | Tenant memberships with exact-route active-member enforcement; Owner suspension/reactivation while preserving `salons.owner_user_id`; immutable Tenant/Platform identity realms; direct create/edit/password/role/status management for Platform identities with bcrypt, session revocation, action replay, version fences, and last-admin governance; Platform Admin direct role-capability authority across all salon Platform tabs without salon assignment, temporary authorization, or PII grant; exact-salon baseline and Admin-granted time-bounded feature/PII access for Platform Ops; immediate revoke/expiry/role-or-assignment invalidation; actual-actor immutable audit | `backend/modules/access/*_test.go`, `frontend/lib/api/access.test.ts`, `backend/internal/middleware/auth_test.go`, `backend/modules/auth/repository_integration_test.go`, `backend/migrations/v64_safety_test.go`, `backend/migrations/v74_safety_test.go`, `backend/migrations/v75_safety_test.go`, `backend/migrations/v76_safety_test.go` |
| SaaS shared Business management | `backend/modules/business/*`, `backend/migrations/V65__saas_business_management.sql`, `backend/modules/salon/repository.go` public-catalog readiness helper | Fixed Tenant `/api/salons/:id/business/*` and Platform `/api/platform/tenants/:tenant_id/business/*` surfaces over the same canonical salon profile, services/categories/consultation profiles, staff/service eligibility, authority-aware hours, public page settings, and customers; active exact membership/capability authorization before repository reads; compatibility Platform Business service/category endpoints use the same direct-Admin or delegated-Ops access contract and fail closed when support-action audit fails; full shared Services remains the canonical Platform workflow; Platform staff contacts omitted and unwritable; provider-linked operational fields and external hours read-only; exact action-key replay, expected resource versions, actual-actor audit, shared scheduling fence, safe audit allowlists, provider/legacy insert version triggers | `backend/modules/business/*_test.go`, `backend/migrations/v65_safety_test.go`, `backend/modules/salon/public_catalog_integration_test.go` |
| SaaS Technical/Operations control plane | `backend/modules/integration_config/platform_handler.go`, `backend/modules/pos_square/platform_handler.go`, `backend/modules/scheduling_manleai_calendar/platform_handler.go`, `backend/modules/scheduling_authority_switch/platform_handler.go`, `backend/modules/notification_delivery/platform_handler.go`, `backend/modules/customer_notification/platform_handler.go`, `backend/modules/operations_health/platform_handler.go`, `backend/migrations/V66__saas_technical_control_plane.sql`, `backend/migrations/V70__platform_ai_runtime_control.sql` | Fixed `/api/platform/tenants/:tenant_id/technical/*` and `/operations/*` routes; exact delegated capabilities; direct tenant operation without owner impersonation; actual Platform actor audit; versioned/idempotent provider config and AI runtime; masked operational recovery; Tenant routes do not register technical provider controls | Platform route tests, V66/V70 safety tests, POS AI-runtime integration test |
| SaaS runtime isolation and fairness | `backend/internal/database/context_connector.go`, `backend/internal/database/runtime_role.go`, `backend/internal/databasecontext/*`, `backend/modules/tenant_runtime/*`, `backend/migrations/V67__tenant_runtime_membership_boundary.sql`, `V68__saas_row_level_security.sql`, `V69__tenant_runtime_quotas_usage.sql`, `V71__public_catalog_projection_boundary.sql`, `V72__platform_pii_scope_enforcement.sql`, `V75__owner_authorized_platform_support.sql`, `V76__platform_admin_authority_and_ops_delegation.sql`, `V78__system_tenant_context_expand.sql`, `V79__system_tenant_contract_preparation.sql`, `V80__strict_system_tenant_rls_contract.sql` | Request actor/system scope is applied per DB operation and reset before connection reuse; production runtime role must be non-owner/non-superuser/non-BYPASSRLS; tenant row isolation and active-membership runtime gate; public safe projection; V76 RLS gives Platform Admin direct role-capability access including PII while Platform Ops still requires exact assignment, temporary feature authorization, and exact active PII grant; V78 adds `app.system_salon_id` and provider locators; V79 moves global worker claim/recovery/cleanup/retention discovery behind bounded worker-only functions, binds returned items to one salon, and enforces composite call-child salon/session keys; V80 requires exact system salon matching for every ordinary provider/worker base-table policy and audits the final policy catalog. Calls scheduling evidence remains select-only and durably call-linked; database-owned tenant quotas/usage plus fair worker claim caps | `backend/internal/database/*_test.go`, `backend/modules/tenant_runtime/*_test.go`, `backend/migrations/v67_safety_test.go` through `v80_safety_test.go`, `backend/migrations/v79_integration_test.go`, `backend/migrations/v80_integration_test.go` |
| Sample-test data profile | `backend/cmd/sample-data/main.go`, `backend/sampledata/*`, `backend/sampledata/migrations/*.sql`, `backend/migrations/V73__sample_data_classification.sql`, `backend/migrations/V74__principal_scope_isolation.sql`, `deploy/local-restart.sh`, `deploy/postgres-*-preflight.sh`, `deploy/postgres-data-profile-guard.sh`, `.github/workflows/ci-cd.yml`, `backend/ent/schema/user.go`, `backend/ent/schema/salon.go`, `frontend/features/platform/tenant-directory.tsx`, `frontend/features/platform/tenant-detail-shell.tsx`, `frontend/features/platform/access-console.tsx`, `frontend/features/platform/tenant-access-console.tsx`, `frontend/features/platform/access-ui.tsx`, `frontend/features/dashboard/dashboard-home.tsx` | Normal migrations add default-`live` classification and immutable principal realm without fixtures; local/protected pre-live flows invoke the separate guarded runner after V73/V74; compatible runs replay; exact-tag reset remains backup-first/sample-only; the fixture provisions Platform-scoped Admin/Ops and a Tenant-scoped Lotus owner while provider configs and PII grants remain absent; UI filters and labels sample records | `backend/sampledata/*_test.go`, `backend/migrations/v73_safety_test.go`, `backend/migrations/v74_safety_test.go`, release-gate shell syntax self-test, local restart/replay verification, `frontend/lib/api/business.test.ts` |
| Distributed API rate limiting | `backend/internal/ratelimit/*`, `backend/internal/middleware/rate_limit.go`, `backend/cmd/api/main.go` | Redis-time atomic token bucket shared across API replicas; HMAC-pseudonymous signed-user or edge-overwritten client-IP identity; global plus route-class decisions; standard limit/retry headers; generic `429 RATE_LIMITED` and fail-closed `503 RATE_LIMIT_UNAVAILABLE`; mandatory production/local-opt-in configuration and Redis-aware health | middleware/unit tests, live two-client Redis integration, backend race suite, CI Redis service |
| Salon profile/settings | `backend/modules/salon/*` | Salon CRUD, settings, AI tone, fail-closed salon-wide consultation toggle validation, authority-aware public catalog settings/readiness with shared scheduling-fence/version validation, imported business hours, and read-only API exposure of `salon_settings.scheduling_authority`; existing settings update does not accept or switch authority | `backend/modules/salon/service_test.go`, `backend/modules/salon/public_catalog_integration_test.go` |
| POS provider-neutral layer | `backend/modules/pos/*` | `POSProvider` contracts, typed provider-write outcome/phase errors, stable safe error-code-to-message mapping that excludes provider response text, derived service/staff field authority and provider-managed write guards, current-authority-aware transactional canonical `ai_bookable` ownership in `Repository.UpdateServiceAIBookable`/`UpdateStaffAIBookable` using the shared booking/calendar advisory lock plus scheduling-authority/version and active-provider link evidence, atomic service owner-controls, monotonic location/generation-fenced full snapshots, optional appointment listing capability, POS entity links, service/staff/customer catalog, service consultation profile persistence/validation, sync jobs/logs/errors, provider switching, salon categories/aliases, V58 cross-table active alias ownership locking/validation, V63 historical provider-diagnostic redaction, V75/V76 Platform Services/Calls catalog routes with direct Admin or delegated Ops authorization and fail-closed action audit, actual-actor repository authorization without Owner-ID substitution, and materialization from the active database-owned nail taxonomy release | `backend/modules/pos/service_test.go`, `backend/modules/pos/handler_test.go`, `backend/modules/pos/platform_support_routes_test.go`, `backend/modules/pos/types_test.go`, `backend/modules/pos/sync_processor_test.go`, `backend/modules/pos/repository_integration_test.go`, `backend/modules/pos/repository_taxonomy_integration_test.go`, `backend/modules/pos/repository_alias_ownership_integration_test.go`, `backend/migrations/v58_safety_test.go`, `backend/migrations/v63_safety_test.go`, `backend/migrations/v75_safety_test.go`, `backend/migrations/v76_safety_test.go` |
| Square adapter | `backend/modules/pos_square/*` | Square OAuth, locations, atomic location-scoped generation-fenced sync with fail-closed freshness, Catalog `available_for_booking`/duration eligibility, active Team plus bookable Booking Profile intersection, Square payloads, salon-local availability ranges, provider-neutral `external_provider` target readiness with separate availability and booking-execution/write-safety gates plus authority version, readiness exposure of current scheduling authority with new-test/AI-enable gates limited to `external_provider`, dashboard test create/cancel writes through the scheduling facade with provider-free history replay and read-only create-lineage authorization for persisted external retries, post-switch external cleanup, sanitized gate errors, stable error classification without returning or persisting Square response details, provider-fenced booking-list pagination with cross-location rejection, token refresh, provider write outcome/error mapping, signed booking-webhook ingestion with root/nested location consistency, exact tenant routing across recoverable connection states, claim-token-fenced event processing and provider/connection-scoped scheduled calendar repair for historical external mirrors, plus V60 authenticated safe event list/detail, backlog/recent-success/calendar-repair health, and action-key-idempotent backend-gated requeue with no raw/provider/PII exposure | `backend/modules/pos_square/*_test.go`, `backend/migrations/v60_safety_test.go`, `backend/migrations/v63_safety_test.go` |
| Scheduling authority boundary and neutral API | `backend/modules/scheduling/*` | Owner-scoped current/origin resolution: validated `CurrentSchedulingAuthority`; operation origin across `booking_attempts` and `scheduling_requests`; retry origin from attempts, including availability dispatch by an exact persisted retry before current-authority fallback; mutually exclusive retry/target availability origin; quote-origin and target-aware availability/mutation resolution; cross-source equality before dispatch; neutral `AvailabilityResult` (`verified_slots`/`request_only`) and `ActionResult` (`confirmed_appointment`/`pending_owner_review`/`external_fallback_pending`); book/reschedule/cancel dispatch through `NeutralExecutor` with legacy `Executor`/`booking.HandlerService` compatibility; Phase 5H conversation-only policy entrypoints with replay-first persisted origin, exact booking-mode/authority fence, disabled zero-dispatch behavior, and pending-approval target preservation/proof stripping; provider-free `RescheduleCandidates`/`ReplayCreate`/`ReplayCancel`; authenticated additive `/scheduling-availability`, `/scheduling-actions`, and scheduling-request list/detail/transition routes with owner-scoped typed/sanitized error mapping; three-authority JSON contracts omit provider-shaped evidence for owner/internal results, retain required external evidence, preserve historical external origin after a switch, and fail unknown origins closed | `backend/modules/scheduling/*_test.go`, `backend/modules/scheduling/authority_compatibility_contract_test.go` |
| External scheduling executor | `backend/modules/scheduling_external_provider/*` | `external_provider` adapter for the four executor-backed methods, delegating to the unchanged booking service; no internal-authority emulation and no Square-specific payload logic; candidate/history lookup is owned by `scheduling.Service` delegation rather than executor selection | `backend/modules/scheduling_external_provider/*_test.go` |
| Owner-manual scheduling executor | `backend/modules/scheduling_owner_manual/*` | Ready non-confirming `owner_manual` executor; request-only availability; canonical active/non-archived/AI-bookable service/staff eligibility without POS links; owner-scoped `SchedulingTargetReadiness` with authority-version and request-capture capability while execution remains false; durable book/reschedule/cancel request creation/replay; separate request/ordered segment/append-only event aggregate; call-session link; queued deduplicated owner-notification outbox row; Phase 5H pending requests keep root authority `owner_manual` and optionally snapshot the selected/target scheduling authority without claiming a reservation; exact legacy NULL target and request replay survive later mode changes; `pending -> contacted|resolved|dismissed` and `contacted -> resolved|dismissed` transitions with row lock, expected-version CAS, action-key replay, and owner actor validation; never creates appointments, booking attempts, POS errors, or reconciliation work | `backend/modules/scheduling_owner_manual/*_test.go`, `backend/migrations/v47_safety_test.go`, `backend/migrations/v55_safety_test.go` |
| Scheduling PII retention | `backend/modules/scheduling_retention/*`, `backend/migrations/V61__scheduling_pii_retention.sql` | Policy-versioned, redaction-not-deletion owner for terminal scheduling request/segment/event PII, terminal owner/customer notification content and destinations, and expired voice-audio bytes; baseline 90-day business-plus-delivery eligibility, explicit JSON audit allowlist, protected pending/contacted/live-lease/retrying/unknown/reconciliation/consent routing state, preserved authority/provider/version/status/timestamp/tenant evidence, bounded fair `FOR UPDATE SKIP LOCKED` processing, per-row transactions, idempotent concurrency, PII-free errors, and no provider calls | `backend/modules/scheduling_retention/*_test.go`, `backend/modules/conversation/retention_integration_test.go`, `backend/migrations/v61_safety_test.go`, `backend/cmd/worker/retention_registration_test.go` |
| ManleAI Calendar configuration/readiness and Phase 4B-4C execution | `backend/modules/scheduling_manleai_calendar/*`, `backend/migrations/V49__manleai_calendar_execution_foundation.sql`, `backend/migrations/V50__manleai_calendar_pooled_capacity_guards.sql`, `backend/migrations/V51__manleai_calendar_lifecycle_guards.sql` | Phase 3 owns V48 configuration/readiness and provider-neutral target readiness derived from internal availability/create capabilities and authority/config versions; V49 owns the authority-native quote/attempt/appointment/service/resource/event ledger. Phase 4B `execution_availability.go` and `execution_repository_aggregate.go` plan and atomically commit exact strict-DST multi-guest staff/resource graphs; V50 enforces quote/guest/capacity/committed-graph/history integrity. Phase 4C `execution_lifecycle.go` owns target-origin, exact-version whole-root reschedule/cancel: reschedule uses a target/version/config/activation-fenced quote and exact replacement plan; cancel is quote-free, releases the exact old-plan snapshot, and leaves zero active children. Both advance the same root by one version and preserve exact event replay after later mutations. `executor.go` requires durable status/version/active-child evidence with no POS fields; `service.go`/`types.go` expose six granular capabilities and `execution_ready` is true only when all are true. V51 enforces release ownership, immutable provenance, contiguous versioned events, exact lifecycle graphs, and terminal cancel. Authority switching is owned by `backend/modules/scheduling_authority_switch`, not this module. | `backend/modules/scheduling_manleai_calendar/execution_availability_test.go`, `backend/modules/scheduling_manleai_calendar/execution_lifecycle_test.go`, `backend/modules/scheduling_manleai_calendar/executor_test.go`, `backend/modules/scheduling_manleai_calendar/execution_repository_integration_test.go`, `backend/modules/scheduling_manleai_calendar/*_test.go`, `backend/migrations/v48_safety_test.go`, `backend/migrations/v49_safety_test.go`, `backend/migrations/v50_safety_test.go`, `backend/migrations/v51_safety_test.go` |
| Scheduling authority switch | `backend/modules/scheduling_authority_switch/*`, `backend/migrations/V52__scheduling_authority_switch_runs.sql`, `backend/migrations/V53__external_availability_quote_authority_fence.sql`, `backend/migrations/V54__owner_first_onboarding.sql`, `backend/migrations/V55__conversation_pending_approval_targets.sql` | Owner-scoped read-only readiness preview and explicit audited commit across the three stable authority tokens; authenticated preview/latest/detail/commit routes; operation/action replay; expected authority-version CAS; shared scheduling advisory fence; transactional target-readiness recheck, including provider-owned `pos.Repository.WithSchedulingFenceTx` evidence for `external_provider`; live external-execution fence; immutable run/event history; explicit reverse-switch reference rather than implicit rollback; external quote authority provenance; owner-first onboarding compatibility; and pending-approval target snapshots. Integrations never select authority. Settings composes `scheduling-authority-switch.tsx` and `frontend/lib/api/scheduling-authority-switches.ts`. | `backend/modules/scheduling_authority_switch/*_test.go`, `backend/migrations/v52_safety_test.go`, `backend/migrations/v53_safety_test.go`, `backend/migrations/v54_safety_test.go`, `backend/migrations/v55_safety_test.go` |
| Booking | `backend/modules/booking/*` | Established external-provider booking execution and safety plus the handler-facing `HandlerService` contract and `ErrSchedulingAuthorityNotReady`/HTTP 409 mapping; V46-compatible DTO/repository snapshots for `scheduling_authority`, authority-native provider/appointment/service/staff/fence evidence, and nullable confirmation provenance while legacy POS fields still drive external fingerprints, retries, and provider dispatch; nullable legacy provider/POS response aliases are deprecated for generic authority/lifecycle discrimination and omitted when internal rows have no value, while external routes retain their required evidence; end-to-end location/generation provider fences, owner-scoped operation-key replay before mutable validation, origin-location-fenced historical appointment actions, durable operation claims/logical fingerprints/leases, phase-aware idempotent lease recovery (`not_started` safe versus `in_flight` unknown unless exact calendar truth converges), exact external create-mirror canonicalization that preserves existing provenance, fills missing confirmation time/source, and never invents an owner actor, single-use availability quotes, V53 `known`/`legacy_unknown`/`target_origin`/`retry_origin` quote provenance, exact safe external-book retry availability with a fresh retry-bound quote and no old-quote reuse, bounded reference-preserving quote retention cleanup, mapping- and target-validated safe-retry lineage/supersession, confirmed appointments, fallback/provider pending, reconciliation task/candidate/resolve APIs, authoritative backend retry policy, advisory-first direct/fallback/lease convergence with exact canonical/raw provider mirror proof, provider-fenced zero-write-stale calendar imports, monotonic calendar mirror writes, raw-identity-gated equal-version mapping enrichment, reschedule, cancel, POS idempotency, POS error/outbox writes | `backend/modules/booking/service_test.go`, `backend/modules/booking/quote_cleanup_processor_test.go`, `backend/modules/booking/repository_integration_test.go`, `backend/modules/scheduling/authority_compatibility_contract_test.go` |
| Customers | `backend/modules/customer/*` | Canonical customer CRUD, archive, search, activity read model, and provider customer lookup facade with bounded public lookup failures rather than wrapped provider diagnostics | `backend/modules/customer/service_test.go` |
| Owner notification delivery | `backend/modules/notification_delivery/*`, `backend/migrations/V56__owner_notification_delivery.sql` | Provider-neutral bounded claims and leases; dispatch-start fencing; immutable attempt/event/action ledgers; monotonic provider callback dedupe; safe pre-dispatch retry; unknown post-dispatch dead-letter protection; owner/tenant-scoped masked list/detail and action-key-idempotent safe requeue; no scheduling confirmation or appointment mutation | `backend/modules/notification_delivery/*_test.go`, `backend/migrations/v56_safety_test.go` |
| Customer appointment SMS | `backend/modules/customer_notification/*`, `backend/migrations/V59__customer_sms_consent_delivery.sql` | Default-off salon policy, explicit per-destination consent/current state plus immutable events, transactional request/appointment outbox, quiet-hours and exact consent/policy/source-version dispatch fences, provider-neutral bounded attempts/callbacks, canonical unknown-outcome protection, one action-key-idempotent owner requeue, masked appointment/request child APIs, no confirmation authority | `backend/modules/customer_notification/*_test.go`, `backend/migrations/v59_safety_test.go`, Twilio and conversation customer-consent tests |
| Twilio Messaging adapter | `backend/modules/notification_twilio/*` | Strict database-backed salon messaging resolution, isolated Twilio Messages REST dispatch, exact-URL/all-form `X-Twilio-Signature` verification, Account SID plus Messaging Service/sender tenant binding, monotonic status mapping, and signed Advanced Opt-Out `OptOutType` transport boundary without Body keyword logic or duplicate replies | `backend/modules/notification_twilio/*_test.go` |
| Conversation runtime | `backend/modules/conversation/*` | Simulator/phone session state; neutral scheduling availability/action use plus provider-free reschedule-candidate history; Phase 5H `booking_mode` matrix with mode/selected-authority review fence, verified-slot-then-request pending flows for internal/external authorities, owner-manual request-only pending flow, disabled pre-planner zero scheduling dispatch, structured party/time fidelity, and no pending confirmation wording; aggregate ManleAI create proof and Phase 4C lifecycle state in `service_internal_lifecycle.go`, including exact target/version, reviewed reschedule quote, cancel reason, explicit state-scoped authorization, target-origin execution after current-authority change, exact historical replay, stale refresh/re-offer, cutoff handoff, durable status/version/active-child validation, and provider-evidence rejection; persisted catalog-backed fuzzy candidate confirmation in `service_fuzzy_confirmation.go`, with exact/alias behavior unchanged and no draft/availability/booking mutation before explicit confirmation; external party/catalog/provider behavior preserved; safe handoff/no false confirmation; per-session serialization, state-revision CAS, event-stable reply replay, database-fenced answer context, Turn Kernel, service understanding, party planning, consultation/safety, handoff, transcript metadata, and retention; V62 party-request hydration and session redaction are fenced by the same salon/session parent key; Platform `platform_handler.go` reuses the same service for direct Admin or currently Admin-granted Ops Calls read/manage/simulate/redact actions, preserves that Platform identity through runtime/scheduling repositories, requires Calls PII for Ops, and records actual-actor audit rather than substituting the salon Owner | `backend/modules/conversation/*_test.go`, `backend/modules/conversation/booking_mode_matrix_test.go`, `backend/modules/conversation/owner_manual_scheduling_test.go`, `backend/modules/conversation/manleai_calendar_scheduling_test.go`, `backend/modules/conversation/manleai_calendar_party_scheduling_test.go`, `backend/modules/conversation/manleai_calendar_lifecycle_conversation_test.go`, `backend/modules/conversation/fuzzy_service_confirmation_test.go`, `backend/modules/conversation/answer_context_test.go`, `backend/modules/conversation/party_tenant_integrity_integration_test.go`, `backend/modules/voice_twilio/phone_booking_flow_test.go`, `backend/migrations/v76_safety_test.go` |
| Training | `backend/modules/training/*` | Knowledge items, owner corrections, correction apply/dismiss, service alias application, V58 salon/normalized-alias ownership locking with typed cross-namespace validation, and read-only training evaluation whose owner-scoped selected authority comes from `scheduling.Service.CurrentSchedulingAuthority`; Tenant routes require active exact membership; Platform routes allow direct Admin or currently Admin-granted Ops `training.read|write`, with Calls PII required for transcript-linked Ops corrections; service-alias application writes canonical structured `service_aliases`, never knowledge text; evaluation returns authority-native confirmation requirement/guardrail fields and never dispatches a scheduling executor/provider | `backend/modules/training/service_test.go`, `backend/modules/pos/repository_alias_ownership_integration_test.go`, `backend/migrations/v58_safety_test.go`, `backend/migrations/v76_safety_test.go` |
| Voice provider-neutral | `backend/modules/voice/*` | Owner-scoped, authority/version-fenced voice status with separate `phone_answering`, `request_capture`, and `automated_booking` dimensions; it composes target readiness owned by `scheduling_owner_manual`, `scheduling_manleai_calendar`, or `pos_square`, treats `phone_booking_ready` as an automated-booking compatibility alias, fails closed on unknown mode/authority or fence drift, and retains the legacy external-provider guidance diagnostics without using them to gate owner/internal authorities; also owns semantic-contract verification, bounded read-only semantic evaluation, guarded model-reply conversion, safe provider diagnostics whose public error string never unwraps provider response text, inbound routing, STT/LLM/TTS/realtime and streaming-speech interfaces, speech turns, and public audio delivery through metadata-first, database-expiry-bounded HMAC capabilities bound to audio/salon/provider/call/session identity with uniform non-enumerating failures and no unsigned fallback | `backend/modules/voice/*_test.go`, `backend/modules/voice/audio_capability_test.go`, `backend/modules/voice/repository_readiness_integration_test.go` |
| Conversation evaluation | `backend/internal/conversationeval/*`, `backend/cmd/conversation-eval*/*` | Three explicitly different evidence sets: (1) the schema-v2 1,000-scenario single-turn semantic contract corpus with a 100-record deterministic source/state audit, which is not a 100-conversation suite; (2) the directly authored 50-execution/45-situation single-turn direct-model pilot; and (3) the independent 100-journey real-salon multi-turn corpus and `conversation-eval-real` runner. The real-salon suite has exact 15/15/10/20/10/10/10/5/5 family coverage, three-to-twelve retained caller turns, at least three distinct catalog fixtures, zero generated/paraphrase journeys, complete session/state/tool evidence, deterministic execution of all 100 through the production Conversation Service, and honest structural/runtime statuses that never claim a model pass. Exactly ten live journeys are separately marked as canaries; their database-configured OpenAI run checkpoints every request, stops on the first journey failure, caps all recognition/reply/consultation/review calls at 60, reviews two batches of five complete interleaved transcripts, and passes only when all ten model executions and reviews pass. All direct and real-salon evaluators use isolated in-memory state and side-effect-blocking booking adapters; they cannot persist production conversations, mutate appointments, call POS, or manufacture booking confirmation evidence. Existing authenticated semantic runtime canaries remain capped at 12. | `backend/internal/conversationeval/*_test.go`, `backend/cmd/conversation-eval/*_test.go` |
| Scheduling load/concurrency verification | `backend/internal/schedulingload/*`, `backend/cmd/scheduling-load-harness/main.go` | Bounded synthetic `owner_manual`, `manleai_calendar`, and authority-switch replay/CAS/resource-conflict verification against an explicitly attested isolated PostgreSQL database; exact DB name/user/prefix and release-migration checksum guards; no destructive cleanup; unique run/tenant evidence; fake external readiness with zero real POS/Twilio/OpenAI calls; PII-free schema-v1 latency/throughput/conflict/invariant/pool report; pass requires zero safety, tenant, idempotency, duplicate, orphan, provider-evidence, and provider-call violations. This harness is not production capacity proof without an approved witnessed representative run. | `backend/internal/schedulingload/*_test.go`, opt-in `TestRunAgainstFreshIsolatedPostgres` |
| Twilio Voice adapter | `backend/modules/voice_twilio/*` | Twilio signatures, allowlisted stream-status audit fields instead of complete callback-form persistence, TwiML, recording mode, Media Streams bridge, caller-input gate, typed reply scheduling, stale-generation suppression, recovery first-byte budget, terminal latch, and fixed-message plus bounded typed Realtime failure diagnostics | `backend/modules/voice_twilio/*_test.go`, `backend/migrations/v63_safety_test.go` |
| OpenAI voice adapter | `backend/modules/voice_openai/*` | OpenAI STT, strict full/guidance structured-turn schemas and input/output with consultation profiles stripped, separate validated local `hour`/`minute` availability constraints converted to canonical minutes, protocol-unknown consultation normalization, consultation mutation values limited to the controlled protocol vocabulary plus the current request's catalog service IDs, and guidance companion/transition fields normalized from the accepted action; per-schema salon/config contract circuits, guarded operational/consultation-question reply, response-token usage observation for evaluation, whole-response TTS, dedicated streaming Speech-to-PCMU conversion, and Realtime input sessions whose transport/error events expose only bounded type/code/parameter diagnostics without provider message bodies | `backend/modules/voice_openai/*_test.go` |
| Integration config | `backend/modules/integration_config/*` | Authenticated salon-scoped Square/Twilio/OpenAI runtime settings and encrypted secrets; Square-only exact-`ErrNotFound` legacy bootstrap resolution; database-only Twilio/OpenAI runtime resolution and technical read responses with no environment inheritance; fail-closed repository/settings/decryption handling; stored-row authority over provider enabled state and credentials; Platform dashboard/Transfer persisted-only reads and secret-source redaction; strict string-valued settings-object scanning; dashboard-managed owner-SMS consent/destination/sender/callback settings; whole-response write-only secret/SID/destination serialization contract; strict database-only `ResolveTwilioMessagingConfig`; narrow database-only `ResolveStoredTwilioAuthToken` for voice audio signing without messaging readiness coupling; and fail-closed `ResolveOpenAIConfigStrict` | `backend/modules/integration_config/routes_test.go`, `backend/modules/integration_config/service_test.go`, `backend/modules/integration_config/repository_test.go`, caller propagation tests in `backend/modules/pos_square/adapter_test.go` and `backend/modules/voice/service_test.go` |
| Configuration transfer | `backend/modules/config_transfer/*`, `backend/migrations/V77__platform_configuration_transfer.sql`, `frontend/app/platform/tenants/[tenantId]/transfer/page.tsx`, `frontend/features/platform/platform-configuration-transfer.tsx`, `frontend/lib/api/platform-configuration-transfer.ts`, `docs/lotus-investor-demo-consultation-pack-v8.json` | Mounted Platform-only tenant-to-tenant/schema-v9 JSON preview/apply/export/run history with v8 upload compatibility and server-owned content-only v7-to-v8 adaptation; upload scope is derived from the file; per-section source-read/target-write authorization; exact source/Business/Technical/authority fences; atomic canonical writes plus actual-actor domain/transfer audit; active provider, authority, secrets, connections, provider hours, and operational history excluded. Full/runtime/provider v7 and v1-v6 stay rejected; earlier Tenant/onboarding v8-v1 handlers remain unregistered compatibility code. | `backend/modules/config_transfer/*_test.go`, `backend/migrations/v77_safety_test.go`, `frontend/lib/api/platform-configuration-transfer.test.ts` |
| Operations health | `backend/modules/operations_health/*`, `backend/migrations/V57__operations_health_job_ledger.sql` | Global safe recurring-job heartbeat/run ledger with cross-replica instance fencing; authenticated owner status combines it with only the requested salon's backlog/oldest/dead-letter aggregates, including safe customer-notification and scheduling-PII due counts, omits content, destinations, irrelevant provider rows, and run identities, treats missing/stale evidence as unsafe, and never reports internal scheduling failures as POS errors | `backend/modules/operations_health/*_test.go`, `backend/cmd/worker/scheduler_test.go`, `backend/migrations/v57_safety_test.go` |
| Public catalog API | `backend/modules/public_catalog/*`, `backend/migrations/V71__public_catalog_projection_boundary.sql`, `frontend/app/s/[slug]/*`, `frontend/features/public/*` | `/api/public/salons/:slug` and `/s/[slug]`; repository consumes only the database-owned authority-aware JSON projection; public scope sees zero base-table rows; owner-manual canonical services, current activated internal catalog/local hours, or current synced external projections; stale publication fails closed; no provider identifiers, owner IDs, staff contacts, secrets, diagnostics, or confirmation action | public catalog/service/salon integration tests, V71 safety and RLS integration tests |

## Backend Helper And Utility Map

| Helper area | Files | Use when triaging |
| --- | --- | --- |
| Server/bootstrap defaults | `backend/internal/config/config.go` | Server ports, CORS, JWT, and provider model normalization; active salon provider settings still resolve from dashboard-backed integration config |
| Database/migrations | `backend/internal/database/database.go`, `backend/internal/database/migrate.go`, `backend/migrations/*.sql` | Schema drift, startup migrator, migration checksums, version ordering; V44 owns the database nail taxonomy release, V45 forward-fills taxonomy aliases/suggestions, V46 adds authority snapshots/provenance, V47 adds the owner-manual request aggregate, V48 adds DB-managed authority/config fences and the internal-calendar configuration aggregate, V49 adds shared-ledger committed internal execution evidence, V50 adds aggregate quote/guest/graph integrity plus pooled capacity/concurrency guards without a second reservation ledger, V51 adds lifecycle release/version/event/plan integrity, V56 adds owner-notification delivery/attempt/event/action safety, V57 adds worker heartbeat/run evidence, V58 enforces cross-table alias ownership, V59 owns customer SMS consent/delivery evidence, V60 adds Square webhook dead-letter/action/requeue safety plus redacted repair diagnostics, V61 adds irreversible policy-versioned scheduling/notification/audio PII redaction guards, V62 fail-closes party-request/session tenant mismatch before installing the composite salon/session foreign key, V63 redacts provider diagnostics, V64 adds the SaaS access-control foundation, V65 adds shared Business versions/actions/events and staff/service eligibility, V73 adds schema-only default-live user/salon classification without fixtures, V74 adds immutable principal-scope isolation, V75 adds support authorization history/feature RLS, V76 makes Admin direct, Ops delegation Admin-owned, V77 owns Platform reviewed-transfer runs/events plus cross-path Business/Technical transfer fences, V78 expands system-tenant context, V79 prepares bounded worker discovery plus composite call-child tenant keys, and V80 enforces the strict provider/worker system-tenant RLS contract |
| Runtime database isolation | `backend/internal/database/context_connector.go`, `backend/internal/database/runtime_role.go`, `backend/internal/databasecontext/*`, `backend/migrations/V67*` through `V80*`, `deploy/postgres-init-runtime-role.sh` | V66 technical ledgers; V67 active membership; V68 RLS; V69 quotas/usage; V70 AI runtime technical audit; V71 public safe projection and zero public base-row visibility; V72 exact Platform PII scopes; V75/V76 feature and Platform authority policies; V78 expand-only system tenant context plus provider ID locators; V79 bounded worker discovery plus call-child composite tenant keys; V80 exact provider/worker system-tenant base-policy matching and policy-catalog audit. API migrates with `MIGRATION_DATABASE_URL`, then serves through the separate verified `DATABASE_URL` runtime role; worker receives runtime credentials only. V80 production cutover requires every running image to be V79-aware before migration. | database context/runtime/RLS integration tests and V66-V80 safety tests |
| Encryption | `backend/internal/encryption/encryption.go` | POS/provider secret encryption, AES-GCM token handling |
| Auth middleware | `backend/internal/middleware/auth.go`, `backend/modules/auth/repository.go` | Shared `RequireAuth` signature/subject validation followed by server-owned current-principal resolution; immutable V74 `tenant|platform` scope selects scope-compatible current roles and permits a primary active membership only for Tenant principals; stale JWT/header scope/tenant/role claims are ignored; `ActorContext` is server-owned; disabled/deleted/mixed identities, missing resolver, mismatched identity, invalid scope, and database errors fail closed | `backend/internal/middleware/auth_test.go`, `backend/modules/auth/repository_integration_test.go` |
| Rate-limit middleware/store | `backend/internal/middleware/rate_limit.go`, `backend/internal/ratelimit` | Route-class policy selection, HMAC identity, Redis atomic decisions, `RateLimit-*`/`Retry-After`, generic 429/503, and Redis health/startup dependency |
| Response helpers | `backend/internal/respond/respond.go` | API response/error shape |
| Validation helpers | `backend/internal/validation/validation.go` | Required fields and phone normalization |
| Logger | `backend/internal/logger/logger.go` | Structured logger initialization |

## Conversation Function Map

Use this file cluster when a defect mentions caller wording, service selection,
name collection, availability replies, party bookings, or transcript output.

Conversation source-of-truth routing is mandatory:

- Canonical services/categories/aliases own guidance identity. Authority-
  eligible booking context is separate: external mode retains provider-link/
  snapshot `ServiceOption.BookingReady`; owner-manual uses canonical request
  eligibility; internal mode uses canonical records plus activated V48
  staff-only/pooled policies and `EvaluateReadiness` capabilities. The selected
  authority fence is owned by `answer_context.go`/`repository.go` and enforced
  before availability/action writes.
- `service_consultation_profiles` owns salon-managed consultation facts and
  required consultation fields for a service. Runtime consultation planning
  must consume the ready profile instead of recreating its meaning in prompt
  maps, phrase matchers, or a fixed question order.
- Conversation/dialog state owns what is already known, unresolved, offered,
  or awaiting confirmation. Presentation wording must not become a parallel
  state machine.
- The captured scheduling authority owns availability and booking results.
  Conversation availability/create/reschedule/cancel enter through
  `backend/modules/scheduling.Service`; reschedule-candidate lookup uses its
  provider-free history delegate. New work may read the current setting, while
  target-aware/historical work uses persisted origin and cross-source equality.
  The confirming `external_provider` executor still requires active-POS
  availability and booking evidence. The ready `owner_manual` executor uses
  canonical provider-free catalog data, returns request-only availability, and
  persists pending owner-review work linked to the call session without
  synthesizing availability or confirmation. `manleai_calendar` create uses
  canonical/V48 policy context, one verified aggregate quote, exact
  guest/service/staff/resource proof, and durable root/child IDs. Phase 4C
  lifecycle state in `service_internal_lifecycle.go` owns the exact internal
  target/version, reviewed reschedule quote or cancellation reason, explicit
  authorization, exact replay, stale/cutoff recovery, and durable result
  validation. Target-origin dispatch remains available after the current
  authority changes; this is not an authority switch.
- A single fuzzy catalog candidate is not a selected service.
  `service_fuzzy_confirmation.go` persists the candidate ID, source/provenance,
  scope, and pending kind in `dialog_state`; only an explicit confirmation
  scoped to that active state or new exact/alias evidence can accept it. A
  negative response rejects it, unclear input repeats, stale source evidence
  invalidates it, and duplicate provider events replay idempotently. Exact and
  owner-defined alias matches retain their existing behavior.
- `conversationServiceRefs` includes complete ready consultation profiles and
  revisions only for active consultation or full semantic model context;
  initial `guidance_turn` input uses the stable recognition vocabulary and
  omits full profiles. `service_consultation.go` selects the
  next unresolved discriminating field and option values from current dialog
  state plus those profiles; guarded generation owns phrasing only. Raw caller
  wording does not narrow consultation candidates unless validated
  compared-service mutations identify catalog IDs.

- Main service orchestration: `backend/modules/conversation/service.go`.
- Availability replies and offered slots:
  `backend/modules/conversation/service_availability.go`.
  Availability invalidation compares complete ordered segment requests and
  deep-cloned before/after session state; offered-slot selection validates
  service and specific-staff assignments. Deterministic availability-question
  evidence overrides a conflicting semantic current-booking summary so an
  in-progress request either repeats valid offered slots or rechecks the
  selected authority for the requested date. Internal stale quote evidence
  clears old proof and reoffers without confirmation.
- Booking tool flow and terminal booking wording:
  `backend/modules/conversation/service_booking_flow.go`. Internal confirmation
  requires durable appointment/attempt IDs and rejects provider-shaped evidence;
  exact response-loss replay retains operation identity, while only typed stale
  availability triggers refresh.
- Intent, reschedule, cancel, and appointment-target selection:
  `backend/modules/conversation/service_intent.go`.
- Internal whole-root lifecycle state, reviewed proof, authorization, replay,
  stale/cutoff recovery, and durable-result validation:
  `backend/modules/conversation/service_internal_lifecycle.go`.
- Persisted one-candidate fuzzy service confirmation:
  `backend/modules/conversation/service_fuzzy_confirmation.go`.
- Service extraction, matching, parsing, and understanding:
  `backend/modules/conversation/service_extraction.go`,
  `backend/modules/conversation/service_matching_parsing.go`,
  `backend/modules/conversation/service_understanding.go`,
  `backend/modules/conversation/turn_kernel.go`,
  `backend/modules/conversation/conversation_act.go`,
  `backend/modules/conversation/turn_interpreter_error.go`,
  `backend/modules/conversation/service_guidance_capability.go`,
  `backend/modules/conversation/service_guidance_recovery.go`,
  `backend/modules/conversation/service_datetime_confirmation.go`,
  `backend/modules/conversation/turn_reducer.go`, and
  `backend/modules/conversation/next_action_planner.go`.
  Configured production turns first enter the Turn Kernel, which derives the
  expected input from dialog state, measures deterministic coverage, scopes
  service/staff model context, and selects a fast, answer, action, recovery, or
  semantic lane. Complete state-scoped evidence such as offered-slot choices,
  typed confirmations, structured questions, actions, and expected fields
  avoids a model round trip. An initial catalog-backed service or category
  request is field collection, not a service-edit operation: it may select one
  exact service or ask for one concrete category option without creating an
  add-or-replace operation prompt. Pending replacement/removal, add-or-replace
  choice, and same-category scope require an existing selected service; an
  initial category-target clarification may retain only its candidate set.
  Initial caller-goal/service-guidance state with no booking progress uses the
  compact `guidance_turn` semantic contract without acts/questions. Its typed
  `guidance_action` must be one of the stable recognizable actions; the
  general goal is derived by the backend instead of being authored a second
  time by the model. The contract also carries a bounded explicit
  `guidance_party_size` so initial party bookings do not depend on a transcript
  phrase parser. New booking, named-service, catalog, consultation, salon
  question, reschedule, cancel, and handoff actions are represented explicitly.
  Runtime fulfillment capability is separately derived from canonical
  active-provider linked guidance services, the salon consultation
  toggle, and ready profiles; current snapshot readiness is evaluated only at
  the provider-call booking fence;
  the shorter spoken recovery choices remain a separate state-owned subset.
  A recognized consultation request therefore remains consultation in
  `catalog_only`, `consultation_disabled`, and `catalog_unavailable` states,
  with a catalog-grounded or truthful technical response instead of schema
  rejection or semantic guessing.
  Semantic service-menu intent always routes to
  the active guidance catalog, never knowledge copy. Corrections,
  multi-intent turns, ambiguity, pending/review/party state, existing booking
  progress, and partial or unconsumed evidence use the full multi-act/question
  contract without a keyword-only gate. The interpreter inherits the active
  simulator/phone request context; there is no conversation-owned 2.5-second
  deadline. The OpenAI adapter retains its 30-second transport ceiling. Typed
  timeout, provider, output, confidence, and catalog rejection outcomes
  preserve the draft. A non-accepted semantic outcome may still consume
  independently validated catalog and captured-field evidence before asking the
  next missing-field question. A syntactic question that also contains a
  deterministic expected-field value is partial coverage, not proof that the
  complete turn was consumed, so constraints such as a requested date plus a
  time window remain on the semantic lane. Valid primary acts, questions, and
  guidance actions are validated independently from auxiliary consultation
  snapshots and mutations: invalid, unknown-as-value, low-confidence, or no-op
  consultation fields are dropped with bounded diagnostics instead of erasing
  the primary turn. When the unresolved field is caller goal or
  service, `service_guidance_recovery.go` owns a state-scoped evidence resolver
  and pure state reducer. The resolver consumes typed `TurnUnderstanding`, Turn
  Kernel answer/action decisions, and catalog/category evidence; it does not
  parse freeform caller meaning with a recovery phrase list. Typed
  `dialog_state.guidance` stores the current stage, dynamically available
  actions, `awaiting_action_choice`, separate caller-no-progress/provider-failure
  counters, last provider outcome, and progress fingerprint. Catalog menu or
  category progress resets
  both counters, exact catalog selection returns to the ordinary draft reducer,
  provider failure increments only the provider counter, and distinct bounded
  handoff reasons separate provider unavailability from caller ambiguity.
  Provider-failure copy is presentation over `offered_actions`: catalog and
  ready-profile consultation options are offered only when present, human
  handoff remains available, and an empty catalog cannot produce menu guidance.
  Only the immediately following `awaiting_action_choice` turn can use the
  bounded option parser; it is inactive as a general intent classifier. The
  recovery is gated by dialog state plus typed evidence; it does not become the
  primary caller-intent parser. Once a guidance action is accepted, booking
  asks for the next missing booking field and salon questions return their
  structured answer; the generic action menu is reserved for genuine
  no-progress or provider-failure recovery. Catalog
  validation owns referenced service/staff IDs;
  the reducer owns draft mutation and dependency invalidation; the planner owns
  missing-field, review, and booking readiness. Customer-name confirmation and
  the main orchestration path both execute that same planner gate; the booking
  executor rechecks the gate defensively before any POS write. Exact offered
  clock selections, including compact meridiem forms such as `12:00PM`, use the
  state-scoped fast lane only when one offered slot matches a bounded
  single-choice grammar. Selecting an `anyone` slot may persist its concrete
  provider assignment for dispatch, but staff-change acknowledgements remain
  keyed to the caller-visible selection mode and must not describe that
  internal assignment as a caller-requested technician change. Directional
  time constraints use a typed `before`/`after`/`exact` plus salon-local minutes
  contract, persist in `dialog_state`, filter current and refreshed offers, and
  are cleared when an exact slot is selected. Pending candidates remain
  contextual. A service correction against an active completed `party_plan`
  remains party-owned: `dialog_state.pending` stores separate target, guest/group,
  operation, and replacement-source prompts, and short guest/add/replace/source
  replies resolve through the deterministic fast lane before
  `turn_reducer.go` mutates only the selected party group. Review authorization
  and offered slots remain intact while that correction is unresolved and are
  invalidated only after a resolved mutation; unresolved party correction
  pending state also gates availability and booking if the semantic interpreter
  becomes unavailable. Review authorization and a
  resolved same-category guest-scope clarification are deterministic state-owned
  controls that cannot be overwritten by semantic interpretation;
  correction/negation evidence prevents review authorization. Only exact
  bounded state confirmations use the deterministic review fast lane;
  freeform approval, correction, or question turns enter the semantic lane.
  Corrections are reduced before their information questions are answered, and
  the answer resumes the freshly revised review. If the current draft was
  already read and authorization remains
  ambiguous after semantic failure, the planner asks one concise authorization
  retry without repeating the full review or calling the booking tool. Phone
  customer-name collection stores bare ASR candidates as typed pending state
  and confirms them before persisting unless the caller explicitly introduces
  a non-risky name or spells it. Transcript metadata is audit/legacy-read
  evidence only. Arbitrary utterances cannot replace a pending name; semantic
  corrections reject names colliding with the current salon, service, or staff
  identities. Unnamed requests for another technician derive the alternatives
  from the current staff catalog and persist multiple choices as typed pending
  state. Provider failure preserves the draft with safe clarification.
  A new-booking date/time correction made after slots were offered is persisted
  as a typed pending confirmation; rejection preserves the original slots, and
  confirmation invalidates them and performs a fresh provider availability
  check.
- Versioned active dialog state and cloning:
  `backend/modules/conversation/conversation_dialog_state.go`,
  `backend/modules/conversation/draft_revision.go`,
  `backend/modules/conversation/types.go`,
  `backend/modules/conversation/repository.go`, and migration
  `backend/migrations/V36__conversation_dialog_state.sql`,
  `backend/migrations/V37__conversation_draft_revision.sql`, and
  `backend/migrations/V38__service_consultation_profiles.sql`, plus the
  fail-closed profile/toggle constraints in
  `backend/migrations/V40__consultation_fail_closed_defaults.sql` and session
  revision in `backend/migrations/V42__conversation_state_revision.sql`. `dialog_state`
  version 5 persists pending clarification, active consultation and guidance
  state, bounded mutation history, no-progress count, and
  draft/review/authorization revisions. `normalizedDialogState` promotes legacy
  version 3 guidance prompt/counter fields into the nested guidance object on
  read; no SQL migration is required for that JSONB-only ownership move.
  Transcript metadata remains audit evidence, not active-state source of truth.
- Per-session turn serialization and optimistic conflict defense:
  `backend/modules/conversation/service.go`,
  `backend/modules/conversation/service_voice_recovery.go`, and
  `backend/modules/conversation/repository.go`. Production `Message` and typed
  unintelligible-voice recovery acquire one PostgreSQL session advisory lock on
  a dedicated connection before the first session read, retain it across
  planning, availability/POS side effects, bounded state-conflict retries, and
  `SaveTurn`, then release it with cancellation-independent cleanup. Both sides
  of a turn persist the event key, so replay of an older provider event returns
  its exact historical AI reply without replacing current session state. Concurrent
  lock holders are bounded from the configured SQL pool size so callback work
  retains connection headroom. Event-key dedupe and `state_revision`
  compare-and-swap remain defense in depth.
- Service consultation and safety handoff:
  `backend/modules/conversation/service_consultation.go`, profile contracts in
  `backend/modules/pos/types.go`, profile persistence in
  `backend/modules/pos/repository.go`, and schema/migration
  `backend/ent/schema/service_consultation_profile.go` plus
  `backend/migrations/V38__service_consultation_profiles.sql` and
  `backend/migrations/V40__consultation_fail_closed_defaults.sql`. Consultation is
  one state-owned lane under the Conversation Supervisor. It collects typed
  needs with validated set/replace/add/remove/clear mutations as the sole
  persistence authority for need fields; free-standing semantic snapshots are
  evidence only. It ranks only
  eligible services with complete `ready` owner-approved profiles containing
  both recommended outcomes and compatible current systems,
  derives unresolved fields and discriminating options from those profiles,
  then asks one guarded generated question at a time. It records
  reasons/revisions, question options, provider outcome/diagnostics, separate
  provider-failure/caller-no-progress counters, and a progress fingerprint in
  `dialog_state`, and never calls availability or POS tools. A question-
  generation failure retries safely and then hands off rather than using a
  fixed field-prompt fallback. While a consultation choice is pending, a
  category/category-alias that narrows that pending set to exactly one service
  is a catalog-backed selection and takes precedence over an auxiliary model
  catalog-question classification; multiple remaining services stay
  ambiguous. Service selection alone enters `awaiting_booking`; explicit
  booking intent is required to mutate the draft. An explicit booking request
  for one currently resolved catalog service exits consultation and starts that
  booking even when the prior recommendation candidate snapshot is empty.
  Existing booking drafts retain a resume phase. Deterministic safety evidence
  is checked globally before normal routing, while validated structured safety
  evidence is handled before state mutation or tool actions. Cancel,
  reschedule, handoff, active party plans, safety handoff, and bounded
  unresolved handoff retain routing precedence.
- Caller name, phone, email, typed name confirmation, dynamic identity
  collision validation, and name-slot repair:
  `backend/modules/conversation/service_customer_name.go`.
- Party/group booking detection and planning:
  `backend/modules/conversation/service_party.go`,
  `backend/modules/conversation/service_party_intent.go`,
  `backend/modules/conversation/service_party_signal.go`.
- Spoken prompts, menu/service clarification, and catalog count/list questions:
  `backend/modules/conversation/service_prompts.go`.
- Answer-source routing and cached structured context:
  `backend/modules/conversation/answer_context.go`,
  `backend/modules/conversation/answer_router.go`. A structured hours question
  without an explicit day uses the salon-local current day; an explicit day
  selects that day's authority-owned periods: `local_override` for ready
  internal context and provider-imported periods for external context.
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
- PII-free turn substage timing contract and recorder:
  `backend/modules/conversation/turn_timing.go`; persistence timing remains
  inside `backend/modules/conversation/repository.go`. Router timing and safe
  attributes include lane, expected input, decision reason, deterministic
  coverage, scoped model service/staff counts, and interpreter outcome.
- DTOs/state: `backend/modules/conversation/types.go`.
- Structured semantic-turn interpretation: `backend/modules/voice/act_interpreter.go`,
  `backend/modules/voice/types.go`, and
  `backend/modules/voice_openai/adapter.go`. The guidance contract always
  receives the complete stable `recognizable_guidance_actions`; runtime
  fulfillment remains separately owned by
  `backend/modules/conversation/service_guidance_capability.go` using canonical
  active-provider linked guidance services,
  `salon_settings.consultation_enabled`, and ready
  `service_consultation_profiles`. Model-proposed acts are
  multi-act/question, confidence-gated, PII-reduced, and active-catalog
  validated inside conversation ownership; catalog category ambiguity remains
  authoritative over a model-proposed concrete target, bare in-progress service
  switches return to catalog-backed confirmation, and deterministic slot
  corrections cannot be discarded by a model-only summary. Consultation model
  output is extraction-only: controlled needs including finish preferences,
  compared catalog IDs, booking
  request, and completion state. The interpreter cannot recommend a service,
  mutate state, or call booking tools. The OpenAI adapter recursively validates
  the supported strict-schema subset before dispatch, opens a salon/config/
  schema-scoped circuit for nonretryable contract 4xx errors, and exposes only
  bounded type/code/parameter/request/fingerprint diagnostics. The owner-scoped
  `POST /api/salons/:id/voice/semantic-check` route sends a synthetic no-PII
  contract request without conversation/POS mutation; success closes the
  matching circuit. `POST /api/salons/:id/voice/semantic-evaluate` accepts one
  bounded structured scenario, verifies owner scope and catalog references,
  calls the same salon-scoped turn model, and returns validated structured output for
  scoring without creating a session or invoking booking tools.

## Frontend Route And UI Map

The following SaaS route map is authoritative after the Phase 3-10 cutover:

| Route | Page/component ownership | Contract |
| --- | --- | --- |
| `/dashboard/*` | `frontend/app/dashboard/layout.tsx`, `frontend/components/layout/surface-gate.tsx`, `frontend/components/layout/tenant-salon-context.tsx`, `frontend/components/layout/app-shell.tsx` | Tenant-only shell; exact active membership selects the salon; Platform-only users are rejected |
| `/dashboard/services` | `frontend/features/dashboard/services-dashboard.tsx` | Reused full Services dashboard: structured service categories, category aliases, service aliases, catalog controls, consultation, and service-grain internal-calendar controls; business-safe readiness only and no provider setup/technical diagnostics |
| `/dashboard/settings` | `frontend/features/business/tenant-business-page.tsx` → `BusinessSettings` | Tenant Business profile, hours, and public-page settings through the shared canonical Business component; no Platform access approval UI and no provider/authority/calendar technical controls |
| `/dashboard/staff`, `/dashboard/customers` | `frontend/features/business/tenant-business-page.tsx`, `business-staff.tsx`, `business-customers.tsx` | Tenant Business objects through `/api/salons/:id/business/*`; no provider secrets, provider setup, scheduling authority switch, technical readiness, or Operations controls |
| `/dashboard/appointments` | `frontend/features/business/tenant-appointments-console.tsx` | Tenant appointment/request Business view scoped to current membership |
| `/dashboard/calls` | `frontend/features/dashboard/calls-dashboard.tsx` | Reused full Calls dashboard for sessions, transcripts, detected details, corrections, lifecycle actions, simulator, party review, and authority-native scheduling evidence |
| `/dashboard/training` | `frontend/features/dashboard/training-dashboard.tsx` | Reused full AI Training dashboard; knowledge remains free-text advisory data, while explicit correction-to-alias writes canonical structured service aliases |
| `/dashboard/integrations` | `frontend/app/dashboard/integrations/page.tsx` | Redirects to Tenant Settings; no Tenant technical provider UI |
| `/platform`, `/platform/tenants` | `frontend/components/layout/platform-app-shell.tsx`, `frontend/features/platform/tenant-directory.tsx` | Platform-only shell and tenant directory |
| `/platform/access` | `frontend/features/platform/access-console.tsx` | Platform Admin-only Platform user creation and editing with direct password input, profile, role, and status controls plus session revocation on password/authority changes; Tenant principals never appear; no salon selector or salon-scoped controls |
| `/platform/tenants/[tenantId]/business` | `frontend/features/platform/platform-business-workspace.tsx` plus shared Business editors | Platform operator manages profile/hours/public-page, Staff, and Customers with actual-actor audit; it does not render a second reduced Services editor, because the full shared Services tab owns services, categories, category aliases, and service aliases |
| `/platform/tenants/[tenantId]/services` | shared `frontend/features/dashboard/services-dashboard.tsx` with `surface="platform"` | Reuses the full Services component and canonical category/service alias workflows; Platform Admin is direct, while Platform Ops needs exact assignment plus current temporary Services authorization; provider setup remains Technical; fatal API errors suppress metrics and empty-state data |
| `/platform/tenants/[tenantId]/training` | shared `frontend/features/dashboard/training-dashboard.tsx` with `surface="platform"` | Reuses the full AI Training component; Platform Admin is direct, while Platform Ops needs exact assignment plus current temporary Training authorization and Calls PII for transcript-linked corrections; fatal API errors suppress metrics and empty-state data |
| `/platform/tenants/[tenantId]/calls` | shared `frontend/features/dashboard/calls-dashboard.tsx` with `surface="platform"` | Reuses the full Calls component with capability-gated controls; Platform Admin is direct, while Platform Ops needs exact assignment, temporary Calls authorization, and linked Calls PII; fatal API errors suppress metrics and empty-state data; status omits technical provider configuration |
| `/platform/tenants/[tenantId]/technical` | `frontend/features/platform/technical-integration-settings.tsx`, shared `frontend/features/integrations/square-integration.tsx` provider panel, `frontend/lib/api/integration-config-contract.ts`, `technical-scheduling-settings.tsx` | Platform-only Square/Twilio/OpenAI using the shared typed provider form contract, AI runtime, ManleAI Calendar, and scheduling-authority technical controls |
| `/platform/tenants/[tenantId]/transfer` | `frontend/features/platform/platform-configuration-transfer.tsx`, `frontend/lib/api/platform-configuration-transfer.ts` | Platform-only tenant-to-tenant, v9/v8 JSON, or scoped content-only v7 transfer; destination implied by route; file-derived portable scope and visible v7-to-v8 adaptation; preview/apply/stale/conflict/replay states; safe run history; no authority/provider/secret/operational transfer |
| `/platform/tenants/[tenantId]/operations` | `frontend/features/platform/platform-operations-console.tsx`, `tenant-runtime-controls.tsx` | Platform Operations status, masked recovery, Square webhook operations, usage, quotas, and fairness controls |
| `/platform/tenants/[tenantId]/access` | `frontend/features/platform/tenant-access-console.tsx`, `frontend/features/platform/access-ui.tsx` | Platform Admin-only tenant Access tab with Tenant-principal-only team lookup, Owner suspension/reactivation without ownership transfer, backend-catalog-driven baseline Platform Ops capabilities, Admin-granted temporary Ops authorization for Services/AI Training/Calls, and exact-scope temporary sensitive-data grants; tenant ID comes from the parent detail route |
| `/platform/tenants/[tenantId]/audit` | `frontend/features/platform/platform-audit-log.tsx` | Tenant-scoped access/Business/Technical/Transfer/Operations immutable audit projections |
| `/s/[slug]` | `frontend/features/public/public-salon-page.tsx` | Public slug-scoped safe catalog from V71 JSON projection; no base-table reads or booking confirmation |

### Historical pre-SaaS component inventory

The table below records the detailed Owner-first dashboard composition. Use the
SaaS table above for route ownership. Services, Calls, Settings, and AI Training
are current again because their original rich dashboard components are reused
directly by the current Tenant routes and, where listed above, by Platform
surface adapters; the other rows remain regression archaeology unless the SaaS
table points to them.

| Route | Page file | Main component | Data/API helpers |
| --- | --- | --- | --- |
| `/login` | `frontend/app/login/page.tsx` | `frontend/features/auth/login-form.tsx` | credentialed `frontend/lib/api/client.ts` plus memory-only `browser-session.ts` |
| `/create-account` | `frontend/app/create-account/page.tsx` | `frontend/features/auth/create-account-form.tsx` | credentialed `frontend/lib/api/client.ts` plus HttpOnly refresh-cookie response |
| `/onboarding` | `frontend/app/onboarding/page.tsx` | `frontend/features/onboarding/salon-profile-form.tsx` | `frontend/lib/api/client.ts`; configuration import is not rendered after SaaS cutover |
| `/dashboard` | `frontend/app/dashboard/page.tsx` | `frontend/features/dashboard/dashboard-home.tsx` | status, voice, calls, appointments, attempts, services, staff APIs; confirmed counts require authority-native durable evidence and catalog summary eligibility branches on selected authority through `frontend/lib/api/scheduling-evidence.ts` |
| `/dashboard/appointments` | `frontend/app/dashboard/appointments/page.tsx` | `frontend/features/dashboard/appointments-dashboard.tsx`, `frontend/features/dashboard/owner-review-requests.tsx`, `frontend/features/dashboard/owner-notification-deliveries.tsx`, `frontend/features/dashboard/customer-notification-status.tsx`, `frontend/features/dashboard/scheduling-readiness-card.tsx`, `frontend/features/dashboard/internal-appointment-create.tsx`, `frontend/features/dashboard/internal-appointment-lifecycle.tsx` | authority/version consistency banner, mixed-origin appointment history and badges, origin- plus backend-capability-scoped row actions, external attempt/reconciliation operations, owner-manual request review, masked owner-notification delivery metrics/list/detail, and masked customer consent/delivery child evidence plus backend-gated bounded safe requeue inside the owning appointment or request detail through `frontend/lib/api/customer-notifications.ts`; capability-aware selected-authority readiness, owner-entered external operations independent of the salon AI-enabled flag, structured guest/service staff-only/pooled internal create, and whole-root internal lifecycle controls using `frontend/lib/api/scheduling-actions.ts`; new external status/setup warnings appear only for selected `external_provider`, external reschedule availability includes the target appointment for origin resolution, exact safe external book retries pass `retry_of_attempt_id`, clear old quote proof, obtain a fresh retry-bound quote, and remain available by persisted origin after a current-authority switch, internal lifecycle eligibility is derived from complete current-plan evidence, external create/lifecycle confirmation uses `frontend/lib/api/scheduling-evidence.ts` with canonical authority evidence rather than a legacy POS ID, cancel requires terminal status and zero active children, missing/invalid/equal cutoff evidence fails closed, uncertain submissions retain only exact replay, and no provider/POS evidence is accepted for internal confirmation |
| `/dashboard/calls` | `frontend/app/dashboard/calls/page.tsx` | `frontend/features/dashboard/calls-dashboard.tsx` | responsive Phone answering, Request capture, and Automated booking readiness cards with current authority/version/mode and backend blockers; sessions, typed consultation state/revisions, paginated full-call realtime events, party requests, and owner corrections; `backend/modules/conversation/scheduling_result_evidence.go` owns the owner/tenant-scoped authority-native request/result projection and `frontend/lib/api/conversation-scheduling-evidence.ts` consumes only its complete/current proof for confirmation, so local session IDs cannot manufacture confirmation and exact historical lifecycle results remain noncurrent |
| `/dashboard/customers` | `frontend/app/dashboard/customers/page.tsx` | `frontend/features/dashboard/customers-dashboard.tsx` | customer CRUD, archive, search |
| `/dashboard/services` | `frontend/app/dashboard/services/page.tsx` | `frontend/features/dashboard/services-dashboard.tsx`, including `ServiceCalendarPolicyEditor` in the existing service form | services, provider field authority, owner controls/consultation, and parent-scoped internal-calendar enablement, capacity mode, buffers, eligible staff, and resource requirements without reselecting the service |
| `/dashboard/staff` | `frontend/app/dashboard/staff/page.tsx` | `frontend/features/dashboard/staff-dashboard.tsx`, `frontend/features/dashboard/staff-calendar-profile.tsx` | staff field authority/CRUD/archive/AI-bookable plus parent-scoped internal weekly periods, service assignments, and staff exceptions without reselecting the staff member |
| `/dashboard/settings` | `frontend/app/dashboard/settings/page.tsx` | `frontend/features/business/tenant-business-page.tsx`, `frontend/features/business/business-settings.tsx` | Tenant Business profile, canonical hours, and public page only. Provider credentials, scheduling-authority switching, internal-calendar technical configuration, and operations health remain in the Platform Technical/Operations tabs. |
| `/dashboard/training` | `frontend/app/dashboard/training/page.tsx` | `frontend/features/dashboard/training-dashboard.tsx` | knowledge items, owner corrections, authority-aware evaluation that renders the backend confirmation requirement/guardrail without a universal Square claim, and explicit correction apply as reusable knowledge or a catalog-backed service alias with loading, error, empty, disabled, submitting, and success states |
| `/dashboard/integrations` | `frontend/app/dashboard/integrations/page.tsx` | `frontend/features/integrations/square-integration.tsx`, `frontend/features/integrations/square-webhook-operations.tsx` | Square OAuth/status/sync/quoted test booking with operation-matched safe-retry lineage, write-only webhook verifier config, and connected-Square child operations for safe metrics/calendar repair, five public filters plus read-only unfiltered `ignored`, list/detail/timeline, pagination, backend-gated requeue, and exact replay through `frontend/lib/api/square-webhook-events.ts`; provider config/switching; and Twilio owner-operational SMS enablement, exact-destination consent, write-only messaging credentials/sender, and callback URL setup; no customer-SMS claim |
| `/dashboard/billing` | `frontend/app/dashboard/billing/page.tsx` | `frontend/features/dashboard/billing-dashboard.tsx` | static gated billing surface |

Scheduling-authority Phase 4C status: the Appointments page has an implemented
owner-manual owner-review queue with status filters/counts, loading/error/empty/
success states, paginated desktop table, responsive mobile cards, detail dialog,
allowed lifecycle actions, required terminal reason, and version-conflict
reload, plus capability-aware readiness and the structured multi-guest,
multi-service staff-only/pooled internal create flow and complete-row
whole-root internal reschedule/cancel controls.
Settings, Staff, and Services
now own internal-calendar configuration at salon/staff/service grain with
backend constraints, version-conflict reload, and responsive states. The UI
shows notification delivery only from V56/backend evidence and never equates it
with appointment confirmation; internal create and lifecycle wording requires
exact durable evidence. Lifecycle controls use the
persisted row origin after a current-authority change, accept only a complete
active current-version plan, fail closed on missing/invalid/equal cutoff
evidence, and preserve exact replay state for uncertain responses.
Integrations remains the provider-setup surface, including explicit owner-SMS
consent/destination/sender/callback settings and the connected-Square child
operations view for V60 safe webhook/repair evidence plus backend-gated exact
requeue. That child view is provider/connection scoped and does not select or
infer scheduling authority. The standalone Scheduling
Calendar is authority-aware for mixed-origin display and current-authority
gating, while its write contract remains limited to external-provider rows.

## POS Calendar Surface Map

| Route | Page file | Main component | Data/API helpers |
| --- | --- | --- | --- |
| `/login` | `pos-calendar/app/login/page.tsx` | `pos-calendar/features/auth/login-form.tsx` | credentialed `pos-calendar/lib/api/client.ts` plus memory-only `browser-session.ts` |
| `/calendar` | `pos-calendar/app/calendar/page.tsx` | `pos-calendar/features/calendar/pos-calendar-client.tsx` | current authority/version banner, mixed-origin appointment/request badges, stale-response-guarded calendar range and event stream/toasts, target-aware availability quotes, and payload-bound historical external-provider reschedule/cancel operations; `pos-calendar/lib/scheduling-evidence.ts` gates confirmation/actions with persisted authority-native evidence and counterexamples reject POS-ID inference; Square Appointments status/sync/new-create controls load only when `external_provider` is the selected scheduling authority, while historical-origin resolution and reconciliation remain provider-neutral; missing authority/origin contracts fail closed, internal rows remain read-only here, and nonce-authorized `lib/security/scheduler-style.ts` owns dynamic event placement without CSP-blocked inline attributes |

The POS calendar app is a standalone authenticated Next.js app with no
dashboard sidebar. Local runtime port is `3091`; production domain is
`pos.knasoftware.com`. It reuses the same admin bearer/cookie session contract
as `frontend/`.
The Day view groups timed appointments into horizontally scrollable technician
lanes, keeps unassigned and referenced inactive technicians visible, and uses
ordered appointment segments when one booking has multiple technician
assignments. Calendar event titles across day, week, month, agenda, and the day
drawer use `time · customer · technician`; service names remain supporting
detail rather than part of the event title.

## Frontend Helper And Utility Map

- API client/session/refresh: credentialed `frontend/lib/api/client.ts` plus
  memory-only `frontend/lib/api/browser-session.ts`; refresh tokens remain in
  the server-set HttpOnly cookie and never enter frontend JSON/state.
- Web CSP owners: `frontend/middleware.ts`, `pos-calendar/middleware.ts`, and
  `landing/proxy.ts`, with app-local pure policy builders/tests under each
  app's `lib/security/content-security-policy.ts`. POS scheduler placement is
  owned by `pos-calendar/lib/security/scheduler-style.ts`.
- Configuration transfer helpers:
  mounted Platform requests live in
  `frontend/lib/api/platform-configuration-transfer.ts`; preview section labels
  and responsive summaries live in
  `frontend/features/configuration-transfer/import-preview.tsx`. The older
  `frontend/lib/api/configuration-transfer.ts` is unmounted compatibility code.
- Environment config: `frontend/lib/config/env.ts`.
- Classname helper: `frontend/lib/utils/cn.ts`.
- Shared API DTOs: `frontend/types/api.ts`.
- Authority-native appointment/attempt evidence guards, selected-authority
  service/staff eligibility, and mapper
  counterexamples: `frontend/lib/api/scheduling-evidence.ts` and
  `frontend/lib/api/scheduling-evidence.test.ts`; Calls scheduling-result proof
  is consumed and tested by
  `frontend/lib/api/conversation-scheduling-evidence.ts` and
  `frontend/lib/api/conversation-scheduling-evidence.test.ts`; the standalone
  calendar owns its corresponding projection and test in
  `pos-calendar/lib/scheduling-evidence.ts` and
  `pos-calendar/lib/scheduling-evidence.test.ts`. The explicit TypeScript test
  files execute through each app's `test:evidence` package script and the shared
  `deploy/run-ts-evidence-tests.mjs` runner.
- Owner-manual request API: `frontend/lib/api/scheduling-requests.ts`.
- Square webhook operations API and typed public filter invariant:
  `frontend/lib/api/square-webhook-events.ts`; shared safe event/metrics/repair
  DTOs live in `frontend/types/api.ts`, and `frontend/lib/api/client.ts`
  exposes response metadata so exact replay can read
  `X-Idempotent-Replay` after the normal auth-refresh path.
- ManleAI Calendar configuration/readiness API:
  `frontend/lib/api/internal-calendar.ts`; shared aggregate/child/mutation DTOs
  and backend constraints live in `frontend/types/api.ts`.
- Dashboard shell: `frontend/components/layout/app-shell.tsx`.
- Deferred loading wrapper: `frontend/components/layout/deferred-page.tsx`.
- UI primitives: `frontend/components/ui/*`.
- Booking display labels: `frontend/features/dashboard/booking-display.ts`.
- Shared service/staff field-authority presentation and conservative edit gates:
  `frontend/features/dashboard/pos-field-authority.tsx`.
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

### Backup, Restore, And Migration Rollback

- Read first: `docs/deployment.md`,
  `docs/operations/postgres-backup-restore.md`, and
  `docs/operations/migration-rollback.md`.
- Tagged deploy owner: `.github/workflows/ci-cd.yml`. It validates the exact
  release tag, `PREVIOUS_IMAGE_DB_COMPATIBLE=true`, and bounded compatibility
  approver plus exact protected storage-path approval before mutation; bundles
  the release migration files; refuses an ambiguous existing PostgreSQL source;
  and runs the checked pre-deploy backup before candidate API startup migration.
- Backup owner: `deploy/postgres-backup.sh`. Dynamic inputs are the explicit
  Compose/env paths, project, source DB/user, private output directory, and
  artifact ID. Fixed guards are custom-format `pg_dump`, `pg_restore --list`,
  SHA-256, mode-`700`/`600`, non-overwrite, and the exact
  `encrypted-private` storage attestation.
- Restore owner: `deploy/postgres-restore-drill.sh`. It requires different
  explicit source/target identities, a new target with the
  `manleai_restore_drill_` prefix, exact artifact/checksum, approval and release
  references, RPO/RTO targets, and a private report path. It creates one target
  and never drops, truncates, renames, replaces, or cleans up a database.
- Verification owner: `deploy/postgres-verify-restore.sh` plus the exact release
  `backend/migrations/*.sql` set. It checks complete
  `app_schema_migrations` name/checksum parity, required tables/constraints,
  and bounded cross-salon/orphan queries before returning an isolated
  `AUTO_MIGRATE=false` API health-check handoff.
- Manual drill owner: `.github/workflows/postgres-restore-drill.yml` and the
  protected `postgres-restore-drill` GitHub environment. Dedicated
  non-production SSH secrets plus the exact host attestation file fence the
  workflow away from the production VPS. Database artifacts and the restored
  target remain on the private host; only the sanitized timestamp/artifact/
  RPO/RTO/duration/approver report is uploaded.
- Rollback contract: migrations are forward-only expand/migrate/contract.
  Automatic deploy failure handling can restart prior images only when that
  exact release declared previous-image compatibility. Image rollback leaves
  the forward database unchanged. Production database recovery requires a
  selected backup, successful isolated restore/verification, reconciliation of
  post-snapshot and provider-side effects, and a separate DBA-approved cutover.
- Operational readiness remains incomplete until encrypted production storage,
  retention/key rotation, business RPO/RTO targets, and a witnessed recurring
  non-production drill plus isolated API handoff evidence are configured.

### Scheduling Authority

- Read first: `docs/scheduling-authority.md`, `CONTEXT.md`,
  `docs/architecture.md`, then `docs/pos-adapter-layer.md` and
  `docs/square-integration.md` only for `external_provider` work.
- Phase 0 data owner: `salon_settings.scheduling_authority` plus immutable
  authority snapshots on `booking_attempts`, `booking_attempt_segments`,
  `appointments`, `appointment_services`, and `availability_quotes`, introduced
  by `backend/migrations/V46__owner_first_scheduling_authority.sql` and mirrored
  in `backend/ent/schema/*`.
- Phase 0 data-path owner: settings read exposure in `backend/modules/salon`
  and additive booking DTO/repository parity in `backend/modules/booking`.
- Phase 1 resolver/dispatch owner: `backend/modules/scheduling.Repository`
  performs owner-scoped current-setting and persisted-origin resolution.
  `scheduling.Service.CurrentSchedulingAuthority` supplies validated new-work
  readiness. Its public read-only `ResolveCreateSchedulingAuthority` resolves
  operation/retry origin equality before current-mode fallback without executor
  or provider dispatch. Phase 2 extends operation-origin lookup across
  `booking_attempts` and `scheduling_requests`, exposes neutral result unions,
  dispatches availability and book/reschedule/cancel through `NeutralExecutor`,
  retains legacy external `Executor`/`booking.HandlerService` compatibility,
  and delegates candidates plus create/cancel replay to provider-free history.
- Phase 5H conversation-policy owner: `backend/modules/scheduling.Service`
  owns `CheckConversationAvailability` and `ExecuteConversationAction`;
  `backend/modules/conversation` persists the reviewed booking-mode/selected-
  authority fence and owns caller copy/state; V55 owns the optional
  `scheduling_requests.target_scheduling_authority` contract for book requests.
  Generic administrator `CheckAvailability`/`ExecuteAction` behavior is
  unchanged. AI Settings owns booking-mode selection; Appointments owner review
  owns target-authority and non-reservation presentation.
- Current executors: `backend/modules/scheduling_external_provider.Adapter`
  is the provider-backed confirming executor and delegates to the existing
  booking service; legacy POS fields own its fingerprints,
  retry/reconciliation, dispatch, and confirmation predicates.
  `backend/modules/scheduling_owner_manual.Executor`
  is a ready non-confirming executor that returns `request_only` and persists
  `pending_owner_review` for book/reschedule/cancel without POS calls.
  `backend/modules/scheduling_manleai_calendar.Executor` is registered for
  verified aggregate availability and atomic multi-guest/multi-service
  staff-only/pooled book plus exact-version whole-root reschedule/cancel; it
  requires durable root appointment/attempt IDs, authoritative status/version/
  active-child count, the operation-specific child snapshot, and no provider/
  POS evidence.
- Owner-manual data owner: V47 `scheduling_requests`,
  `scheduling_request_segments`, and `scheduling_request_events`, with the
  reciprocal `call_sessions.scheduling_request_id` and
  `owner_notifications.scheduling_request_id`. Creation uses one database
  transaction plus unique operation ownership, row locking, and fingerprint
  comparison; transitions use action-key replay, request row locks,
  expected-version CAS, and append-only events. The outbox is queued/deduped;
  request creation is not delivery proof.
- Owner-notification delivery owner: V56,
  `backend/modules/notification_delivery`, and
  `backend/modules/notification_twilio`. The provider-neutral processor owns
  bounded claim/lease/dispatch fencing, immutable attempts/events/actions,
  monotonic callback dedupe, safe pre-dispatch retry, and ambiguous post-
  dispatch dead-letter protection. Twilio owns only REST/signature/status
  translation. `backend/modules/integration_config` owns exact-destination
  consent and strict database-only messaging configuration; Appointments owns
  masked inspection/backend-gated requeue, while Integrations owns setup.
- Phase 3 configuration owner:
  `backend/modules/scheduling_manleai_calendar` plus V48. The root config,
  monotonic fence, `local_override` hours, independent service/staff
  eligibility, staff periods, service capacity/resource policy, resource pools,
  exceptions, activation version, constraints, blockers, and immutable events
  form one owner-scoped aggregate. Every mutation requires `action_key` and
  `expected_config_version`; exact replay returns the aggregate, mismatched key
  reuse or stale version conflicts, and scheduling-relevant canonical/child
  changes stale prior activation. `configuration_ready` may pass while an
  operation capability is false; all six capabilities are evaluated
  independently and aggregate `execution_ready` is true only when every one is
  true. This current/new-work readiness view is separate from historical
  target-origin dispatch.
- Phase 4A committed-execution persistence owner: V49 extends
  `availability_quotes`, `availability_quote_slots`, `booking_attempts`,
  `booking_attempt_segments`, `appointments`, and `appointment_services`; adds
  normalized quote slot segments/resources, attempt resource snapshots,
  release-preserving appointment resource history, and append-only
  `manleai_calendar_execution_events`; and mirrors the contract in Ent.
  Internal rows are authority/config/target-version fenced, prohibit fake POS
  provider evidence and `owner_manual`, preserve one appointment root per party
  with concrete guest/service-unit segments, exclude overlapping active staff
  ranges with `[)` semantics, and require committed success evidence.
- Phase 4B database-guard owner:
  `backend/migrations/V50__manleai_calendar_pooled_capacity_guards.sql` validates
  aggregate quote resources and guest-party invariants, locks active pools in
  canonical UUID order, probes base/override capacity across half-open
  boundaries, rejects concurrent over-capacity claims, requires the consumed
  quote/attempt/root appointment/child services/resources/event graphs to match
  exactly in both directions, and makes consumed quote plus committed book
  history immutable. It uses the V49 ledger rather than a second reservation
  ledger.
- Phase 4B runtime owners:
  `execution_availability.go` plans canonical V48 staff-only/pooled policies,
  local hours, schedules, buffers, exceptions, notice/horizon/step, resource
  capacity/overrides, strict local DST, `[)` overlap, deterministic `anyone`
  assignment, sequential same-guest services, and safe cross-guest overlap;
  `execution_repository_aggregate.go` normalizes ordered quantity-one guest
  units, creates two-minute exact aggregate quotes, locks/rechecks the graph and
  conflicts/capacity, and commits one root plus all children;
  `execution_repository.go` retains the common V49 execution repository;
  `executor.go` normalizes exact aggregate quote/slot proof;
  `scheduling.Service` resolves availability-quote origin alongside operation/
  retry/target evidence before dispatch.
- Phase 4C database/runtime owner: V51
  `backend/migrations/V51__manleai_calendar_lifecycle_guards.sql` adds exact
  release-attempt ownership, immutable root provenance/party identity,
  one-version transitions, unique contiguous versioned execution events,
  cancelled-terminal state, exact reschedule quote/attempt/replacement-plan
  equality, and exact cancellation old-plan/zero-active-child integrity.
  `execution_lifecycle.go` locks the target root/version and active plan;
  replans and atomically replaces the whole graph for reschedule; captures and
  releases the exact old graph with no quote for cancel; and loads the event/
  attempt snapshot for historical exact replay after later root mutations.
- Canonical AI-bookable owner controls: the existing service/staff PATCH routes
  and `Repository.UpdateServiceAIBookable`/`UpdateStaffAIBookable` remain in
  `backend/modules/pos`. Enabling locks the exact
  `booking-calendar-reconciliation:<salonID>` advisory key, owner-scoped salon
  row/active provider, scheduling authority/version, canonical row, and
  active-provider link in that fixed transaction order.
  `owner_manual` and `manleai_calendar` may enable eligible local canonical
  rows without POS evidence; `external_provider` additionally requires current
  provider-scoped sync/link evidence. Disabling is owner-scoped and idempotent
  even after provider evidence, active state, or archive state changes. These
  toggles affect future-work eligibility only and never rewrite persisted
  attempt or appointment authority.
- Not-ready behavior: an unsupported ManleAI capability, unknown token, or
  missing executor returns `booking.ErrSchedulingAuthorityNotReady` before a
  write/provider dispatch. Neutral/legacy handlers map that sentinel to generic
  HTTP 409 `SCHEDULING_AUTHORITY_NOT_READY`; quote drift/conflict/capacity maps
  to `AVAILABILITY_QUOTE_STALE`, changed operation proof maps to
  `SCHEDULING_OPERATION_CONFLICT`, stale lifecycle target evidence never
  confirms success, and a closed cutoff remains a typed no-write failure.
  Square gate handlers use bounded messages without wrapped internal details.
- Composition root: `backend/cmd/api/main.go` wires the resolver, external
  adapter, owner-manual repository/service/executor, neutral scheduling handler/
  routes, ManleAI Calendar configuration repository/service/handler/routes and
  registered executor, legacy booking handler, conversation service, and Square
  test-write service. External lease/calendar persistence is
  explicitly authority-fenced. Square webhook target/repair selection stays
  provider/connection-scoped for historical external convergence, while
  downstream calendar writes cannot mutate internal-origin rows.
- Square readiness: `backend/modules/pos_square.Service.Readiness` exposes the
  owner-scoped current `scheduling_authority`; `CanTestBooking` and
  `CanEnableAIBooking` require `external_provider`. New test create/enable fail
  authority-not-ready under internal current modes after replay and lineage
  lookup, while exact external replay, a persisted external safe retry, and
  target-origin test cancellation remain available after a switch. The retry
  uses underlying provider readiness without changing the public new-work gate.
- `manleai_calendar` configuration/readiness, V49 committed-execution
  persistence, V50 aggregate guards, structured multi-guest/multi-service
  staff-only/pooled availability and create, plus V51 whole-root lifecycle
  execution are runtime features. Explicit authority switching is owned by
  `backend/modules/scheduling_authority_switch`, V52-V55, and the Settings
  preview/commit surface; it preserves persisted historical origin.
- Frontend scheduling owner includes
  `frontend/features/dashboard/owner-review-requests.tsx`,
  composed by `appointments-dashboard.tsx`, with helper
  `frontend/lib/api/scheduling-requests.ts` and shared DTOs in
  `frontend/types/api.ts`. It owns request filters, desktop/mobile queue, detail,
  lifecycle transitions, version-conflict reload, and no-delivery/no-confirm
  copy. Phase 3 configuration uses `internal-calendar-setup.tsx`,
  `scheduling-readiness-card.tsx`, parent-scoped Staff/Service panels, and
  `frontend/lib/api/internal-calendar.ts`. Internal create uses
  `internal-appointment-create.tsx` and
  `frontend/lib/api/scheduling-actions.ts`; helpers
  `buildAggregateCreatePayload`, `hasCompleteAggregateQuote`,
  `hasDurableAggregateConfirmation`, `groupAvailabilitySegmentsByGuest`,
  `schedulingOperationForPayload`, `shouldRetainSchedulingReplayProof`, and
  `schedulingActionConflict` own exact quote/payload/child identity,
  guest-group presentation, response-loss replay state, and typed conflict
  handling. `internal-appointment-lifecycle.tsx` plus the same API helper own
  complete internal-row eligibility, strict cutoff display/gating, reviewed
  reschedule proof, cancel confirmation, durable lifecycle result guards, and
  exact uncertain-response replay. None of these is an authority-switch
  surface. Authority selection and explicit reverse-switch review live in
  Settings via `frontend/features/dashboard/scheduling-authority-switch.tsx`
  and `frontend/lib/api/scheduling-authority-switches.ts`; Integrations remains
  provider setup and never selects authority.
- Confirmation contract: `owner_manual` pending only;
  `manleai_calendar` requires one atomic root/child internal commit plus durable
  appointment/attempt IDs, exact child service/resource evidence, and a
  success-only execution event, with no POS evidence. Reschedule additionally
  requires the exact replacement graph and one-version status transition;
  cancel requires terminal status and zero active children while retaining the
  released old-plan attempt snapshot for audit/replay;
  `external_provider` successful provider result plus required booking ID and
  metadata. Party operations are all-or-none.
- Switching contract: explicit owner action only; preserve originating
  authority and never treat integration connect/sync/import/webhook work as a
  switch.
- Tests: `backend/migrations/v46_safety_test.go`,
  `backend/migrations/v47_safety_test.go`,
  `backend/migrations/v48_safety_test.go`,
  `backend/modules/salon/service_test.go`, `backend/modules/scheduling/*_test.go`,
  `backend/modules/scheduling_external_provider/*_test.go`, booking handler/API
  regressions, `backend/modules/scheduling_owner_manual/*_test.go`, and
  conversation owner-manual/scheduling-facade regressions, plus
  `backend/modules/scheduling_manleai_calendar/execution_availability_test.go`,
  `executor_test.go`, `execution_repository_integration_test.go`, configuration
  tests, `backend/migrations/v49_safety_test.go`,
  `backend/migrations/v50_safety_test.go`,
  `backend/migrations/v51_safety_test.go`,
  `backend/modules/conversation/manleai_calendar_scheduling_test.go`,
  `backend/modules/conversation/manleai_calendar_party_scheduling_test.go`, and
  `backend/modules/conversation/manleai_calendar_lifecycle_conversation_test.go`,
  `backend/modules/conversation/fuzzy_service_confirmation_test.go`, and
  `answer_context_test.go` for Phase 4A-4C planning, capacity overrides,
  reverse pool-order concurrency, exact graph/guest/lifecycle invariants,
  target-origin replay, stale/cutoff/no-partial-write evidence, conversation
  authorization, and persisted fuzzy confirmation.

### POS And Square

- Read first: `docs/pos-adapter-layer.md`, `docs/square-integration.md`,
  `docs/api.md`.
- Backend owner: `backend/modules/pos`, `backend/modules/pos_square`,
  `backend/modules/integration_config`.
- Webhook/repair owner: `backend/modules/pos_square/webhook.go`,
  `webhook_repository.go`, `webhook_processor.go`, public registration in
  `backend/modules/pos_square/routes.go`, worker wiring in
  `backend/cmd/worker/main.go`, schema in
  `backend/migrations/V41__square_booking_webhooks_and_calendar_repair.sql`.
  Authenticated owner operations are owned by
  `webhook_operations.go`, `webhook_operations_repository.go`, the same
  handler/routes boundary, and
  `backend/migrations/V60__square_webhook_operations.sql`; the public filter
  rejects `ignored`, while an unfiltered safe list may expose it read-only.
- Full snapshot, range-calendar import, and booking-dispatch fence owner:
  `backend/modules/pos/repository.go`, `backend/modules/booking/service.go`,
  `backend/modules/booking/repository.go`, `backend/modules/pos_square/adapter.go`, and
  `backend/migrations/V43__pos_snapshot_generation.sql`.
- Frontend owner: `frontend/features/integrations/square-integration.tsx`,
  `frontend/features/integrations/square-webhook-operations.tsx`,
  `frontend/lib/api/square-webhook-events.ts`,
  `frontend/features/dashboard/services-dashboard.tsx`,
  `frontend/features/dashboard/staff-dashboard.tsx`,
  `pos-calendar/features/calendar/pos-calendar-client.tsx` for calendar sync.
- Data owner: `pos_connections`, `pos_entity_links`, `pos_sync_jobs`,
  `pos_sync_logs`, `pos_errors`, `salon_integration_configs`,
  `square_booking_webhook_events`, `square_calendar_repair_state`,
  `square_booking_webhook_actions`,
  `salons.active_pos_provider`.
- Tests: `backend/modules/pos/*_test.go`,
  `backend/modules/pos_square/*_test.go`,
  `backend/migrations/v60_safety_test.go`,
  `backend/modules/integration_config/service_test.go`,
  `backend/modules/config_transfer/service_test.go`.
- Skill/subagent: `pos-adapter-slice`, `pos_backend_reviewer`,
  `security_privacy_reviewer`.

### Booking, Availability, Reschedule, Cancel

- Read first: `docs/scheduling-authority.md`, `docs/architecture.md`,
  `docs/pos-adapter-layer.md`, `docs/api.md`, `CONTEXT.md`.
- Authority-sensitive entrypoint owner: `backend/modules/scheduling` with the
  ready adapters in `backend/modules/scheduling_external_provider` and
  `backend/modules/scheduling_owner_manual`; additive neutral HTTP routes live
  in `backend/modules/scheduling/routes.go`, legacy external routes retain
  `backend/modules/booking.Handler` through `booking.HandlerService`,
  conversation callers live in `backend/modules/conversation`, and Square test
  writes receive the same facade from `backend/cmd/api/main.go`. New
  availability/actions can use the current setting; operation origin is read
  across external attempts and owner-manual requests, retry/target origin owns
  historical dispatch, and all origins present must agree. Availability accepts
  exactly one of retry or target origin. Only an exact safe, unsuperseded
  external book fallback can use retry availability; it performs a fresh
  provider lookup and creates a V53 `retry_origin` quote rather than reusing
  prior quote evidence.
- Provider-free scheduling history: `scheduling.Service.RescheduleCandidates`,
  `ReplayCreate`, and `ReplayCancel` delegate directly to the booking history
  service without executor selection or provider calls.
- External-provider execution/history owner: `backend/modules/booking`.
- Owner-manual execution/history owner:
  `backend/modules/scheduling_owner_manual`; its `scheduling_requests`, ordered
  segments, and append-only events are separate from booking attempts,
  appointments, POS errors, and reconciliation. The Appointments owner-review
  queue can transition request status but cannot confirm or change an
  appointment.
- Integrity/reconciliation owner: `backend/modules/booking/handler.go`,
  `service.go`, `repository.go`, `types.go`, `routes.go`, and
  `backend/migrations/V39__booking_integrity_reconciliation_quotes.sql`.
  Phase-aware booking lease recovery is owned by
  `Repository.ExpireBookingOperationLeases`,
  `Repository.SweepExpiredBookingOperationLeases`, and worker wiring in
  `backend/cmd/worker/main.go`; expired pre-dispatch claims are retry-safe,
  while expired in-flight claims remain reconciliation-required unless exact
  authoritative calendar truth converges under the advisory-first lock. Lease
  candidates and provider-calendar persistence/matching/reconciliation
  explicitly require `scheduling_authority=external_provider`; internal-origin
  rows cannot be updated by that maintenance. Exact external create-mirror
  canonicalization preserves existing confirmation provenance, fills only
  missing `confirmed_at`/`confirmation_source`, leaves
  `confirmed_by_user_id` unset without a real actor, and is timestamp-idempotent
  on repeated recovery.
  Candidate routing is
  `GET /api/salons/:id/booking-reconciliations/:attempt_id/candidates`;
  resolution uses the same exact repository predicate under lock, and manual
  resolution/calendar import share a salon-scoped transaction advisory lock.
  V39 normalizes only duplicates proven `not_started`, prefers a
  dispatched/unknown canonical attempt, and aborts when one fingerprint group
  contains multiple outcomes that require reconciliation. Safely superseded
  rows point to their canonical attempt, close task bookkeeping as
  `resolved/superseded`, and cannot be listed, resolved, lease-swept,
  reacquired, started, or finalized.
- Availability-quote retention owner:
  `backend/modules/booking/quote_cleanup_processor.go`,
  `backend/modules/booking/repository.go`, worker wiring in
  `backend/cmd/worker/main.go`, and V39 cleanup indexes. Every five minutes the
  bounded cleanup drains at most eight batches of 250 quotes, keeps a 24-hour
  unconsumed-expiry grace and 30-day orphan-consumed audit window, and
  preserves every booking-attempt reference.
- Frontend owner: `frontend/features/dashboard/appointments-dashboard.tsx`
  owns authority/version evidence, mixed-origin rows, separate reschedule/cancel
  capability gates, owner-manual review, masked delivery operations, internal lifecycle, and historical
  external operations; `frontend/features/integrations/square-integration.tsx`
  owns provider test booking and owner-SMS setup; `pos-calendar/features/calendar/pos-calendar-client.tsx`
  owns standalone mixed-origin display, external-only write controls, and
  realtime booking toasts.
- Data owner: `salon_settings.scheduling_authority`; V46 authority snapshots
  and confirmation provenance on `booking_attempts`,
  `booking_attempt_segments`, `appointments`, `appointment_services`, and
  `availability_quotes`; plus the current `booking_attempts` external-provider
  operation ledger (operation key, fingerprint,
  processing lease, provider outcome, retry policy, reconciliation status,
  target provider-version baseline for appointment mutations), `booking_attempt_segments`,
  `availability_quotes`, `availability_quote_slots`,
  `booking_reconciliation_tasks`, `booking_reconciliation_events`,
  `appointments`, `appointment_services`, `appointments.pos_sync_status`,
  `appointments.last_pos_synced_at`, `appointments.pos_sync_error`, `pos_errors`,
  `owner_notifications`; plus V47 owner-manual `scheduling_requests`,
  `scheduling_request_segments`, `scheduling_request_events`, and reciprocal
  call-session/notification request links; plus V56 notification claim fields,
  `owner_notification_delivery_attempts`,
  `owner_notification_delivery_events`, and
  `owner_notification_delivery_actions`.
- Tests: `backend/modules/booking/service_test.go`,
  `backend/modules/conversation/service_test.go`,
  `backend/modules/conversation/owner_manual_scheduling_test.go`,
  `backend/modules/scheduling_owner_manual/*_test.go`,
  `backend/modules/voice_twilio/phone_booking_flow_test.go`,
  `backend/modules/notification_delivery/*_test.go`,
  `backend/modules/notification_twilio/*_test.go`, and
  `backend/migrations/v56_safety_test.go`.
- Skill/subagent: `booking-safety-tdd`, `pos_backend_reviewer`,
  `production-release-review`.

### Owner Notification Delivery

- Read first: `docs/operations/owner-notification-delivery.md`, `docs/api.md`,
  `docs/deployment.md`, and `docs/scheduling-authority.md`.
- Backend owner: `backend/modules/notification_delivery` for provider-neutral
  state/retry/replay; `backend/modules/notification_twilio` for Twilio REST and
  signed callbacks; `backend/modules/integration_config` for strict salon-
  scoped messaging config; `backend/cmd/worker/main.go` for the stable job.
- Frontend owner: `frontend/features/dashboard/owner-notification-deliveries.tsx`
  and `frontend/lib/api/owner-notification-deliveries.ts` for operations;
  `frontend/features/integrations/square-integration.tsx` for Twilio setup.
- Data owner: V56 claim fields on `owner_notifications`, plus
  `owner_notification_delivery_attempts`,
  `owner_notification_delivery_events`, and
  `owner_notification_delivery_actions`; provider config remains in
  `salon_integration_configs`.
- Safety boundary: request creation/queueing, provider acceptance, sent, and
  delivered are distinct; unknown post-dispatch outcome cannot retry; owner
  API is tenant-fenced and PII/provider-ID safe. This V56 owner-delivery
  boundary neither owns nor infers customer consent; V59
  `backend/modules/customer_notification` owns customer appointment SMS, and
  neither flow uses a message-body keyword parser.
- Tests: `backend/modules/notification_delivery/*_test.go`,
  `backend/modules/notification_twilio/*_test.go`,
  `backend/modules/integration_config/service_test.go`, and
  `backend/migrations/v56_safety_test.go`.
- Skill/subagent: `database-workflow`, `production-release-review`,
  `security_privacy_reviewer`.

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
  contracts and semantic-check service/route: `backend/modules/voice/types.go`,
  `service.go`, `handler.go`, and `routes.go`; OpenAI structured-turn, input,
  and output adapters: `backend/modules/voice_openai/adapter.go`, `realtime.go`,
  `speech_stream.go`, and `speech_resampler.go`; paced Twilio output buffering:
  `backend/modules/voice_twilio/speech_playout.go`; bounded
  confidence-rejection recovery policy: `backend/modules/voice_twilio/realtime_recovery.go`.
  The default
  dashboard mode `streaming_tts` uses Realtime only for input, sends backend-approved
  text to dedicated Speech TTS, requests raw signed little-endian PCM 24 kHz,
  applies a stateful anti-aliasing 3:1 resampler, and encodes PCMU 8 kHz. It sends
  one bounded 200 ms startup block, then drains a bounded backpressure queue on a
  monotonic 20 ms cadence instead of using provider HTTP chunk timing. Provider
  completion and Twilio playout completion are separate lifecycle stages. Replies
  use a bounded typed scheduler with input generations and workflow priority;
  backend results supersede stale recovery/progress output, no new TTS begins
  during caller speech or pending transcription, and terminal output latches once.
  Low-priority recovery TTS has a four-second first-provider-byte budget. Streaming
  barge-in cancels immediately even within the legacy guard. Application request
  IDs, Twilio clear/mark, and generation checks reject late audio.
  `buffered_realtime` is the legacy fallback that still binds provider
  response IDs and validates the complete output transcript before release. GA input
  requires transcription logprobs and applies
  location-neutral policy-aware mean, low-tail, and VAD-coherence admission;
  `automatic` starts each call at `standard` and switches that call to
  `strong_noise_rejection` after structured degraded-audio evidence; confidence-rejected
  transcripts do not enter conversation state. Provider transcript-completion
  replays are deduplicated before admission/recovery by their item/transcript key.
  Rejected transcripts use bounded
  in-stream recovery and do not trigger recording/gather fallback: short retry,
  scoped retry with the last approved question, noise coaching, then a typed
  `voice_input_unintelligible` owner callback handoff on the fourth consecutive
  rejection when the caller number is callable. Missing caller ID completes with
  call-again guidance without promising a callback. Accepted transcripts reset
  the streak. Automatic, standard, and stronger-rejection input use near-field
  noise reduction; minimal processing does not force it. Legacy `noisy_salon`,
  `balanced`, and `quiet_room` config values normalize to canonical location-neutral
  policies. Accepted and rejected transcript timing events retain PII-free
  decision/reason, configured/effective policy, adaptive action/audio-quality signal,
  item ID, mean/min logprob, token count, VAD duration, rejection streak, and
  recovery action diagnostics when available. `backend_turn_done` accumulates
  route/config, session-load, answer-context, turn-router, semantic-interpreter,
  availability/POS, and turn-persistence durations through
  `backend/modules/voice/backend_turn_diagnostics.go`; it records only safe
  router/interpreter labels including `turn_semantic_contract`, scoped-context
  counts, reply kind/input generation/suppression, and bounded interpreter
  provider/status/stage/request-ID/error-type/error-code/error-parameter/schema-
  fingerprint/circuit correlation fields, never transcript,
  provider response body, or caller data. Speech output timing/failures retain correlation
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
- Consultation owner UI: each structured consultation profile stays inside its
  parent Service edit form; the salon-wide runtime toggle and coverage stay in
  Settings; per-call needs, recommendations, profile revisions, selection, and
  exit result are shown in Calls from typed dialog state.
- Data owner: `knowledge_items`, `owner_corrections`, `service_aliases`,
  `service_categories`, `service_category_aliases`,
  active `service_taxonomy_releases` plus its category/concept/alias children,
  `service_consultation_profiles`, the legacy summary fallback in
  `services.ai_description`, `salon_settings.consultation_enabled`, persisted
  `call_sessions.dialog_state` including the scoped catalog-backed
  `fuzzy_service_confirmation` pending state, and diagnostic metadata on
  transcript messages.
- Tests: `backend/modules/conversation/service_understanding_test.go`,
  `backend/modules/conversation/service_understanding_eval_test.go`,
  `backend/modules/conversation/fuzzy_service_confirmation_test.go`,
  `backend/modules/conversation/conversation_act_test.go`,
  `backend/modules/conversation/service_test.go`,
  `backend/modules/training/service_test.go`,
  `backend/modules/voice/act_interpreter_test.go`, and
  `backend/modules/voice_openai/adapter_test.go`, and the generated corpus plus
  review artifacts under `backend/modules/conversation/testdata`.
- Skill/subagent: `voice-ai-runtime`, `salon-ops-workflow`,
  `business-logic-overlap-analysis`, `repo_mapper`.

### SaaS Actor Context, Membership, Delegation, And PII Grants

- Read first: `docs/architecture.md`, `docs/api.md`,
  `docs/operations/migration-rollback.md`, and
  `docs/operations/release-gate.md`.
- Authentication owner: `backend/internal/middleware/auth.go` and
  `backend/modules/auth/repository.go`; signed tokens prove identity only and
  current database state creates `ActorContext`.
- Authorization owner: `backend/modules/access`; route code supplies the fixed
  Tenant or Platform surface plus exact salon, capability, and optional PII
  scope. Do not accept these from headers, request bodies, or JWT claims.
- Data owner: V64 `salon_memberships`, `platform_role_assignments`,
  `platform_salon_assignments`, `platform_salon_assignment_permissions`,
  `platform_pii_access_grants`, `access_control_actions`, and
  `access_control_events`; capability delegation comes from
  `permissions.delegation_scope`; `backend/modules/access` publishes capability
  dependency requirements and nested user summaries for access rows.
- Platform support history schema: V75; direct-Admin/delegated-Ops authority correction: V76
  `platform_support_access_requests` and
  `platform_support_access_request_permissions`. Platform Admin access is
  direct from the current role capability and includes the PII scopes required
  by the control plane. Platform Ops first needs its ordinary exact-salon
  assignment capability and then an Admin-granted, time-bounded feature
  authorization. Calls authorization additionally creates and requires the
  request-linked `calls` PII child and grant. Grant, legacy-pending cancellation,
  and revocation live in the Platform tenant Access tab; Tenant Settings has no
  Platform authorization workflow. Every authorized Platform feature action
  records the actual actor, salon, exact capability/PII scope, method, and route
  in immutable access audit before domain work proceeds.
- Principal-realm owner: V74 `users.principal_scope` plus composite foreign
  keys. Salon ownership, membership, and legacy `user_roles` require `tenant`;
  Platform roles, salon delegation, and PII grants require `platform`. Scope is
  immutable, independent from `live|sample_test`, and a person who needs both
  realms uses separate login identities. The migration fails on historical
  mixed evidence rather than choosing a side.
- Frontend owner: `frontend/features/platform/access-console.tsx` owns only
  Platform user creation/editing and global role/status governance.
  `frontend/features/platform/tenant-access-console.tsx` owns salon memberships,
  exact-salon Platform Ops baseline delegation, Admin-granted temporary feature
  authorization, and temporary non-Calls Ops PII grants under
  `/platform/tenants/[tenantId]/access`; Calls PII is linked to the temporary
  Calls authorization. Shared rendering lives
  in `frontend/features/platform/access-ui.tsx`, but the API lookup contracts are
  separate fixed Platform and Tenant endpoints; capability
  selection consumes `frontend/lib/api/access-contract.ts` and the backend
  catalog rather than a frontend permission list.
- Fixed invariants: `salons.owner_user_id` remains owner source of truth;
  Platform Admin may suspend/reactivate the Owner membership without changing
  that ownership identity or demoting its role; the last active Platform Admin
  remains protected; Platform Admin is direct across all Platform tenant tabs;
  Platform Ops is exact-salon, exact-capability, and time-bounded where feature
  authorization is required; Calls authorization and linked PII last no more
  than 24 hours, non-PII support lasts no more than 30 days; access actions are
  replay-safe and audit rows immutable.
- Operator bootstrap owner: `backend/cmd/platform-access/main.go`; it creates
  one dedicated Platform identity from an owner-only password file while no
  active Platform Admin exists, supports exact replay, and never promotes a
  Tenant identity.
- Sample-test provisioning owner: `backend/sampledata` and
  `backend/cmd/sample-data/main.go`; it is separate from startup migration and
  refuses live users/salons. Root `make restart` and protected pre-live CD own
  invocation; `deploy/postgres-*-preflight.sh` and the post-migration profile
  guard own reset/refusal policy. The `live` profile must never invoke it.
- Shared Business owner: `backend/modules/business` and V65. Tenant and
  Platform routes are fixed by route registration, use the same canonical
  records and mutation rules, and never accept surface or actor identity from
  the caller. The Phase 4 UI cutover is complete; new SaaS UI work must use
  `/business/*`.
- Technical/Operations owner: the Platform handlers and V66/V70 listed in the
  Backend Module Map. Tenant users have no technical provider UI or route.
- Runtime/database owner: `backend/internal/databasecontext`, the contextual
  connector/runtime-role verifier, `backend/modules/tenant_runtime`, and
  V67-V76. RLS is tenant defense-in-depth, public reads use only V71, Platform
  PII reads require the exact V72 scope, and V75 requires active base Platform
  authority; V76 makes Platform Admin direct and keeps current Admin-granted Ops
  authorization for supported feature rows.
- Tests: `backend/modules/access/service_test.go`,
  `backend/modules/access/routes_test.go`,
  `backend/modules/access/repository_integration_test.go`,
  `backend/internal/middleware/auth_test.go`,
  `backend/modules/auth/repository_integration_test.go`,
  `backend/migrations/v74_safety_test.go`,
  `backend/migrations/v75_safety_test.go`,
  `backend/migrations/v64_safety_test.go`,
  `backend/modules/business/*_test.go`, and
  `backend/migrations/v65_safety_test.go`.

### Services, Staff, Categories, Customers

- Read first: `docs/architecture.md`, `docs/api.md`,
  `docs/canonical-pos-ownership-checklist.md`.
- Backend owner: `backend/modules/pos` for services/staff/categories and
  `backend/modules/customer` for customers.
- Frontend owner for the full Services workflow:
  `frontend/features/dashboard/services-dashboard.tsx`, reused by Tenant
  `/dashboard/services` and Platform `/platform/tenants/[tenantId]/services` via
  a surface-aware API adapter. Staff and Customers continue through shared
  `frontend/features/business/*` editors.
- Data owner: `services`, `service_consultation_profiles`, `staff`, `customers`,
  `service_categories`, `service_category_aliases`, `service_aliases`,
  `service_taxonomy_releases`, `service_taxonomy_categories`,
  `service_taxonomy_category_aliases`, `service_taxonomy_service_concepts`,
  `service_taxonomy_service_aliases`, and `pos_entity_links`; field authority is
  derived by `backend/modules/pos/service.go` from canonical/provider identity
  plus adapter capabilities and is consumed by
  `frontend/features/dashboard/pos-field-authority.tsx`.
- Placement invariant: service aliases are structured children of one service;
  category aliases are structured children of one service category. Both stay
  inside the full Services parent workflow and feed catalog-backed service
  understanding. They are never converted into free-text AI Training
  knowledge. AI Training may apply an explicitly reviewed correction to a
  canonical service alias, but the resulting record still lives in
  `service_aliases` and is managed from Services.
- Tests: `backend/modules/pos/service_test.go`,
  `backend/modules/customer/service_test.go`.
- Skill/subagent: `salon-dashboard-ui`, `salon-ops-workflow`,
  `pos_backend_reviewer`, `frontend_product_reviewer`.

### Party Booking Requests

- Read first: `CONTEXT.md`, `docs/architecture.md`, `docs/api.md`.
- Backend owner: `backend/modules/conversation` party files plus
  `backend/modules/scheduling_manleai_calendar` and V49/V50 for Phase 4B
  aggregate internal availability/create, and `backend/modules/booking` for
  external-provider booking.
- Frontend owner: `frontend/features/dashboard/calls-dashboard.tsx` for party
  review records and `frontend/features/dashboard/internal-appointment-create.tsx`
  inside Appointments for owner-created internal parties.
- Data owner: structured session party plan and aggregate quote proof;
  `party_booking_requests` for exception review; internal root
  attempts/appointments plus service/resource children for `manleai_calendar`;
  provider attempts/appointments for `external_provider`.
- Tests: `backend/modules/conversation/service_test.go`,
  `backend/modules/conversation/manleai_calendar_party_scheduling_test.go`,
  `backend/modules/scheduling_manleai_calendar/execution_repository_integration_test.go`,
  `backend/migrations/v50_safety_test.go`, and
  `backend/modules/voice_twilio/phone_booking_flow_test.go`.
- Skill/subagent: `voice-ai-runtime`, `salon-ops-workflow`,
  `pos_backend_reviewer`.

### Historical Scheduling Replay Copy

- Backend owner: `backend/modules/conversation/service_booking_flow.go` for
  client wording after `scheduling.ActionResult.Replayed`; scheduling evidence
  and immutable event replay remain owned by the scheduling authority modules.
- Rule: exact historical book/reschedule replay preserves the corresponding
  `booking_confirmed`/`booking_rescheduled` audit outcome but states that the
  operation succeeded at that time and current status may have changed. It
  never uses current-state “confirmed”, “has been booked”, or “has been
  rescheduled” wording. Cancel replay remains terminal/current-safe. Exact
  duplicate conversation-event replay continues to return its persisted
  `ReplayAIMessage` unchanged.
- Tests:
  `backend/modules/conversation/manleai_calendar_scheduling_test.go` and
  `backend/modules/conversation/manleai_calendar_lifecycle_conversation_test.go`.

### Configuration Transfer

- Read first: `docs/architecture.md`, `docs/api.md`,
  `docs/deployment.md`.
- Backend owner: `backend/modules/config_transfer`.
- Frontend owner:
  `frontend/app/platform/tenants/[tenantId]/transfer/page.tsx`,
  `frontend/features/platform/platform-configuration-transfer.tsx`,
  `frontend/lib/api/platform-configuration-transfer.ts`, and shared summary
  rendering in `frontend/features/configuration-transfer/import-preview.tsx`.
- Data owner: Platform schema v9 plus v8 upload compatibility and guarded
  content-only v7-to-v8 canonicalization. V77 owns safe reviewed runs/events
  and collection fences; canonical values remain owned by
  Business, Services, Training, salon settings, and integration config.
  Scheduling authority/provider selection, connections, secrets,
  provider-imported hours, service/staff/customer/appointment/call records, and
  operational history are excluded. `active_pos_provider` is report-only;
  v9 `integration_providers` contains only persisted salon-scoped source rows,
  and absent providers never use legacy environment fallback.
- Authorization boundary: actual Platform actor; every selected source read and
  target write uses the section's existing Business/Services/Training/Technical
  capability. Run listing uses `audit.read`. No transfer bypass capability.
- Concurrency boundary: apply acquires `backend/modules/scheduling/fence`, locks
  the reviewed run and source/target resource fences, and rechecks the source
  fingerprint plus exact scheduling authority/version before atomic domain,
  Business/Technical-ledger, and transfer-event writes.
- Compatibility boundary: earlier Tenant/onboarding v8-v1 handlers and UI code
  are retained but not registered/rendered after SaaS cutover. Mounted Platform
  routes accept v7 only with explicit content sections (categories, aliases,
  consultation profiles, knowledge), canonicalize it to v8 before fingerprint
  and audit, and reject runtime/provider v7 plus v1-v6.
- Tests: `backend/modules/config_transfer/*_test.go`,
  `backend/migrations/v77_safety_test.go`, and
  `frontend/lib/api/platform-configuration-transfer.test.ts`.
- Skill/subagent: `business-analysis`, `production_release_planner`,
  `security_privacy_reviewer`.

### Public Catalog

- Read first: `docs/architecture.md`, `docs/api.md`, `DESIGN.md`.
- Backend owner: `backend/modules/public_catalog`,
  public-catalog settings in `backend/modules/salon`.
- Frontend owner: `frontend/features/business/business-settings.tsx`.
- Landing owner: `frontend/app/s/[slug]/page.tsx` and
  `frontend/features/public/public-salon-page.tsx`; the separate `landing/`
  app is compatibility-only during deployment cutover.
- Data owner: V71 `read_public_catalog` returns the public-safe salon profile
  plus services/staff/hours eligible for the persisted scheduling authority;
  public DB scope cannot select base-table rows; the public flag is not
  sufficient when current readiness is stale.
- Publish transaction owner: `backend/modules/salon/repository.go`, using the
  shared scheduling advisory fence and optional authority-version comparison.
- Tests: `backend/modules/public_catalog/service_test.go`,
  `backend/modules/salon/public_catalog_integration_test.go`.
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
  `voice_audio_outputs`; V62 owns the composite
  `party_booking_requests(salon_id, call_session_id)` parent fence.
- Redaction contract: `redactSessionInTx` owner/tenant-fences the session and
  clears customer/contact fields plus every PII-capable conversation state
  container: `dialog_state`, `party_plan`, `booking_segments`,
  `reschedule_candidates`, offered-slot/quote proof, transcript metadata/body,
  handoff/party summaries and guest details, webhook payloads, and audio bytes.
  Re-redaction is idempotent, preserves the original `redacted_at`, repairs
  residual JSONB on historically redacted rows, and remains compatible with
  later expiry-worker audio cleanup.
- Tests: `backend/modules/conversation/service_test.go`,
  `backend/modules/conversation/retention_integration_test.go`,
  `backend/modules/voice/service_test.go`. The PostgreSQL retention integration
  suite covers complete GET/list/row clearing, cross-owner non-mutation,
  historical repair, stable re-redaction timestamps, immediate future-audio
  clearing, and idempotent later expiry processing.
- Skill/subagent: `security_privacy_reviewer`,
  `production-release-review`.

### Calls Scheduling Result Evidence

- Read first: `docs/scheduling-authority.md`, `docs/api.md`, `DESIGN.md`.
- Backend projection owner:
  `backend/modules/conversation/scheduling_result_evidence.go`, invoked by
  session list/detail/replay reads in
  `backend/modules/conversation/repository.go`.
- Frontend consumer:
  `frontend/lib/api/conversation-scheduling-evidence.ts` and
  `frontend/features/dashboard/calls-dashboard.tsx`.
- Data owners: `scheduling_requests` for nonconfirming `owner_manual` request
  evidence; `booking_attempts`, `manleai_calendar_execution_events`,
  `appointments`, and their immutable child graphs for completed
  `manleai_calendar` operations; provider-success attempts and matching
  external appointment mirrors for `external_provider`.
- Safety boundary: the projection is owner/tenant scoped, makes no provider
  calls, fails closed for unknown/partial/legacy evidence, requires every split
  party root, and separates the exact historical operation result from the
  appointment's current lifecycle state. The Calls UI must not treat local
  session `appointment_id`, `booking_attempt_id`, or `outcome` as confirmation.
  Pending-approval display uses the validated request
  `target_scheduling_authority`; it does not infer the selected target from
  local IDs or presentation copy.
- Platform read boundary: V75/V76 lets a directly authorized Admin or currently
  Admin-granted Platform Ops Calls request
  select only scheduling rows durably linked to an authorized call session. It
  grants no general Appointments listing/mutation, no scheduling execution, and
  no provider write. The full Calls component is shared; Platform controls are
  hidden or rejected by exact `calls.read|manage|simulate|redact` capabilities,
  and every Calls capability additionally requires the request-linked `calls`
  PII grant.
- Tests:
  `backend/modules/conversation/scheduling_result_evidence_test.go`,
  `backend/modules/conversation/scheduling_result_evidence_integration_test.go`, and
  `frontend/lib/api/conversation-scheduling-evidence.test.ts`.
- Mapping impact: map updated for the new backend projection and frontend
  evidence consumer.

### Scheduling And Notification PII Retention

- Read first: `docs/architecture.md`, `docs/scheduling-authority.md`,
  `docs/api.md`, `docs/deployment.md`.
- Backend owner: `backend/modules/scheduling_retention`,
  `backend/cmd/worker/main.go`; retained read shaping remains with
  `backend/modules/scheduling_owner_manual`,
  `backend/modules/notification_delivery`, and
  `backend/modules/customer_notification`.
- Frontend owner: `frontend/features/dashboard/owner-review-requests.tsx`,
  `frontend/features/dashboard/owner-notification-deliveries.tsx`, with shared
  redaction markers in `frontend/types/api.ts`.
- Data owner: V61 retention/redaction fields on `scheduling_requests`,
  `scheduling_request_segments`, `scheduling_request_events`,
  `owner_notifications`, `customer_notification_deliveries`, and
  `voice_audio_outputs`. Consent/STOP routing state remains owned by
  `customer_sms_consents` and is not redacted by this processor.
- Tests: `backend/modules/scheduling_retention/*_test.go`,
  `backend/modules/conversation/retention_integration_test.go`,
  `backend/migrations/v61_safety_test.go`, and
  `backend/cmd/worker/retention_registration_test.go`.
- Mapping impact: additions or ownership changes in any V61 class, retention
  field, DTO, worker registration, queue metric, or owner UI require a map
  update.

## Triage Keyword Index

| Keywords / symptoms | Start with | Then inspect |
| --- | --- | --- |
| PostgreSQL backup, pg_dump, pg_restore, restore drill, backup checksum, app_schema_migrations checksum, isolated restore, encrypted-private artifact, database recovery, migration rollback, image rollback, previous-image compatibility, expand/migrate/contract, RPO, RTO | `docs/operations/postgres-backup-restore.md`, `docs/operations/migration-rollback.md`, `deploy/postgres-backup.sh`, `deploy/postgres-restore-drill.sh`, `deploy/postgres-verify-restore.sh` | `.github/workflows/ci-cd.yml`, `.github/workflows/postgres-restore-drill.yml`, `docs/deployment.md`, `docs/production-readiness-checklist.md`, `backend/internal/database/migrate.go`, `backend/migrations`, `docker-compose.prod.yml` |
| scheduling load, concurrency harness, owner_manual replay load, transition CAS, internal party create load, pooled resource conflict, authority switch commit race, duplicate appointment, orphan evidence, expected conflict, p50, p95, p99, throughput, DB pool, non-production attestation, dedicated load database, migration checksum fingerprint, production capacity proof | `docs/operations/scheduling-load-harness.md`, `backend/cmd/scheduling-load-harness/main.go`, `backend/internal/schedulingload` | `docs/production-readiness-checklist.md`, scheduling owner repositories/services, `app_schema_migrations`, opt-in fresh-PostgreSQL integration test and sanitized schema-v1 report |
| operations health, worker heartbeat, job run, stale job, abandoned run, distributed worker lease, JOB_LEASE_EXPIRED, JOB_PANIC, JOB_RUN_FAILED, queue backlog, oldest queued, dead letter count, operations/status, V57 | `docs/operations/operations-health.md`, `backend/modules/operations_health`, `backend/cmd/worker/scheduler.go` | V57 heartbeat/run tables and constraints, worker job registration, authenticated route, Settings operations-health component/API/types, scheduler and PostgreSQL integration tests |
| owner notification delivery, owner operational SMS, notification_delivery, V56, delivery attempt, dispatch_started_at, provider accepted, SMS queued not delivered, Twilio MessageSid, messaging service SID, status callback, inbound callback, X-Twilio-Signature, owner SMS consent, masked destination, dead letter, DELIVERY_OUTCOME_UNKNOWN, safe requeue, duplicate SMS, notification backlog | `docs/operations/owner-notification-delivery.md`, `backend/modules/notification_delivery`, `backend/modules/notification_twilio` | V56 delivery fields/attempt/event/action ledgers, strict Twilio messaging resolver in `backend/modules/integration_config`, worker job registration, Appointments delivery component/API/types, Integrations owner-SMS settings, operations health evidence, migration/repository/signature tests |
| customer SMS, appointment updates, explicit SMS consent, customer_sms_consents, customer_notification_deliveries, V59, consent requested, owner attestation, OptOutType, STOP, START, HELP, quiet hours, customer_notification_delivery, customer_notifications queue, source snapshot stale, external version zero, customer SMS requeue | `backend/modules/customer_notification`, `backend/migrations/V59__customer_sms_consent_delivery.sql`, `docs/api.md` | conversation post-review consent hook, database request/appointment outbox triggers, Twilio Account/Messaging Service/sender binding, worker registration, Settings policy card, appointment and owner-review request child panels/API, operations-health queue/job, V59/repository/processor/signature tests |
| scheduling authority switch, preview switch, commit switch, reverse switch, rollback_of_switch_run_id, switch readiness conflict, live external execution, V52, V53, V54, V55 | `backend/modules/scheduling_authority_switch`, `docs/scheduling-authority.md` | authenticated preview/latest/detail/commit routes, shared scheduling fence, provider-owned transactional readiness via `pos.Repository.WithSchedulingFenceTx`, immutable switch events, Settings `scheduling-authority-switch.tsx`, API helper, migration safety and PostgreSQL integration tests |
| legacy POS field, pos_booking_id authority inference, pos_appointment_id confirmation, active_pos_provider authority, deprecated provider alias, internal POS field, compatibility removal gate, unknown authority mapper | `docs/operations/owner-first-compatibility.md`, `frontend/lib/api/scheduling-evidence.ts`, `pos-calendar/lib/scheduling-evidence.ts` | booking/scheduling DTO JSON contracts, external-only provider/reconciliation consumers, internal NULL/omitted invariants, public catalog and configuration-transfer exclusions, authority counterexample tests |
| scheduling authority, owner-first, owner_manual, manleai_calendar, external_provider, scheduling facade, CurrentSchedulingAuthority, ResolveCreateSchedulingAuthority, ResolveAvailabilityQuoteSchedulingAuthority, operation origin, quote origin, booking_attempts, scheduling_requests, retry origin, target appointment origin, cross-source authority conflict, verified_slots, request_only, confirmed_appointment, pending_owner_review, external_fallback_pending, scheduling-availability, scheduling-actions, owner review requests, contacted, resolved, dismissed, expected version, action key, request event, V47, V48, V49, V50, V51, call session request link, owner notification queued, no delivery claim, canonical catalog without POS link, provider-free replay, exact committed replay, historical event replay, response loss, ReplayCreate, ReplayCancel, reschedule candidates, Square readiness authority, SCHEDULING_AUTHORITY_NOT_READY, AVAILABILITY_QUOTE_STALE, SCHEDULING_OPERATION_CONFLICT, no fake POS evidence, no reconciliation, scheduling_authority_version, authority_appointment_version, target_authority_appointment_version, config version, configuration_ready, execution_ready, capabilities, staff_only_availability, staff_only_create, pooled_capacity, party_create, reschedule capability, cancel capability, LIFECYCLE_ENGINE_UNAVAILABLE, local_override hours, activated_version, stale activation, MANLEAI_CALENDAR_CONFIG_VERSION_CONFLICT, released_by_attempt_id, active_child_count, resource pool, capacity override, sorted pool lock, exact graph, guest invariant, party_size, guest_reference, root appointment, child evidence, staff weekly schedule, service policy, calendar exception, authority switch, internal calendar confirmation, durable internal appointment ID | `docs/scheduling-authority.md`, `backend/modules/scheduling`, `backend/modules/scheduling_owner_manual`, `backend/modules/scheduling_external_provider`, `backend/modules/scheduling_manleai_calendar` | `salon_settings.scheduling_authority` and DB-managed authority version; operation/quote origins in `booking_attempts`/`scheduling_requests`/`availability_quotes`; target origins in `appointments`; V48 configuration owner; V49 execution ledger; V50 aggregate quote/capacity/graph/guest/history guards; V51 lifecycle release/version/event/plan guards; neutral handlers/routes; `execution_availability.go`, `execution_repository.go`, `execution_repository_aggregate.go`, `execution_lifecycle.go`, `executor.go`; owner-review/config/internal-create/lifecycle components and API helpers; conversation aggregate/lifecycle proof fences; external adapter, booking compatibility, Square readiness and external maintenance |
| confirm, confirmed, rescheduled, cancelled, internal root appointment ID, booking attempt ID, target version, result version, status, active child count, complete children, partial children, exact committed graph, replacement graph, released old-plan snapshot, resource allocations, response loss, exact committed replay, historical event replay, replayed, no provider evidence, fallback pending, provider pending, POS failed, no booking id, duplicate appointment, operation key, request fingerprint, retry lineage, Square test retry, superseded attempt, retry blocked, reconciliation required, candidate match, not_started lease, pre-dispatch crash, lease sweep, POS timeout unknown, HTTP 5xx write, truncated provider response, post-write lookup | `backend/modules/scheduling_manleai_calendar/executor.go`, `backend/modules/scheduling_manleai_calendar/execution_repository.go`, `backend/modules/scheduling_manleai_calendar/execution_repository_aggregate.go`, `backend/modules/scheduling_manleai_calendar/execution_lifecycle.go`, `backend/modules/booking/service.go` | V49 execution ledger, V50 exact create graph/history guards, V51 lifecycle/event/release guards, internal repository integration tests, frontend `hasDurableAggregateConfirmation`/lifecycle result guards/`schedulingOperationForPayload`/`shouldRetainSchedulingReplayProof`, conversation create/lifecycle flows, external lease recovery/reconciliation and Square adapter tests |
| availability, availability quote, aggregate quote, target-aware quote, target appointment version, pooled capacity, capacity override, resource conflict, concurrent claim, sorted pool lock, quote expired, quote cleanup, quote retention, unbounded quote growth, slot fingerprint, local day, timezone, DST, ambiguous wall time, nonexistent wall time, strictLocalInstant, strictLocalMinute, open slots, offered slots, stale segment, conservative external conflict, incomplete staff evidence, no common time, split, staggered, deterministic staff assignment, same-guest sequential, cross-guest overlap | `backend/modules/scheduling_manleai_calendar/execution_availability.go`, `backend/modules/scheduling_manleai_calendar/execution_repository_aggregate.go`, `backend/modules/scheduling_manleai_calendar/execution_lifecycle.go`, `backend/modules/booking`, `backend/modules/conversation/service_availability.go` | aggregate and target-aware planner/repository tests, `backend/migrations/v50_safety_test.go`, `backend/migrations/v51_safety_test.go`, quote cleanup processor, V39/V49/V50/V51 quote and lifecycle guards, provider adapter, `service_matching_parsing.go`, `internal-appointment-create.tsx`, `internal-appointment-lifecycle.tsx`, POS Calendar, conversation tests |
| Square OAuth, token expired, refresh token, location, sync, catalog import, `available_for_booking`, team member booking profile, non-bookable staff, calendar sync, booking webhook, signature key, notification URL, event dedupe, claim token, calendar repair, stale booking version, webhook operations, webhook backlog, webhook dead letter, `square-webhook-events`, `can_requeue`, exact webhook requeue, `X-Idempotent-Replay`, safe webhook diagnostic, V60 | `pos-adapter-slice`, `backend/modules/pos_square` | `backend/modules/pos`, `integration_config`, V41 webhook/repair tables, V60 action/requeue/redaction guards, worker, Integrations `square-webhook-operations.tsx`, `frontend/lib/api/square-webhook-events.ts`, POS calendar UI |
| POS mapping, provider link, active provider, AI bookable, AI-bookable toggle, local only, sync failed, current authority eligibility, booking-calendar-reconciliation lock | `backend/modules/pos`, `docs/canonical-pos-ownership-checklist.md` | `Repository.UpdateServiceAIBookable`, `Repository.UpdateStaffAIBookable`, `salon_settings.scheduling_authority`, `salons.active_pos_provider`, canonical service/staff rows, `pos_entity_links`, Services/Staff UI, repository integration and sync tests |
| service alias ownership, category alias ownership, structured alias, Services categories, category management missing, aliases missing, cross-table alias conflict, duplicate normalized alias, alias reactivation, alias deactivation transfer, V58 | `frontend/features/dashboard/services-dashboard.tsx`, `backend/migrations/V58__cross_table_service_alias_ownership.sql`, `backend/modules/training/repository.go`, `backend/modules/pos/repository.go` | aliases are structured service/category children managed in the reused Services dashboard, never free-text knowledge; salon/normalized-alias transaction advisory lock, symmetric database triggers, config-transfer alias upserts, taxonomy materialization, V58 static safety test and real PostgreSQL parallel invariant test |
| semantic turn, semantic provider error, invalid schema, invalid_json_schema, schema fingerprint, semantic check, circuit open, guidance_action, recognizable_guidance_actions, available_guidance_actions, service_guidance_capability, catalog_only, consultation_disabled, catalog_unavailable, awaiting_action_choice, service guidance recovery, caller goal recovery, guidance_provider_unavailable, separate provider failure counter, no-progress handoff, multi-intent, service alias, category alias, service understanding, fuzzy service, fuzzy_service_confirmation, explicit catalog confirmation, candidate provenance, stale fuzzy candidate, wrong service, category narrowed to one service, bare service switch, stale date after summary, staff/date/customer correction, add/replace/remove/undo, question plus correction, non-native wording, ASR paraphrase | `backend/modules/conversation/conversation_act.go`, `service_fuzzy_confirmation.go`, `service_guidance_capability.go`, `service_guidance_recovery.go`, `turn_reducer.go`, `next_action_planner.go`, `backend/modules/voice_openai/adapter.go` | typed `dialog_state` pending fuzzy confirmation with catalog candidate/source/provenance, dialog-state normalization/repository, strict voice readiness fence, voice semantic interpreter/check route, structured service catalog answer routing, transcript metadata, `fuzzy_service_confirmation_test.go`, golden conversation and adapter contract tests |
| AI consultation, service recommendation, help me choose, current nail system, desired outcome, lower maintenance, consultation mutation, replace preference, clear preference, consultation question, pending consultation choice, category narrowed to one pending service, explicit booking after consultation, profile options, provider failure count, progress fingerprint, consultation profile, consultation_completed, awaiting_booking, profile revision, safety handoff, medical suitability | `backend/modules/conversation/service_consultation.go`, `backend/modules/conversation/service_understanding.go`, `backend/modules/conversation/turn_kernel.go`, `backend/modules/pos/types.go`, `backend/modules/pos/repository.go` | V38/V40 migrations and Ent schema, ready profile context, guarded question phrasing, global deterministic/structured safety gate, semantic consultation extraction, Services profile UI, Settings toggle/coverage, Calls typed audit state, consultation golden tests |
| service menu, how many services, how many I book, what do I have, current booking summary, service count, repeated clarification, informational service question | `backend/modules/conversation/conversation_act.go`, `backend/modules/conversation/service_prompts.go`, `backend/modules/conversation/answer_router.go` | `backend/modules/conversation/service.go`, dialog state, party flow, golden tests |
| final review, stale review, draft revision, reviewed revision, authorized revision, natural approval, repeated final review, review timeout, concise review retry, book it, just book this for me, correction during review, repeated same-category guest question, no progress loop | `backend/modules/conversation/conversation_act.go`, `backend/modules/conversation/draft_revision.go`, `next_action_planner.go`, `service.go` | repository, V36/V37 migrations, booking flow, conversation and phone tests |
| concurrent conversation turn, duplicate POS side effect, stale session snapshot, session state conflict, advisory lock, same-event replay | `backend/modules/conversation/service.go`, `backend/modules/conversation/service_voice_recovery.go` | `backend/modules/conversation/repository.go`, conversation concurrency tests, booking operation ledger/idempotency |
| AI training, Platform AI Training, owner correction, knowledge, FAQ answer, correction to service alias, stale policy, training evaluation, pos_confirmation_required, confirmation_requirement, confirmation_guardrail, universal Square training claim | `backend/modules/training`, `frontend/features/dashboard/training-dashboard.tsx`, `backend/modules/scheduling/service.go`, answer router/context | shared surface-aware Training UI; knowledge is advisory free text; correction-to-alias persists canonical structured `service_aliases`; Platform Admin direct access or current Admin-granted Ops authorization, with Calls PII for transcript-linked Ops work; authority guardrail tests |
| Platform Transfer, configuration transfer, tenant-to-tenant, JSON upload, schema v9, schema v8 compatibility, scoped v7 content pack, v7-to-v8 adaptation, included_sections, source fingerprint, target fences, stale preview, action replay, active_pos_provider report-only, local_business_hours, provider-managed hours, transfer run, V77 | `backend/modules/config_transfer/platform_*`, `backend/modules/config_transfer/types.go`, `backend/migrations/V77__platform_configuration_transfer.sql`, `backend/modules/scheduling/fence`, `frontend/features/platform/platform-configuration-transfer.tsx`, `frontend/lib/api/platform-configuration-transfer.ts`, `docs/lotus-investor-demo-consultation-pack-v7.json`, `docs/lotus-investor-demo-consultation-pack-v8.json` | Platform tenant Transfer tab; per-section source/target capability checks; v9 export/v8 upload plus guarded content-only v7 canonicalization; file-derived UI scope; atomic canonical writes and Business/Technical/transfer audit; exact source/target/authority fence; no authority/provider/secret/connection/operational transfer; legacy Tenant/onboarding routes unmounted |
| party booking, group booking, two people, multi-guest, multi-service, split booking, aggregate availability, all-or-none, partial child, party request, party service correction, guest_ref, guest_reference, quantity, sort_order, party_size, guest invariant, party_service_guest, party_service_operation, party_service_source, party request tenant mismatch, cross-salon session, V62 | `backend/modules/conversation/service_party.go`, `service_availability.go`, `service_booking_flow.go`, `repository.go`, `types.go`, `party_booking_request_fidelity_test.go`, `party_tenant_integrity_integration_test.go`, `manleai_calendar_party_scheduling_test.go` | structured `PartyPlan` guest references and ordered `guest_service_requests` JSONB, V62 composite salon/session FK and tenant-filtered hydration/redaction, `backend/modules/scheduling_manleai_calendar/execution_repository_aggregate.go`, V50 guards/tests, external booking service, Appointments internal create, Calls UI per-guest owner-review rows with legacy fallback, party golden tests |
| name captured wrong, background phrase captured as name, pending customer name, salon/service/staff name collision, arbitrary utterance replaced name, bare phone name, service instead of name, spelling, phone/email, another technician, staff alternative | `service_customer_name.go`, `turn_reducer.go`, `turn_kernel.go` | typed dialog pending state, conversation golden tests, transcript audit metadata |
| reschedule, cancel, move appointment, appointment target, target-aware availability, exact target version, replacement plan, cancel reason, lifecycle confirmation, lifecycle cutoff, cutoff equality, stale target, released_by_attempt_id, active_child_count, historical event replay, current active plan, operation/retry/target authority equality, current authority changed, cross-origin conflict, ordinal option, day view, week view, month view, agenda, Tomorrow button, appointment warning | `backend/modules/scheduling/service.go`, `backend/modules/scheduling_manleai_calendar/execution_lifecycle.go`, `backend/modules/conversation/service_internal_lifecycle.go`, `backend/modules/conversation/service_intent.go`, `backend/modules/booking/service.go` | V51 guards and lifecycle planner/repository tests, `manleai_calendar_lifecycle_conversation_test.go`, `internal-appointment-lifecycle.tsx`, `frontend/lib/api/scheduling-actions.ts`, POS Calendar UI, external booking tests |
| Twilio signature, webhook, TwiML, recording, media stream, stream fallback | `backend/modules/voice_twilio` | `backend/modules/voice`, phone demo memo |
| OpenAI STT, TTS, realtime, model, voice, guarded reply, background noise, background-noise handling, automatic, strong noise rejection, false transcript, transcript logprob, repeated progress reply, stale recovery reply, caller speaking over TTS, first-byte timeout, terminal reply repeated, spoken fact mismatch, clipped first syllable, stuttered TTS startup, startup audio buffer, realtime transport fallback | `backend/modules/voice_openai`, `backend/modules/voice`, `backend/modules/voice_twilio/handler.go` | integration config, conversation runtime, realtime event timeline, voice tests |
| slow AI response, backend latency, backend_turn_done, turn_router_ms, turn_route, turn_expected_input, turn_semantic_contract, turn_recognizable_guidance_actions, turn_available_guidance_actions, turn_guidance_action, service_guidance_capability, service_guidance_catalog_available, service_guidance_recommendation_ready, full_turn, guidance_turn, turn_interpreter_ms, turn_interpreter_schema_fingerprint, turn_interpreter_outcome, turn_interpreter_failure_stage, turn_interpreter_http_status_class, turn_interpreter_request_id, input_generation, reply_kind, reply_suppressed, availability_pos_ms, save_turn_ms, fast lane, semantic lane | `backend/modules/conversation/turn_kernel.go`, `backend/modules/conversation/conversation_act.go`, `backend/modules/conversation/service_guidance_capability.go`, `backend/modules/voice/backend_turn_diagnostics.go`, `backend/modules/conversation/turn_timing.go`, `backend/modules/voice_twilio/handler.go` | conversation router/service/interpreter, provider availability/POS calls, Calls realtime event timeline, voice and conversation tests |
| AI tone, speaking style, concise/warm/professional | `backend/modules/salon`, `conversation.RuntimeConfig`, `voice.ModelRequest` | Settings UI, config transfer |
| integration config, provider secrets, Platform Technical, active provider config, exact ErrNotFound, env fallback, legacy bootstrap, malformed settings, decrypt failure, stored secret empty, fail closed | `backend/modules/integration_config`, `/platform/tenants/:tenant_id/technical`, Platform technical integration/status APIs | runtime resolver and repository scanner first; stored row owns enabled/settings/secrets; Tenant UI has no provider configuration surface; only Square retains the exact-missing legacy bootstrap path, while Twilio/OpenAI runtime and reads are database-only |
| provider diagnostic redaction, raw provider error, Square response detail, Twilio StreamError, OpenAI Realtime error message, write-only integration response, V63 | `backend/modules/pos`, `backend/modules/pos_square`, `backend/modules/voice`, `backend/modules/voice_openai`, `backend/modules/voice_twilio`, `backend/modules/integration_config`, `backend/migrations/V63__provider_diagnostic_redaction.sql` | stable POS error-code messages, bounded voice diagnostics, allowlisted stream-status audit fields, whole-response secret serialization tests, release-gate security ownership, V63 historical cleanup |
| public catalog, published slug, public services, landing page, staff privacy, public readiness, stale published page, call to request, authority-aware catalog, publish fence, public base-table leak, V71 | `backend/modules/public_catalog`, `backend/modules/salon/repository.go`, `backend/migrations/V71__public_catalog_projection_boundary.sql`, `frontend/app/s/[slug]/page.tsx` | Business Settings public catalog card, scheduling authority/version, RLS integration test |
| legacy config transfer, legacy export, legacy import preview, request id, duplicate import | `backend/modules/config_transfer/service.go`, `backend/modules/config_transfer/repository.go`, `frontend/features/dashboard/settings-dashboard.tsx`, `frontend/features/onboarding/salon-profile-form.tsx` | Retained compatibility code only; not mounted/rendered after SaaS cutover |
| archive, call-session redaction, transcript PII, dialog_state PII, party_plan PII, booking_segments PII, reschedule_candidates PII, historical redaction repair, idempotent re-redaction, call audio cleanup, composite call tenant key | `backend/modules/conversation/repository.go`, `backend/modules/conversation/retention_processor.go`, `backend/migrations/V79__system_tenant_contract_preparation.sql` | owner/tenant-fenced lifecycle actions; V79 bounded expiry discovery plus item-bound redaction; complete state/transcript/handoff/party/webhook/audio clearing; composite salon/session ownership for transcript/handoff/webhook/audio children; `backend/modules/conversation/retention_integration_test.go`; Calls UI lifecycle filters and worker |
| scheduling PII retention, notification retention, V61, V79, retention_expires_at, redacted_at, redaction_version, 90-day boundary, terminal business state, terminal delivery state, SKIP LOCKED, redacted owner request, redacted destination, expired voice audio, retention allowlist, irreversible redaction | `backend/modules/scheduling_retention`, `backend/migrations/V61__scheduling_pii_retention.sql`, `backend/migrations/V79__system_tenant_contract_preparation.sql` | owner-manual/owner-notification/customer-notification retained DTOs, V61 database guards, V79 bounded worker-only discovery and exact item salon binding, worker and operations-health registration, owner-review/delivery UI markers, PostgreSQL integration and migration safety tests |
| SaaS tenant, multi-tenant, ActorContext, principal_scope, immutable principal realm, tenant membership, Owner revoke, Owner reactivate, tenant_owner, tenant_business_manager, Platform Admin, platform_admin, Platform Ops, platform_ops, create Platform user, edit Platform user, direct password, Platform roles page, tenant Access tab, temporary Ops authorization, support expiry, Platform Services, Platform Calls, Platform AI Training, Platform Transfer, salon assignment, delegated capability, PII grant, sensitive data access, customers scope, calls scope, appointments scope, notifications scope, access action, access audit, Technical tab, Transfer tab, Operations tab, runtime role, RLS, system_salon_id, provider locator, worker tenant binding, worker discovery, composite call tenant key, expand contract, contract preparation, strict RLS contract, policy catalog audit, V64, V74, V75, V76, V77, V78, V79, V80, last admin | `backend/modules/access`, `backend/modules/business`, Platform handlers, `backend/modules/config_transfer/platform_*`, `backend/modules/tenant_runtime`, `backend/internal/database`, `backend/internal/databasecontext`, `backend/internal/middleware/auth.go`, `backend/migrations/V64*` through `V80*`, `backend/cmd/platform-access/main.go`, `frontend/features/platform/access-console.tsx`, `frontend/features/platform/tenant-access-console.tsx`, `frontend/features/platform/platform-configuration-transfer.tsx`, `frontend/features/platform/access-ui.tsx`, `frontend/lib/api/access-contract.ts`, `frontend/lib/api/session-contract.ts` | immutable identity realms; current principal plus exact-route Tenant membership; Platform Admin direct control-plane access; exact-salon baseline and time-bounded Admin grants for Ops; direct Platform user lifecycle with password hashing/session revocation; Owner suspension without ownership transfer; per-section transfer authorization; actual-actor audit; version/idempotency; V78 system tenant context; V79 worker contract preparation and call-child tenant keys; V80 strict provider/worker base-policy matching; last-admin safety; RLS parity |
| sample data, sample_test, Lotus Nails Studio, fixture migration, sample_data_migrations, sample Platform Admin, sample Platform Ops, pre-live data, data profile, DEPLOY_DATA_PROFILE, SAMPLE_DATA_ENV_B64, SAMPLE_TEST_RESET_RELEASE_TAG, migration checksum mismatch, migration preflight, V73, V74, data classification, principal scope | `backend/sampledata`, `backend/cmd/sample-data/main.go`, `backend/migrations/V73__sample_data_classification.sql`, `backend/migrations/V74__principal_scope_isolation.sql`, `deploy/local-restart.sh`, `deploy/postgres-migration-preflight.sh`, `deploy/postgres-sample-target-preflight.sh`, `deploy/postgres-data-profile-guard.sh`, `.github/workflows/ci-cd.yml` | normal migrations stay fixture-free; sample Admin/Ops are Platform principals and the Lotus owner is a Tenant principal; local restart and protected pre-live deploy provision/replay the separate fixture; backup-first exact-tag reset only for incompatible `sample_test`; `live` refuses reset/sample residue and preserves bootstrap |
| auth, RequireAuth, current principal, stale JWT salon claim, stale JWT role claim, disabled signed token, deleted user token, refresh cookie, HttpOnly, SameSite, memory-only access token, localStorage migration, concurrent refresh replay, tenant leak, salon ownership, token exposure, cross salon | `backend/internal/middleware/auth.go`, `backend/modules/auth`, `frontend/lib/api/browser-session.ts`, `pos-calendar/lib/api/browser-session.ts`, `security_privacy_reviewer` | server-owned active-user/current-salon/current-role resolution; generic fail-closed authentication; deterministic one-successor refresh rotation with bounded exact replay; host-only `/api/auth` cookie transport; credentialed frontend/POS clients; handler, repository integration, middleware, and browser-session tests |
| Content Security Policy, CSP, nonce, unsafe-inline, unsafe-eval, wildcard source, inline style, scheduler placement | `frontend/middleware.ts`, `pos-calendar/middleware.ts`, `landing/proxy.ts`, each app's `lib/security/content-security-policy.ts` | dynamic root layouts, production CSP tests and builds, nonce-bearing Next scripts/styles, `pos-calendar/lib/security/scheduler-style.ts` for data-driven event placement without style attributes |
| Redis rate limit, distributed rate limit, token bucket, 429, 503, Retry-After, RateLimit-Limit, client IP, X-ManleAI-Client-IP, abuse protection | `backend/internal/ratelimit`, `backend/internal/middleware/rate_limit.go`, `backend/cmd/api/main.go` | HMAC-pseudonymous identities, atomic Redis script, route-class policies, fail-closed dependency response/health, Caddy header overwrite, Compose/config defaults, Redis integration and middleware tests |
| loading, empty, error, disabled, gated UI, copy, responsive | `salon-dashboard-ui`, `DESIGN.md` | page component and API helper |
| Owner-first release gate, release contract, migrate twice, fresh PostgreSQL CI, V46 through latest, tenant security contract, Redis security test, browser session test, CSP test, secret redaction, public PII absence, notification masking, customer notification, Square webhook operations, scheduling PII retention, callback signature, high-risk race suite, manifest test ownership, TypeScript authority evidence, test:evidence, run-ts-evidence-tests, code-ready not operational-ready | `deploy/owner-first-release-gate.sh`, `deploy/owner-first-release-gate.manifest`, `deploy/run-ts-evidence-tests.mjs`, `.github/workflows/ci-cd.yml`, all three web package scripts | `docs/operations/release-gate.md`, `docs/deployment.md`, `docs/production-readiness-checklist.md`, manifest-owned backend integration/security packages, live Redis 7 CI service, frontend/POS browser-session and CSP evidence, landing CSP evidence, POS scheduler-style evidence, and all three web typecheck/build jobs |
| CI/CD deploy, release tag, VPS deploy, GHCR image publish/sequential pull, SSH key, image extraction, healthcheck, release-scoped candidate env, atomic env promotion, previous env/Compose rollback, public domain smoke, migration checksum preflight, exit 42, pre-live reset, data profile guard, temporary sample credentials, systemd Caddy, project-edgectl, worker image | `.github/workflows/ci-cd.yml`, `deploy/production-domain-smoke.sh`, `deploy/postgres-migration-preflight.sh`, `deploy/postgres-sample-target-preflight.sh`, `deploy/postgres-data-profile-guard.sh` | `docs/deployment.md`, `docs/operations/migration-rollback.md`, `docker-compose.prod.yml`, `backend/Dockerfile`, deploy templates, GitHub Actions deploy log |

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
