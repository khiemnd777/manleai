# Scheduling Authority

## Purpose

This document is the normative product and architecture contract for deciding
who owns availability, booking execution, and lifecycle confirmation for a
salon. Read it before the POS adapter and Square integration documents.

ManleAI is Owner-first: a salon must be able to operate without treating an
external POS connection as the product's universal scheduling prerequisite.
External providers remain supported scheduling authorities through the adapter
boundary, beginning with Square Appointments.

Phase 1 implements the authority-neutral resolver and dispatch boundary. Phase
2 adds the ready `owner_manual` request-only/pending-review executor, additive
neutral API, conversation path, and Appointments review queue. Phase 3 adds the
V48 owner-managed `manleai_calendar` configuration/readiness aggregate, API,
and dashboard workflow. Phase 4A registers staff-only availability and atomic
single-guest create. Phase 4B extends `manleai_calendar` to structured
multi-guest, multi-service staff-only and pooled availability/create with one
all-or-none internal commit; `external_provider` remains the provider-backed
executor boundary. V86 adds Atomic Slot Commit as a mandatory outbound
concurrency fence for external create/reschedule. V87 adds exact Square
connection/config/location/API/scope evidence and enables buyer-write single
create only. Seller-write, reschedule, party, and resource-capacity automation
remain fail-closed. Phase 4C adds target-origin, whole-root
internal reschedule/cancel and V51 lifecycle guards. Aggregate
`execution_ready` is true only when all six declared operation capabilities are
ready, so consumers must still use the exact operation capability.

Phase 5H makes `salon_settings.booking_mode` the AI receptionist's conversation
policy without changing the generic administrator scheduling API. The
conversation-specific scheduling entrypoints apply this exact matrix:

| Selected authority | `pending_approval` | `confirmed_booking` | `disabled` |
| --- | --- | --- | --- |
| `owner_manual` | request-only availability and one pending owner-review request | invalid configuration | no availability, request, or executor action |
| `manleai_calendar` | verify internal availability, then persist one pending owner-review request without an internal commit | existing atomic internal confirmation path | no availability, request, or executor action |
| `external_provider` | verify provider availability, then persist one pending owner-review request without a provider create | provider confirmation only after operation-specific capability, Atomic Slot Commit, and provider success; Square supports buyer-write single create only | no availability, request, or executor action |

Pending approval preserves ordered guest/service/staff/requested-time detail,
but strips quote, slot-fingerprint, retry, and target-version execution proof
before dispatching to `owner_manual`. The request root authority remains
`owner_manual`; `target_scheduling_authority` records the selected or persisted
target authority. It is selection/audit evidence, not a reservation or
confirmation. V55 permits that optional target on new book requests without
backfilling legacy NULL targets.

Conversation action dispatch remains replay-first. An exact persisted booking
attempt or scheduling request replays through its original authority even if
the current booking mode or selected authority later changes. Origin-free work
must match the current `(booking_mode, scheduling_authority)` review fence;
`dialog_state.reviewed_booking_mode` and
`dialog_state.selected_scheduling_authority` make a policy change invalidate
the prior final review. Disabled origin-free work stops before missing-field,
availability, request, executor, or provider actions.

## Protocol Tokens

The only scheduling-authority protocol values are:

- `owner_manual`
- `manleai_calendar`
- `external_provider`

These values are stable schema and API tokens. Product copy may use friendly
labels, but runtime behavior, persisted records, and tests must use the exact
tokens. Do not infer an authority from an integration connection, provider
name, booking mode, UI page, or the presence of provider identifiers.

## Terms

**Scheduling Authority**
The salon-scoped selection that owns availability, booking execution, and the
durable evidence required for confirmed, rescheduled, or cancelled wording.

**Authority Readiness**
The authority-specific prerequisites that must pass before a scheduling write.
Readiness is not confirmation evidence.

**Authority Evidence**
The durable result that proves the selected authority completed the requested
operation. The required evidence differs by authority.

**Originating Authority**
The authority captured when an appointment or pending request is created.
Historical reschedule, cancellation, retry, reconciliation, and audit work uses
this immutable origin rather than silently following a later salon switch.

**Pending Owner-Review Request**
A durable, non-confirmed request for owner action. A request ID, owner
notification, review status, or caller approval is not appointment confirmation
evidence.

## Implemented Phase 1-4C Boundaries

`backend/modules/scheduling` owns authority resolution and the
authority-neutral scheduling interface. The resolver reads persisted authority
evidence through salon/owner-scoped queries. It reads
`salon_settings.scheduling_authority` only for genuinely new availability or
create work that has no persisted origin. Integration state, caller wording,
handler choice, and provider identifiers do not select the mode.

The concrete boundary is `Repository` plus `Service`. The service retains the
legacy `booking.HandlerService` compatibility surface and adds neutral
`CheckAvailability` and `ExecuteAction` operations. Executors implement either
the legacy external-provider contract or the neutral result contract:

- `AvailabilityResult.kind` is `verified_slots` with a nested legacy
  availability result, or `request_only` with no synthetic slots;
- `ActionResult.kind` is `confirmed_appointment`, `pending_owner_review`, or
  `external_fallback_pending`; and
- the operation token is `book`, `reschedule`, or `cancel`.

Resolution follows these rules:

- availability without `target_appointment_id` uses the current salon setting;
  target-aware reschedule availability uses the target appointment's persisted
  originating authority;
- create/action first resolves an existing `operation_key` across
  `booking_attempts` and `scheduling_requests`, then requires an optional
  `retry_of_attempt_id` to have the same persisted booking-attempt authority;
  only work with no persisted operation or retry origin uses the current salon
  setting; and
- reschedule and cancel require the persisted target-appointment authority to
  equal any authority found from `operation_key` and
  `retry_of_attempt_id`. A cross-source mismatch returns
  `booking.ErrOperationConflict` before executor or provider dispatch.

`RescheduleCandidates`, `ReplayCreate`, and `ReplayCancel` are deliberately
provider-free history delegation. They query established persisted history and
do not select an executor or call a provider. Appointment/calendar/history and
reconciliation reads likewise delegate unchanged, so a later salon setting
cannot reinterpret already persisted records.

