# Architecture

## Current Milestone

This codebase implements Milestone 1, Milestone 2, Milestone 3 booking safety,
the Milestone 4 deterministic conversation simulator, the Milestone 5 live
telephony webhook foundation, the Milestone 6 external AI voice provider layer,
the Milestone 7A-7C salon knowledge/training slices, canonical POS
ownership/provider-switch gates, configuration transfer, public catalog
publishing, call lifecycle retention, the V46 additive scheduling-authority
data foundation, the Phase 1 authority-neutral scheduling dispatch boundary,
the Phase 2 owner-manual request workflow, the Phase 3 ManleAI Calendar
configuration/readiness aggregate, Phase 4A staff-only execution, Phase 4B
structured multi-guest/multi-service pooled all-or-none availability/create,
and Phase 4C whole-root internal reschedule/cancel for
the AI Receptionist system. Square-backed `external_provider` and aggregate
`manleai_calendar` are confirming executors with authority-native evidence;
`owner_manual` persists non-confirming pending work. V52-V55 implement explicit
owner-reviewed authority preview/commit with authority-version and readiness
fences, immutable audit history, and an explicit inverse-run reference. The
Settings UI exposes the review, blocked, conflict, commit, and recovery states;
provider connect/sync/import/webhook actions never switch authority implicitly.
V61 adds policy-versioned, irreversible PII redaction for terminal scheduling
and notification records while retaining authority, provider, status, version,
timestamp, tenant, and audit evidence. V62 fail-closes any historical
party-request/session tenant mismatch and replaces the legacy session-only
foreign key with `(salon_id, call_session_id)` ownership.

The backend is organized as:

```txt
cmd/api              Fiber HTTP server
cmd/worker           Independently scheduled POS sync, booking lease, quote cleanup, Square webhook/repair, notification delivery, call retention, and scheduling-PII retention worker entrypoint
cmd/scheduling-load-harness Bounded isolated scheduling replay/CAS/atomicity verification; never a production runtime
internal/config      environment config
internal/database    PostgreSQL connection bootstrap and startup migrations
internal/schedulingload Synthetic Owner-first concurrency workloads, target guards, invariant gate, and schema-versioned report
internal/encryption  AES-GCM token encryption
internal/middleware  current-principal JWT auth plus distributed route-class rate-limit enforcement
internal/ratelimit   atomic Redis token bucket and typed allow/deny/dependency decisions
modules/auth         login, HttpOnly refresh-cookie rotation, roles
modules/salon        salon profile, settings, synced business hour periods
modules/pos          provider-neutral POS contracts and persistence
modules/pos_square   Square adapter and Square integration routes
modules/public_catalog public-safe salon catalog read API
modules/scheduling   owner-scoped authority resolution and authority-neutral scheduling dispatch
modules/scheduling_external_provider external-provider executor delegating to the existing booking service
modules/scheduling_owner_manual request-only executor and pending owner-review aggregate
modules/scheduling_manleai_calendar owner-scoped configuration/readiness plus Phase 4B aggregate create and Phase 4C lifecycle executor
modules/scheduling_authority_switch owner-reviewed readiness preview, explicit authority-version-fenced commit, and audited reverse-switch reference
modules/scheduling_retention bounded terminal-record PII redaction with policy/version fences and no provider calls
modules/booking      booking attempts, appointments, and fallback pending safety
modules/customer     canonical customer CRUD, activity read model, and POS lookup facade
modules/notification_delivery provider-neutral owner-message delivery claims, attempts, events, replay, and dead-letter policy
modules/notification_twilio Twilio Messaging REST dispatch plus signed status and inbound callbacks
modules/integration_config encrypted salon-scoped provider app credentials and runtime settings
modules/config_transfer safe salon configuration export/import previews and applies
modules/conversation state-driven simulator/phone sessions, semantic turn reduction, service consultation, transcripts, summaries, and handoffs
modules/training     salon-authored knowledge base, owner corrections, and service aliases
modules/voice        provider-neutral live voice runtime, authority-aware three-dimension readiness/status, semantic-contract verification, routing, and webhook event audit
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

The dashboard and POS browser session contract stores access tokens only in
memory and keeps the rotating refresh token in a host-only HttpOnly,
SameSite-Strict cookie scoped to `/api/auth`. Exact concurrent refresh replay
returns one deterministic successor during a short grace period; it never
creates two live successors. All three Next.js applications render dynamically
behind request-specific nonce CSP middleware/proxy. Production CSP excludes
wildcards, `unsafe-inline`, and `unsafe-eval`; POS scheduler placement uses
nonce-authorized generated style rules rather than inline style attributes.

Production always applies an HMAC-pseudonymous
global client bucket and a route-class bucket through one atomic Redis script.
The Caddy project route overwrites the trusted client-IP header. Redis failure
is a request-protection dependency failure, not a reason to bypass limiting,
and `/healthz` reports the dependency unavailable.

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

## Scheduling Authority Contract

`docs/scheduling-authority.md` is the normative boundary for availability,
booking execution, confirmation evidence, party atomicity, and authority
switching. The exact protocol tokens are `owner_manual`,
`manleai_calendar`, and `external_provider`.

Implemented Phase 1-4C dependency direction:

```txt
conversation / owner UI
        |
authority-neutral scheduling service
        |
        +-- owner_manual -> scheduling_owner_manual -> pending request aggregate
        +-- manleai_calendar -> scheduling_manleai_calendar -> V49 ledger + V50 aggregate guards + V51 lifecycle guards
        +-- external_provider -> modules/pos.POSProvider -> concrete adapter

owner configuration UI/API
        -> scheduling_manleai_calendar -> V48 configuration/readiness aggregate
owner appointment UI / conversation
        -> neutral availability/action routes -> aggregate quote + atomic root/child create/lifecycle