`Service.CurrentSchedulingAuthority` is the owner-scoped, known-token read for
surfaces that must describe or gate genuinely new work. Square readiness uses
it to expose `scheduling_authority` and permits new Square test creation or AI
booking enablement only when the current authority is `external_provider`.
`Service.ResolveCreateSchedulingAuthority` is the public, read-only create-
lineage resolver. It resolves an existing operation key and optional retry
attempt with the same equality and tenant checks used by create dispatch; only
when neither origin exists does it fall back to
`CurrentSchedulingAuthority`. It validates the resolved protocol token but
does not select an executor or call a provider. Square test create uses this
method after provider-free replay, so a valid persisted external safe retry may
continue after the current salon mode changes while an origin-free new test
create remains blocked by an internal current mode. Cancellation dispatch
follows the external target origin, so cleanup cancellation also remains
available after a later salon switch.

Authority-sensitive booking API endpoints and conversation scheduling actions
enter through this boundary. The resolved behavior is:

- `external_provider` delegates through
  `backend/modules/scheduling_external_provider` to the exact existing booking
  service and POS path. V86 strengthens that path with the Atomic Slot Commit
  gate described below; provider confirmation, quote, fence, idempotency,
  retry, reconciliation, and error behavior otherwise remains authoritative.
- `owner_manual` delegates through
  `backend/modules/scheduling_owner_manual`. Availability is `request_only`;
  book, reschedule, and cancel return `pending_owner_review` only after the
  request aggregate commits. This executor never calls a provider or creates
  an appointment, booking attempt, POS error, or reconciliation task.
- `manleai_calendar` delegates through the registered
  `backend/modules/scheduling_manleai_calendar.Executor`. Phase 4C returns
  verified aggregate slots and supports atomic `book` for structured guests,
  ordered service units, staff-only or pooled policies, concrete staff
  assignments, and exact resource allocations, plus target-versioned
  whole-root `reschedule` and quote-free whole-root `cancel`. Configuration or
  activation alone does not enable a capability.

`scheduling.AuthorityNotReadyError` unwraps
`booking.ErrSchedulingAuthorityNotReady`. Authority-sensitive booking HTTP
handlers expose a generic, sanitized
`409 SCHEDULING_AUTHORITY_NOT_READY`; quote/conflict drift is
`409 AVAILABILITY_QUOTE_STALE`, and changed logical input under an existing
operation identity is `409 SCHEDULING_OPERATION_CONFLICT`. A concurrent
external slot loser is `409 SLOT_COMMIT_CONFLICT`; an exact operation still in
progress is `202 SLOT_CLAIM_IN_PROGRESS`; and a dispatched operation whose
provider result cannot be proved is `202 SLOT_OUTCOME_UNKNOWN`. These results are
not pending requests and never authorize confirmed wording. Square test
create/cancel writes also use
the scheduling facade, while their response-loss replay checks use the
provider-free history delegates. Square gate handlers map authority and other
gate failures to bounded public messages rather than returning internal error
text.

The additive scheduling handler exposes neutral result shapes and owns stable
scheduling-action/request error codes. Legacy availability/create/reschedule/
cancel handlers remain external-provider compatibility routes; under current
`owner_manual` callers use `scheduling-availability` and `scheduling-actions`
rather than expecting legacy booking-attempt or appointment response shapes.

External-provider lease recovery and provider-calendar persistence, matching,
and reconciliation explicitly filter `scheduling_authority=external_provider`
and cannot mutate internal-origin rows. Square webhook target and scheduled
repair selection intentionally remain Square provider/connection-scoped so
they can preserve historical external-provider mirrors after a later salon
switch; downstream booking-calendar writes remain fenced to external origins.
These maintenance paths do not use the current salon setting as operation
dispatch and do not reinterpret provider records as internal scheduling.
When exact external calendar evidence terminalizes a create during lease or
fallback/direct convergence, mirror canonicalization preserves any existing
`confirmed_at` and `confirmation_source`, fills only missing values with the
canonicalization time and `external_provider`, and leaves
`confirmed_by_user_id` unset unless an actual actor was already recorded.
Repeated recovery is idempotent and does not rewrite those timestamps.

## External Atomic Slot Commit

An external availability quote is evidence for what the provider returned; it
is not a reservation. Before a new external `book` or `reschedule` can cross
the provider boundary, the booking service requires both:

- adapter-declared `AtomicCreateNoOverlap` or
  `AtomicRescheduleNoOverlap` plus `ConcreteStaffAssignment`; and
- one current, unexpired
  `external_provider_scheduling_capability_evidence` row bound to the exact
  connection capability version, salon integration config/version, provider,
  location, Square API version, and normalized OAuth-scope fingerprint under
  verification contract `square-buyer-single-create-v1` for new Square work.

For Square, `APPOINTMENTS_WRITE` must be present and
`APPOINTMENTS_ALL_WRITE` absent. This makes `AtomicCreateNoOverlap` and
`ConcreteStaffAssignment` true only for single create. All automated
reschedule, party, and resource-capacity flags remain false. Existing V86
evidence can preserve an already persisted historical claim/replay path, but it
cannot authorize a new V87 Square claim.

The V86 `external_slot_claims` and `external_slot_claim_intervals` ledger is an
outbound ManleAI concurrency fence, not a second provider calendar. The
linearization point is the PostgreSQL transaction that consumes the quote,
creates the booking attempt, and installs all required half-open `[start,end)`
staff/resource intervals. Sorted transaction-scoped PostgreSQL advisory locks
stabilize multi-resource lock order, and the GiST exclusion constraint remains
the commit-time authority that makes overlapping claims mutually exclusive across API
replicas. If Call A commits that transaction first, Call B receives
`SLOT_COMMIT_CONFLICT` before any provider/customer write. The conversation
clears stale authorization and selected-slot proof, runs fresh authoritative
availability, truthfully says that nothing was booked for the losing request,
and offers only the refreshed alternatives.

Create confirmation keeps the claim active. The provider-neutral repository can
install a future capability-approved reschedule's new
interval fragments while the old claim remains active, then atomically releases
the exact old plan only after verified provider success; this prevents an
extension outside the old range from becoming an unprotected gap. Square V87
does not grant that reschedule capability. Cancellation
releases the active claim only after verified provider cancellation. Definite
pre-dispatch/provider rejection releases the new claim. Dispatch-started,
unknown, and reconciliation-required results retain their intervals and cannot
be blindly retried or manually released without authoritative non-creation
evidence. Availability subtracts these active local claims in addition to the
provider response.

V86 does not backfill historical attempts into claims and does not manufacture
external resource-capacity evidence. Current external party create is request-
only/fail-closed and performs zero sequential provider child writes. A future
whole-party provider contract needs separate approval and is not implied by
V86/V87. `manleai_calendar` continues to use its V49-V51 aggregate transaction
and resource-capacity guards independently.

Phase 2 adds the owner-review queue inside `/dashboard/appointments`. It does
not add authority selection, an authority-switch endpoint/UI, or internal
calendar management.

Phase 3 adds internal-calendar configuration management. Settings owns the
salon policy, `local_override` hours,
resources, salon/resource exceptions, and activation audit. Staff and Service
edit flows own their respective child schedule/policy data. Appointments shows
authority/readiness and Phase 4B structured multi-guest, multi-service create
for capability-ready staff-only and pooled policies plus Phase 4C whole-root
lifecycle actions for complete internal-origin appointment rows. None of these
surfaces switches authority.

## Authority Modes

### `owner_manual`

The implemented executor collects a complete, salon-scoped book, reschedule,
or cancellation request and creates one durable pending owner-review aggregate.
Availability returns `request_only`; it records a time preference without
claiming a verified opening. No availability quote or slot fingerprint is
accepted on this path.

The source of truth is separate from the external booking ledger:

- `scheduling_requests` owns authority, stable operation key/fingerprint,
  operation type, source, lifecycle/version, optional call-session/target,
  customer, timezone, party size, requested range, notes, and resolution state;
- immutable `scheduling_request_segments` snapshot canonical service name and
  duration, optional canonical staff name, guest reference, quantity, requested
  range, and order; and
- append-only `scheduling_request_events` records the initial creation and
  every status transition with a payload-bound action key, request version,
  trusted owner actor when applicable, and bounded payload.

Book has no target. Reschedule/cancel requires either a tenant-owned target
appointment with its resolved origin or a durable free-text target description.
The owner-manual executor validates active, non-archived, AI-bookable canonical
services with positive duration and active, non-archived, AI-bookable canonical
staff. It does not require a POS connection or provider entity link.

Creation runs in one database transaction: request, ordered segments,
`request_created` event, reciprocal call-session link when present, and one
deduplicated `owner_manual_request_pending` notification/outbox row with
`delivery_status=queued`. Operation uniqueness, row-locked replay checks, and
the stored fingerprint prevent duplicate or mismatched work under concurrent
calls. The scheduling aggregate repository does not deliver notifications;
V56's registered provider-neutral `notification_delivery` worker consumes
eligible rows through the salon-scoped `notification_twilio` transport.
`queued` alone is not sent, delivered, or notified evidence. Exact operation-
key/fingerprint replay returns the same request without duplicating any row;
changed logical data conflicts.

Review lifecycle is `pending -> contacted -> resolved|dismissed`, with direct
`pending -> resolved|dismissed` also allowed. `resolved` and `dismissed` are
terminal and require a resolution reason. Every transition requires
`expected_version`, increments the version, and appends one `status_changed`
event. Exact action-key/fingerprint replay is idempotent; a reused action key
with changed transition data conflicts.

The request ID and review status remain non-confirmation evidence. No review
transition confirms, reschedules, or cancels an appointment. No owner-manual
failure creates a provider/POS error or provider reconciliation record.

### `manleai_calendar`

V48 and `backend/modules/scheduling_manleai_calendar` now own the configuration
foundation: root policy and monotonic config fence; salon-local timezone;
`local_override` business hours; canonical service policies and eligible staff;
staff weekly schedules; optional resource pools/requirements; scoped temporal
exceptions; immutable configuration events; API constraints; and typed
configuration/execution blockers. Provider-imported or migrated business hours
do not satisfy this authority's readiness.

Every mutation is salon-scoped and requires a stable `action_key` plus
`expected_config_version`. Tenant routes require an active authorized Tenant
membership; Platform routes require the actor's salon-scoped
`technical.write` capability. V91 makes the repository, activation,
exception, and immutable configuration-event database guards use that same
predicate while preserving the actual actor ID instead of impersonating the
salon Owner. Exact key/fingerprint replay returns the current
aggregate without a second logical mutation; changed payload reuse conflicts,
and stale fences fail. The returned config version is monotonic and may advance
more than once when one logical replacement mutates multiple child rows; it is
not an edit count. Database exclusions prevent overlap among local salon hours,
one staff member's weekly periods, and active same-scope exceptions.

`configuration_ready` means the current aggregate has the required config,
local hours, eligible/configured/enabled service policies, eligible assigned
staff with weekly schedules, and valid resource relationships. The root stores
the latest owner activation for the exact `activated_version`, while immutable
config events retain each activation action. Any later scheduling-relevant
change advances the fence, makes the prior activation stale, and requires
re-activation after configuration blockers are resolved. It does not change
`salon_settings.scheduling_authority`.

Phase 4C readiness is operation-specific. The response exposes:

- `staff_only_availability` and `staff_only_create`, true only when
  configuration is ready, activation matches the current config, the selected
  authority is `manleai_calendar`, and at least one valid staff-only policy is
  configured;
- `pooled_capacity`, true when the same engine fence is ready and at least one
  enabled pooled policy exists;
- `party_create`, true when that fence is ready, at least one service is
  enabled, and `max_party_size` exceeds one; and
- `reschedule` and `cancel`, true when the same selected-authority,
  configuration, and current-activation engine fence is ready and at least one
  service is enabled.

Aggregate `execution_ready` is true only when all six capabilities are true.
Its false value must not disable an exact operation capability that is true,
and a true configuration/activation state must not be treated as execution
permission on its own. These flags describe current new-work readiness;
historical internal target dispatch follows persisted origin/version even
after a later salon authority change.

Phase 4B availability uses the salon timezone, canonical active AI-bookable
services/staff, enabled V48 `staff_only` and `pooled` policies, eligible staff,
weekly staff schedules, only `local_override` salon hours, buffers, exceptions,
resource requirements, pool capacities and capacity overrides, minimum notice,
horizon, and slot-step policy. It normalizes service quantities into ordered
quantity-one units. Services for one guest are sequential; different guests
may overlap only when staff and resources fit. `staff_selection_mode=anyone`
assigns eligible staff deterministically, but every returned segment identifies
one concrete staff member. Time ranges are half-open `[start,end)`; nonexistent
and ambiguous DST wall times are rejected instead of guessed.

Internal appointments and recognized external-origin appointments block staff
ranges. If external conflict evidence cannot identify a valid concrete
staff/time range, availability fails closed as stale/conflicting evidence
instead of offering a potentially unsafe slot.