```

`backend/modules/scheduling.Repository` resolves authority from owner-scoped
persisted evidence. Its `scheduling.Service` implements
`booking.HandlerService` for legacy compatibility and also exposes neutral
`CheckAvailability`/`ExecuteAction`. `LegacyExecutor` preserves the four
external-provider methods; `NeutralExecutor` returns `verified_slots` or
`request_only` availability and `confirmed_appointment`,
`pending_owner_review`, or `external_fallback_pending` actions for
book/reschedule/cancel.
Availability without a target and a genuinely new create use
`salon_settings.scheduling_authority`. Existing operation keys and retry
attempts use persisted origin; operation-key resolution queries both
`booking_attempts` and `scheduling_requests`, while retry-attempt resolution is
booking-attempt-specific. Target-aware reschedule
availability uses the target appointment origin. Reschedule/cancel require the
operation, retry-attempt, and target-appointment origins that are present to
agree, otherwise they fail with an operation conflict before dispatch.

Reschedule-candidate lookup plus create/cancel response-loss replay are
provider-free history delegation: they do not select an executor or call a
provider. Authority-sensitive booking HTTP endpoints, conversation scheduling
actions, and Square test create/cancel writes enter through the scheduling
facade for actual writes.

`scheduling.Service.CurrentSchedulingAuthority` provides the owner-scoped
known-token read used by Square readiness. That response exposes
`scheduling_authority`; new test creation and AI-booking enablement are allowed
only for current `external_provider`, while exact persisted replay and
external-target cleanup cancellation remain available after a later switch.
The public read-only
`scheduling.Service.ResolveCreateSchedulingAuthority` resolves an existing
operation/retry lineage before falling back to the current mode, validates the
resolved token, and never selects an executor or calls a provider. Square test
create uses it after replay so a valid persisted external safe retry can
continue after a current-mode switch; only origin-free test creation is gated
by the current mode.

`external_provider` remains the only provider-backed confirming executor. The
`backend/modules/scheduling_external_provider.Adapter` delegates those methods
to the established booking service and therefore preserves the existing
Square-backed `POSProvider` behavior exactly. Phase 2 registers
`backend/modules/scheduling_owner_manual.Executor` as a neutral executor:
availability is request-only and actions persist pending owner review without
calling POS or creating appointments. Phase 4B extends the registered
`backend/modules/scheduling_manleai_calendar.Executor` to verified aggregate
availability and atomic `book` execution for structured guests, ordered service
units, staff-only/pooled policies, concrete staff assignments, and resource
allocations. Phase 4C adds exact target-versioned whole-root `reschedule` and
`cancel` execution. Authority selection and switching are explicit through the
owner-reviewed V52-V55 switch workflow; connection state and
`active_pos_provider` cannot select an authority.

Voice readiness composes scheduling evidence without reimplementing authority
rules. `scheduling_owner_manual`, `scheduling_manleai_calendar`, and
`pos_square` each expose provider-neutral target readiness with an authority
version plus separate availability/request-capture and execution capabilities.
`voice.Service.Status` combines only those typed results with telephony
readiness, salon AI enablement, and `booking_mode`. It fails closed on an
unknown authority or mode, missing target readiness, and an authority/version
fence mismatch. `request_capture_ready` can therefore be true for
`owner_manual` while `automated_booking_ready` remains false; Square evidence
is consulted only for selected `external_provider`. Legacy `ready` mirrors
`phone_answering_ready`, and legacy `phone_booking_ready` mirrors
`automated_booking_ready`.

The neutral handlers expose unavailable capabilities as generic sanitized
`409 SCHEDULING_AUTHORITY_NOT_READY`, changed operation proof as
`409 SCHEDULING_OPERATION_CONFLICT`, and drift/conflict evidence as
`409 AVAILABILITY_QUOTE_STALE`. Square test gate errors also use bounded public
messages and never return wrapped internal diagnostic text.

External booking lease recovery and provider-calendar persistence, matching,
and reconciliation explicitly fence records to `external_provider`. Square
webhook target and scheduled-repair selection remains Square
provider/connection-scoped to preserve historical external mirrors after a
later authority switch; downstream calendar mutation cannot overwrite an
internal-origin row. When exact external calendar truth terminalizes a create,
mirror canonicalization preserves existing confirmation provenance, fills a
missing `confirmed_at` at that transaction and a missing
`confirmation_source` with `external_provider`, and does not manufacture a
`confirmed_by_user_id`. Repeated recovery leaves the provenance timestamps
unchanged.

Implemented V46 Phase 0 persistence adds:

- `salon_settings.scheduling_authority`, defaulting existing/current runtime
  state to `external_provider`;
- authority snapshots on `booking_attempts`, `booking_attempt_segments`,
  `appointments`, `appointment_services`, and `availability_quotes`;
- authority-native provider, appointment, service, staff, idempotency, location,
  and snapshot fields where the corresponding record needs them; and
- nullable appointment confirmation provenance (`confirmed_at`,
  `confirmed_by_user_id`, `confirmation_source`).

V46 backfills authority snapshots from each record's existing persisted POS
evidence. It deliberately leaves historical confirmation provenance null
because no reliable confirmation time or actor can be reconstructed. Active
mirror canonicalization is separate from that historical backfill: it
may fill only missing confirmation time/source when exact authoritative
external evidence terminalizes an operation. Current
repository routing, operation fingerprints, retry policy, reconciliation,
provider dispatch, and confirmation predicates remain driven by the legacy POS
fields for exact Square parity. The existing Settings read response exposes the
selected authority, but the Settings update contract does not accept or switch
it in Phase 0.

Implemented V47 Phase 2 persistence is a separate owner-manual aggregate:

- `scheduling_requests` owns immutable logical request data for book,
  reschedule, and cancel plus `pending`, `contacted`, `resolved`, or `dismissed`
  lifecycle/version state;
- immutable `scheduling_request_segments` snapshot ordered canonical service,
  staff, guest, quantity, duration, and requested-time data;
- append-only `scheduling_request_events` own payload-bound creation and status-
  transition audit/idempotency;
- `owner_notifications.scheduling_request_id` links the deduplicated
  `owner_manual_request_pending` row created with `delivery_status=queued`; and
- `call_sessions.scheduling_request_id` reciprocally links at most one request
  to its tenant-owned call session.

`scheduling_owner_manual.Repository.CreateOrReplay` uses one database
transaction for the request, segments, initial event, call link, and queued
outbox row. The operation unique key plus row-locked fingerprint checks make an
exact replay return the existing aggregate while changed logical data
conflicts. Status transitions lock the request and use expected-version compare-
and-swap before updating lifecycle timestamps and appending an event in the same
transaction. The repository does not deliver notifications, write
appointments/booking attempts, or create POS errors/reconciliation tasks.

Implemented V48 Phase 3 persistence adds the configuration boundary:

- database-managed `salon_settings.scheduling_authority_version`, incremented
  only by a real authority-token change;
- `manleai_calendar_configs` as root policy, monotonic config fence, and latest
  version-fenced owner activation evidence, with no default/backfill row;
- `salon_business_hour_periods.source=local_override` as the only internal-
  calendar hours, separate from provider-imported and migrated periods;
- tenant-fenced service policies, service/staff joins, staff weekly periods,
  resource pools, service/resource requirements, and immutable/cancellable
  exceptions. Service/staff eligibility is config-owned independently of a
  service-policy row so Staff-first setup does not depend on Service-first
  ordering; and
- immutable config events with salon-unique action key/fingerprint,
  previous/result fences, owner actor, fixed event type, and bounded payload.

Database exclusion constraints reject overlapping local hours, one staff
member's weekly periods, and active same-scope exceptions. Root/child/local-
hour mutations plus salon timezone and canonical service/staff eligibility
changes invalidate the config fence. A bulk replacement may therefore advance
the monotonic version more than once; it is a concurrency fence, not an API
mutation counter.

Implemented V49 Phase 4A persistence adds the authority-native execution
foundation:

- authority/config/version-fenced availability quotes with normalized slot
  segments and resource evidence;
- nullable external-provider columns on shared booking/appointment ledgers,
  with shape checks that forbid POS/provider evidence on internal rows;
- immutable internal execution events whose committed create evidence must
  reference the exact appointment, attempt, quote, authority, config, and
  logical request fingerprint;
- internal appointment service/resource shapes and active staff occupied-range
  exclusion using half-open `[start,end)` semantics; and
- replay evidence that returns the same committed appointment/attempt IDs for
  the exact operation key and rejects changed logical input.

Implemented V50 Phase 4B builds database-enforced aggregate safety on that
ledger:

- aggregate quote resource integrity plus the exact guest-reference/
  `party_size` invariant;
- canonical sorted resource-pool locking and half-open base/override capacity
  checks that prevent concurrent over-capacity commits;
- exact bidirectional equality among the consumed quote, attempt segments,
  root appointment, child service/resource rows, and committed event; and
- immutability of consumed quote evidence and committed book history, without a
  second reservation ledger.

Implemented V51 Phase 4C adds database-enforced lifecycle safety to the same
ledger:

- `appointment_services.released_by_attempt_id` pairs every internal plan
  release with the exact lifecycle attempt and permits only one release-only
  transition; allocation release remains derived through its parent segment;
- `(salon_id, appointment_id, authority_appointment_version)` is unique on
  execution events, and deferred graph validation requires one contiguous
  event for every root version;
- internal root origin, customer, party, and confirmation provenance are
  immutable; each lifecycle mutation advances exactly one version, and a
  cancelled root is terminal;
- reschedule requires exact equality among the target-versioned consumed quote,
  lifecycle attempt snapshot, and newly active plan/resource graph while the
  exact previous plan is released by that attempt; and
- cancel requires an exact attempt snapshot of the previous plan/resources and
  leaves no active or same-version replacement plan.

The captured scheduling authority owns lifecycle evidence:

- `owner_manual` creates pending owner-review work and never confirms
  automatically.
- `manleai_calendar` confirms only after one conflict-safe atomic transaction
  returns a durable internal appointment ID.
- `external_provider` confirms only after provider success returns the required
  provider booking ID/metadata and local persistence succeeds.

Party operations are all-or-none. Every historical appointment action,
operation replay, retry, and reconciliation keeps its originating authority;
the current salon setting is used only when no persisted operation, retry, or
target origin exists.
Connecting or syncing Square never implicitly changes authority, and
`active_pos_provider` selects an adapter only inside `external_provider`.

## Core Boundary

ManleAI is the system of record for salon operational data: canonical services,
staff, customers, AI controls, scheduling-authority configuration, owner
workflow state, pending requests, logs, and training data. External providers
are projections and, under `external_provider`, booking execution layers. The
selected authority owns availability and execution; the active POS provider
owns real provider availability and provider booking execution only for the
external-provider mode.

Canonical identity does not imply that every imported field is writable from
ManleAI. Field-level operational authority is derived from the canonical
record, its provider identity/link, the active adapter, and declared provider
write capabilities:

- For a provider-backed service without service-upsert capability, name,
  standard description, duration, price, and active status are provider-managed
  and read-only in ManleAI.
- For provider-backed staff without staff-upsert capability, name, phone,
  email, and active status are provider-managed and read-only in ManleAI.
- Categories, aliases, AI consultation data, `ai_bookable`, and local archive
  state remain ManleAI-managed controls.
- Local-only records remain editable in ManleAI. In the current
  `external_provider` runtime they are not booking-ready until they have a
  valid link for the active provider; later internal authority modes must not
  invent provider links to make owner-managed records eligible.
- A failed, stale, or unmapped provider link does not transfer operational-field
  authority back to ManleAI. The dashboard must surface the sync blocker and
  keep provider-managed fields read-only.

Service and staff API responses expose this derived contract as
`field_authority`; clients must not reconstruct it from `source`,
`sync_status`, or provider-specific UI branches.

For the current Square Appointments production release, operating hours are owned in Square Appointments. ManleAI
imports the selected Square location's business hour periods into
`salon_business_hour_periods` and uses them as a local safety filter before
offering availability slots. The local dashboard displays those periods and can
trigger sync, but it does not edit the source hours.

Provider IDs are mappings, not primary product identity. Square Appointments is
the first real POS integration; future POS names are architecture targets until
implemented. `salons.active_pos_provider` records the provider that booking,
availability, service/staff management, and phone readiness currently evaluate
inside the Square-backed external-provider runtime. It is not the scheduling
authority selector.
Current `services` and `staff` rows still carry legacy provider fields such as
`pos_provider`, `pos_service_id`, and `pos_staff_id`. Provider links in
`pos_entity_links` are the durable mapping model; old provider links are kept
for history and compatibility while preserving the current external-provider
booking boundary.

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

Current authority-sensitive execution branches:

```txt
legacy booking HTTP / Square compatibility
        -> scheduling.Service -> scheduling_external_provider.Adapter
        -> booking.Service -> modules/pos.POSProvider -> concrete adapter