The create transaction acquires the shared salon booking/reconciliation lock,
checks exact committed replay first, then locks and revalidates current
authority/config/activation, the exact ordered quote graph, canonical catalog
and policies, full-day schedules, staff conflicts, resource conflicts, and
effective pool capacity. It atomically persists one confirmed root booking
attempt and appointment, all attempt/appointment service segments, every
resource allocation, quote consumption, and one immutable committed execution
event. Any stale child or capacity failure rolls back the whole graph.
Confirmation requires non-empty internal root appointment and attempt IDs plus
complete child evidence and carries no provider/POS evidence.

An offered slot, caller approval, attempted write, timeout, or client-side
success state is not confirmation. A stale quote/conflict returns typed
non-confirmation and requires availability refresh. If the commit succeeded but
the response was lost, exact replay with the same operation key and logical
fingerprint returns the same durable IDs; changed input conflicts. Internal
failures never create provider/POS errors or reconciliation work.

Phase 4C lifecycle execution uses the same root and ledger. Target-aware
availability resolves the persisted internal target origin, locks its exact
`authority_appointment_version`, and reads only that version's unreleased plan.
It preserves party size, ordered service IDs, quantity-one units, and guest
references while returning a complete newly assigned staff/time/resource plan.
The quote carries target ID/version, originating authority version, and current
config version; it requires current activation and excludes the target's old
plan from conflict checks.

Reschedule checks exact replay first. A new mutation requires the exact active
target version, open reschedule cutoff, current config/activation, unconsumed
target-versioned quote, exact replacement shape, and revalidated catalog,
schedule, conflict, and capacity evidence. One transaction snapshots the new
plan on the lifecycle attempt, releases every old segment/resource through that
attempt, advances the same root by one version to `rescheduled`, inserts the
exact new plan, consumes the quote, and appends the matching version event.

Cancel also checks replay first, then requires the exact active internal target
version and open cancellation cutoff. It accepts no quote, slot fingerprint,
replacement segments, requested range, timezone, or party size. It does not
require the current salon authority or current config activation. One
transaction snapshots the exact old plan/resources on the cancellation attempt,
releases every active child, advances the root by one version to terminal
`cancelled`, and appends the matching event with no active replacement plan.
A null cutoff is no backend cutoff; the dashboard deliberately disables a new
action when its cutoff evidence is missing or invalid.

V51 owns lifecycle release and version integrity. Each released segment records
`released_by_attempt_id`; each root version has one unique, contiguous execution
event; tenant/origin/customer/party/confirmation provenance is immutable;
cancelled roots are terminal; reschedule quote/attempt/new-plan graphs must
match exactly; and cancellation attempt history must equal the exact released
old plan while the root has zero active children.

Lifecycle action results return the same durable root ID, a non-empty internal
attempt ID, target and result authority-appointment versions, authoritative
status, `active_child_count`, and the operation's exact child snapshot.
Reschedule returns the new active plan. Cancel reports zero active children but
returns the released-plan attempt snapshot for audit/replay. Exact replay loads
that historical event snapshot after later mutations; changed input conflicts.
No lifecycle row or response contains provider/POS evidence.

Phase 4C status: structured aggregate availability/create and whole-root
reschedule/cancel are implemented. Explicit authority switching is implemented
separately by `backend/modules/scheduling_authority_switch`; switching the
current setting never rewrites this module's persisted appointment origins.

### `external_provider`

The selected external provider owns real availability and provider-side
booking execution. ManleAI calls the provider only through the provider-neutral
adapter boundary. Confirmation requires a successful provider result with the
required booking metadata, including a non-empty provider booking ID, followed
by successful local persistence of the operation and appointment mirror.

Unknown, failed, disconnected, stale, permission-blocked, or incomplete
provider outcomes remain unconfirmed. They use the existing fallback,
notification, provider error, retry, and reconciliation rules in
`docs/pos-adapter-layer.md`. Square Appointments is currently the only
implemented production external-provider adapter.

Current status: ready through the authority-neutral boundary. Its executor
delegates to the existing provider booking service without changing behavior.

## Source-Of-Truth Matrix

| Decision | `owner_manual` | `manleai_calendar` | `external_provider` |
| --- | --- | --- | --- |
| Service/staff scheduling eligibility | Owner-managed canonical records and request policy | Canonical active, AI-bookable, non-archived records plus enabled service policy, assigned staff, weekly schedule, and positive service duration | Canonical records with valid selected-provider links and provider readiness |
| Business hours | Owner-managed request policy; no automatic slot promise | Only `salon_business_hour_periods.source=local_override`; salon timezone owns interpretation | Provider-authoritative/imported schedule required by the adapter contract |
| Availability | Not automatically promised | Verified aggregate slots with exact ordered guest/service/staff/resource evidence and authority/config-fenced quote; target-aware reschedule availability also carries the exact target/root version | Selected provider availability |
| Create evidence | Pending owner-review request only | One all-or-none internal commit plus durable root appointment/attempt IDs and complete child service/resource graph | Successful provider result plus provider booking ID and local persistence |
| Reschedule/cancel evidence | Pending owner review only | Same durable internal root advanced by exactly one version, one exact lifecycle attempt/event snapshot, released old children, a complete active replacement plan for reschedule or zero active children for cancel | Successful mutation of the originating provider booking with required version/evidence |
| Ambiguous outcome | Keep the request pending and dedupe owner work | Keep unconfirmed; exact committed replay returns the same IDs, while stale/conflicting evidence refreshes availability | Keep unconfirmed and apply provider reconciliation policy |
| Error owner | Scheduling/request audit | Internal scheduling audit/error | Provider/POS error and reconciliation |

Canonical salon, service, staff, customer, conversation, and training records
remain ManleAI-owned in every mode. Scheduling authority changes execution
ownership; it does not transfer tenant identity or expose provider IDs as
product identity.

## Readiness Contract

Every automated scheduling attempt must prove:

1. One scheduling authority is selected and its captured version/fence is
   current.
2. The salon, services, staff, customer, and conversation state belong to the
   same `salon_id`.
3. Required services and staff are active and eligible under that authority.
4. The operation has a stable operation key and logical fingerprint.
5. Final review authorization, when required, matches the unchanged draft.
6. The authority-specific availability and write gates pass immediately before
   dispatch or commit.

Additional gates by mode:

- `owner_manual`: request/event persistence and the deduplicated queued owner-
  notification outbox transaction must be available; automatic verified-slot
  and confirmed-appointment readiness is always false.
- `manleai_calendar`: gate new work with its exact capability. Availability,
  create, and reschedule additionally require the selected internal authority,
  current activation/config fences, canonical eligibility, strict-time
  aggregate planning, conflict-safe exact guest/service/staff/resource quote
  evidence, sorted pool locking and capacity validation when pooled,
  transaction isolation, and durable internal root/child/event persistence.
  Cancel is quote-free and does not require the current authority/config/
  activation fence, because target origin owns historical dispatch; it still
  requires the exact root/version, an open cutoff, an exact old-plan attempt
  snapshot, atomic release, and durable terminal event. `execution_ready` is
  true only when all six declared operation capabilities are true.
- `external_provider`: connection, selected adapter/location, completed
  snapshot, provider links, availability proof, write permissions, and the
  provider-specific version/fence must be ready.

Connecting or syncing an external integration never implicitly changes the
selected scheduling authority. `active_pos_provider` selects an adapter only
inside `external_provider`; it must not carry scheduling-authority meaning. An
explicit blank provider is not Square: genuinely new external work fails closed
until the reviewed tenant-bound provider activation commits. Persisted retry and
target origins continue to select their historical executor/provider without
consulting that current blank selection.

## Confirmation And Lifecycle Invariants

- Final review or caller approval authorizes an attempt; it is not
  confirmation.
- `owner_manual` creates pending owner-review work and never automatically
  confirms.
- `manleai_calendar` confirms only after an atomic internal commit returns a
  durable root appointment ID, booking-attempt ID, and complete child graph.
  The result must contain no provider/POS evidence.
- `external_provider` confirms only after provider success returns the required
  booking ID and metadata and the local transaction persists the result.
- Rescheduled and cancelled wording requires the corresponding
  authority-specific durable mutation evidence, not only an existing booking
  or appointment ID.
- Model output, UI state, webhook payloads, transcript wording, notification
  delivery, and owner-review status cannot manufacture confirmation evidence.
- Unknown outcomes remain unconfirmed. Exact internal response-loss retry must
  preserve the operation key, logical fingerprint, and authority so a committed
  replay returns the same IDs; changed evidence conflicts.
- Error records must identify the actual failure domain. Internal scheduling
  failures must not be mislabeled as POS failures.

## Party And Multi-Segment Operations

Party operations are all-or-none at the caller-facing confirmation boundary:

- `owner_manual`: create one structured pending owner-review request covering
  the complete party plan; never confirm any child automatically.
- `manleai_calendar`: Phase 4B submits one structured aggregate request and
  receives one exact quote covering every guest/service unit, concrete staff
  assignment, occupied range, and resource allocation. Create rechecks that
  complete graph and capacity under the transaction locks, then commits one
  root appointment with all children or commits nothing. Missing guest labels,
  a guest-reference count that differs from `party_size`, incomplete children,
  stale resource evidence, or any conflict keeps the whole operation
  unconfirmed.
- `external_provider`: require one provider operation proven atomic for the
  whole party and every required staff/resource allocation. The current path
  performs no sequential child writes and hands off for owner review while that
  capability is unavailable.

A partial or unknown result must never be flattened into a confirmed party
result. Structured owner-review and reconciliation records retain applicable
authority evidence.

## Phase 4B-4C Data-Driven Execution Contract

- **Source of truth:** the persisted salon authority/version; V48 canonical
  service/staff eligibility, enabled capacity policies, `local_override` hours,
  weekly schedules, buffers, resource requirements/pools, exceptions, config
  fence, and activation; V49 quote/attempt/appointment/service/resource/event
  evidence; V50 database guards over that evidence; and V51 lifecycle release,
  exact-version, exact-plan, event, and terminal-state guards.
- **Dynamic inputs:** salon, authority/config versions, preferred date,
  configured `max_party_size`, ordered guest references and service units,
  staff selection modes, concrete assignments, schedules, buffers, existing
  conflicts, pool capacities/overrides, requested customer details, quote proof,
  operation identity, exact lifecycle target/version, cutoff, and cancellation
  reason where the conversation contract requires it. No salon, service,
  staff, guest, date, resource, or lifecycle target value is encoded as a
  runtime product rule.
- **Fixed invariants:** the three protocol authority tokens; quantity-one
  normalized units; all-or-none root/child commit; exact guest-reference count
  equality with `party_size` when references are present; half-open
  `[start,end)` ranges; deterministic lock ordering under the V50
  `READ COMMITTED` pool-lock protocol; complete durable graph evidence; exact
  one-version lifecycle transitions; cancelled as terminal; release ownership
  by the lifecycle attempt; and no POS/provider fields.
- **Hardcoding audit:** passed for Phase 4B-4C. Runtime planning, lifecycle
  targets, and UI choices are
  derived from the owners above; fixed constants are protocol, concurrency, or
  booking-safety invariants rather than salon-specific semantic rules.

## Authority Switching

An authority switch is an explicit authorized-actor action with reviewable
readiness and dry-run evidence. Platform Admin acts directly and does not need
Tenant approval; Platform Ops requires exact delegated authority. A connection,
sync, import, provider webhook, or configuration
transfer must not switch authority implicitly.

Switching must:

1. Capture the requested new authority, current authority, actor, and stable
   switch operation key.
2. Validate new-authority readiness without mutating existing appointments.
3. Prevent concurrent booking dispatch during the final switch fence.
4. Preserve every appointment, attempt, quote, and segment's originating
   authority and authority-native identifiers.
5. Avoid automatic migration, duplicate creation, or cross-authority replay of
   historical appointments.
6. Route reschedule, cancellation, retry, and reconciliation through the
   originating authority.
7. Make rollback explicit and safe; never silently reinterpret provider
   appointments as internal appointments or the reverse.

Configuration transfer does not move live scheduling authority, appointments,
provider connections, or provider state unless a later versioned transfer
contract explicitly adds that operational workflow.

The implemented Phase 5 domain workflow is salon-scoped and explicit:

- V52 owns immutable switch runs/events and an optional exact inverse
  `rollback_of_switch_run_id`; a reverse switch is a new reviewed operation,
  never an in-place rollback or reinterpretation of historical work.
- `backend/modules/scheduling_authority_switch` exposes authenticated preview,
  latest, detail, and commit routes. Preview is read-only. Commit requires a
  ready preview, exact source authority/version, stable action key, shared
  scheduling advisory fence, live-external-execution check, and transactional
  readiness revalidation.
- External-provider commit readiness is rechecked inside the provider-owned
  transaction callback exposed by `pos.Repository.WithSchedulingFenceTx`.
  V53 preserves exact external quote authority provenance across switches;
  V54 owns owner-first onboarding compatibility, and V55 owns optional
  pending-approval target-authority evidence.