neutral scheduling HTTP / conversation
        -> scheduling.Service -> scheduling.NeutralExecutor
        +-- scheduling_owner_manual -> scheduling request aggregate
        +-- scheduling_manleai_calendar -> aggregate verified slots / V49 ledger + V50/V51 guards
        +-- external_provider legacy adapter -> booking/POS path

ManleAI Calendar configuration HTTP
        -> scheduling_manleai_calendar.Handler/Service/Repository
        -> config + local hours + child policies/resources/exceptions/events
        -> configuration readiness + readiness.capabilities
```

The authority-neutral service and conversation runtime do not import
`modules/pos_square`. Only the external-provider executor delegates to the
established booking service, which depends on `modules/pos.POSProvider` and
must not import `modules/pos_square`. `scheduling_owner_manual` depends on the
canonical salon/service/staff/request schema and never on POS. Internal
scheduling must not be represented by a fake POS adapter.

Historical appointment/calendar reads, reconciliation, provider sync, Square
webhook repair, and worker repair continue to use their established external
booking paths where they operate on existing provider-origin records. Phase 1
does not reinterpret historical origin or route provider maintenance through a
salon's current authority selection. The additive neutral scheduling routes do
not replace those compatibility/history routes.

Under `owner_manual`, conversation answer context marks active, non-archived,
AI-bookable canonical services with positive duration as requestable and uses
active canonical staff without requiring a provider snapshot/link. Availability
collects the caller's preferred time under `request_only` and creates no quote.
After unchanged-draft review, book/reschedule/cancel use `ExecuteAction`; a
committed request returns `pending_owner_review`, stores
`call_sessions.scheduling_request_id`, ends with explicit non-confirmation copy,
and never manufactures appointment/attempt/POS/reconciliation evidence. A
persistence or dependency failure hands off without claiming the request was
recorded or a notification was delivered. Existing provider-origin targets
continue through their originating external executor even when the salon's
current mode is owner manual.

`scheduling_manleai_calendar` reads and mutates one owner-scoped
aggregate. Every mutation carries a stable action key and expected config
version, executes under the repository's salon serialization/fence checks, and
returns the refreshed aggregate plus replay status. Exact replay does not apply
a second logical edit; mismatched action reuse and stale versions conflict.
Readiness separates configuration blockers from operation blockers. In Phase
4C, `staff_only_availability` and `staff_only_create` become true only when the
aggregate is configuration-ready, activation is current, the selected authority
is `manleai_calendar`, and a staff-only policy is configured.
`pooled_capacity` additionally requires an enabled pooled policy;
`party_create` requires an enabled service and configured maximum party size
above one. `reschedule` and `cancel` use the same selected-authority,
configuration, and current-activation engine fence plus at least one enabled
service. Aggregate `execution_ready` is true only when all six capabilities are
true. Consumers gate the exact operation, not the aggregate. Persisted target-
origin lifecycle dispatch remains available after a later current-authority
change and is not inferred from this new-work aggregate.

Phase 4B availability is calculated from salon timezone, canonical active
AI-bookable services/staff, V48 service policy/eligible staff/weekly periods,
`local_override` hours, service/salon buffers, exceptions, resource
requirements, pool capacity and overrides, minimum notice, horizon, and slot
step. Quantity expands into ordered quantity-one units; services for one guest
remain sequential and different guests overlap only when staff and resources
fit. `anyone` is assigned deterministically and every slot segment contains a
concrete staff assignment. Nonexistent or ambiguous local DST wall times are
rejected. Existing internal appointments block exact staff/resource ranges;
external-origin conflicts block mapped staff ranges, while incomplete external
staff/time evidence fails closed instead of offering a potentially unsafe slot.

Create acquires the shared salon booking/reconciliation lock, performs exact
committed replay before quote expiry checks, then revalidates authority version,
config/activation, the exact quote graph, canonical service/staff/resource
policy, full-day schedules, conflicts, and effective capacity in one
transaction. It atomically inserts the confirmed root attempt and appointment,
all attempt/appointment service segments, every resource allocation, quote
consumption, and the committed execution event. Confirmation is returned only
with non-empty durable root appointment/attempt IDs and complete exact child
evidence; it contains no provider/POS evidence.

Phase 4C target-aware availability enters through the neutral scheduling
service, which resolves the persisted appointment origin before executor
selection. The internal repository locks the exact active target version and
hydrates only `plan_version=authority_appointment_version` rows whose
`released_at` is null. A reschedule quote preserves the target's party size,
ordered service IDs, and guest references while allowing the planner to assign
a new concrete staff/time/resource graph. It carries the target appointment ID
and version, uses the target's originating scheduling-authority version, uses
the current internal config version, excludes the target's old plan from
conflict calculations, and still requires current config activation. The
selected salon authority may differ because the target origin authorizes this
historical work.

Reschedule performs replay before current-state validation, then requires the
exact target version, an open reschedule cutoff, current config/activation,
unconsumed target-versioned quote, canonical graph, conflict, and capacity
evidence under the shared salon lock. One transaction snapshots the new plan
onto the lifecycle attempt, releases the complete old plan and resources with
that attempt as owner, advances the same root by one version/status, inserts
the complete replacement plan, consumes the quote, and appends the matching
versioned event. Any partial release or graph mismatch rolls back everything.

Cancel also performs exact replay first and then requires the persisted
internal target, current target version, active status, and open cancellation
cutoff. It accepts no quote, replacement segments, requested range, or
timezone. It deliberately does not require the salon's current authority or a
current config activation: the target origin and exact old snapshot authorize
historical cancellation. The transaction snapshots that old plan/resources
on the attempt, releases all active children, advances the root by one version
to terminal `cancelled`, writes the lifecycle event, and leaves no active plan.
The backend treats a null cutoff as no cutoff; the dashboard fails closed when
the cutoff value is missing or invalid.

Lifecycle results return the same durable root ID, a non-empty internal attempt
ID, `target_authority_appointment_version`, the next
`authority_appointment_version`, authoritative `appointment_status`, and
`active_child_count`. Reschedule returns the exact new active children; cancel
returns the immutable released-plan attempt snapshot while reporting zero
active children. No result contains POS/provider evidence. Exact operation
replay loads the historical event/attempt snapshot, so a reschedule or cancel
response remains recoverable after a later mutation without being rewritten to
the root's newest plan.

Conversation answer context derives internal service/staff choices from the
canonical catalog and activated V48 policies and calls `EvaluateReadiness` for
the exact operation capability fence; it does not approximate readiness with
weaker SQL. A complete party plan becomes one aggregate availability request
and one reviewed quote, not independent child quotes. The booking action
preserves the ordered guest/service/staff/resource proof and rejects partial
confirmed children before confirmed wording. Quote-stale/resource conflict
evidence reopens the whole draft without partial confirmation; response-loss
replay keeps the one root operation identity. Internal lifecycle conversation
state requires explicit persisted confirmation of the reviewed whole-root
replacement, or a captured cancellation reason followed by explicit whole-root
confirmation. Stale target/version refreshes candidates and reoffers without
false success; cutoff rejection and provider-shaped/partial results never
produce rescheduled/cancelled wording. Existing external-provider party
behavior remains on its established per-child provider path.

`frontend/features/dashboard/owner-review-requests.tsx` is composed inside the
Appointments dashboard. It uses feature-local API helpers for paginated/status-
filtered list, wrapped detail, and optimistic transition calls. Desktop renders
a table; smaller viewports render cards. Loading, empty, list/detail error,
success, terminal, disabled/saving, pagination, and version-conflict reload
states are explicit. This UI updates request review state only and makes no
notification-delivery or appointment-confirmation claim.

Calendar configuration UI consumes `frontend/lib/api/internal-calendar.ts` and shared typed
contracts. Salon-wide policy, `local_override` hours, resource pools,
salon/resource exceptions, version-fenced activation audit, and full readiness live in
Settings. Staff weekly schedules/eligible services/staff exceptions live inside
the selected staff member's edit flow. Service enablement, capacity mode,
buffers, eligible staff, and resource requirements live inside the selected
service's edit flow. Appointments renders scheduling readiness, the Phase 4B
structured internal create flow in
`frontend/features/dashboard/internal-appointment-create.tsx`, and Phase 4C
whole-root lifecycle dialogs in
`frontend/features/dashboard/internal-appointment-lifecycle.tsx`. These flows use
neutral scheduling helpers in `frontend/lib/api/scheduling-actions.ts`, builds
structured guests and ordered service rows from capability-ready policies,
shows exact assigned staff/resource proof, retains the operation key/payload
after an unknown response for committed replay, clears stale proof on typed
quote/conflict responses, and shows confirmation only when durable root/child
evidence matches the quote. Lifecycle helpers build target-versioned
reschedule availability/action payloads and quote-free cancel payloads, reject
incomplete active-plan hydration, validate status/version/active-child evidence,
retain exact replay identity after an unknown response, and fail closed on
missing, invalid, closed, or equality-at-cutoff client evidence. The current
salon authority does not reroute an internal-origin row. All config surfaces
reload after version conflict.

Booking workflow state belongs to the backend. In the current
`external_provider` implementation, create-booking, reschedule, cancel, and
dashboard test-booking requests first create a `booking_attempts` row with
`pos_pending` and a backend-owned POS idempotency key. The POS adapter is then
called as an outbound writer. For customer identity, booking resolves or
creates the ManleAI canonical customer, reuses an active `pos_entity_links`
customer mapping when present, or asks the active `POSProvider` to
search/create a provider customer and then stores the mapping. If customer
lookup/linking or appointment creation fails, the same attempt is finalized as
`fallback_pending`, a POS error and owner notification are recorded, and no
confirmed appointment is created. If the provider returns a POS booking ID and
booking version, the same attempt is finalized as
confirmed/rescheduled/cancelled and the appointment state is written in the
backend database. Reschedule, cancel, and test-booking cleanup requests must
leave the internal appointment unchanged unless the provider succeeds.

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
Owner notifications are durable outbox/in-product records here. V56 adds a
provider-neutral delivery state machine, claim lease and dispatch marker,
immutable attempt/event/action ledgers, provider-event deduplication, and
salon-fenced owner operations. The worker claims bounded batches through
`modules/notification_delivery`; `modules/notification_twilio` is the isolated
Twilio Messaging adapter. A pre-dispatch failure may retry within the bounded
policy, while any ambiguous post-dispatch outcome is dead-lettered and never
automatically retried. Provider acceptance is not delivery proof: only a
signed monotonic delivery callback may advance a row to `delivered`.

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

Calendar range reads are local mixed-origin reads over authority-confirmed
appointments plus external `pos_pending` and fallback-pending booking attempts.
Calendar sync is an active-provider import path: the provider-neutral POS layer may
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
V60 adds a separate authenticated owner-operations read/action boundary over
that durable state: safe list/detail, queue and recent-success metrics,
calendar-repair health, and bounded action-key-idempotent requeue only when the
backend returns `can_requeue`. The connected Square card renders those safe
fields and exact replay evidence; raw payloads, provider identifiers,
signatures/tokens/claim state, customer data, provider responses, and raw
errors never cross that boundary. This operational view remains provider/
connection scoped and does not select scheduling authority.

The Milestone 4 conversation simulator and Milestone 5 live phone webhook path
call the Phase 1 scheduling facade through a provider-neutral booking tool.
They do not import Square packages, read POS tokens, build Square payloads, or
use Square location IDs directly. Under `external_provider`, the facade reaches
the unchanged booking service through its external executor; an internal token
that has no ready executor fails closed before provider dispatch. The runtime
owns booking slot state, including date-only requests before a time is known,
so model wording cannot erase already-collected service, date, time, customer,
or staff details. Deterministic date, time, staff, and customer evidence is
applied before accepting a model-only summary path, so a semantic summary
classification cannot discard a concrete correction such as a new requested
day. Mutable session booking segments, offered-slot segments, party plans, and
versioned `dialog_state` are deep-cloned per turn. `dialog_state` owns pending
typed clarifications, bounded mutation history, no-progress recovery,
`draft_revision`, `reviewed_revision`, and `authorized_revision` instead of
reconstructing active control state from transcript metadata.

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

The Phase 5H conversation boundary reads `booking_mode` separately from
scheduling authority. Final review stores both values; a later change to
either makes the review stale. `pending_approval` may verify availability
through the selected internal or external executor, but it persists the action
through `owner_manual` with the selected authority as non-reserving target
evidence and never invokes an internal commit or provider create. `disabled`
stops new conversation scheduling before missing-field collection or tool
dispatch. Exact persisted operation replay remains origin-bound inside the
scheduling boundary and therefore precedes current-policy gating.

The current external-provider runtime checks provider-neutral availability,
offers slots, and only calls booking creation after required fields are present
and `draft_revision == reviewed_revision == authorized_revision`. Any service,
staff, date/time, guest, party, or customer correction advances the draft
revision; dependency-bearing changes clear offered slots and all corrections
invalidate stale review authorization. Selected segments and
`staff_selection_mode=anyone` survive unrelated turns. Current booking
confirmations remain impossible unless the booking service returns a
POS-confirmed booking attempt, appointment, and provider booking ID. If AI
booking is disabled, semantic interpretation cannot proceed safely, a customer
requests a human, clarification cannot make progress, or POS cannot confirm,
the runtime creates a handoff or fallback pending flow and avoids confirmed
wording. Later authority modes must replace only the scheduling execution and
evidence gate, not these conversation-state and review safeguards.

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
salon. Owner-notification Twilio messaging is stricter: its runtime resolver
uses only the encrypted salon record and never falls back to environment
configuration. Explicit owner-SMS enablement, exact-destination consent,
Account SID/Auth Token, sender or Messaging Service, and public HTTPS callback
URLs form its configuration fence.

The Milestone 7 training layer stores owner-authored salon knowledge,
corrections, and service aliases as salon-scoped data. Conversation runtime may
read active knowledge as advisory context for FAQ and policy answers,
transcript-linked corrections can be reviewed into reusable knowledge or
structured service aliases, and training evaluation previews can test active
knowledge without creating call sessions or bookings. Knowledge never replaces
structured authority-owned answers, the scheduling/booking service, or
authority-specific confirmation checks. Current provider-owned structured
answers remain part of the Square external-provider runtime.

AI receptionist tone is salon-scoped runtime configuration on
`salon_settings`. Tone presets guide spoken reply style, but backend
conversation guardrails still own slot collection, handoff routing, and
authority-specific confirmation wording.

Service understanding is a backend domain layer, not prompt-only behavior. The conversation runtime interprets customer service utterances against the active salon service catalog, salon-scoped `service_aliases`, active `service_categories`, and active `service_category_aliases`. Exact catalog service names win over aliases; aliases keep their established direct catalog-selection behavior; category names and category aliases create catalog-backed clarification candidates. The implemented Phase 6 fuzzy-confirmation slice treats one fuzzy-selected catalog candidate as proposed identity only: it persists `dialog_state.pending.prompt_key=fuzzy_service_confirmation`, leaves the draft and availability proof unchanged, and asks the caller to confirm that exact current catalog service. Only a state-scoped affirmative or exact/alias catalog evidence resolves that pending identity; rejection, unclear input, wrong state, changed source selection, or a stale catalog candidate cannot book or guess a service. Its bounded affirmative grammar is active only for that persisted prompt and is not a general caller-intent classifier. Catalog ambiguity is authoritative over the structured semantic interpreter: a model-proposed add or replacement cannot narrow a category candidate set to one concrete service unless the caller supplied concrete service evidence. A bare concrete service utterance that would switch an in-progress draft uses the catalog-backed confirmation flow before mutation. Owner corrections can be applied into service aliases with a stable `(salon_id, normalized_alias)` key, while category aliases use their own stable `(salon_id, normalized_alias)` key and conflict with active service aliases so one phrase cannot mean both a single service and a category. Common US nail concepts are versioned in `service_taxonomy_releases` and its category/concept/alias child tables. Refresh materializes active taxonomy data into salon system categories and exact, unique-target service aliases; it never owns or creates local service/POS records and never overwrites owner/imported classifications. The forward-only V45 fill attaches taxonomy category aliases and exact-name suggestions to the active category record with the matching slug, including owner/imported category records, while preserving owner/import aliases and reviewed/manual assignments. Service-understanding decisions and fuzzy-confirmation provenance/results are written to transcript metadata so call reviews can explain why a service was selected or why the AI asked for confirmation.

The current external-provider conversation runtime uses an `AIAnswerContext`
loaded from the active provider scope and cached briefly per salon. The answer
router chooses structured sources before owner-authored knowledge:
active-provider POS-linked AI-bookable services for service/menu answers,
imported `salon_business_hour_periods` for open/close questions,
active-provider staff records for staff questions, booking availability prompts
for incomplete availability questions, and active knowledge only after those
structured sources do not answer the request. A business-hours question without
a requested day answers from the salon-local current day instead of reciting
the full weekly schedule; an explicit day remains authoritative. Transcript
metadata records `answer_source`, `answer_source_reason`,
`answer_source_confidence`, `router_intent`, source record IDs when available,
and whether the context came from cache. This keeps current answers explainable
while preventing stale knowledge from overriding synced services, staff,
business hours, or external-provider booking rules. Later internal modes must
replace the provider-scoped eligibility/context source with authority-owned
data rather than fake provider links.

Service consultation is part of the conversation runtime, not free-text
knowledge. The current external-provider path may compare only active-provider,
POS-linked, synced, AI-bookable services and may state only structured
name/category/duration/price plus the ready profile's owner-approved summary,
with `services.ai_description` and then `description` retained only as legacy
summary fallbacks. Candidate IDs come from validated compared-service mutations
rather than raw current-system wording; they and their profile revisions are
persisted in typed dialog state and audit metadata. A bare affirmative after
multiple options does not select a service; a catalog-selected reply while the
session is in consultation enters the existing booking/service-edit state
machine, which invalidates stale slots and rechecks availability. Cancel,
reschedule, human handoff, and active party-plan actions take precedence over
consultation. Pain, injury, infection, allergy, or other health-suitability
questions create an owner handoff without medical advice or booking
confirmation. Future internal modes derive consultation eligibility from their
authority-eligible canonical catalog.

Group and party operations use the same authority-specific confirmation boundary
as single-customer scheduling and are all-or-none. In the current
`external_provider` runtime, the conversation resolves party size and guest
service counts into catalog-backed segments, calls provider-neutral
availability, and invokes booking only after slot selection and required
customer details. It may confirm only after every required provider child
succeeds, or one provider result durably covers all segments. Partial or unknown
results require supported rollback/reconciliation and remain unconfirmed.
`owner_manual` always creates a structured pending owner-review request;
`manleai_calendar` will require one atomic all-child commit. Ambiguous service
families still require catalog-backed clarification before availability.

Call sessions are operational records, not an unbounded owner-facing inbox. The Calls dashboard defaults to active sessions and supports archived and redacted lifecycle filters. Active lifecycle sessions carry a 90-day retention timestamp; the worker redacts expired sessions by clearing customer PII, transcript bodies, handoff summaries, party request representative details, webhook payloads, and temporary voice audio while preserving booking, handoff, provider call, outcome, and timestamp audit links. Manual redaction is irreversible from the dashboard and is blocked while a session is active.

V61 extends the same redaction-not-deletion rule to terminal scheduling and
notification evidence. The `scheduling_pii_retention` worker uses a baseline
90-day policy, bounded `FOR UPDATE SKIP LOCKED` claims, and one-row
transactions. It redacts customer/request snapshots, party guest references,
non-allowlisted event payload fields, owner/customer notification message and
destination data, and expired voice audio bytes. It preserves originating
authority, operation/provider/audit IDs, versions, statuses, timestamps, tenant
ownership, delivery outcome, and an explicit policy version. Pending or
contacted owner work, live delivery leases, queued/retrying/unknown delivery,
open reconciliation work, and active consent/STOP routing keys are ineligible.
The worker never calls a provider, and repeated or concurrent runs are
idempotent.

V62 makes the party-request retention boundary database-enforceable. Every
`party_booking_requests` row must reference a `call_sessions` row through the
same salon ID; migration preflight aborts rather than repairing or reassigning
any mismatch. Runtime hydration also filters by both salon and session. This
ensures session redaction cannot silently miss a malformed cross-salon child.

Public salon catalog pages are owner-published by slug and revalidate the
persisted scheduling authority on every read. `owner_manual` publishes from
active canonical AI-bookable services without requiring staff or a POS link;
`manleai_calendar` publishes enabled canonical services and `local_override`
hours only while the current calendar activation is valid; and
`external_provider` publishes only current-connection, synced/linked catalog
projections. Settings publication shares the scheduling advisory fence and an
authority-version comparison with switching, so readiness cannot be validated
against one authority and written after another. A stale published page fails
closed. Public DTOs never expose staff contact details, provider/POS IDs,
provider tokens, sync errors, or owner identifiers, and every CTA remains a
call-to-request action rather than web confirmation.

Configuration transfer schema v8 exports sanitized portable setup intent only, including salon
profile, AI settings, public catalog settings, integration runtime settings,
service category taxonomy, service category aliases, service aliases that can
be matched to existing target services, portable service consultation profiles,
and owner-authored knowledge. Schema v8 retains the v7 `included_sections`
contract and accepts v7 bundles for backward compatibility, so
curated data packs can import taxonomy, aliases, and consultation profiles
without overwriting salon profile, provider configuration, or AI runtime
settings. Import
previews and applies use stable request IDs, skip secrets and operational
records, and must not change scheduling authority/version, scheduling switch
history, owner-review requests/outbox rows, internal-calendar configuration or
execution evidence, or recreate services, staff, customers, appointments, POS
tokens, call sessions, transcripts, provider connections, synced business hour
periods, provider switch runs, party booking requests, voice webhook events, or
provider-side state. Apply acquires the same salon advisory fence used by an
authority switch and rechecks the exact previewed authority/version inside the
transaction; a concurrent change fails reviewably before any import write.
Service category imports use stable slug and
normalized-alias keys and reject category aliases that conflict with active
service aliases. Service alias imports use stable normalized-alias keys and
skip aliases whose target service cannot be resolved on the target salon.
Consultation profile imports resolve an existing target service by normalized
name plus duration, block missing or ambiguous targets, and derive eligibility
from the destination authority: canonical active AI-bookable services are
eligible for `owner_manual` and `manleai_calendar`, while `external_provider`
also requires the active-provider link/sync/version evidence. Profiles
upsert by stable `(salon_id, service_id)` without changing revisions for
identical data. Services and provider mappings are never created by transfer.
The exported `active_pos_provider` remains portable external-adapter intent and
is reported as source, destination, and result in preview. It never selects or
switches scheduling authority. If changing it while `external_provider` is the
destination authority would change the confirming executor, import blocks and
requires the explicit provider-switch workflow.
Booking mode is likewise reported as source, destination, and result. Incoming
`confirmed_booking` under `owner_manual` is never applied silently: preview and
apply emit a structured warning and report `pending_approval` as the result;
exact replay preserves that decision and import-run identity.

## Operations Health And Worker Run Fencing

V57 adds provider-neutral technical evidence for recurring work. The generic
worker scheduler records a claimed run, refreshes its lease while it executes,
and records a bounded success, failure, cancellation, or panic result. The
current row and immutable terminal run history contain only stable job names,
timestamps, duration, bounded processed count, and safe error class/code. They
contain no raw errors, payloads, secrets, provider entity IDs, or PII. Exact
job/run/worker-instance matching fences late heartbeats and finishes, while an
expired run is closed as abandoned before a replacement claim.

`backend/modules/operations_health` combines that global worker evidence with
owner-checked, salon-filtered queue aggregates. Square-only rows appear only
for salons with relevant Square connection history. Internal scheduling health
does not become a POS error, and the API never exposes cross-salon totals or run
identities. The Settings surface is read-only except for refresh and safe links
to the owning workflow. See `docs/operations/operations-health.md` for the
status policy and the difference between code-ready instrumentation and
configured monitoring.

The stable `notification_delivery` worker job runs the V56 owner-notification
consumer. Its provider-neutral processor persists `dispatch_started_at` before
the Twilio HTTP call, treats a pre-dispatch lease expiry as safely recoverable,
and treats a post-dispatch expiry or ambiguous response as an unknown-outcome
dead letter. The Appointments surface exposes only masked/safe delivery
evidence and an action-key-idempotent requeue for definitive safe failures;
unknown outcomes cannot be requeued. See
`docs/operations/owner-notification-delivery.md` for configuration, callback,
monitoring, and incident policy.

The stable `scheduling_pii_retention` job runs the V61 redaction processor.
Operations health exposes only salon-scoped due counts and safe job evidence;
it never exposes content, destinations, raw errors, or cross-salon totals.
Technical availability of this job does not approve a legal retention period,
backup-deletion policy, litigation-hold process, or jurisdiction-specific
compliance rule.

## Customer SMS Consent And Delivery

V59 and `modules/customer_notification` own a ledger separate from V56 owner
notifications. `customer_sms_consents` is the current per-salon normalized
destination state; immutable consent events retain explicit conversation,
owner-attested, and signed Twilio Advanced Opt-Out evidence. Policy is
salon-scoped in `salon_settings`, disabled by default, version-fenced, and uses
the salon timezone plus configured quiet hours.

Scheduling transactions enqueue customer delivery rows through database
triggers only when current policy and consent are eligible. Request creation
and appointment create/reschedule/cancel therefore commit their outbox row or
roll back together. A delivery binds the exact consent version, policy version,
source object, source status, and source version. Readiness, the final
pre-dispatch marker, and owner requeue all revalidate those snapshots; a later
request transition or appointment lifecycle change suppresses stale copy.
External authority version zero is preserved as authoritative version zero.

The `customer_notification_delivery` worker owns bounded claims, quiet-hours
deferral, final consent/policy/source locking, dispatch-start evidence,
provider acceptance, monotonic callbacks, and terminal suppression/dead-letter
state. Ambiguous post-dispatch outcomes use the canonical
`DELIVERY_OUTCOME_UNKNOWN` evidence and are never requeueable. Owner requeue is
limited to one additional bounded attempt cycle and is action-key idempotent.
Operations health exposes only salon-filtered backlog/dead-letter counts and
the separate worker job; no destination, body, or provider ID enters health
DTOs.

`modules/notification_twilio` remains the transport boundary. It verifies the
exact callback URL and all form parameters, then binds the callback Account SID
and Messaging Service/sender routing identity to the stored salon transport.
Inbound consent consumes `OptOutType`, not message wording, and leaves Twilio's
Advanced Opt-Out reply provider-owned.

## Current Release Boundary

The Owner-first Phase 0-6 application scope described by this architecture is
code-complete in the repository. That scope includes the Phase 0/1 foundation,
Phase 2 owner-manual workflow, Phase 3 configuration/readiness, Phase 4A-4C
internal create and whole-root lifecycle execution, the V52-V55 owner-reviewed
authority-switch workflow, V56 owner-notification delivery, V57 operations
health, V58 cross-table alias ownership, V59 customer appointment SMS consent
and delivery, V60 Square webhook operations, V61 scheduling PII retention, and
V62 party-request tenant integrity. The next release work is operational
validation and external readiness, not an unimplemented application milestone
after Phase 6.

This is code-ready evidence only. Operational production readiness still
requires dashboard-managed live provider configuration and credentials, real
Square sandbox payload validation, witnessed phone/SMS/callback behavior,
configured monitoring and on-call response, approved production backup
storage/retention/capacity, witnessed load and restore drills, approved RPO/RTO
evidence, and the remaining security/compliance approvals tracked by the
production-readiness checklist. None of those external gates is implied by the
implemented code or a passing release gate.

## Data Ownership

SQL files under `backend/migrations` are the current database source of truth.
The API startup migrator applies them once and records checksums in
`app_schema_migrations`. Ent schema files mirror the table structure so
generated clients can be introduced without changing the domain boundaries.

Domain ownership is separate from scheduling execution. ManleAI owns canonical
salon records and owner workflow state. The captured scheduling authority owns
availability and lifecycle evidence. Under `external_provider`, the active POS
owns provider-side availability, booking IDs, booking versions, and the success
or failure of provider execution. Under `manleai_calendar`, a confirmed,
rescheduled, or cancelled result requires a successful atomic internal commit
with durable appointment evidence. `owner_manual` persists its own
request/segment/event aggregate, remains pending owner work, and never auto-
confirms. The implemented resolver chooses the authority-sensitive executor
from persisted operation/target origin or
`salon_settings.scheduling_authority`; the ready external executor still uses
the existing POS fields and Square path. Historical external reads and
maintenance preserve their originating provider behavior.

Generic response consumers use `scheduling_authority` and authority-native
evidence before any compatibility alias. The complete legacy POS/provider field
inventory and removal gates live in
`docs/operations/owner-first-compatibility.md`; internal rows never receive
fabricated provider evidence to satisfy an external response shape.

Internal-calendar configuration remains distinct from scheduling execution:
V48 policy, local hours, staff schedules, service capacity policy, resources,
exceptions, immutable config events, and readiness are ManleAI-owned
configuration sources. Phase 4B derives aggregate verified quotes from those
sources and commits one root plus exact child service/resource evidence into
the V49 authority-native ledger under V50 capacity/concurrency/graph guards;
Phase 4C advances the same root and ledger under V51 lifecycle guards.
`configuration_ready` may pass while every execution capability is false, and
aggregate `execution_ready` is true only when all six declared capabilities
are true. Activation records
owner audit for one exact config version, becomes stale after relevant changes,
and does not change authority.

Tenant isolation is enforced by owner-scoped queries in repositories and by
salon ownership checks before POS actions.