- Platform Scheduling owns the operator selection workflow through
  `frontend/features/platform/platform-scheduling-authority-control.tsx` and
  `frontend/lib/api/scheduling-authority-switches.ts`. The same card loads the
  independent persisted `booking_mode` through
  `backend/modules/scheduling_behavior` and
  `frontend/lib/api/scheduling-behavior.ts`, presents the backend-owned
  effective behavior, and saves booking mode through its own versioned,
  idempotent, audited command. Authority and booking-mode mutations share the
  scheduling fence but never change each other. The legacy Tenant
  preview/commit component remains compatibility code. Integrations remains
  provider setup and never changes authority implicitly.

The normalized Platform v2 workflow presents this as one operator-owned
authority change. Target selection loads readiness automatically and one
visible change action performs the version-fenced commit. Preview/run/event
records remain internal immutable audit and replay evidence. Platform Admin
does not require Tenant approval; Platform Ops still requires
`technical.write`. Readiness, live-execution, authority-version, idempotency,
atomicity, and originating-authority rules remain unchanged.

A blocked/stale preview, authority-version drift, changed readiness evidence,
or live external execution prevents commit. The authorized operator must
resolve the blocker and retry the one-step command; the implementation creates
fresh internal evidence and does not bypass those
operational gates.

## API And Persistence Contract

Phase 0 added authority fields and immutable authority snapshots. Phase 1 uses
the selected setting only for genuinely new availability/create dispatch and
uses persisted operation, retry-attempt, and target-appointment origins for
historical work. Phase 2 adds the separate owner-manual request aggregate and
includes both booking attempts and scheduling requests when resolving an
operation-key origin.

Implemented V46 ownership is:

- `salon_settings.scheduling_authority` owns the selected salon mode and
  defaults existing behavior to `external_provider`.
- `booking_attempts` stores `scheduling_authority`, authority provider,
  appointment ID/version, target version, idempotency key, and
  location/snapshot fence.
- `booking_attempt_segments` stores the authority plus authority provider and
  service/version/staff identities.
- `appointments` stores the authority plus authority provider,
  appointment/version/customer identities and nullable confirmation
  provenance.
- `appointment_services` stores the authority plus authority provider and
  service/version/staff identities.
- `availability_quotes` stores the authority plus authority provider and
  location/snapshot fence.

V46 backfills existing rows as `external_provider` from their own persisted POS
or parent evidence. Historical `confirmed_at`, `confirmed_by_user_id`, and
`confirmation_source` remain null because the old rows do not prove the exact
confirmation timestamp or actor; the migration does not invent them. When a
later active operation proves exact authoritative external calendar truth,
repository mirror canonicalization may fill missing confirmation time/source
at that transition while preserving any provenance already stored. Phase 0
repository/DTO work keeps these generic snapshots aligned with legacy POS
fields, while legacy fields remain the external-provider source for provider
dispatch, fingerprints, retry, reconciliation, and confirmation. Phase 1
routing resolves authority through the owner-scoped repository before
execution. Persisted operation/retry/target origins take precedence and must
agree; only origin-free new work reads the current setting. The Settings read
response exposes `scheduling_authority`; the existing Settings update request
intentionally does not accept or switch it.

Implemented V47 Phase 2 ownership is:

- `scheduling_requests` is the owner-manual aggregate root with one stable
  `(salon_id, scheduling_authority, operation_key)` identity, immutable logical
  inputs, optimistic version, and lifecycle timestamps;
- `scheduling_request_segments` and `scheduling_request_events` are immutable
  child evidence; event action keys are unique within a request;
- `owner_notifications.scheduling_request_id` links the deduplicated queued
  owner-review outbox row; and
- `call_sessions.scheduling_request_id` plus the request's
  `call_session_id` form a tenant-consistent reciprocal one-to-one audit link.

Implemented V56 delivery ownership is separate from the scheduling aggregate:

- `owner_notifications` remains the durable in-product/outbox root, while
  delivery status never confirms a request or mutates an appointment;
- provider-neutral attempts/events/actions preserve bounded claim, callback,
  replay, and dead-letter evidence;
- provider acceptance is not delivery proof, and only signed monotonic
  provider events may record terminal delivery; and
- a pre-dispatch failure may retry within the bounded technical policy, while
  an ambiguous post-dispatch outcome is dead-lettered and cannot be requeued.

Implemented V61 retention ownership is also separate from execution dispatch:

- `backend/modules/scheduling_retention` redacts, but does not delete, terminal
  request, notification, and expired-audio PII under a policy-versioned
  baseline 90-day fence;
- scheduling request roots, segments, and events retain originating/target
  authority, operation and audit identities, versions, statuses, timestamps,
  and tenant ownership, so historical resolution and exact replay lineage do
  not fall back to the salon's current authority after redaction;
- event JSON retains only an explicit safe audit allowlist; notification
  message/destination fields and party guest references are removed while
  provider IDs, provider versions, delivery outcomes, and audit identifiers
  remain evidence; and
- pending/contacted requests, live delivery leases, queued/retrying/unknown
  outcomes, open reconciliation work, and active consent/STOP routing keys are
  ineligible. The retention worker makes no provider call.

Implemented V48 Phase 3 ownership is:

- database-managed `salon_settings.scheduling_authority_version` changes only
  when the authority token changes; configuration/activation does not change it;
- `manleai_calendar_configs` owns the root policy, monotonic config fence, and
  latest version-fenced owner activation evidence; immutable events retain
  earlier activations, and V48 creates no default/backfill row;
- `salons.timezone` remains timezone authority, and only
  `salon_business_hour_periods.source=local_override` belongs to internal-
  calendar hours. These rows have no provider/location/freshness evidence and
  use explicit midnight-end plus non-overlap constraints;
- `manleai_calendar_staff_weekly_periods`, service policies, service/staff and
  service/resource joins, resource pools, and exceptions are tenant-fenced
  children of the config while canonical `services` and `staff` retain identity
  and eligibility ownership. Service/staff eligibility does not require a
  service-policy row, so Staff-first configuration has no setup-order
  dependency;
- exception core data is immutable and active same-scope ranges cannot overlap;
  replacement is cancel plus create; and
- immutable `manleai_calendar_config_events` owns salon-unique action keys,
  payload fingerprints, fixed event type, previous/result config fences, owner
  actor, and bounded payload. Canonical timezone/service/staff eligibility and
  every calendar child mutation invalidate the monotonic config fence.

Implemented V49 Phase 4A foundation ownership is:

- shared booking/appointment ledgers accept authority-native internal rows but
  enforce null POS/provider evidence for `manleai_calendar` shapes;
- availability quotes snapshot authority/config versions, expiry, request and
  slot fingerprints, normalized slot segments, and resource evidence;
- immutable internal execution events bind the committed appointment, attempt,
  quote, operation key, and logical request fingerprint;
- internal appointment service/resource records retain the committed plan, and
  active internal staff occupied ranges are protected by a database exclusion
  constraint using `[start,end)`; and
- shape, tenant, root/segment, quote-consumption, and durable-evidence guards
  prevent fake provider evidence, partial internal confirmation, and
  cross-authority mutation.

Implemented V50 Phase 4B guard ownership is:

- aggregate quote validation requires the complete canonical service, staff,
  capacity-mode, resource-requirement, active-pool, guest-reference, and
  `party_size` graph;
- all normalized units have `quantity=1`; guest references are either absent
  for a single guest or present on every segment with the distinct count equal
  to `party_size`;
- sorted active resource-pool locking plus half-open capacity/override checks
  serialize concurrent claims and prevent over-capacity commits, independent
  of caller segment order;
- the committed quote, attempt, root appointment, ordered child service graph,
  resource-allocation graph, and execution event must match exactly in both
  directions; and
- consumed quote evidence and committed book history are immutable. V50 uses
  the V49 appointment/resource evidence and does not add a second reservation
  ledger.

Implemented V51 Phase 4C guard ownership is:

- `appointment_services.released_by_attempt_id` identifies the exact lifecycle
  attempt that released each old-plan row, with tenant/origin consistency;
- appointment identity, customer/party shape, originating authority, and
  confirmation provenance remain immutable while every lifecycle transition
  advances `authority_appointment_version` by exactly one;
- one unique, contiguous execution event owns each root authority-appointment
  version, and cancelled roots are terminal;
- a reschedule quote, lifecycle attempt, released old plan, replacement active
  plan, and event graph must match exactly under the target/config/activation
  fences; and
- cancellation records the exact released old-plan attempt snapshot, leaves no
  active service/resource rows, and preserves that historical event result for
  exact replay after later root mutations.

Phase 3 authenticated configuration routes use the
`/api/salons/:id/manleai-calendar` aggregate plus config/hours, staff, services,
resources, exceptions, and activation children. Reads are owner-scoped. Every
mutation returns the uniform `{ "manleai_calendar": { ... }, "replayed":
boolean }` wrapper; callers replace local state with that returned aggregate.
Complete route/payload/error contracts live in `docs/api.md`.

The additive authenticated neutral routes are
`POST /api/salons/:id/scheduling-availability`,
`POST /api/salons/:id/scheduling-actions`,
`GET /api/salons/:id/scheduling-requests`,
`GET /api/salons/:id/scheduling-requests/:request_id`, and
`PATCH /api/salons/:id/scheduling-requests/:request_id`. The existing booking,
availability, appointment, reconciliation, and Square routes remain external-
provider compatibility surfaces.

The authenticated authority-switch routes are
`POST /api/salons/:id/scheduling-authority-switches/preview`,
`GET /api/salons/:id/scheduling-authority-switches/latest`,
`GET /api/salons/:id/scheduling-authority-switches/:run_id`, and
`POST /api/salons/:id/scheduling-authority-switches/:run_id/commit`.
The canonical Platform command is
`PUT /api/v2/platform/tenants/:tenant_id/scheduling/authority`, with
`POST .../authority/readiness` for read-only readiness and
`GET .../authority/history/latest` for the latest immutable run projection.

The neutral availability/action routes carry aggregate internal quote, durable
root/child confirmation, and Phase 4C lifecycle execution evidence; no separate
provider-shaped internal route is introduced. The authority-switch API owns
cross-authority preview/commit and explicit inverse-run state. Reconciliation
state and bounded execution-failure diagnostics remain separate from switch
history and must not overload existing POS fields. The current settings,
configuration/readiness aggregate, neutral scheduling result unions, immutable
origins, pending request state, and external evidence/error contracts remain
their source contracts; presentation copy must not infer or replace them.

Legacy `pos_*`, Square, and `active_pos_provider` fields remain compatibility
and external-provider fields. Internal modes must not populate fake provider
IDs, fake provider versions, or fake POS errors to satisfy old shapes.

`docs/api.md` is the source of truth for complete endpoint payloads and error
codes.

## UI And Copy Contract

Authority-aware owner surfaces must show the selected authority, readiness,
blockers, and the exact next action near scheduling controls when those
controls exist.

- `owner_manual`: emphasize pending request review; do not show Square as a
  prerequisite or imply automatic availability.
- `manleai_calendar`: show internal hours, calendar conflicts, and atomic write
  readiness, while separating configuration readiness, aggregate execution
  completeness, and exact operation capabilities.
- `external_provider`: show selected provider setup, sync, mapping, and write
  blockers while preserving the provider's confirmation boundary.

Loading, empty, error, disabled, switch-in-progress, conflict, success, pending,
and reconciliation states must be distinct. Confirmation copy is derived from
authority evidence, never from optimistic client state.

The current Appointments page includes the Phase 2 owner-review request queue,
owner-notification delivery operations, scheduling readiness, structured
internal create, and whole-root internal lifecycle controls for complete
internal-origin appointment rows. Delivery rows expose only masked/safe
operational evidence; notification success does not alter scheduling evidence
or confirmation wording. Configuration
is placed in Settings for salon policy/hours/resources/exceptions/activation,
inside each Staff edit flow for that staff member's schedule/assignments/
exceptions, and inside each Service edit flow for that service's policy/staff/
resource requirements. These surfaces consume backend constraints and blockers,
preserve action keys for safe retry, reload on config-version conflict, and
remain responsive across desktop/mobile. Internal create builds structured
guests and ordered service rows from enabled capability-matching policies,
uses one verified aggregate quote, shows concrete assigned staff and resource
allocations, preserves exact replay identity after unknown response, clears
stale proof on typed conflict, and confirms only with durable root/child
evidence. Integrations and provider calendar remain external-provider surfaces.
Lifecycle rows use the persisted target origin even after a later current-
authority change, hydrate only the active current-version plan, require exact
target/version and reviewed reschedule proof, fail closed when client cutoff
evidence is missing/invalid/equal-to-now, and lock uncertain submissions to an
exact replay. Reschedule confirms only the exact replacement graph; cancel
confirms only terminal status, a one-version advance, and zero active children.
When an exact historical book or reschedule result is replayed after a later
lifecycle change, conversation copy reports that the prior operation succeeded
at that time and that current status may differ. It must not restate the replay
as a current “confirmed”, “has been booked”, or “has been rescheduled” claim.
Duplicate conversation-event replay still returns its original persisted AI
message unchanged. Cancel replay remains terminal and current-safe.
Platform Scheduling provides authority selection, automatic readiness,
blockers, one explicit authority-change action, independent AI booking-mode
selection, the persisted effective behavior, and latest-run evidence. A
readiness preview is labelled as a readiness check; only a committed run is
labelled as the last authority change. Internal
preview/commit details remain immutable backend evidence rather than two
operator-facing buttons.

## Security, Privacy, And Tenancy

- Enforce `salon_id` ownership before reading or mutating authority settings,
  schedules, appointments, requests, switches, and evidence.
- Validate the acting owner's salon relationship before recording owner action.
- Keep external-provider tokens encrypted and inside the provider boundary.
- Do not expose provider identifiers on internal-authority records.
- Preserve existing call, party, customer, transcript, recording, and retention
  protections in every authority mode.
- Enforce party-request-to-call-session ownership through the same
  `(salon_id, call_session_id)` parent key; migration preflight must fail closed
  on historical mismatches instead of moving a child between tenants.
- Treat V61 redaction markers as irreversible audit state. Never reconstruct
  removed PII from event copy, provider copy, current salon settings, or a
  different authority executor.
- Audit authority changes and confirmation evidence without storing secrets or
  unnecessary caller PII.

## Idempotency And Observability

Every create, reschedule, cancel, pending-request, party, and authority-switch
operation requires a stable salon-scoped operation key and logical fingerprint.
Exact replay returns the prior result without duplicating appointments,
requests, provider writes, notifications, or audit events. A changed authority,
target, customer, time, service/staff assignment, or party plan is a different
logical operation.

Every ManleAI Calendar configuration mutation similarly requires a salon-
unique action key, logical fingerprint, and expected config fence. Exact replay
returns the current aggregate with `replayed=true`; changed key reuse or stale
fence conflicts. Immutable config events retain owner actor and previous/result
fences. Because database triggers invalidate the fence for root, child, local-
hour, timezone, and canonical eligibility changes, the version is monotonic but
is not required to increase by exactly one per API request.

Operational evidence should record the authority token, originating authority,
authority version/fence, operation key, evidence type/ID, result class, retry
policy, rollback state, reconciliation state, and bounded failure diagnostics.
Do not infer these fields from presentation copy.

## Implementation Status

- Implemented Phase 0 foundation: documentation, V46 persistence, DTO
  compatibility, and nullable confirmation-provenance fields without invented
  historical values.
- Implemented Phase 1 boundary: owner-scoped current/origin authority
  resolution including the public read-only create-lineage resolver, cross-
  source equality checks, booking API/conversation and Square test-write
  dispatch, provider-free history replay/candidate delegation, active exact-
  mirror provenance completion, and the external-provider adapter over the
  exact existing Square/POS path.
- Implemented Phase 2 owner-manual workflow: V47 request/segment/event
  aggregate, operation/action idempotency, request-only availability, neutral
  action result, book/reschedule/cancel conversation flow and call-session
  linkage, queued notification outbox creation, additive API, and responsive
  Appointments review queue. It performs no provider dispatch or appointment
  write and makes no delivery claim.
- Implemented Phase 3 ManleAI Calendar configuration: V48 config/version/event
  ownership, local hours, staff schedules, service capacity policies, resource
  pools/requirements, exceptions, activation audit, owner-scoped aggregate and
  mutation APIs, configuration blockers, and Settings/Staff/Services/
  Appointments UI.
- Implemented Phase 4A staff-only execution: V49 quote/ledger/event/exclusion
  evidence, strict-DST availability, registered executor, quote-origin
  dispatch, replay-first atomic single-guest create, durable internal
  confirmation, conversation integration, and responsive Appointments create.
- Implemented Phase 4B aggregate execution: structured multi-guest,
  multi-service staff-only/pooled planning, deterministic staff and resource
  allocation, one all-or-none root/child commit, conversation aggregate proof,
  Appointments guest/service builder, and V50 capacity, concurrency, exact-
  graph, guest-party, and history guards.
- Implemented Phase 4C lifecycle execution: target-origin reschedule
  availability, exact root/version and open-cutoff validation, quote-backed
  whole-root reschedule, quote-free whole-root cancel, atomic old-plan release,
  same-root one-version transitions, exact historical event replay,
  current-plan-only hydration, V51 lifecycle guards, explicit conversation
  authorization/reason handling, and fail-closed Appointments controls.
- Implemented Phase 5 authority switching: V52 immutable preview/commit audit,
  shared-fence authority/version CAS, transactional target-readiness recheck,
  live external-execution guard, explicit inverse-run reference, V53 external
  quote provenance, V54 owner-first onboarding compatibility, V55 pending-
  approval target evidence, authenticated routes, and responsive Settings UI.
- Implemented Phase 6 application and operations hardening: V56 provider-
  neutral owner-notification delivery with Twilio transport isolation; V57
  recurring-job heartbeat/run evidence and owner-scoped queue health; V58
  cross-table service/category alias ownership; V59 separate customer SMS
  consent, policy, source-fenced delivery ledger, worker, callbacks, and owner
  operations; V60 safe Square webhook metrics/detail/requeue operations; V61
  policy-versioned terminal scheduling/notification PII redaction; and V62
  database-enforced party-request/call-session tenant integrity. These slices
  preserve authority-native confirmation evidence and do not make notification
  state part of scheduling confirmation.
- Current operation contract: all six capabilities may be true when the exact
  selected-authority, current activation/config, and service-policy predicates
  pass; aggregate `execution_ready` is true only when every capability is true.
  Historical target-origin dispatch is distinct from this new-work readiness
  view.
- Remaining release work is operational/external evidence tracked by the
  production-readiness checklist, not missing V56-V62 application
  implementation. Code readiness does not prove live provider credentials and
  callback delivery, real Square sandbox payload behavior, representative
  capacity, configured monitoring/on-call response, approved production backup
  storage and retention, witnessed load/restore drills, approved RPO/RTO, or
  remaining security/compliance approval. Owner and customer notification
  delivery remain operationally separate from authority confirmation.

No mode is production-ready merely because its protocol token or database field
exists. Readiness requires the complete authority-specific flow and tests.
