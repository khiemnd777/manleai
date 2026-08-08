# API

The additive normalized Platform tenant contract is defined in
`docs/platform-tenant-api-v2.md`. Legacy routes documented below remain active
compatibility adapters until their registered consumers have moved and the v2
removal gates pass. v2 does not introduce dual writes; both surfaces delegate
to the same domain services and repositories.

Base URL: `http://localhost:18089`

All endpoints except login, bootstrap owner setup, refresh, logout, health,
public registration and invitation acceptance, public catalog endpoints, the Square OAuth callback, Twilio voice webhooks, and
the signed Twilio Messaging callbacks require:

```txt
Authorization: Bearer <access_token>
```

Production API requests pass through an atomic Redis token bucket before route
dispatch. The limiter applies both a global client bucket and a route-class
bucket, derives Redis identities with HMAC instead of storing raw client IPs or
bearer tokens, and returns `RateLimit-Limit`, `RateLimit-Remaining`, and
`RateLimit-Reset`. A rejected request returns generic `429 RATE_LIMITED` plus
`Retry-After`; a required Redis failure returns generic
`503 RATE_LIMIT_UNAVAILABLE` and never falls through to the protected route.
The edge overwrites `X-ManleAI-Client-IP`; caller-supplied values are not trusted.

## Scheduling Authority Status And API Honesty

`docs/scheduling-authority.md` is the normative confirmation contract. The
exact authority tokens are `owner_manual`, `manleai_calendar`, and
`external_provider`.

Unless a section is explicitly labeled as planned, this document describes the
current API. Phase 2 routes authority-neutral availability plus book,
reschedule, and cancel actions through `backend/modules/scheduling.Service`.
Availability without a target and an origin-free new action resolve the owner-
scoped persisted `salon_settings.scheduling_authority`. Existing operation
keys resolve their origin across `booking_attempts` and
`scheduling_requests`; retry attempts resolve their booking-attempt origin;
target-aware availability and appointment mutations resolve their target-
appointment origin. Reschedule/cancel require every operation, retry, and
target origin present to agree before dispatch.

Reschedule-candidate lookup, `ReplayCreate`, and `ReplayCancel` are provider-free
history delegation and do not select an executor or call a provider. Existing
appointment/calendar history and reconciliation reads continue through the
established booking service. External lease recovery and provider-calendar
persistence/matching/reconciliation are explicitly fenced to
`external_provider`. Square webhook target and repair selection remains Square
provider/connection-scoped so historical external mirrors can converge after a
later authority switch; downstream calendar writes cannot mutate internal-
origin rows.

`external_provider` has the only provider-backed confirming executor, implemented by
`backend/modules/scheduling_external_provider.Adapter` delegating to the
booking service with V86 Atomic Slot Commit capability and claim gates. V87
permits only a Square buyer-level, concrete-staff, single-create operation when
current expiring evidence matches the exact connection, integration config,
location, API version, and normalized OAuth scopes. Square seller-level create,
all Square reschedule, external party create, and external resource-capacity
execution remain request-only/fail-closed before provider dispatch.
`owner_manual` has a ready, non-confirming executor
in `backend/modules/scheduling_owner_manual`: availability returns
`request_only`, and book/reschedule/cancel actions create or replay durable
pending owner-review requests. It does not call a POS provider or fabricate an
appointment, provider ID, POS error, or reconciliation record. Phase 4C
registers `manleai_calendar` for verified aggregate availability and atomic
`book` plus whole-root `reschedule`/`cancel` across structured multi-guest,
multi-service staff-only and pooled plans. It confirms only with durable
internal root appointment/attempt IDs, exact target/result versions, status,
active-child count, and the operation's complete child snapshot, and it returns
no provider/POS evidence. V52-V55 expose explicit owner-reviewed authority
preview/commit, readiness/version/concurrency fences, immutable audit history,
and an explicit inverse-run reference; the switch controls live in Settings,
not Appointments or Integrations. The Appointments page contains the owner-
review queue, owner-notification delivery operations, scheduling readiness,
structured internal create, and target-origin lifecycle dialogs.

`scheduling.Service.CurrentSchedulingAuthority` provides a validated,
owner-scoped current-token read. Square readiness exposes that value as
`scheduling_authority` and sets `can_test_booking` and
`can_enable_ai_booking` false unless it is `external_provider`. This is a gate
for new work, not a rule that hides or strands persisted external-origin work.
V86 adds the independent `atomic_slot_commit` readiness check and V87 makes it
operation-specific. Both values remain false unless the selected provider can
perform the requested operation and current exact database evidence exists.
For Square, `can_enable_ai_booking` proves only supported buyer-level single
create; conversation and scheduling actions still apply independent
reschedule, party, and resource-capacity gates.
The read-only `scheduling.Service.ResolveCreateSchedulingAuthority` resolves a
create operation/retry lineage first and falls back to the current token only
when no persisted origin exists. Square test create calls it after
provider-free replay; it does not dispatch an executor or provider.

Runtime confirmation remains strict: an external create/reschedule first
requires a committed V86 slot claim, and a Square/external-provider operation
is confirmed, rescheduled, or cancelled only after provider success returns the
required provider booking metadata and the backend persists it. An
`owner_manual` result is always `pending_owner_review`, and the owner-review UI
cannot turn it into an appointment or mutate a target appointment. A
`manleai_calendar` result is confirmed only after the V49 atomic commit passes
the applicable V50 capacity/concurrency/exact-graph/guest-party and V51
lifecycle guards and returns durable internal root/version/child evidence.
Current `pos_*`, Square, and
`active_pos_provider` fields remain external-provider compatibility fields and
are null in persisted internal rows.

Generic clients must branch on `scheduling_authority` and the authority-native
IDs, status, versions, and backend capabilities before reading a legacy POS or
provider field. A non-empty `pos_booking_id`, `pos_appointment_id`, Square
connection/readiness result, or `active_pos_provider` never selects an
authority. `authority_appointment_version` remains present even when an
external provider's valid baseline version is zero. Unknown authority fails
closed. The field-by-field compatibility
inventory, public/config-transfer exclusions, and removal gates are maintained
in `docs/operations/owner-first-compatibility.md`.

### Implemented Authority-Not-Ready Error

The authenticated neutral scheduling handlers map
`booking.ErrSchedulingAuthorityNotReady` to:

```json
{
  "error": {
    "code": "SCHEDULING_AUTHORITY_NOT_READY",
    "message": "The salon's scheduling authority is not ready for scheduling actions."
  }
}
```

The HTTP status is `409 Conflict`. This generic message does not reveal the
selected token, unavailable capability, provider state, or wrapped internal
error. It is the implemented fail-closed behavior when the resolved origin or
current selection cannot execute the requested action; it is not a fallback
booking request, does not authorize provider dispatch, and must not be
presented as confirmed. Reschedule-candidate lookup and create/cancel replay do not return
this error merely because the current setting is unready: they are persisted
history reads with no provider dispatch.

### Implemented Authority-Neutral Scheduling API

These authenticated routes are additive. Existing `/availability`,
`/booking-attempts`, and appointment mutation routes remain the external-
provider compatibility API; `owner_manual` callers use the neutral routes.

`POST /api/salons/:id/scheduling-availability`

Accepts the provider-neutral availability body documented for
`POST /api/salons/:id/availability`, including the mutually exclusive
`target_appointment_id` and `retry_of_attempt_id` origin selectors described
there. The response is a discriminated union:

```json
{
  "service_id": "manicure-service-uuid",
  "staff_selection_mode": "anyone",
  "segments": [
    {
      "service_id": "manicure-service-uuid",
      "staff_selection_mode": "anyone",
      "guest_reference": "guest-1",
      "quantity": 1
    },
    {
      "service_id": "pedicure-service-uuid",
      "staff_selection_mode": "specific",
      "staff_id": "staff-lan-uuid",
      "guest_reference": "guest-2",
      "quantity": 1
    }
  ],
  "party_size": 2,
  "preferred_date": "2026-07-28",
  "limit": 20
}
```

```json
{
  "kind": "request_only",
  "scheduling_authority": "owner_manual"
}
```

For `external_provider` and ready Phase 4C `manleai_calendar`, `kind` is
`verified_slots` and `verified_slots` contains the authority-native quote/slot
response. `owner_manual` returns no synthetic openings, quote, or slot
fingerprint. Internal availability accepts ordered segments with
`staff_selection_mode` `specific` or `anyone`, `guest_reference`, and
quantity. Quantities are expanded into ordered quantity-one units; multi-guest
requests require a non-empty guest reference on every unit and the distinct
guest-reference count must equal `party_size`. Every returned slot contains
the exact concrete staff/resource plan:

```json
{
  "kind": "verified_slots",
  "scheduling_authority": "manleai_calendar",
  "verified_slots": {
    "quote_id": "availability-quote-uuid",
    "request_fingerprint": "sha256-hex",
    "expires_at": "2026-07-28T12:02:00Z",
    "service_id": "manicure-service-uuid",
    "service_name": "Gel manicure",
    "staff_selection_mode": "anyone",
    "preferred_date": "2026-07-28",
    "duration_minutes": 45,
    "timezone": "America/Chicago",
    "slots": [
      {
        "fingerprint": "sha256-hex",
        "start_time": "2026-07-28T15:00:00Z",
        "end_time": "2026-07-28T15:45:00Z",
        "staff_selection_mode": "anyone",
        "segments": [
          {
            "service_id": "manicure-service-uuid",
            "service_name": "Gel manicure",
            "staff_id": "staff-mai-uuid",
            "staff_name": "Mai",
            "staff_selection_mode": "anyone",
            "guest_reference": "guest-1",
            "quantity": 1,
            "duration_minutes": 45,
            "scheduled_start_time": "2026-07-28T15:00:00Z",
            "scheduled_end_time": "2026-07-28T15:45:00Z",
            "occupied_start_time": "2026-07-28T14:55:00Z",
            "occupied_end_time": "2026-07-28T15:50:00Z",
            "buffer_before_minutes": 5,
            "buffer_after_minutes": 5,
            "resource_allocations": []
          },
          {
            "service_id": "pedicure-service-uuid",
            "service_name": "Spa pedicure",
            "staff_id": "staff-lan-uuid",
            "staff_name": "Lan",
            "staff_selection_mode": "anyone",
            "guest_reference": "guest-2",
            "quantity": 1,
            "duration_minutes": 45,
            "scheduled_start_time": "2026-07-28T15:00:00Z",
            "scheduled_end_time": "2026-07-28T15:45:00Z",
            "occupied_start_time": "2026-07-28T15:00:00Z",
            "occupied_end_time": "2026-07-28T15:45:00Z",
            "buffer_before_minutes": 0,
            "buffer_after_minutes": 0,
            "resource_allocations": [
              {
                "resource_pool_id": "pedicure-chair-pool-uuid",
                "resource_name": "Pedicure chairs",
                "units_allocated": 1
              }
            ]
          }
        ]
      }
    ]
  }
}
```

The internal quote expires after two minutes. Services for one guest are
scheduled sequentially; different guests may overlap only when staff and
resource capacity permit. Availability rejects ambiguous or nonexistent
salon-local DST wall times and fails closed with `AVAILABILITY_QUOTE_STALE`
when staff/resource conflict evidence cannot be verified safely.

For a whole-root internal reschedule, the request additionally includes
`target_appointment_id` and copies the target's current `party_size` plus exact
ordered service/guest shape into `segments`. The neutral service resolves the
target's persisted `manleai_calendar` origin even if the salon's current
authority differs. The target must be active and expose exactly one unreleased
plan at `authority_appointment_version`; target-aware planning excludes that
old plan from conflict checks but still requires the current internal config
and activation fence and an open reschedule cutoff. The response binds both
the outer and nested availability result to the same target version:

```json
{
  "kind": "verified_slots",
  "scheduling_authority": "manleai_calendar",
  "target_authority_appointment_version": 2,
  "verified_slots": {
    "quote_id": "target-versioned-quote-uuid",
    "target_authority_appointment_version": 2,
    "slots": []
  }
}
```

The replacement quote preserves party size, service order, quantity-one units,
and guest references; it may contain newly assigned staff, times, buffers, and
resources. A changed target version, closed cutoff, stale activation/config, or
incomplete active plan produces a fail-closed error and no lifecycle mutation.

Availability errors are `400 INVALID_REQUEST` for malformed JSON,
`400 VALIDATION_ERROR`,
`409 SCHEDULING_AUTHORITY_NOT_READY`,
`409 AVAILABILITY_QUOTE_STALE`,
`404 SCHEDULING_RESOURCE_NOT_FOUND`,
`409 SCHEDULING_PROVIDER_UNAVAILABLE`, or
`502 SCHEDULING_AVAILABILITY_FAILED`.

`POST /api/salons/:id/scheduling-actions`

Accepts `operation_type` (`book`, `reschedule`, or `cancel`), a stable
`operation_key`, customer details, timezone, requested time, ordered segments,
party size, notes, call-session linkage, and target evidence when applicable.
The authenticated HTTP handler owns the source and records it as
`owner_dashboard`; clients cannot override it. A target is forbidden for book.
Reschedule/cancel require either a tenant-owned target appointment with its
authority or a durable `target_description`. `owner_manual` rejects retry IDs,
availability quote IDs, and slot fingerprints because it neither dispatches a
provider nor claims verified availability.

```json
{
  "operation_type": "book",
  "operation_key": "owner-request-uuid",
  "call_session_id": "optional-session-uuid",
  "customer_name": "Linh Tran",
  "customer_phone": "+13125550101",
  "customer_email": "linh@example.com",
  "requested_start_time": "2026-07-28T20:00:00Z",
  "requested_timezone": "America/Chicago",
  "party_size": 1,
  "segments": [
    {
      "service_id": "service-uuid",
      "staff_selection_mode": "anyone",
      "quantity": 1
    }
  ],
  "notes": "Afternoon is also acceptable"
}
```

An accepted owner-manual action returns `202 Accepted`:

```json
{
  "kind": "pending_owner_review",
  "operation_type": "book",
  "scheduling_authority": "owner_manual",
  "pending_owner_review": {
    "scheduling_request_id": "request-uuid",
    "status": "pending",
    "version": 1,
    "request": {}
  }
}
```

A Phase 4B internal create requires `operation_type=book`, the exact
`party_size`, exact ordered quantity-one segments copied from the verified slot
(including guest reference, concrete service/staff, selection mode, and
scheduled start/end), `availability_quote_id`, and `slot_fingerprint`.
`retry_of_attempt_id` and targets are forbidden. The action's root requested
range must equal the earliest child start and latest child end. A successful
all-or-none commit returns `201 Created`:

```json
{
  "kind": "confirmed_appointment",
  "operation_type": "book",
  "scheduling_authority": "manleai_calendar",
  "confirmed_appointment": {
    "appointment_id": "internal-appointment-uuid",
    "booking_attempt_id": "internal-attempt-uuid",
    "appointment_status": "confirmed",
    "active_child_count": 1,
    "children": [
      {
        "appointment_service_id": "appointment-service-uuid",
        "guest_reference": "guest-1",
        "service_id": "manicure-service-uuid",
        "staff_id": "staff-mai-uuid",
        "staff_selection_mode": "anyone",
        "quantity": 1,
        "scheduled_start_time": "2026-07-28T15:00:00Z",
        "scheduled_end_time": "2026-07-28T15:45:00Z",
        "occupied_start_time": "2026-07-28T14:55:00Z",
        "occupied_end_time": "2026-07-28T15:50:00Z",
        "buffer_before_minutes": 5,
        "buffer_after_minutes": 5,
        "resource_allocations": []
      }
    ]
  }
}
```

The internal result never includes `external_attempt_id`, `appointment`, or
`external_attempt`. Confirmation requires both non-empty internal root IDs and
a child list that exactly matches the quoted guest/service/staff/timing/buffer/
resource graph. The database also verifies that graph bidirectionally and
enforces effective pooled capacity under sorted locks before commit. If the
commit response is lost, repeat the exact operation key, payload, quote, and
slot proof; an exact committed replay returns the same IDs and children with
`"replayed": true`. Reusing the operation key with changed normalized data
returns `SCHEDULING_OPERATION_CONFLICT`. A stale/consumed/drifted quote,
capacity conflict, or invalid execution graph returns
`AVAILABILITY_QUOTE_STALE` and authorizes no confirmed wording.

A Phase 4C internal reschedule replaces the whole active root plan. It requires
the target/version-bound quote above, the target's exact immutable party/
service/guest shape, the complete selected replacement assignment, and the
target evidence:

```json
{
  "operation_type": "reschedule",
  "operation_key": "internal-reschedule-operation-uuid",
  "availability_quote_id": "target-versioned-quote-uuid",
  "slot_fingerprint": "sha256-hex",
  "customer_name": "Linh Tran",
  "customer_phone": "+13125550101",
  "party_size": 1,
  "segments": [
    {
      "service_id": "manicure-service-uuid",
      "staff_id": "staff-mai-uuid",
      "staff_selection_mode": "specific",
      "guest_reference": "guest-1",
      "quantity": 1,
      "requested_start_time": "2026-07-29T15:00:00Z",
      "requested_end_time": "2026-07-29T15:45:00Z"
    }
  ],
  "requested_start_time": "2026-07-29T15:00:00Z",
  "requested_end_time": "2026-07-29T15:45:00Z",
  "requested_timezone": "America/Chicago",
  "target_appointment_id": "internal-appointment-uuid",
  "target_scheduling_authority": "manleai_calendar",
  "expected_target_authority_appointment_version": 2
}
```

The repository replays an exact committed operation before validating current
state. A new execution then requires the exact active target version, open
reschedule cutoff, current internal config/activation, unexpired unconsumed
quote, and revalidated complete replacement graph. One transaction releases
the old plan/resources through the new lifecycle attempt, advances the same
root by one version, installs the exact new plan, consumes the quote, and
appends the matching event. A successful response is:

```json
{
  "kind": "confirmed_appointment",
  "operation_type": "reschedule",
  "scheduling_authority": "manleai_calendar",
  "target_authority_appointment_version": 2,
  "authority_appointment_version": 3,
  "confirmed_appointment": {
    "appointment_id": "internal-appointment-uuid",
    "booking_attempt_id": "internal-reschedule-attempt-uuid",
    "appointment_status": "rescheduled",
    "active_child_count": 1,
    "children": [
      {
        "appointment_service_id": "replacement-service-row-uuid",
        "guest_reference": "guest-1",
        "service_id": "manicure-service-uuid",
        "staff_id": "staff-mai-uuid",
        "staff_selection_mode": "specific",
        "quantity": 1,
        "scheduled_start_time": "2026-07-29T15:00:00Z",
        "scheduled_end_time": "2026-07-29T15:45:00Z",
        "occupied_start_time": "2026-07-29T15:00:00Z",
        "occupied_end_time": "2026-07-29T15:45:00Z",
        "buffer_before_minutes": 0,
        "buffer_after_minutes": 0,
        "resource_allocations": []
      }
    ]
  }
}
```

A Phase 4C internal cancel carries no availability quote, slot fingerprint,
party size, segments, requested range, or timezone:

```json
{
  "operation_type": "cancel",
  "operation_key": "internal-cancel-operation-uuid",
  "target_appointment_id": "internal-appointment-uuid",
  "target_scheduling_authority": "manleai_calendar",
  "expected_target_authority_appointment_version": 3,
  "notes": "Customer requested cancellation"
}
```

Cancel resolves the persisted internal target origin and exact version, checks
the cancellation cutoff, snapshots and releases the exact active plan, advances
the same root to terminal `cancelled`, and leaves zero active children in one
transaction. It does not require the current salon authority, an availability
quote, or current config activation. A successful response is:

```json
{
  "kind": "confirmed_appointment",
  "operation_type": "cancel",
  "scheduling_authority": "manleai_calendar",
  "target_authority_appointment_version": 3,
  "authority_appointment_version": 4,
  "confirmed_appointment": {
    "appointment_id": "internal-appointment-uuid",
    "booking_attempt_id": "internal-cancel-attempt-uuid",
    "appointment_status": "cancelled",
    "active_child_count": 0,
    "children": [
      {
        "appointment_service_id": "replacement-service-row-uuid",
        "guest_reference": "guest-1",
        "service_id": "manicure-service-uuid",
        "staff_id": "staff-mai-uuid",
        "staff_selection_mode": "specific",
        "quantity": 1,
        "scheduled_start_time": "2026-07-29T15:00:00Z",
        "scheduled_end_time": "2026-07-29T15:45:00Z",
        "occupied_start_time": "2026-07-29T15:00:00Z",
        "occupied_end_time": "2026-07-29T15:45:00Z",
        "buffer_before_minutes": 0,
        "buffer_after_minutes": 0,
        "resource_allocations": []
      }
    ]
  }
}
```

The real cancel result's `children` preserve the exact released-plan attempt
snapshot for audit/replay even though `active_child_count` is zero. Current
appointment and candidate reads hydrate only the current version's unreleased
plan, so the cancelled root itself exposes no active segments. Exact lifecycle
replay returns the historical event's target/result versions, status, attempt
ID, and child snapshot after later root mutations; changed operation-key data
conflicts instead of being reinterpreted against the latest root.

An external create confirmed by the provider returns
`201 confirmed_appointment`; confirmed reschedule/cancel returns `200`; a
provider fallback returns `202 external_fallback_pending`. Confirmation is
valid only when the corresponding evidence object contains the required
persisted appointment/provider result.

Action errors are `400 INVALID_REQUEST` for malformed JSON,
`400 VALIDATION_ERROR`,
`409 SCHEDULING_OPERATION_CONFLICT`,
`409 SCHEDULING_AUTHORITY_NOT_READY`,
`409 AVAILABILITY_QUOTE_REQUIRED`, `409 AVAILABILITY_QUOTE_STALE`,
`404 SCHEDULING_RESOURCE_NOT_FOUND`,
`409 SCHEDULING_PROVIDER_UNAVAILABLE`, or
`500 SCHEDULING_ACTION_FAILED`.

`GET /api/salons/:id/scheduling-requests?status=<pending|contacted|resolved|dismissed>&limit=50&offset=0`

Returns the owner-scoped queue directly as
`scheduling_requests`, `limit`, `offset`, and `has_more`. `limit` defaults to
50 and is capped at 100. Each request contains its immutable authority,
operation and customer/request snapshots, ordered `segments`, lifecycle
timestamps, current `version`, and append-only `events`.

V61-retained terminal rows remain readable audit records and add `redacted`,
nullable `redacted_at`, and `redaction_version` fields on the request, segments,
and events. When `redacted=true`, customer/request PII and guest references are
removed and event payloads contain only the explicit safe audit allowlist plus
the redaction marker. Authority, operation/audit IDs, versions, statuses,
timestamps, tenant ownership, and provider evidence remain intact. Pending and
`contacted` requests are never retention-redacted.

`GET /api/salons/:id/scheduling-requests/:request_id`

Returns `{ "scheduling_request": { ... } }` for one owner-scoped request.
`scheduling_authority` is always the request executor origin `owner_manual`.
The optional `target_scheduling_authority` separately records the authority
whose availability or existing appointment the request targeted. It does not
prove that the requested time was reserved. Legacy requests may omit the
target, and exact replay preserves that absence rather than manufacturing a
current target.
List errors are `400 VALIDATION_ERROR`, `404 SALON_NOT_FOUND`, or
`500 SCHEDULING_REQUESTS_FAILED`; detail errors are `400 VALIDATION_ERROR`,
`404 SCHEDULING_REQUEST_NOT_FOUND`, or `500 SCHEDULING_REQUEST_FAILED`.

`PATCH /api/salons/:id/scheduling-requests/:request_id`

```json
{
  "action_key": "owner-dashboard-action-uuid",
  "expected_version": 1,
  "status": "contacted",
  "resolution_reason": "",
  "note": "Left a voicemail"
}
```

Valid transitions are `pending -> contacted|resolved|dismissed` and
`contacted -> resolved|dismissed`. `resolved` and `dismissed` require a
resolution reason; terminal rows cannot transition again. The repository locks
the request row, checks `expected_version`, applies a version compare-and-swap,
and appends the status event in the same database transaction. Replaying the
same action key and payload is idempotent; reusing it for different transition
data is a conflict. The response wrapper is
`{ "scheduling_request": { ... } }`.
Retained redacted rows are already terminal and cannot be changed or restored.

Transition errors are `400 INVALID_REQUEST` for malformed JSON,
`400 VALIDATION_ERROR`,
`409 SCHEDULING_REQUEST_VERSION_CONFLICT`,
`409 SCHEDULING_REQUEST_ACTION_CONFLICT`,
`409 SCHEDULING_REQUEST_TERMINAL`,
`409 SCHEDULING_REQUEST_TRANSITION_CONFLICT`,
`404 SCHEDULING_REQUEST_NOT_FOUND`, or
`500 SCHEDULING_REQUEST_UPDATE_FAILED`.

Request creation persists the request, immutable ordered segment snapshots,
initial event, reciprocal call-session link when supplied, and one deduplicated
queued `owner_notifications` outbox row in one database transaction. Operation
uniqueness plus row locks and fingerprint comparison make exact replay safe and
reject changed payloads. V56 delivery processing is separate from scheduling:
request creation still proves only that work was queued, and no request or
appointment status is changed by SMS delivery.

### Owner Notification Delivery API

The authenticated delivery routes require Platform Operations access to the
fixed `:tenant_id` and an active `notifications` PII grant. They return
masked/safe operational evidence only: no message body, full phone
number, provider message ID, provider response, secret, or raw internal error.

`GET /api/platform/tenants/:tenant_id/operations/owner-notification-deliveries?status=<status>&limit=25&offset=0`

Returns `deliveries`, bounded delivery `metrics`, `limit`, `offset`, and
`has_more`. `limit` defaults to 25 and is capped at 100. Statuses are `queued`,
`delivering`, `provider_accepted`, `sent`, `delivered`, `failed`,
`undelivered`, `dead_letter`, and `disabled`. `provider_accepted` means Twilio
accepted/queued the API request; it is not delivery proof.

Each row includes its notification type, in-product status, delivery status,
provider name, masked destination, attempt count, safe provider status/error
code, timestamps, and backend-owned `can_requeue` decision. Detail additionally
includes immutable safe delivery events.

V61-retained rows add `redacted`, nullable `redacted_at`, and
`redaction_version`. A redacted row omits destination content, returns no
message body or raw error, and always has `can_requeue=false`; safe provider,
status, attempt, audit ID, and timestamp evidence remains available.

`GET /api/platform/tenants/:tenant_id/operations/owner-notification-deliveries/:notification_id`

Returns `{ "delivery": { ... } }` for one salon-owned row.

`POST /api/platform/tenants/:tenant_id/operations/owner-notification-deliveries/:notification_id/requeue`

```json
{
  "action_key": "owner-notification-requeue-uuid"
}
```

Requeue is action-key idempotent and allowed only for a definitive safe
dead-letter state. A post-dispatch unknown outcome cannot be requeued because a
second send could duplicate the SMS. Exact replay returns the same result and
sets `X-Idempotent-Replay: true`; changed key reuse conflicts. Retention-
redacted rows cannot be requeued.

Errors are `400 OWNER_NOTIFICATION_DELIVERY_INVALID`,
`404 OWNER_NOTIFICATION_DELIVERY_NOT_FOUND`,
`409 OWNER_NOTIFICATION_DELIVERY_CONFLICT`,
`409 OWNER_NOTIFICATION_REQUEUE_BLOCKED`, or
`500 OWNER_NOTIFICATION_DELIVERY_FAILED`.

`POST /api/notifications/twilio/status`

Public Twilio Messaging status callback. It requires a valid
`X-Twilio-Signature` calculated from the exact configured HTTPS callback URL
and every received form parameter. `MessageSid`/`SmsSid` resolves the owning
salon before the salon's database-backed Auth Token is used for verification.
Accepted signed events are deduplicated and applied monotonically, then return
`204`. A later lower-rank callback cannot downgrade `delivered` or another
terminal outcome. The route never returns the provider message ID in a JSON
response.

`POST /api/notifications/twilio/inbound/:salon_id`

Public signed Twilio Messaging inbound callback. It verifies the exact URL and
all form parameters and returns `204`, but deliberately does not parse message
keywords or mutate product state. Twilio Messaging Service opt-out handling is
an operational provider configuration; customer SMS and customer consent are
outside this owner-operational delivery slice. See
`docs/operations/owner-notification-delivery.md`.

### Implemented ManleAI Calendar Configuration API

These routes manage structured `manleai_calendar` configuration and readiness.
Phase 4C execution still uses the neutral scheduling routes above. Every route
is authenticated,
and the repository requires the authenticated user to own `:id`. A salon,
staff member, service, resource, or exception outside that tenant is returned
as the tenant-safe not-found error rather than exposing cross-salon existence.

The aggregate source of truth is the V48 root configuration and its children:
the DB-managed config-version fence; `salons.timezone`; only
`salon_business_hour_periods` rows whose source is `local_override`; canonical
services and staff; independent service-to-staff eligibility; staff weekly
periods; service policy, capacity mode, buffers, and resource requirements;
resource pools; active and cancelled exceptions; version-fenced activation;
and immutable configuration events. Imported or legacy-migrated hours do not
satisfy internal-calendar readiness, and no provider hours are copied into the
internal configuration. V49 separately owns execution quotes, appointment/
attempt/service/resource evidence, and immutable execution events; V50 adds
aggregate quote, guest-party, capacity/concurrency, exact committed graph, and
history guards; V51 adds lifecycle release ownership, event-version uniqueness,
terminal cancellation, and exact old/new plan graph guards. The configuration
read aggregate does not expose either immutable event ledger.

Implemented routes:

| Method and path | Purpose | Successful response |
| --- | --- | --- |
| `GET /api/platform/tenants/:tenant_id/technical/manleai-calendar` | Read the full Platform-authorized aggregate | `200 { "manleai_calendar": { ... } }` |
| `PUT /api/platform/tenants/:tenant_id/technical/manleai-calendar/config` | Create or replace the root policy | `200 { "manleai_calendar": { ... }, "replayed": false }` |
| `PUT /api/platform/tenants/:tenant_id/technical/manleai-calendar/hours` | Replace all `local_override` weekly salon periods | uniform mutation response |
| `GET|PUT /api/platform/tenants/:tenant_id/technical/manleai-calendar/staff/:staff_id` | Read or replace one canonical staff member's periods and eligible services | typed aggregate response |
| `GET|PUT /api/platform/tenants/:tenant_id/technical/manleai-calendar/services/:service_id` | Read or replace one service policy, eligible staff, and resource requirements | typed aggregate response |
| `/api/platform/tenants/:tenant_id/technical/manleai-calendar/resources/*` | List/create/update/archive resource pools | uniform mutation response |
| `/api/platform/tenants/:tenant_id/technical/manleai-calendar/exceptions/*` | Create/cancel salon, staff, or resource exceptions | uniform mutation response |
| `POST /api/platform/tenants/:tenant_id/technical/manleai-calendar/activate` | Record Platform-actor activation for the exact resulting config version | uniform mutation response |

Every mutation includes these top-level fields:

```json
{
  "action_key": "stable-owner-action-key",
  "expected_config_version": 4
}
```

Creating the root config requires `expected_config_version: 0`. Every later
write uses the latest returned `config_version`. An exact replay is checked by
salon-unique `action_key` plus the normalized request fingerprint before the
version compare-and-swap and returns:

```json
{
  "manleai_calendar": {},
  "replayed": true
}
```

Reusing the key for different normalized input conflicts. A new action key
with a stale expected version also conflicts. The version is a monotonic
concurrency fence, not a count of user actions: a replace operation may advance
it more than once because V48 fences every scheduling-relevant child change.
Timezone, canonical service eligibility fields, canonical staff eligibility
fields, root policy, local hours, and all scheduling-relevant children can make
an earlier activation stale.

Mutation-specific bodies add these fields to the mutation metadata:

- Config: `slot_step_minutes`, `minimum_booking_notice_minutes`,
  `booking_horizon_days`, nullable `reschedule_cutoff_minutes` and
  `cancellation_cutoff_minutes`, `max_party_size`, and default before/after
  buffers.
- Hours: `periods[{day_of_week,start_minute,end_minute}]`; the range is
  half-open in local wall-clock minutes and `end_minute=1440` explicitly means
  midnight at the end of the day.
- Staff: `weekly_periods[...]` plus `eligible_service_ids`. These eligibility
  links do not require a pre-existing service-policy row, so the Staff-first
  workflow has no ordering dependency.
- Service: `enabled`, nullable `capacity_mode` (`staff_only` or `pooled`),
  nullable before/after buffer overrides, `eligible_staff_ids`, and
  `resource_requirements[{resource_pool_id,units_required}]`.
- Resource create/update: `name` and `capacity`.
- Exception create: `scope_type`, the matching optional `staff_id` or
  `resource_pool_id`, `effect`, `starts_at`, `ends_at`, optional
  `capacity_override`, and optional `reason`. Salon/staff exceptions use
  `available` or `unavailable`; resource exceptions use
  `capacity_override`.
- Resource archive, exception cancel, and activation use only the mutation
  metadata.

The full aggregate contains `salon_id`, `timezone`, selected
`scheduling_authority`, `authority_version`, `config_version`, nullable
`config`, `hours`, `staff_profiles`, `service_policies`, `resources`,
`exceptions`, `readiness`, and server-owned `constraints`. The constraints
object is the API source for numeric bounds and supported capacity/exception
tokens; clients must not duplicate those choices as a second product taxonomy.

Readiness is returned as:

```json
{
  "configuration_ready": true,
  "execution_ready": true,
  "authority_version": 1,
  "config_version": 9,
  "capabilities": {
    "staff_only_availability": true,
    "staff_only_create": true,
    "pooled_capacity": true,
    "party_create": true,
    "reschedule": true,
    "cancel": true
  },
  "blockers": []
}
```

Configuration blocker codes are `CONFIG_REQUIRED`, `LOCAL_HOURS_REQUIRED`,
`ELIGIBLE_SERVICES_REQUIRED`, `SERVICE_POLICY_REQUIRED`,
`ENABLED_SERVICE_REQUIRED`, `SERVICE_INELIGIBLE`,
`SERVICE_CAPACITY_MODE_REQUIRED`, `SERVICE_STAFF_REQUIRED`,
`STAFF_INELIGIBLE`, `STAFF_SCHEDULE_REQUIRED`, `POOLED_RESOURCE_REQUIRED`,
`STAFF_ONLY_RESOURCE_NOT_ALLOWED`, `RESOURCE_ARCHIVED`, and
`RESOURCE_CAPACITY_EXCEEDED`. Execution blocker codes are
`CONFIG_NOT_ACTIVATED` in the Phase 4C runtime. Older lifecycle/pooled/party
unavailable tokens are retained as stable constants but are not emitted for a
ready Phase 4C aggregate.

`configuration_ready` covers only persisted configuration rules. Activation
requires that dimension to pass; it advances the config fence and sets
`activated_version` to the exact result version. Later scheduling-relevant
changes retain the prior audit evidence but make it stale until a new
activation records the current version. Activation is repeatable and never
switches `salon_settings.scheduling_authority`. In Phase 4C,
`staff_only_availability` and `staff_only_create` become true only when
configuration is ready, activation is current, the selected authority is
`manleai_calendar`, and a valid staff-only policy exists. `pooled_capacity`
also requires an enabled pooled policy; `party_create` requires an enabled
service and `max_party_size > 1`. Reschedule and cancel use the same engine
fence and require at least one enabled service. Aggregate `execution_ready` is
true only when all six capability fields are true. Clients must use
`readiness.capabilities` for current new-work action gating; persisted internal
targets remain routed by origin and version after a later selected-authority
change. The legacy
`constraints.execution_engine_available` only means at least one execution
slice exists and is not an operation gate.

All routes use the standard error envelope. Stable errors are:

- `400 MANLEAI_CALENDAR_VALIDATION_ERROR`;
- `404 MANLEAI_CALENDAR_NOT_FOUND`;
- `409 MANLEAI_CALENDAR_CONFIG_REQUIRED`;
- `409 MANLEAI_CALENDAR_CONFIG_VERSION_CONFLICT`;
- `409 MANLEAI_CALENDAR_ACTION_CONFLICT`;
- `409 MANLEAI_CALENDAR_NOT_READY`; and
- `500 MANLEAI_CALENDAR_FAILED`.

### Remaining Planned Authority-Aware Contract

Remaining APIs must complete authority state across every mode without
overloading provider fields:

- complete versioned authority-switch state, rollback state, and cross-
  authority failure/reconciliation classification beyond the implemented
  request, external-provider, and Phase 4C internal lifecycle contracts;
- explicit authority-switch review, dry-run, conflict, and completion state.

The execution rules are fixed: `owner_manual` creates pending owner-review work
and never confirms automatically; Phase 4C `manleai_calendar` confirms
aggregate create or whole-root lifecycle only after an all-or-none internal
commit returns durable root/version/status and exact child evidence;
`external_provider` confirms only after provider success returns the required
booking ID and metadata. Connecting or syncing an integration must not switch
authority implicitly, and historical actions keep their originating authority.

Do not populate internal modes with fake provider IDs, provider versions, POS
errors, or Square readiness keys. Remaining endpoint paths and complete
payloads belong in this file only after the corresponding implementation
exists.

## Canonical Entity Semantics

Services, staff, and customers are ManleAI-owned canonical records. Under
`external_provider`, the active POS provider owns real availability and booking
execution. Under `owner_manual`, eligible services and staff come directly
from active, non-archived, AI-bookable canonical records; services must have a
positive duration, and no POS link is required. API clients should treat
ManleAI `id` values as product identity and provider IDs as mappings used only
behind the external-provider-neutral booking contract. Current
`manleai_calendar` configuration derives eligibility from canonical records
without fake provider mappings; Phase 4B staff-only/pooled aggregate
availability/create and Phase 4C lifecycle execution use that authority-owned
source and persist no fake provider mappings.

Some endpoints still expose legacy Square provider fields alongside canonical
IDs because Square Appointments is the first real POS integration. API
responses include provider link and sync fields such as:

```json
{
  "sync_status": "synced",
  "pos_provider": "square",
  "pos_linked": true,
  "last_synced_at": "2026-06-10T15:00:00Z",
  "sync_error": ""
}
```

Sync status meanings:

- `local_only`: ManleAI record exists without an active-provider mapping.
- `syncing`: a provider sync or projection operation is in progress.
- `synced`: canonical record has a valid active-provider mapping.
- `sync_failed`: the latest provider sync failed and booking must be gated.
- `unmapped`: record needs an owner or sync workflow to link it to the provider.
- `archived`: record is retained for history but unavailable for new booking.

Local-only, unmapped, sync-failed, or archived services/staff may be visible in
management views after the canonical migration, but the current
external-provider runtime must not use them for availability checks or booking
attempts. Current confirmed, rescheduled, or cancelled appointment responses
still require active POS provider success and required provider booking
metadata.

## Auth

`POST /api/auth/login`

```json
{
  "email": "owner@example.com",
  "password": "your-password"
}
```

Returns an access token, its expiry, user, immutable `principal_scope`, roles,
and a primary salon ID only for a Tenant principal. The
refresh token is not present in JSON. It is delivered as the host-only
`manleai_refresh` cookie with `HttpOnly`, `SameSite=Strict`, path `/api/auth`,
the configured refresh lifetime, and `Secure` in production.

Invalid email/password and disabled-account login return the same generic
`401 INVALID_CREDENTIALS` response. For a found disabled account, bcrypt
comparison occurs before the status check, so the response does not reveal
whether its supplied password was correct. The endpoint does not expose whether
an account exists or is disabled.

`GET /api/auth/bootstrap/status`

Unauthenticated endpoint used by a fresh deployment to determine whether first
owner setup is still open.

```json
{
  "available": true
}
```

`POST /api/auth/bootstrap-owner`

Unauthenticated one-time endpoint for creating the first dashboard owner. It is
available only while the `users` table is empty. The backend assigns the
`salon_owner` role and immutable `principal_scope=tenant`; clients do not send
a role or scope.

```json
{
  "email": "owner@example.com",
  "full_name": "Owner Name",
  "password": "minimum-8-characters"
}
```

Returns an access token, its expiry, user, roles, and an empty primary salon ID
until onboarding creates the first salon. The refresh token uses the same
cookie-only contract as login and is omitted from JSON. After any user exists,
this endpoint returns `409 BOOTSTRAP_CLOSED`.

`POST /api/auth/refresh-token`

The request body is empty. The browser supplies `manleai_refresh` as a
credentialed cookie; refresh tokens in JSON are not accepted.

Refresh is an atomic, active-user-only, one-time rotation. The submitted token
is consumed in the same transaction that conditionally creates one hashed
successor. Exact concurrent response-loss replay within the bounded five-second
rotation grace returns that same successor, so parallel browser refreshes do
not create multiple live tokens. Changed-successor reuse and later reuse are
invalid. A disabled user's old token is consumed without creating a successor,
and the API returns the same generic `401 INVALID_REFRESH_TOKEN` used for an
invalid or expired refresh token. The response JSON contains only the access
session fields and replaces the HttpOnly refresh cookie.

`POST /api/auth/logout`

The request body is empty. The server revokes the cookie token and expires the
same host-only `/api/auth` cookie. Dashboard and POS browser clients keep access
tokens only in JavaScript memory, send `credentials: include`, refresh after a
reload through the cookie, and remove the legacy `access_token` and
`refresh_token` local-storage keys without copying their values.

`GET /api/auth/me`

Returns the current user, immutable top-level `principal_scope`, current
database-owned role names from that realm, and a primary active salon only for
a Tenant principal. Platform principals never receive a salon membership in
their session. The signed token's scope, salon, and role claims are not returned
as current authorization state; current PostgreSQL state owns the response.

## SaaS Access-Control Foundation

V64 adds authenticated Platform access-management APIs. Every route below is
owned by the Platform surface and requires a current active `platform_admin`
assignment with `platform.access.manage`. A legacy `super_admin` role, a Tenant
membership, a caller-supplied header, or a JWT role claim does not satisfy that
check. Platform Ops cannot manage access.

`GET /api/platform/access/platform-users?query=<name-or-email>&limit=50`

`POST /api/platform/access/platform-users`

`PUT /api/platform/access/platform-users/:user_id`

Platform Admin can create and edit dedicated Platform identities with
`email`, `full_name`, a direct password, `platform_admin|platform_ops` role,
and `active|revoked` role status. Create requires a password of at least eight
characters; edit accepts an optional replacement password. Passwords are
bcrypt-hashed before persistence, never returned, and password/role changes
revoke refresh sessions. Updates are action-key idempotent and
expected-version fenced; the last-active-Admin guard applies to edits and
revocation.

Returns only bounded active or disabled `platform` principal summaries for the
global role workflow.

`GET /api/platform/access/salons/:salon_id/tenant-users?query=<name-or-email>&limit=50`

Returns only bounded active or disabled `tenant` principal summaries for the
selected salon's team workflow. The salon path is required even though the
directory realm is global, so the endpoint cannot be reused as an ambiguous
cross-surface user directory. User IDs identify mutation targets but never
prove authorization; every mutation reloads current database-owned state.

`GET /api/platform/access/capabilities`

Returns the database-owned capability catalog with `name`, `display_name`,
`scope`, `delegation_scope`, and `requires`. Only capabilities with
`delegation_scope=salon` may be attached to a Platform Ops salon assignment.
`requires` publishes capability dependencies used by both backend validation
and the Platform UI; for example, selecting a write capability includes its
declared read capability.

`GET /api/platform/access/roles`

Returns current `platform_admin` and `platform_ops` assignments, including
status, optimistic version, and a nested `user` summary (`id`, `email`,
`full_name`, `status`, `principal_scope`, and `data_classification`). Membership, salon-assignment,
and PII-grant list and mutation responses below expose the same nested user
summary so existing rows do not depend on a separate bounded user search.

`PUT /api/platform/access/platform-users/:user_id/role`

```json
{
  "action_key": "platform-role-change-uuid",
  "role": "platform_ops",
  "status": "active",
  "expected_version": 0
}
```

The target must be an existing active Platform principal. Tenant principals
return the same not-found outcome as an unavailable target and are also
rejected by database constraints. `expected_version=0` creates the first
assignment; later changes require the exact current version. One user
has at most one Platform role assignment. The last active Platform Admin cannot
be revoked or converted, including by concurrent requests. Any Platform role
or status transition revokes the target user's active salon assignments and PII
grants atomically and records a bounded immutable child event for each affected
object; reactivating the role requires fresh explicit delegation and does not
restore stale access.

`GET /api/platform/access/salons/:salon_id/memberships`

`PUT /api/platform/access/salons/:salon_id/memberships/:user_id`

```json
{
  "action_key": "tenant-membership-change-uuid",
  "role": "tenant_business_manager",
  "status": "active",
  "expected_version": 0
}
```

V64 backfills `salons.owner_user_id` as the exact `tenant_owner` membership.
Platform Admin may revoke or reactivate that membership without changing
`salons.owner_user_id`; the Owner remains visible in Access and ownership
history is preserved. The Owner role cannot be demoted or reassigned through
this API. Additional members use `tenant_business_manager`. Active exact-salon memberships authorize
the fixed Tenant Business routes below; they never authorize Platform,
Technical, Operations, or Audit routes.
Membership targets must be active Tenant principals; Platform principals are
not eligible and are rejected before mutation and by database constraints.

`GET /api/platform/access/salons/:salon_id/assignments`

`PUT /api/platform/access/salons/:salon_id/assignments/:user_id`

```json
{
  "action_key": "ops-salon-assignment-uuid",
  "status": "active",
  "permissions": ["business.read", "business.write"],
  "expected_version": 0
}
```

The target requires an active `platform_ops` role. Permissions are resolved
from the database capability catalog. A write capability requires its matching
read capability. Assignment to one salon grants nothing on another salon.
Platform Admin never needs a salon assignment, support authorization, or PII
grant. Platform Ops needs the exact active salon assignment and base capability;
Services, AI Training, and Calls additionally require the temporary Admin grant
described below.

`GET /api/platform/access/salons/:salon_id/support-requests`

`POST /api/platform/access/salons/:salon_id/support-requests`

```json
{
  "action_key": "support-request-uuid",
  "user_id": "platform-user-uuid",
  "capabilities": ["services.read", "services.write"],
  "pii_scopes": [],
  "reason": "SUPPORT-2048",
  "expires_at": "2026-08-20T12:00:00Z"
}
```

Only Platform Admin can list, create, revoke, or cancel legacy pending rows.
Creation grants access immediately; there is no Tenant Owner approval step.
The target must be an active Platform Ops identity that already holds every
requested capability through its exact-salon assignment. Supported capabilities are
`services.read`, `services.write`, `training.read`, `training.write`,
`calls.read`, `calls.manage`, `calls.simulate`, and `calls.redact`. Dependencies
are expanded and validated by the backend. Non-PII authorization may last at
most 30 days. Any `calls.*` capability requires `pii_scopes:["calls"]`, which
reduces the entire request maximum to 24 hours.

`POST /api/platform/access/salons/:salon_id/support-requests/:request_id/cancel`

`POST /api/platform/access/salons/:salon_id/support-requests/:request_id/revoke`

Decision bodies require `action_key` and exact `expected_version`. Revocation
is immediate. Expiry, role/status
change, or Platform Ops assignment/capability change also removes effective
access and never revives on later reactivation.

`GET /api/platform/tenants/:id/support-access/effective`

Returns only the current Platform caller's effective capabilities, PII scopes,
and earliest temporary expiry for UI gating. Platform Admin receives its
role-derived feature capabilities and direct PII scope without an expiry. It does not
return or confer another actor's access. Authorization is re-evaluated on every
feature request; this response is not an authorization token.

`GET /api/platform/access/salons/:salon_id/pii-grants`

`POST /api/platform/access/salons/:salon_id/pii-grants`

```json
{
  "action_key": "pii-grant-uuid",
  "user_id": "platform-user-uuid",
  "scope": "customers",
  "reason": "support-change-reference",
  "expires_at": "2026-07-26T12:00:00Z"
}
```

This general Ops grant endpoint accepts `customers`, `appointments`, and
`notifications`. Calls grants cannot be created here; Calls PII is created only
as the child of a temporary Admin-granted Calls authorization.
Expiry must be in the future and no more than 24 hours from creation. The
target Platform Ops user must already have the underlying salon capability.
Platform Admin is exempt because its control-plane access is direct.
`reason` is an opaque change reference, not free text. It must match
`[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}`; arbitrary prose or customer data is
rejected.

`POST /api/platform/access/salons/:salon_id/pii-grants/:grant_id/revoke`

```json
{
  "action_key": "pii-revoke-uuid",
  "expected_version": 1
}
```

Revocation is version-fenced and immediately removes eligibility. Expiry and
revocation are checked on every policy evaluation rather than copied into the
JWT.

`GET /api/platform/access/audit?salon_id=<optional>&limit=50&offset=0`

Returns immutable bounded access events. Event details contain stable access
tokens, versions, status, capability names, and expiry evidence, not the
PII-grant change reference or customer/provider payloads.

Every mutation requires a stable `action_key`. Exact reuse with the same
canonical payload returns the stored response and
`X-Idempotent-Replay: true`; changed reuse returns
`409 ACCESS_ACTION_CONFLICT`. Stale optimistic versions return
`409 ACCESS_VERSION_CONFLICT`. Other stable errors are `400 ACCESS_INVALID`,
`403 ACCESS_FORBIDDEN`, `404 ACCESS_RECORD_NOT_FOUND`,
`409 LAST_PLATFORM_ADMIN`, and sanitized `500 ACCESS_OPERATION_FAILED`.

The policy service distinguishes the route-owned Tenant and Platform surfaces.
Tenant access requires an active membership for the exact salon on every
Tenant workspace route. Platform Ops
requires an active role, exact salon assignment, and exact delegated
capability. Platform PII access additionally requires a non-revoked,
non-expired grant for the actor, salon, and PII scope. Services, Training, and
Calls additionally require a current Admin-granted temporary authorization for
Ops. Platform Admin bypasses assignments, temporary authorizations, and PII grants
while still requiring its active role capability. Every
successful Platform support route records the actual actor, salon, capability,
PII scope, HTTP method, and route in immutable access audit before domain work.
Provider, scheduling,
conversation, and compatibility APIs retain their separately documented
authorization boundaries.

## Platform Services, AI Training, And Calls APIs

These V75 routes reuse the same canonical domain services and rich dashboard
components as the Tenant surface. They do not copy records into a Platform
store and do not impersonate the Owner.

- Services: `/api/platform/tenants/:id/services*`,
  `/service-categories*`, `/service-category-aliases*`, and
  `/service-aliases*` require exact `services.read|write`. Service aliases and
  category aliases remain structured children of their canonical service or
  category; they are not free-text knowledge.
- The older compatibility endpoints under
  `/api/platform/tenants/:tenant_id/business/services*` and
  `/business/service-categories*` remain callable for existing clients, but
  now require both their existing Business authorization and the exact
  `services.read|write` capability. Platform Admin satisfies that directly;
  Platform Ops additionally requires its current Admin-granted authorization. Their support-action audit
  must succeed before domain work. The Platform Business UI no longer renders
  the reduced editor; the full shared Services tab is the canonical management
  surface.
- Business-safe external readiness:
  `GET /api/platform/tenants/:tenant_id/services/external-scheduling-readiness`
  requires `services.read` and returns the same bounded provider-neutral
  projection as the Tenant Business readiness endpoint. Provider identifiers,
  configuration, sync logs, webhook state, diagnostics, and test controls stay
  under Platform Technical.
- AI Training: `/api/platform/tenants/:id/knowledge-items*` and
  `/training/evaluate` require exact `training.read|write`. Owner corrections
  additionally require Calls PII for Platform Ops; Platform Admin is direct. Applying an explicit
  correction as a service alias persists the canonical `service_aliases` row;
  it never turns that alias into knowledge text.
- Calls sessions and lifecycle:
  `/api/platform/tenants/:id/conversation-sessions*` and
  `/party-booking-requests*` use `calls.read`, `calls.manage`,
  `calls.simulate`, or `calls.redact` by action, always with Calls PII.
  `/api/platform/tenants/:id/calls/services`, `/staff`, and
  `/owner-corrections` provide the Calls-scoped catalogs/correction action.
  `GET /api/platform/tenants/:id/voice/status` returns a business-safe status
  projection and omits provider URLs, credentials, model/voice settings, and
  diagnostics. Platform Calls services pass the authenticated Platform identity
  through runtime, scheduling, and persistence authorization; they never replace
  it with `salons.owner_user_id`. Platform Admin is authorized directly by its
  role capability. Platform Ops still requires the exact active salon assignment,
  delegated capability, and Calls PII grant.

Calls authorization may select only scheduling evidence durably linked to an
authorized call session. It grants no general Appointments access, no booking,
reschedule, or cancellation API, and no provider write. The simulator remains
inside the existing conversation/scheduling safety boundaries and cannot infer
confirmation from local transcript or knowledge state.

Calls-scoped service and consultation metadata reads are permitted only for the
authorized Calls renderer and require the same request-linked Calls PII grant.
They do not grant `services.read`, expose the Services management endpoints, or
permit any service/category/profile mutation.

The normalized detail route
`GET /api/v2/platform/tenants/:tenant_id/calls/:session_id` returns the session
under `data`, including optional `detail_warnings`. `session_id` is constrained
to a GUID at the router boundary, so static Calls resources such as
`GET /api/v2/platform/tenants/:tenant_id/calls/readiness` cannot be interpreted
as conversation-session identifiers even when conversation routes are
registered before voice routes. Valid legacy JSON values
whose top-level shape no longer matches optional transcript metadata or party
guest details are omitted from that projection and reported through a safe
warning; persisted source data is not rewritten. A real child-read failure is
reported without raw database text as `CONVERSATION_TRANSCRIPT_FAILED`,
`CONVERSATION_HANDOFF_FAILED`, `CONVERSATION_PARTY_REQUEST_FAILED`, or
`CONVERSATION_SCHEDULING_EVIDENCE_FAILED`.

## SaaS Shared Business APIs

V65 exposes one Business contract through two route-owned surfaces. Tenant
routes require an active membership for the exact salon. Platform routes
require Platform Admin or exact-salon Platform Ops `business.read` or
`business.write`. The caller cannot choose a surface or impersonate a salon
owner through a header or request body.

Tenant directory and resource prefix:

- `GET /api/salons/`
- `/api/salons/:id/business`

Platform directory and resource prefix:

- `GET /api/platform/tenants/`
- `/api/platform/tenants/:tenant_id/business`

Both prefixes expose the same canonical resources:

- `GET|PATCH /profile`
- `GET|POST /services`, `PATCH /services/:service_id`, and
  `POST /services/:service_id/archive`
- `GET|POST /service-categories`, `PATCH` and `archive` by category ID
- `GET|POST /staff`, `PATCH` and `archive` by staff ID, and
  `PUT /staff/:staff_id/services`
- `GET|PUT /business-hours`
- `GET|PATCH /public-catalog`
- `GET|POST /customers`, `PATCH` and `archive` by customer ID

Every mutation includes `action_key` and `expected_version`. A create requires
version `0`; an update requires the exact returned resource version. Exact
replay returns the stored resource ID/version and sets
`X-Idempotent-Replay: true`. Reusing an action key with changed input or using
a stale version fails without a partial write. Business actions and events
record the actual Tenant or Platform actor and contain only an allowlisted
resource/version/change-field shape.

Business DTOs exclude provider credentials, provider IDs, diagnostics,
scheduling-authority controls, and technical readiness. Platform staff
responses omit phone and email and Platform writes cannot modify those fields.
Platform customer reads or writes additionally require an active exact-salon
`customers` PII grant, including for Platform Admin. Tenant customer access is
covered by its exact membership.

Provider-linked service, staff, and customer operational fields are read-only.
Owner-controlled service AI description, category, consultation profile, and
AI-bookable controls retain their canonical POS/scheduling validation.
Connected external business hours are imported read-only; local/internal hours
write only `source=local_override`. Business-relevant mutations share the
scheduling advisory fence, and public page publishing reuses the canonical
selected-authority readiness policy.

## SaaS Platform Tenant v2 APIs

The active Platform tenant-detail frontend uses the normalized root
`/api/v2/platform/tenants/:tenant_id`. Platform Admin has direct capability
authority and acts as the actual actor; no Tenant approval is required.
Platform Ops needs an active exact-salon assignment with the matching
delegation, current temporary authorization where declared, and the exact PII
grant for sensitive projections. Tenant memberships do not satisfy Platform
routes, and the tenant ID comes only from the fixed route.

Canonical workflow resources include:

- collection/context: `GET /api/v2/platform/tenants` and `GET /context`
- Business: `/business/profile`, `/business/hours`, `/business/public-page`,
  `/staff`, `/customers`, `/services`, and `/service-categories`
- AI Receptionist: `/calls/*`, `/knowledge`, `/corrections`, `/evaluations`,
  and `GET|PUT /ai-receptionist/runtime`
- Integrations: `/integrations`, `/integrations/:provider`, Square connection,
  sync, activation and safety verification, Twilio voice-routing verification,
  and OpenAI runtime verification
- Scheduling: `POST /scheduling/authority/readiness`,
  `PUT /scheduling/authority`, authority history, and
  `/scheduling/internal-calendar/*`
- Operations: `/operations/overview`, `/operations/runtime-limits`,
  `/operations/provider-events/square`, and `/operations/owner-notifications`
- governance/history: `/access`, `/audit-events`, and
  `/configuration-transfers/*`

Successful v2 responses use `{ "data": ..., "meta": ... }`. The exact route
and response rules are normative in `docs/platform-tenant-api-v2.md`.

### Legacy Platform compatibility routes

The routes below remain mounted for compatibility only. The active Platform
tenant-detail frontend does not call them. They delegate to the same domain
services and persistence owners as v2 and must not introduce dual writes.

- `GET|PUT /api/platform/tenants/:tenant_id/technical/integration-configs[/square|/twilio|/openai]`
- `GET /api/platform/tenants/:tenant_id/technical/square/status`
- `GET /api/platform/tenants/:tenant_id/technical/square/connect-url`
- `GET /api/platform/tenants/:tenant_id/technical/square/locations`
- `POST /api/platform/tenants/:tenant_id/technical/square/select-location`
- `POST /api/platform/tenants/:tenant_id/technical/square/sync`
- `POST /api/platform/tenants/:tenant_id/technical/square/scheduling-capability/re-evaluate`
- `POST /api/platform/tenants/:tenant_id/technical/square/active-provider/activate`
- `POST /api/platform/tenants/:tenant_id/technical/square/ai-booking/enable|disable`
- `/api/platform/tenants/:tenant_id/technical/manleai-calendar/*`
- `/api/platform/tenants/:tenant_id/technical/scheduling-authority-switches/*`

The compatibility Square AI routes delegate to the same persisted runtime
state, but canonical management is authority-neutral under
`/api/v2/platform/tenants/:tenant_id/ai-receptionist/runtime`.
AI runtime enable/disable requires `action_key` and
`expected_version`. Exact replay returns the same versioned state; changed-key
reuse and stale versions fail without changing runtime state. The persisted
technical action/event records contain the actual Platform actor, never an
impersonated salon owner.

Tenant-safe POS Calendar readiness is separate:
`GET /api/salons/:id/business/external-scheduling-readiness`. It returns only
the selected authority, aggregate service/staff/hour counts, external-new-work
readiness, and the bounded booking-write blocker boolean. It omits connection,
merchant/location, token, scope, sync-log, diagnostic, and test-booking fields.

Platform Operations endpoints are rooted at
`/api/platform/tenants/:tenant_id/operations`. They cover status, masked owner
and customer notification recovery, Square webhook operations, and
`/runtime` tenant quota/usage controls. Platform Ops needs exact delegated
`operations.read` or `operations.write`. V72 additionally requires an active,
unexpired exact PII grant for any underlying calls, appointments, customers,
or notifications data; Platform Admin is not exempt.

## Tenant Registration And Provisioning

`POST /api/public/tenant-registration-requests` is unauthenticated and accepts
the bilingual marketing form. It requires a caller-generated UUID
`submission_key`, `tenant-registration-contact-v1` consent, a supported locale
and source page, normalized US contact data, and bounded free text. A durable
insert returns `202 Accepted` with `request_reference`; exact retry returns the
same reference and `X-Idempotent-Replay: true`, while changed payload reuse
returns `409 TENANT_REGISTRATION_SUBMISSION_CONFLICT`. The honeypot path returns
the same generic receipt shape without persistence. The public write has its
own 10/hour, burst-3 distributed rate-limit class. No raw IP, provider
credential, or booking/provider configuration is persisted.

Platform review routes require an authenticated Platform identity:

- `GET /api/platform/registration-requests` — masked paginated queue, counts,
  search, status, assignee, and received-date filters.
- `GET /api/platform/registration-requests/:request_id` — authorized full
  detail, immutable timeline, immutable internal notes, and allowed transitions.
- `PATCH /api/platform/registration-requests/:request_id` — version-fenced,
  action-key-idempotent assignment, status transition, and validated
  provisioning-draft update. Ops may prepare this draft; it is not a Tenant
  creation action and retention redacts its PII.
- `POST /api/platform/registration-requests/:request_id/notes` — immutable
  versioned internal note.

Platform Admin and Ops receive `platform.registration_requests.read|manage`.
Only Platform Admin receives `platform.tenants.provision` and may call:

- `GET /api/platform/tenant-identities?query=...` — bounded search of active
  Tenant identities for explicit selection.
- `POST /api/platform/registration-requests/:request_id/provision` — one atomic
  transaction creates or selects the verified Owner identity, salon,
  membership/settings/hours, converts the request, and records safe audit and
  replay evidence. The new salon always starts with `owner_manual` scheduling
  authority. Intake booking-system text never creates `salon_integration_configs`,
  selects `active_pos_provider`, or enables AI booking.
- `POST /api/platform/registration-requests/:request_id/owner-invitation` —
  creates or explicitly rotates a single-use 72-hour invitation. Only the
  SHA-256 token hash is stored; the raw token appears in the first successful
  response only and is unavailable on replay.

`POST /api/auth/owner-invitations/accept` is public, rate-limited as an auth
attempt, and requires the raw token plus a 12–128 character password. It
atomically activates the invited Tenant identity, consumes the invitation, and
revokes existing refresh tokens. Responses are non-enumerating.

Terminal `converted`, `declined`, and `spam` requests receive a 180-day
retention deadline. The registered worker redacts contact, salon/location,
operational free text, and internal-note PII after expiry while preserving safe
status/version/timestamp/source/plan/audit evidence. Legal approval of the
retention period and contact-consent wording remains an external release gate;
the repository implements the stated policy but does not claim legal approval.

## Public Catalog

`GET /api/public/salons/:slug`

Public unauthenticated endpoint for the customer-facing `landing/` app. Returns
only salons whose owner has enabled the public catalog and whose current
selected scheduling authority still passes public-page readiness. A stale
published flag fails closed after an authority/configuration/provider change.
The response is public-safe: no provider identifiers, POS IDs, provider tokens,
owner IDs, staff phone/email, sync errors, or raw provider payloads are returned.
Public database context cannot select any tenant base-table row; the repository
consumes only the V71 authority-aware `read_public_catalog` JSON projection.
The former `GET /api/public/salon` first-tenant endpoint is not registered.

Eligibility follows the persisted authority: `owner_manual` needs at least one
active canonical AI-bookable service and does not require staff or POS;
`manleai_calendar` uses enabled canonical services, current activation, and
`local_override` hours; `external_provider` uses the current active connection
and synced/linked provider projection. This endpoint does not check a requested
time, create a booking attempt, or confirm appointments. Every public CTA is a
phone request, not web booking.

```json
{
  "salon": {
    "slug": "lotus-nails-studio",
    "name": "Lotus Nails Studio",
    "phone": "+16292536211",
    "address": "1200 W Sample Ave",
    "city": "Chicago",
    "state": "IL",
    "zip_code": "60601",
    "timezone": "America/Chicago",
    "primary_language": "en",
    "secondary_language": "vi"
  },
  "scheduling_authority": "owner_manual",
  "scheduling_authority_version": 3,
  "services": [
    {
      "name": "Classic Manicure",
      "description": "Trim, shape, cuticle care, and polish.",
      "ai_description": "Classic manicure",
      "duration_minutes": 45,
      "price_from": 35,
      "price_display": "$35.00"
    }
  ],
  "staff": [],
  "hours": [
    {
      "day_of_week": 1,
      "start_local_time": "09:30:00",
      "end_local_time": "19:00:00",
      "source": "local_migrated"
    }
  ],
  "booking_note": "Call the salon to request an appointment. Availability and confirmation are provided by the salon."
}
```

## Salons

`GET /api/salons`

Returns salons owned by the authenticated user.

`POST /api/salons`

```json
{
  "name": "Lotus Nails Studio",
  "phone": "+16292536211",
  "address": "1200 W Sample Ave",
  "city": "Chicago",
  "state": "IL",
  "zip_code": "60601",
  "timezone": "America/Chicago",
  "primary_language": "en",
  "secondary_language": "vi",
  "handoff_phone": "+13125550102"
}
```

`GET /api/salons/:id/settings`

`PUT /api/salons/:id/settings`

The GET response includes `scheduling_authority`. Phase 2 uses that owner-scoped
persisted value for current-authority readiness and genuinely new availability
or scheduling actions. Historical operations resolve origin across external
`booking_attempts` and owner-manual `scheduling_requests`; retries, target-aware
availability, reschedule, and cancellation use their persisted origin instead.
The current settings PUT request intentionally does not accept or switch the
field; including an unknown JSON field does not create an authority workflow.
Existing/backfilled rows default to `external_provider`. If an internal token
is selected outside this API, `owner_manual` uses the request-only executor and
`manleai_calendar` fails with `SCHEDULING_AUTHORITY_NOT_READY`; neither falls
through to Square. Exact external replay and external-target cleanup remain
origin-routed.

Returns and updates the other owner-scoped AI receptionist settings. `ai_tone` accepts
`professional_warm`, `natural_human`, `friendly_young`, or `concise_calm`.
The tone controls spoken reply style only; booking guardrails and scheduling-
authority confirmation rules still override style. External confirmation uses
the Square/provider evidence gate; owner-manual wording is always pending owner
review and explicitly not confirmation.
`consultation_enabled` is the salon-wide
runtime gate for AI service consultation. It does not make any service eligible by
itself; eligibility still requires an active-provider link, AI booking eligibility,
and a complete `ready` service consultation profile with at least one recommended
outcome and one compatible current system. The setting defaults to `false`, and an
enable request is rejected until at least one service meets that full eligibility
contract.

```json
{
  "scheduling_authority": "external_provider",
  "ai_greeting": "Thanks for calling Lotus Nails Studio.",
  "ai_voice": "professional_female",
  "ai_tone": "natural_human",
  "booking_mode": "pending_approval",
  "recording_enabled": true,
  "recording_consent_message": "This call may be recorded.",
  "sms_confirmation_enabled": true,
  "sms_reminder_enabled": true,
  "reminder_hours_before": 24,
  "handoff_enabled": true,
  "consultation_enabled": true
}
```

`GET /api/salons/:id/public-catalog`

Returns owner-scoped public page settings and publish readiness.

```json
{
  "salon_id": "...",
  "public_slug": "lotus-nails-studio",
  "public_catalog_enabled": true,
  "public_path": "/s/lotus-nails-studio",
  "scheduling_authority": "owner_manual",
  "scheduling_authority_version": 3,
  "eligible_service_count": 3,
  "eligible_staff_count": 0,
  "published_hours_count": 5,
  "bookable_service_count": 3,
  "bookable_staff_count": 0,
  "can_publish": true,
  "readiness_label": "Owner-managed appointment requests",
  "readiness_blockers": [],
  "updated_at": "2026-06-25T14:30:00Z"
}
```

`PUT /api/salons/:id/public-catalog`

```json
{
  "public_slug": "lotus-nails-studio",
  "public_catalog_enabled": true,
  "expected_scheduling_authority_version": 3
}
```

The update acquires the same salon advisory fence as authority switches,
revalidates readiness inside the transaction, and optionally compares the
authority version supplied by the dashboard. Version drift returns
`SCHEDULING_AUTHORITY_CHANGED`; current readiness failure returns
`PUBLIC_CATALOG_NOT_READY`. Disabling remains allowed so owners can take a page
offline. Readiness follows the authority-specific rules described above; staff
and POS are not universal publishing prerequisites.

`GET /api/salons/:id/business-hours`

Returns Square-style business hour periods currently imported for the salon.
Owner-facing editing happens in Square Appointments; the local dashboard reads
these periods and can trigger Square sync.

```json
{
  "periods": [
    {
      "day_of_week": 1,
      "start_local_time": "09:30:00",
      "end_local_time": "13:00:00",
      "source": "imported",
      "provider": "square",
      "provider_location_id": "L123",
      "provider_period_index": 0,
      "last_synced_at": "2026-06-25T14:30:00Z"
    },
    {
      "day_of_week": 1,
      "start_local_time": "14:00:00",
      "end_local_time": "19:00:00",
      "source": "imported",
      "provider": "square",
      "provider_location_id": "L123",
      "provider_period_index": 1,
      "last_synced_at": "2026-06-25T14:30:00Z"
    }
  ]
}
```

`PUT /api/salons/:id/business-hours`

Returns `409 BUSINESS_HOURS_POS_MANAGED`. Business hours are managed in Square
and imported through
`POST /api/platform/tenants/:tenant_id/technical/square/sync`.

## Configuration Transfer

The mounted SaaS surface is Platform-only and target-scoped:

- `GET /api/platform/tenants/:tenant_id/configuration-transfer/export?sections=<comma-separated>`
- `POST /api/platform/tenants/:tenant_id/configuration-transfer/preview`
- `POST /api/platform/tenants/:tenant_id/configuration-transfer/apply`
- `GET /api/platform/tenants/:tenant_id/configuration-transfer/runs?limit=25`

The destination is always `:tenant_id`; clients do not send a second target
selector. A tenant source requires `source_type=tenant` and
`source_tenant_id`. A file source requires `source_type=json_upload` and a
`configuration` bundle. Both require an explicit `included_sections` list.
Platform v10 export supports `local_business_hours`; v9 input remains accepted
and is normalized to v10, while v8 JSON remains accepted without that section.
A v7 JSON file is accepted only when it declares an
explicit content-only scope limited to `service_categories`,
`service_aliases`, `service_consultation_profiles`, and `knowledge_base`.
The server canonicalizes an accepted v7 pack to v8 before fingerprinting,
preview, run audit, and apply, and returns the
`legacy_v7_content_pack_adapted_to_v8` warning. Full/runtime/provider v7
bundles and v1-v6 bundles remain unsupported on Platform routes.
JSON export/upload is capped at 3 MB; larger same-platform sources use direct
tenant-to-tenant transfer instead of materializing a file.
For `integrations`, v10 and compatibility-v9 bundles use
`integration_providers`; only providers with
a salon-scoped persisted source record are listed and eligible to apply (the
field may be omitted when none exists).
Missing source records are no-ops, and legacy environment fallback is never
exported as tenant configuration. Compatibility v8 uploads treat their three
provider blocks as explicit file input.

```json
{
  "source_type": "tenant",
  "source_tenant_id": "source-salon-id",
  "included_sections": [
    "salon_profile",
    "ai_receptionist",
    "public_booking_page",
    "local_business_hours",
    "service_categories",
    "service_aliases",
    "service_consultation_profiles",
    "knowledge_base"
  ]
}
```

Preview is write-free for canonical salon configuration, but creates a safe
review run and immutable preview event. The run stores source/target identity,
schema, section list, source fingerprint, Business/Technical version fences,
scheduling-authority version, safe summary/warnings/conflicts, and the actual
Platform actor. It never stores the raw bundle or secret values.
If the selected changes would invalidate internal activation or external
provider readiness while the same transfer requests an enabled public page,
preview returns a conflict. Apply configuration first, complete the existing
readiness workflow, then publish explicitly.

Apply repeats the exact request and adds the preview/action identity:

```json
{
  "preview_id": "reviewed-run-id",
  "action_key": "platform-transfer-client-stable-key",
  "source_type": "tenant",
  "source_tenant_id": "source-salon-id",
  "included_sections": ["salon_profile", "knowledge_base"]
}
```

Every selected section maps to its existing capability owner. Salon profile,
public page, and local hours require `business.read|write`; categories,
aliases, and consultation profiles require `services.read|write`; knowledge
requires `training.read|write`; AI receptionist and provider non-secret
settings require `technical.read|write`. Tenant-to-tenant preview/apply checks
the selected read capabilities on the source and target; apply checks the
selected write capabilities on the target. Recent runs require `audit.read`.
Allowed support actions are recorded before domain work.

Apply acquires the scheduling fence, locks the reviewed run and every selected
source/target version fence, rechecks the source fingerprint and destination
authority/version, then updates canonical rows and Business/Technical ledgers
in one transaction. Any stale source, target, or authority returns
`409 CONFIGURATION_TRANSFER_STALE` with zero destination domain writes. Exact
action replay returns the original applied run; changed reuse returns
`409 CONFIGURATION_TRANSFER_ACTION_CONFLICT`.

`active_pos_provider` is report-only and is never imported. Scheduling
authority and switch history, provider connections/tokens, credentials,
services, staff, customers, appointments, scheduling requests, calls,
recordings, provider-imported hours, and operational history are always
excluded. Integration transfer changes only portable non-secret settings and
preserves destination secrets. Local hours transfer only
`source=local_override`; provider-managed source or destination hours produce
a preview conflict.

### Retained legacy compatibility code (not mounted after SaaS cutover)

The module still contains the earlier owner/onboarding v8-v1 import logic for
code and data compatibility, but `cmd/api` does not register the following
Tenant/onboarding routes. They are not part of the current HTTP surface.

`GET /api/salons/:id/configuration-export`

Returns a sanitized, owner-scoped JSON snapshot for moving setup information to
another system or importing into another salon. This transfer bundle includes
configuration only:

- Salon profile
- AI receptionist settings
- Public booking page settings
- Integration runtime settings without secrets
- Service category taxonomy and service category aliases
- Service aliases that can be matched to existing target services
- Service consultation profiles with portable target-service references
- AI Training knowledge base

Schema v8 is the retained legacy export contract. Secret-bearing integrations return only configuration values and secret status
metadata such as `client_secret_configured`, `auth_token_configured`,
`api_key_configured`, and `*_source`. It does not export services, staff,
customers, appointments and child/resource rows, booking attempts/segments,
availability quotes/slots, fallback requests, scheduling authority/version and
switch history, scheduling requests/events/notification outbox rows, ManleAI
Calendar configuration/execution evidence, call sessions/transcripts/recordings,
POS entity links/sync jobs/logs/errors, provider switch state, synced business
hour periods, party booking requests, voice webhook/audio records, POS OAuth
tokens, API keys, client secrets, encrypted secrets, or POS connection state.
Scheduling authority is operational state and is never exported, imported, or
implicitly changed. `active_pos_provider` is retained only as portable adapter
intent; it is reported explicitly during preview and is never authority consent.
Schema v8 retains the portable `service_consultation_profiles` and
`included_sections` contract introduced by v7. A full export lists every supported configuration section;
a curated pack may list only taxonomy, service aliases, and consultation
profiles so it cannot overwrite salon, provider, or AI runtime configuration.
Import remains backward compatible with v1-v7 bundles. Bundles from v1-v5 do
not contain the consultation toggle and therefore default it to disabled;
schema v6 and later preserve the explicit value subject to profile readiness.

```json
{
  "schema_version": "manleai.salon_configuration.v8",
  "exported_at": "2026-06-26T15:00:00Z",
  "secrets_exported": false,
  "operational_data_exported": false,
  "included_sections": ["salon_profile", "ai_receptionist", "public_booking_page", "integrations", "service_categories", "service_aliases", "service_consultation_profiles", "knowledge_base"],
  "excluded_data": ["services", "staff", "customers", "appointments", "booking_attempts", "availability_quotes", "scheduling_authority", "scheduling_authority_version", "scheduling_authority_switch_runs", "scheduling_requests", "manleai_calendar_configs", "manleai_calendar_execution_events", "owner_notifications", "call_sessions", "pos_entity_links", "pos_sync_jobs", "pos_errors", "provider_switch_runs", "salon_business_hour_periods", "pos_oauth_tokens", "api_keys", "client_secrets", "encrypted_secrets"],
  "requires_secret_reentry": ["square", "twilio", "openai"],
  "salon_profile": {
    "name": "Lotus Nails Studio",
    "phone": "+16292536211",
    "address": "1200 W Sample Ave",
    "city": "Chicago",
    "state": "IL",
    "zip_code": "60601",
    "timezone": "America/Chicago",
    "primary_language": "en",
    "secondary_language": "vi",
    "handoff_phone": "+13125550102",
    "ai_enabled": true,
    "active_pos_provider": "square",
    "updated_at": "2026-06-25T14:30:00Z"
  },
  "ai_receptionist": {
    "ai_greeting": "Thanks for calling Lotus Nails Studio.",
    "ai_voice": "professional_female",
    "ai_tone": "natural_human",
    "booking_mode": "confirmed_booking",
    "recording_enabled": true,
    "recording_consent_message": "This call may be recorded.",
    "sms_confirmation_enabled": true,
    "sms_reminder_enabled": true,
    "reminder_hours_before": 24,
    "handoff_enabled": true,
    "consultation_enabled": true,
    "updated_at": "2026-06-25T14:30:00Z"
  },
  "public_booking_page": {
    "public_slug": "lotus-nails-studio",
    "public_catalog_enabled": true,
    "public_path": "/s/lotus-nails-studio",
    "updated_at": "2026-06-25T14:30:00Z"
  },
  "integrations": {
    "square": {
      "provider": "square",
      "configured": true,
      "environment": "sandbox",
      "client_id": "square-app-id",
      "redirect_url": "https://api.example.com/api/integrations/square/callback",
      "api_version": "2026-05-20",
      "client_secret_configured": true,
      "client_secret_source": "database"
    },
    "twilio": {
      "provider": "twilio",
      "configured": true,
      "voice_routing_enabled": false,
      "voice_inbound_number": "",
      "voice_routing_configured": false,
      "voice_routing_blockers": [],
      "public_base_url": "",
      "incoming_path": "",
      "turn_path": "",
      "recording_path": "",
      "stream_path": "",
      "voice_transport": "realtime_stream",
      "inbound_webhook_url": "",
      "turn_webhook_url": "",
      "recording_webhook_url": "",
      "stream_webhook_url": "",
      "auth_token_configured": true,
      "auth_token_source": "database"
    },
    "openai": {
      "provider": "openai",
      "enabled": true,
      "configured": true,
      "base_url": "https://api.openai.com/v1",
      "transcription_model": "gpt-4o-mini-transcribe",
      "reply_model": "gpt-4.1-mini",
      "speech_model": "tts-1",
      "speech_voice": "alloy",
      "speech_output_mode": "streaming_tts",
      "realtime_enabled": true,
      "realtime_model": "gpt-realtime-2",
      "realtime_voice": "alloy",
      "realtime_noise_profile": "automatic",
      "realtime_instructions": "",
      "api_key_configured": true,
      "api_key_source": "database"
    }
  },
  "service_categories": {
    "count": 1,
    "items": [
      {
        "source_key": "service_category:manicure",
        "name": "Manicure",
        "slug": "manicure",
        "description": "Hand nail services.",
        "status": "active",
        "source": "manual",
        "sort_order": 10,
        "aliases": [
          {
            "source_key": "service_category_alias:mani",
            "alias": "mani",
            "normalized_alias": "mani",
            "source": "owner",
            "status": "active",
            "confidence": 0.94,
            "created_at": "2026-06-25T14:30:00Z",
            "updated_at": "2026-06-25T14:30:00Z"
          }
        ],
        "created_at": "2026-06-25T14:30:00Z",
        "updated_at": "2026-06-25T14:30:00Z"
      }
    ]
  },
  "service_aliases": {
    "count": 1,
    "items": [
      {
        "source_key": "service_alias:overlay",
        "alias": "overlay",
        "normalized_alias": "overlay",
        "target_service": {
          "name": "Builder Gel",
          "duration_minutes": 75,
          "price_display": "$70.00"
        },
        "source": "owner",
        "status": "active",
        "confidence": 0.94,
        "created_at": "2026-06-25T14:30:00Z",
        "updated_at": "2026-06-25T14:30:00Z"
      }
    ]
  },
  "service_consultation_profiles": {
    "count": 1,
    "items": [
      {
        "source_key": "service_consultation_profile:builder gel|75",
        "target_service": {
          "name": "Builder Gel",
          "duration_minutes": 75,
          "price_display": "$70.00"
        },
        "status": "ready",
        "recommended_outcomes": ["maintain", "add_strength"],
        "compatible_current_systems": ["natural", "gel"],
        "length_capabilities": ["keep", "shorten"],
        "priority_tags": ["durability", "lower_maintenance"],
        "finish_options": ["gel_polish", "glossy"],
        "maintenance_note": "Return for professional maintenance as growth becomes visible.",
        "owner_approved_summary": "A structured gel service for clients who want strength and long wear."
      }
    ]
  },
  "knowledge_base": {
    "count": 1,
    "items": [
      {
        "source_key": "knowledge:3d9df47b6f8f4b97a7fcaf3e8d0f14be",
        "title": "Deposit policy",
        "category": "policy",
        "body": "Deposits are required for groups of four or more.",
        "status": "active",
        "source": "owner",
        "created_at": "2026-06-25T14:30:00Z",
        "updated_at": "2026-06-25T14:30:00Z"
      }
    ]
  }
}
```

`POST /api/salons/:id/configuration-import/preview`

Validates a transfer bundle and returns a dry-run summary. Preview does not
write to the database.

Request shape, with a full exported bundle or scoped v8/v7 pack in
`configuration`:

```json
{
  "configuration": {
    "schema_version": "manleai.salon_configuration.v8",
    "...": "full export or scoped configuration pack"
  }
}
```

Response:

```json
{
  "request_id": "import-preview-id",
  "dry_run": true,
  "status": "previewed",
  "schema_version": "manleai.salon_configuration.v8",
  "included_sections": ["salon_profile", "ai_receptionist", "public_booking_page", "integrations", "service_categories", "service_aliases", "service_consultation_profiles", "knowledge_base"],
  "target_scheduling_authority": "owner_manual",
  "target_scheduling_authority_version": 4,
  "source_active_pos_provider": "square",
  "target_active_pos_provider": "square",
  "result_active_pos_provider": "square",
  "source_booking_mode": "confirmed_booking",
  "target_booking_mode": "pending_approval",
  "result_booking_mode": "pending_approval",
  "can_apply": true,
  "summary": [
    {"section": "salon_profile", "created": 0, "updated": 6, "unchanged": 5, "skipped": 0, "conflicts": 0},
    {"section": "service_categories", "created": 2, "updated": 1, "unchanged": 4, "skipped": 0, "conflicts": 0},
    {"section": "service_aliases", "created": 3, "updated": 1, "unchanged": 4, "skipped": 2, "conflicts": 0},
    {"section": "service_consultation_profiles", "created": 7, "updated": 0, "unchanged": 0, "skipped": 0, "conflicts": 0},
    {"section": "knowledge_base", "created": 4, "updated": 2, "unchanged": 8, "skipped": 0, "conflicts": 0}
  ],
  "warnings": [
    {"section": "ai_receptionist", "code": "confirmed_booking_incompatible_with_owner_manual", "message": "Confirmed booking mode was not imported because Owner confirmation is request-only. The final imported booking mode is pending approval.", "field": "booking_mode"},
    {"section": "integrations", "code": "secret_reentry_required", "message": "square secret values are not included in the export. Re-enter secrets or reconnect this provider after import.", "field": "square"}
  ],
  "conflicts": [],
  "excluded_data": ["services", "staff", "customers", "appointments", "booking_attempts", "availability_quotes", "scheduling_authority", "scheduling_authority_version", "scheduling_authority_switch_runs", "scheduling_requests", "manleai_calendar_configs", "manleai_calendar_execution_events", "owner_notifications", "call_sessions", "pos_entity_links", "pos_sync_jobs", "pos_errors", "provider_switch_runs", "salon_business_hour_periods", "pos_oauth_tokens", "api_keys", "client_secrets", "encrypted_secrets"],
  "requires_secret_reentry": ["square", "twilio", "openai"]
}
```

`POST /api/salons/:id/configuration-import`

Applies a previously previewed transfer bundle. The request should include a
client-generated `request_id`. The backend stores an import run with
`payload_fingerprint`; reusing the same `request_id` with the same payload is
idempotent, while reusing it with a different payload returns a conflict.

```json
{
  "request_id": "2c8a6d65-b7ac-4e26-8d11-c2b37d2b8908",
  "configuration": {
    "schema_version": "manleai.salon_configuration.v8",
    "...": "full export or scoped configuration pack"
  }
}
```

Import idempotency:

- Salon profile, AI receptionist, public booking page, and integration settings update the existing salon-scoped rows.
- Integration secrets are preserved if already present and are never imported from the transfer file.
- Service categories upsert by stable slug; category aliases upsert by normalized alias and conflict with active service aliases.
- Service aliases upsert by normalized alias only when their target service resolves to exactly one existing target-salon service by name and duration; missing targets are skipped with warnings, and active category-alias conflicts block apply.
- Consultation profiles resolve by normalized service name plus duration and upsert by `(salon_id, service_id)`. Missing or ambiguous targets block apply. A `ready` profile requires a canonical active AI-bookable service with duration for `owner_manual` and `manleai_calendar`; `external_provider` additionally requires active-provider link, sync, and version evidence. The transfer never creates services or POS mappings.
- Identical consultation profile imports remain `unchanged`; they do not increment profile revision.
- `included_sections` limits both preview and apply. Scoped consultation packs can leave salon profile, integrations, AI runtime settings, and knowledge untouched.
- Knowledge base entries upsert by `source_key`, backed by a unique `(salon_id, import_key)` index.
- Re-importing the same file reports `unchanged` or `updated`; it does not create duplicate knowledge rows.
- `ai_enabled` is portable intent and has no universal Square prerequisite. `booking_mode=confirmed_booking` is converted to `pending_approval` with a warning when the destination authority is `owner_manual`; confirming modes still rely on their own runtime readiness after import.
- Preview and apply both report `source_booking_mode`, `target_booking_mode`, and `result_booking_mode`. The owner-manual conversion emits `confirmed_booking_incompatible_with_owner_manual`; exact request replay returns the same final mode and warning with the original import-run identity.
- Public publishing reuses the destination salon's public-catalog readiness source of truth, including its selected-authority service, staff, hours, activation, and external-connection blockers as applicable; configuration transfer does not maintain a second Square-first readiness rule.
- Apply acquires the shared scheduling-authority advisory fence and rechecks the exact previewed `target_scheduling_authority` and version inside the transaction. A concurrent switch returns `target_scheduling_authority_changed` and writes nothing.
- Preview reports source, destination, and result `active_pos_provider`. A different adapter while the destination uses `external_provider` returns `active_provider_change_requires_provider_switch`; import never changes the confirming executor.

`POST /api/onboarding/configuration-import/preview`

Validates a transfer bundle before the owner has created a salon. This endpoint
is auth-only and does not require `salon_id`. It returns the same dry-run shape
as salon-scoped import preview, but `can_apply=false` with an
`owner_salon_exists` conflict when the owner already has a salon.

`POST /api/onboarding/configuration-import`

Creates the owner's first salon from a transfer bundle and applies supported
configuration in one transaction. The request should include a client-generated
`request_id`; reusing the same `request_id` and same payload is idempotent and
returns the same `salon_id` and `import_run_id`.

```json
{
  "request_id": "2c8a6d65-b7ac-4e26-8d11-c2b37d2b8908",
  "configuration": {
    "schema_version": "manleai.salon_configuration.v8",
    "...": "full exported configuration bundle"
  }
}
```

Response includes the created salon id:

```json
{
  "import_run_id": "...",
  "salon_id": "...",
  "request_id": "2c8a6d65-b7ac-4e26-8d11-c2b37d2b8908",
  "dry_run": false,
  "status": "applied",
  "schema_version": "manleai.salon_configuration.v8",
  "included_sections": ["salon_profile", "ai_receptionist", "public_booking_page", "integrations", "service_categories", "service_aliases", "service_consultation_profiles", "knowledge_base"],
  "target_scheduling_authority": "owner_manual",
  "target_scheduling_authority_version": 1,
  "source_active_pos_provider": "square",
  "target_active_pos_provider": "square",
  "result_active_pos_provider": "square",
  "source_booking_mode": "confirmed_booking",
  "target_booking_mode": "pending_approval",
  "result_booking_mode": "pending_approval",
  "can_apply": true,
  "summary": [],
  "warnings": [
    {"section": "ai_receptionist", "code": "confirmed_booking_incompatible_with_owner_manual", "message": "Confirmed booking mode was not imported because Owner confirmation is request-only. The final imported booking mode is pending approval.", "field": "booking_mode"}
  ],
  "conflicts": [],
  "excluded_data": ["services", "staff", "customers", "appointments", "booking_attempts", "availability_quotes", "scheduling_authority", "scheduling_authority_version", "scheduling_authority_switch_runs", "scheduling_requests", "manleai_calendar_configs", "manleai_calendar_execution_events", "owner_notifications", "call_sessions", "pos_entity_links", "pos_sync_jobs", "pos_errors", "provider_switch_runs", "salon_business_hour_periods", "pos_oauth_tokens", "api_keys", "client_secrets", "encrypted_secrets"],
  "requires_secret_reentry": ["square", "twilio", "openai"]
}
```

Onboarding import requires a full bundle; scoped v8/v7 data packs are imported from
Settings after the salon exists. Onboarding always creates the destination with
`owner_manual` authority and never imports scheduling authority/version,
services, staff,
customers, appointments, POS tokens, API keys, client secrets, or encrypted
secrets. It can import service category taxonomy and aliases because those are
understanding configuration, not service records or provider state. Service
aliases and consultation profiles are deferred because target services do not
exist yet, and consultation remains disabled until a later Settings import
resolves the profile pack against configured destination services. Incoming
`ai_enabled=true` remains portable and may apply immediately. Incoming
`confirmed_booking` is explicitly reported and converted to `pending_approval`;
public publishing remains off until the new owner-manual salon has canonical
AI-bookable services. None of these steps require a Square connection.

## Integration Configuration

Provider credentials and runtime settings are salon-scoped and Platform-
Technical-scoped. Tenant memberships alone cannot read or write them.
Secret values are write-only: responses expose only safe configured/source
metadata. `salon_integration_configs` is the sole runtime source for Square,
Twilio, and OpenAI. An exact missing row, repository failure, malformed settings,
disabled row, missing required credential, or decryption failure stops runtime
resolution and never inherits process configuration.

Square status includes `initial_activation`: the current active-provider value
and version, exact config/connection fence versions, readiness checks, and
`can_activate`. The activation request requires `action_key`,
`expected_version`, `expected_integration_config_version`, and
`expected_connection_capability_version`. It is allowed only when the current
provider is explicitly blank and current tenant-bound Square config,
merchant/location, and successful-sync evidence still match. Exact replay
returns the same result; stale evidence or an already-selected provider returns
sanitized `409`. OAuth, location selection, and sync do not activate a provider.

OpenAI additionally requires the server-owned `openai_public` destination, an
exact tenant/config identity, a positive credential revision, and the purpose-
separated credential HMAC identity written with the encrypted key. The API
never returns that HMAC. `configured=true`/`runtime_resolvable=true` means only
that the saved tenant configuration passes the shared local runtime validator;
it is not proof of a successful provider request.

Missing provider rows return database-owned unconfigured state. For a stored
row whose secret is empty or unreadable, source is `none`, the matching
configured flag is false, and no process credential is reported as active.
Invalid persisted settings
make this authenticated read fail safely instead of returning an empty settings
map.

The complete serialized response omits every write-only credential and control
field, including Square client/webhook secrets, Twilio Auth Token, full
Account/Messaging Service/sender/destination values, OpenAI API keys, and their
`clear_*` request controls. Configured/source booleans, OpenAI's monotonic
credential revision and uniqueness-established boolean, the masked Account SID
hint, and the explicitly masked owner SMS destination are the only bounded
secret-state evidence returned. The OpenAI credential HMAC is never returned.

`POST /api/platform/tenants/:tenant_id/technical/openai/runtime-verification`

Requires `technical.write`, `action_key`, and `expected_config_version`. The
request performs local tenant/config preflight and queues a durable version-
fenced verification run; it does not call OpenAI in the HTTP request. Exact
action replay returns the same run, changed action-key reuse conflicts, and a
stale expected version returns `409`.

`GET /api/platform/tenants/:tenant_id/technical/openai/runtime-verification`

Requires `technical.read` and returns the latest durable run plus each required
capability (`transcription`, both semantic contracts, reply, speech, streaming
speech, and enabled Realtime). This GET never calls OpenAI. Evidence becomes
`stale` whenever the integration config version, credential revision,
destination-policy version, or verification contract no longer matches.
`live_verified` is presentation state, not a provider field: it is true only
when the latest run is `succeeded`, `fresh=true`, and every required capability
is `verified`. Provider acceptance does not prove conversation quality.

`GET /api/platform/tenants/:tenant_id/technical/integration-configs`

```json
{
  "square": {
    "provider": "square",
    "configured": true,
    "environment": "sandbox",
    "client_id": "square-app-id",
    "redirect_url": "https://api.example.com/api/integrations/square/callback",
    "api_version": "2026-05-20",
    "client_secret_configured": true,
    "client_secret_source": "database",
    "webhook_notification_url": "https://api.example.com/api/integrations/square/webhook",
    "webhook_configured": true,
    "webhook_signature_key_configured": true,
    "webhook_signature_key_source": "database"
  },
  "twilio": {
    "provider": "twilio",
    "configured": true,
    "voice_route_id": "3f17f690-7de4-4b26-91b8-2763ca15489d",
    "voice_routing_enabled": true,
    "voice_inbound_number": "+13125550123",
    "voice_routing_configured": true,
    "voice_routing_blockers": [],
    "account_sid_hint": "AC••••cdef",
    "public_base_url": "https://api.example.com",
    "incoming_path": "/api/voice/twilio/3f17f690-7de4-4b26-91b8-2763ca15489d/incoming",
    "turn_path": "/api/voice/twilio/3f17f690-7de4-4b26-91b8-2763ca15489d/turn",
    "recording_path": "/api/voice/twilio/3f17f690-7de4-4b26-91b8-2763ca15489d/recording",
    "stream_path": "/api/voice/twilio/3f17f690-7de4-4b26-91b8-2763ca15489d/stream",
    "voice_transport": "realtime_stream",
    "inbound_webhook_url": "https://api.example.com/api/voice/twilio/3f17f690-7de4-4b26-91b8-2763ca15489d/incoming",
    "turn_webhook_url": "https://api.example.com/api/voice/twilio/3f17f690-7de4-4b26-91b8-2763ca15489d/turn",
    "recording_webhook_url": "https://api.example.com/api/voice/twilio/3f17f690-7de4-4b26-91b8-2763ca15489d/recording",
    "stream_webhook_url": "wss://api.example.com/api/voice/twilio/3f17f690-7de4-4b26-91b8-2763ca15489d/stream",
    "auth_token_configured": true,
    "auth_token_source": "database",
    "owner_sms_enabled": true,
    "owner_sms_destination_masked": "+1******0199",
    "owner_sms_consent_attested": true,
    "owner_sms_consent_attested_at": "2026-07-24T08:00:00Z",
    "account_sid_configured": true,
    "messaging_service_configured": true,
    "sender_configured": false,
    "notification_status_path": "/api/notifications/twilio/status",
    "notification_inbound_path": "/api/notifications/twilio/inbound",
    "notification_status_url": "https://api.example.com/api/notifications/twilio/status",
    "notification_inbound_url": "https://api.example.com/api/notifications/twilio/inbound/salon-uuid"
  },
  "openai": {
    "provider": "openai",
    "enabled": true,
    "configured": true,
    "runtime_resolvable": true,
    "runtime_blockers": [],
    "base_url": "https://api.openai.com/v1",
    "destination_profile": "openai_public",
    "destination_managed": true,
    "transcription_model": "gpt-4o-mini-transcribe",
    "reply_model": "gpt-4.1-mini",
    "speech_model": "tts-1",
    "speech_voice": "alloy",
    "speech_output_mode": "streaming_tts",
    "realtime_enabled": true,
    "realtime_model": "gpt-realtime-2",
    "realtime_voice": "alloy",
    "realtime_noise_profile": "automatic",
    "realtime_instructions": "",
    "api_key_configured": true,
    "api_key_source": "database",
    "credential_revision": 2,
    "credential_unique": true
  }
}
```

`PUT /api/platform/tenants/:tenant_id/technical/integration-configs/square`

```json
{
  "environment": "sandbox",
  "client_id": "square-app-id",
  "client_secret": "new-secret-or-empty-to-keep-existing",
  "clear_client_secret": false,
  "redirect_url": "https://api.example.com/api/integrations/square/callback",
  "api_version": "2026-05-20",
  "api_base_url": "",
  "webhook_notification_url": "https://api.example.com/api/integrations/square/webhook",
  "webhook_signature_key": "new-signature-key-or-empty-to-keep-existing",
  "clear_webhook_signature_key": false
}
```

`webhook_configured=true` means only that an HTTPS notification URL and an
encrypted signature key are stored for inbound verification. It does not prove
that a Square subscription exists or that recent deliveries succeeded. Secret
encryption failures abort the update and preserve previously stored secrets.
Configuration transfer preserves the target salon's deployment URL and never
exports a webhook signature key; that key must be re-entered at the target.

`PUT /api/platform/tenants/:tenant_id/technical/integration-configs/twilio`

```json
{
  "public_base_url": "https://api.example.com",
  "voice_inbound_number": "+13125550123",
  "voice_routing_enabled": true,
  "auth_token": "new-token-or-empty-to-keep-existing",
  "clear_auth_token": false,
  "incoming_path": "/api/voice/twilio/incoming",
  "turn_path": "/api/voice/twilio/turn",
  "recording_path": "/api/voice/twilio/recording",
  "stream_path": "/api/voice/twilio/stream",
  "voice_transport": "realtime_stream",
  "owner_sms_enabled": true,
  "owner_sms_destination": "+13125550199",
  "clear_owner_sms_destination": false,
  "owner_sms_consent_attested": true,
  "account_sid": "AC0123456789abcdef0123456789abcdef",
  "clear_account_sid": false,
  "messaging_service_sid": "MG0123456789abcdef0123456789abcdef",
  "clear_messaging_service_sid": false,
  "sender_phone": "",
  "clear_sender_phone": false,
  "notification_status_path": "/api/notifications/twilio/status",
  "notification_inbound_path": "/api/notifications/twilio/inbound"
}
```

The four legacy path fields remain accepted during the expand-release rollback
window, but the tenant-bound URLs returned by the API are computed from the
immutable Twilio integration-row UUID and cannot be edited. Enabling Voice
routing requires a canonical E.164 inbound number, valid Account SID, stored
Auth Token, and host-only public HTTPS base. A database partial unique index
prevents the same active inbound number from being enabled for two tenants.
Configuration transfer excludes route ID, inbound number, public callback
base, routing enablement, computed URLs, credentials, and live verification;
only `voice_transport` is portable.

`GET /api/platform/tenants/:tenant_id/technical/voice-routing-status`

Requires Platform `technical.read` for the exact tenant and returns separate
configuration and live-call evidence:

```json
{
  "routing_configured": true,
  "live_verified": true,
  "last_verified_inbound_at": "2026-07-30T08:15:00Z",
  "last_observed_inbound_at": "2026-07-30T08:15:00Z",
  "verification_stale": false,
  "blockers": []
}
```

`live_verified=true` means an incoming tenant-bound webhook was accepted with
the current route fingerprint. It is not inferred from saved configuration.

Owner operational SMS is an additive Twilio use case. Enabling it requires an
explicit E.164 owner destination and consent attestation for that exact
destination, encrypted Account SID/Auth Token, either Messaging Service SID or
sender phone, and public HTTPS callback URLs. Changing the destination requires
fresh consent attestation. Blank write-only secret/SID fields preserve their
stored values unless the matching `clear_*` flag is true. Responses expose only
the masked destination, configured booleans, and callback URLs. The owner-SMS
runtime resolver uses only the salon's database record and never takes the
legacy environment fallback. Voice Twilio/OpenAI resolution is also
database-only; only the exact-missing Square compatibility bootstrap remains.

`PUT /api/platform/tenants/:tenant_id/technical/integration-configs/openai`

```json
{
  "enabled": true,
  "api_key": "new-key-or-empty-to-keep-existing",
  "clear_api_key": false,
  "base_url": "https://api.openai.com/v1",
  "transcription_model": "gpt-4o-mini-transcribe",
  "reply_model": "gpt-4.1-mini",
  "speech_model": "tts-1",
  "speech_voice": "alloy",
  "speech_output_mode": "streaming_tts",
  "realtime_enabled": true,
  "realtime_model": "gpt-realtime-2",
  "realtime_voice": "alloy",
  "realtime_noise_profile": "automatic",
  "realtime_instructions": ""
}
```

`base_url` is retained only for request compatibility and must be blank or the
canonical value shown above. The server always stores and returns the managed
`openai_public` destination; tenant-controlled hosts are rejected. A blank API
key preserves an existing key only inside that same server-owned destination
identity. Any key write or clear advances the credential revision and makes
prior verification evidence stale.

`realtime_noise_profile` is the compatibility field name for the location-neutral
background-noise handling policy. Canonical values are `automatic` (default),
`standard`, `strong_noise_rejection`, and `minimal_processing`. Automatic mode
starts each call with standard transcript admission and switches only that call
to stronger admission after structured confidence/VAD evidence of degraded
audio. Missing provider confidence metadata fails closed but does not count as
noise evidence. Legacy imports are canonicalized without losing their prior
behavior: `noisy_salon` becomes `strong_noise_rejection`, `balanced` becomes
`standard`, and `quiet_room` becomes `minimal_processing`.

`GET /api/salons/:id/services`

Returns ManleAI-owned services for dashboard tables, including Square-imported
services and local-only services. A service without a valid active-provider link
is manageable by the owner but not eligible for availability or booking.

```json
{
  "services": [
    {
      "id": "...",
      "salon_id": "...",
      "pos_provider": "square",
      "pos_service_id": "SQUARE_SERVICE_VARIATION_ID",
      "pos_service_version": 123,
      "name": "Classic Manicure",
      "description": "Trim, shape, cuticle care, and polish.",
      "ai_description": "Classic manicure",
      "duration_minutes": 45,
      "price_from": 35,
      "price_display": "$35.00",
      "ai_bookable": true,
      "active": true,
      "sync_status": "synced",
      "last_synced_at": "2026-06-10T15:00:00Z",
      "sync_error": "",
      "source": "imported",
      "pos_linked": true,
      "service_category_id": "category-manicure-id",
      "category_name": "Manicure",
      "category_slug": "manicure",
      "category_source": "manual",
      "category_confidence": 1,
      "category_reviewed_at": "2026-06-25T14:30:00Z",
      "field_authority": {
        "operational_source": "provider",
        "provider": "square",
        "provider_label": "Square Appointments",
        "operational_write_mode": "provider_read_only"
      },
      "consultation_profile": {
        "status": "ready",
        "recommended_outcomes": ["maintain", "color_refresh"],
        "compatible_current_systems": ["natural", "regular_polish"],
        "length_capabilities": ["keep", "shorten"],
        "priority_tags": ["shorter_visit", "lower_cost"],
        "finish_options": ["regular_polish", "glossy"],
        "maintenance_note": "Owner-approved upkeep guidance.",
        "owner_approved_summary": "Classic nail care with regular polish.",
        "revision": 3
      }
    }
  ]
}
```

`field_authority` is the backend-owned field-level write contract. It is not
the salon scheduling authority. Clients must
not infer editability from `source`, `sync_status`, or `pos_linked`. Current
Square-imported services return `provider_read_only`; local-only services return
`operational_source=manleai` and `operational_write_mode=local`.

`POST /api/salons/:id/services`

Creates a local ManleAI service. The API always creates local services with
`ai_bookable=false`, `sync_status=local_only`, and `pos_linked=false`. Under
`owner_manual` or `manleai_calendar`, an owner may later enable an active,
non-archived local service with a positive duration as canonical AI-bookable
eligibility without adding a POS link. Under `external_provider`, availability
and booking eligibility still require the active-provider sync and link evidence
described by the AI-bookable PATCH contract below.
If the active provider later declares service write support, the mutation can
queue a `pos_sync_jobs` outbox job and move through `syncing`, `synced`, or
`sync_failed`. Square Appointments service writes are not enabled in the current
slice.

`consultation_profile` is the optional owner-managed child record for this
service. Its controlled arrays accept outcomes (`maintain`, `shorten`,
`add_length`, `add_strength`, `repair`, `removal`, `color_refresh`), current
systems (`natural`, `regular_polish`, `gel`, `dip`, `acrylic`, `extension`),
length capabilities (`keep`, `shorten`, `add_length`), priorities
(`durability`, `lower_maintenance`, `lower_cost`, `shorter_visit`), and finishes
(`natural`, `regular_polish`, `gel_polish`, `glossy`, `matte`, `nail_art`). A
`ready` profile requires at least one recommended outcome and at least one
compatible current system. Optional length, priority, and finish fields refine
ranking but cannot publish an otherwise incomplete profile. Summary and
maintenance text are trimmed and limited to 320 Unicode characters. Identical
retry payloads are no-ops; a changed profile increments `revision`.

Only `ready` profiles attached to active-provider, POS-linked, AI-bookable
services participate in consultation ranking. `ai_description`, then
`description`, remains a display-summary fallback for legacy records; neither
field can make a draft or disabled profile recommendation-eligible. Health or
medical suitability questions are handed to the owner.

Active consultation control state lives in `call_sessions.dialog_state`, with
transcript metadata retained only as audit evidence. Appointment cancel,
reschedule, human handoff, and active party-plan actions take precedence over
consultation. Selecting a service during consultation does not start booking
until the caller also gives explicit booking intent.

```json
{
  "name": "Gel Removal",
  "description": "Removal service for existing gel polish.",
  "ai_description": "Removes existing gel polish before a new service.",
  "duration_minutes": 20,
  "price_from": 10,
  "active": true,
  "service_category_id": "category-removal-id",
  "consultation_profile": {
    "status": "draft",
    "recommended_outcomes": ["removal"],
    "compatible_current_systems": ["gel"],
    "length_capabilities": [],
    "priority_tags": [],
    "finish_options": [],
    "maintenance_note": "",
    "owner_approved_summary": "Removes existing gel polish before a new service."
  }
}
```

Returns `201`:

```json
{
  "service": {
    "id": "...",
    "name": "Gel Removal",
    "duration_minutes": 20,
    "price_from": 10,
    "ai_bookable": false,
    "active": true,
    "sync_status": "local_only",
    "source": "local",
    "pos_linked": false,
    "service_category_id": "category-removal-id",
    "category_source": "manual",
    "consultation_profile": {
      "status": "draft",
      "recommended_outcomes": ["removal"],
      "compatible_current_systems": ["gel"],
      "length_capabilities": [],
      "priority_tags": [],
      "finish_options": [],
      "owner_approved_summary": "Removes existing gel polish before a new service.",
      "revision": 1
    }
  }
}
```

`PUT /api/salons/:id/services/:service_id`

Updates a non-archived service's operational fields only when
`field_authority.operational_write_mode` is `local` or `provider_sync`. A
`provider_read_only` service returns `409 PROVIDER_MANAGED_FIELDS`; the API does
not create a local override that would be overwritten by the next provider
import. If `active=false` is saved on an editable record, `ai_bookable` is also
disabled.

```json
{
  "name": "Classic Manicure",
  "description": "Trim, shape, cuticle care, and polish.",
  "ai_description": "Includes nail shaping, cuticle care, and regular polish.",
  "duration_minutes": 45,
  "price_from": 35,
  "active": true,
  "service_category_id": "category-manicure-id",
  "consultation_profile": {
    "status": "ready",
    "recommended_outcomes": ["maintain", "color_refresh"],
    "compatible_current_systems": ["natural", "regular_polish"],
    "length_capabilities": ["keep", "shorten"],
    "priority_tags": ["shorter_visit"],
    "finish_options": ["regular_polish"],
    "maintenance_note": "Owner-approved upkeep guidance.",
    "owner_approved_summary": "Includes nail shaping, cuticle care, and regular polish."
  }
}
```

`PATCH /api/salons/:id/services/:service_id/owner-controls`

Atomically updates ManleAI-owned service enrichment without mutating
provider-managed operational fields. Repeating the same consultation profile is
idempotent and does not create another profile row or increment its revision.

```json
{
  "ai_description": "Owner-approved comparison guidance.",
  "service_category_id": "category-manicure-id",
  "consultation_profile": {
    "status": "ready",
    "recommended_outcomes": ["maintain"],
    "compatible_current_systems": ["natural"],
    "length_capabilities": ["keep"],
    "priority_tags": [],
    "finish_options": ["regular_polish"],
    "maintenance_note": "Owner-approved upkeep guidance.",
    "owner_approved_summary": "Owner-approved comparison guidance."
  }
}
```

`POST /api/salons/:id/services/:service_id/archive`

Soft-archives a service by setting `active=false`, `ai_bookable=false`,
`sync_status=archived`, and `archived_at`. Archived services remain visible for
history but cannot be used for availability or booking.

`PATCH /api/salons/:id/services/:service_id/ai-bookable`

```json
{
  "ai_bookable": false
}
```

Updates only the canonical AI booking eligibility flag for a service. The
owner-scoped mutation locks the shared booking/calendar reconciliation fence,
the current scheduling authority/version, the active POS provider, and the
service in one transaction. Every authority requires the service to be active,
non-archived, and have a positive duration. Under `owner_manual` or
`manleai_calendar`, an eligible local canonical service may be enabled without
POS evidence. Under `external_provider`, the service must also belong to the
active provider, be `synced`, and have a synced active-provider
`pos_entity_links` row with a nonblank provider ID and a positive provider
version from `COALESCE(link.provider_version, services.pos_service_version, 0)`.
This rule is provider-neutral; it does not branch on Square. Disabling remains owner-scoped, allowed, and
idempotent even when the service is inactive, archived, or no longer has valid
provider evidence.

Changing `ai_bookable` invalidates any current ManleAI Calendar activation via
the V48 configuration-version trigger. This endpoint does not append a separate
actor-level eligibility audit event; that audit surface remains future work.

`PATCH /api/salons/:id/services/:service_id/category`

Assigns or clears a service category. Passing a category ID marks the assignment
as owner-reviewed `manual` with confidence `1`; passing an empty
`service_category_id` clears the assignment and returns it to `unassigned`.

```json
{
  "service_category_id": "category-manicure-id"
}
```

`GET /api/salons/:id/service-categories`

Returns owner-scoped service categories with service counts and aliases. These
groups help the AI clarify service requests but are not directly bookable.

```json
{
  "service_categories": [
    {
      "id": "category-manicure-id",
      "salon_id": "...",
      "name": "Manicure",
      "slug": "manicure",
      "description": "Hand nail services.",
      "status": "active",
      "sort_order": 10,
      "source": "manual",
      "service_count": 4,
      "aliases": [
        {
          "id": "category-alias-id",
          "category_id": "category-manicure-id",
          "category_name": "Manicure",
          "alias": "mani",
          "normalized_alias": "mani",
          "source": "owner",
          "status": "active",
          "confidence": 0.94
        }
      ]
    }
  ]
}
```

`POST /api/salons/:id/service-categories`

Creates an owner-managed category. Slugs are derived from names and are unique
per salon.

```json
{
  "name": "Waxing",
  "description": "Waxing services.",
  "sort_order": 80
}
```

`PUT /api/salons/:id/service-categories/:category_id`

Updates category name, description, and sort order.

`POST /api/salons/:id/service-categories/:category_id/archive`

Archives a category, clears matching service assignments back to `unassigned`,
and archives its category aliases.

`POST /api/salons/:id/service-categories/:category_id/restore`

Restores an archived category.

`POST /api/salons/:id/service-categories/:category_id/aliases`

Creates or updates a category alias by `(salon_id, normalized_alias)`. The
backend rejects aliases that conflict with active service aliases.

```json
{
  "alias": "mani",
  "confidence": 0.94
}
```

`POST /api/salons/:id/service-category-aliases/:alias_id/archive`

Archives one category alias.

`POST /api/salons/:id/service-categories/suggestions/refresh`

Idempotently materializes the active `en-US` database-owned nail taxonomy
release into salon system categories and aliases. Exact normalized local
service names may receive a taxonomy category suggestion and service aliases
only when the taxonomy concept resolves to one unique local service target.
Unknown or ambiguous local services remain unassigned; system aliases that
conflict with owner/imported aliases are skipped. The refresh never creates a
service or POS entity link and never overrides manual/imported categories or
owner/imported aliases. Taxonomy releases and their category/concept/alias
children are versioned database data seeded by migration V44, not Go phrase
maps or API request constants.

```json
{
  "refresh": {
    "created_categories": 0,
    "restored_system_categories": 0,
    "created_aliases": 0,
    "updated_system_aliases": 0,
    "skipped_alias_conflicts": 0,
    "created_service_aliases": 12,
    "updated_system_service_aliases": 0,
    "skipped_service_alias_conflicts": 1,
    "suggested_services": 3,
    "skipped_reviewed_services": 5,
    "skipped_ambiguous_services": 0,
    "unmatched_unreviewed_services": 1
  }
}
```

`GET /api/salons/:id/staff`

Returns ManleAI-owned staff records for dashboard tables, including
Square-imported staff and local-only staff. Under `owner_manual` or
`manleai_calendar`, an active, non-archived local staff member may be enabled as
canonical AI-bookable eligibility without a POS link. Under `external_provider`,
availability and booking eligibility require valid active-provider sync and link
evidence.

```json
{
  "staff": [
    {
      "id": "...",
      "salon_id": "...",
      "pos_provider": "square",
      "pos_staff_id": "SQUARE_TEAM_MEMBER_ID",
      "name": "Mai Nguyen",
      "phone": "+13125550101",
      "email": "mai@example.com",
      "ai_bookable": true,
      "active": true,
      "sync_status": "synced",
      "last_synced_at": "2026-06-10T15:00:00Z",
      "sync_error": "",
      "source": "imported",
      "pos_linked": true,
      "field_authority": {
        "operational_source": "provider",
        "provider": "square",
        "provider_label": "Square Appointments",
        "operational_write_mode": "provider_read_only"
      }
    }
  ]
}
```

`POST /api/salons/:id/staff`

Creates a local ManleAI staff member. Local-only staff are visible in the Staff
dashboard but cannot be used for availability or booking until an active POS
provider link exists. The API always creates local staff with
`ai_bookable=false`, `sync_status=local_only`, and `pos_linked=false`.
If the active provider later declares staff write support, the mutation can
queue a `pos_sync_jobs` outbox job and move through `syncing`, `synced`, or
`sync_failed`. Square Appointments staff writes are not enabled in the current
slice.

```json
{
  "name": "Linh Tran",
  "phone": "+13125550102",
  "email": "linh@example.com",
  "active": true
}
```

Returns `201`:

```json
{
  "staff_member": {
    "id": "...",
    "name": "Linh Tran",
    "phone": "+13125550102",
    "email": "linh@example.com",
    "ai_bookable": false,
    "active": true,
    "sync_status": "local_only",
    "source": "local",
    "pos_linked": false
  }
}
```

`PUT /api/salons/:id/staff/:staff_id`

Updates a non-archived staff member's operational fields only when
`field_authority.operational_write_mode` is `local` or `provider_sync`. A
`provider_read_only` staff member returns `409 PROVIDER_MANAGED_FIELDS`; current
Square Appointments records must be edited in Square and imported through sync.
If `active=false` is saved on an editable record, `ai_bookable` is also disabled.

```json
{
  "name": "Mai Nguyen",
  "phone": "+13125550101",
  "email": "mai@example.com",
  "active": true
}
```

`POST /api/salons/:id/staff/:staff_id/archive`

Soft-archives a staff member by setting `active=false`, `ai_bookable=false`,
`sync_status=archived`, and `archived_at`. Archived staff remain visible for
history but cannot be used for availability or booking.

`PATCH /api/salons/:id/staff/:staff_id/ai-bookable`

```json
{
  "ai_bookable": true
}
```

Updates only the canonical AI booking eligibility flag for a staff member. The
owner-scoped mutation uses the same shared transaction fence as the service
toggle. Every authority requires active, non-archived staff. Under
`owner_manual` or `manleai_calendar`, an eligible local canonical staff member
may be enabled without POS evidence. Under `external_provider`, the staff row
must also belong to the active provider, be `synced`, and have a synced
active-provider `pos_entity_links` row with a nonblank provider ID. Disabling
remains owner-scoped, allowed, and idempotent even when the staff member is
inactive, archived, or no longer has valid provider evidence.

Changing `ai_bookable` invalidates any current ManleAI Calendar activation via
the V48 configuration-version trigger. This endpoint does not append a separate
actor-level eligibility audit event; that audit surface remains future work.

`POST /api/salons/:id/availability`

This is the legacy external-provider compatibility route. The handler enters
the scheduling facade before executing availability, but its response shape is
the existing verified-slot contract. Authority-neutral clients, including
`owner_manual`, use `/scheduling-availability` so `request_only` can be
represented explicitly. A genuinely new availability request without
`target_appointment_id` or `retry_of_attempt_id` resolves
`salon_settings.scheduling_authority`; a
reschedule availability request with `target_appointment_id` resolves the
target appointment's persisted originating authority instead of the salon's
current selection. A request with `retry_of_attempt_id` resolves the persisted
retry attempt's origin before current-authority fallback. The two origin fields
are mutually exclusive. The ready `external_provider` executor returns
provider-neutral available booking slots
from the active POS provider
for one or more AI-bookable services and optional AI-bookable staff members. Use
`segments` for multi-service booking; the legacy `service_id` and `staff_id`
fields remain supported for single-service booking. These IDs are ManleAI
canonical IDs that must resolve through a valid active-provider link before the
POS adapter is called.
The backend resolves all requested services and staff against
`salons.active_pos_provider`. The provider request covers the requested
salon-local calendar day, including daylight-saving boundaries; API clients do
not send a provider timezone or construct UTC day ranges.
`staff_selection_mode` is either `specific` or `anyone`; `anyone` means the
customer did not request a named technician. Results are filtered to synced
business hour periods in the salon timezone. A slot must fit inside one imported
period; split-day periods are not collapsed. This endpoint does not create a
booking attempt or confirm an appointment.

```json
{
  "service_id": "...",
  "staff_id": "...",
  "staff_selection_mode": "specific",
  "segments": [
    {
      "service_id": "...",
      "staff_id": "...",
      "staff_selection_mode": "specific"
    }
  ],
  "preferred_date": "2026-06-10",
  "target_appointment_id": "optional-reschedule-target-uuid",
  "limit": 5
}
```

For a backend-eligible safe retry of an external-provider book fallback, omit
`target_appointment_id` and send the exact non-superseded fallback attempt:

```json
{
  "retry_of_attempt_id": "safe-external-fallback-attempt-uuid",
  "service_id": "...",
  "staff_id": "...",
  "staff_selection_mode": "specific",
  "segments": [
    {
      "service_id": "...",
      "staff_id": "...",
      "staff_selection_mode": "specific"
    }
  ],
  "preferred_date": "2026-06-10",
  "limit": 5
}
```

This retry path is available from persisted `external_provider` origin even if
the salon later selects another authority. It is not a general bypass: the
attempt must still be `fallback_pending`, `retry_policy=safe`, unsuperseded,
owner-scoped, and an exact match for the original date, ordered service/staff
intent, provider/location fence, and requested time. The backend performs a
fresh provider availability lookup and, only for the exact original slot,
creates a new V53 `retry_origin` quote bound to `retry_of_attempt_id`. The prior
quote is never reused. Unsafe, changed, cross-tenant, target-plus-retry, or
non-book retry availability is rejected before provider dispatch.

Returns:

```json
{
  "scheduling_authority": "external_provider",
  "authority_provider": "square",
  "quote_id": "availability-quote-uuid",
  "request_fingerprint": "sha256-hex",
  "expires_at": "2026-06-10T14:05:00Z",
  "service_id": "...",
  "service_name": "Classic Manicure",
  "staff_id": "...",
  "staff_name": "Mai Nguyen",
  "staff_selection_mode": "specific",
  "segments": [
    {
      "service_id": "...",
      "service_name": "Classic Manicure",
      "staff_id": "...",
      "staff_name": "Mai Nguyen",
      "staff_selection_mode": "specific",
      "duration_minutes": 45
    }
  ],
  "preferred_date": "2026-06-10",
  "duration_minutes": 45,
  "timezone": "America/Chicago",
  "slots": [
    {
      "fingerprint": "sha256-hex",
      "start_time": "2026-06-10T15:00:00Z",
      "end_time": "2026-06-10T15:45:00Z",
      "staff_id": "...",
      "staff_name": "Mai Nguyen",
      "staff_selection_mode": "specific",
      "segments": [
        {
          "service_id": "...",
          "service_name": "Classic Manicure",
          "staff_id": "...",
          "staff_name": "Mai Nguyen",
          "staff_selection_mode": "specific",
          "duration_minutes": 45
        }
      ]
    }
  ]
}
```

When at least one usable slot is returned, the backend persists a short-lived,
single-use availability quote and returns `quote_id`, `request_fingerprint`,
`expires_at`, and one `fingerprint` per slot. These fields are intentionally
omitted when `slots` is empty. Owner-facing HTTP create, reschedule, and Square
test-booking writes must submit the quote ID and selected slot fingerprint;
missing evidence returns `409 AVAILABILITY_QUOTE_REQUIRED`, while expired,
consumed, provider-snapshot-stale, or payload-mismatched evidence returns
`409 AVAILABILITY_QUOTE_STALE`.

An availability quote is not a reservation. For capability-ready external
create/reschedule, the first dispatch-capable transaction consumes the quote
and installs the concrete staff/resource claim. A concurrent overlap returns
`409 SLOT_COMMIT_CONFLICT` before any provider/customer write. Active claims
are removed from later availability results; callers must re-query instead of
reusing the losing slot.

For safe retry availability, the returned quote is likewise fresh and
single-use. The later booking retry must submit both that quote and the same
`retry_of_attempt_id`; a normal current-authority quote or an old quote cannot
authorize the persisted retry lineage.

Conversation callers do not accept client-supplied HTTP quote fields. They
retain the backend quote proof attached to the selected offered slot and
re-query provider availability immediately before create or reschedule. The
runtime replaces the proof only when the refreshed slot has the same start,
end, and ordered service/staff assignment. A changed or missing slot is
re-offered and no POS write is dispatched. External party booking is currently
fail-closed before any child write because the installed provider cannot prove
one whole-party atomic operation. Internal `manleai_calendar` party booking
retains its aggregate all-or-none transaction.

Availability quotes are ephemeral authorization evidence, not the booking
audit source of truth. The worker deletes unconsumed quotes only after expiry
plus a 24-hour grace period and deletes orphaned consumed quotes only after a
30-day audit window. Cleanup runs every five minutes and drains at most eight
lock-skipping batches of 250 quotes per run; it never deletes a quote while any
`booking_attempts.availability_quote_id` or
`consumed_by_attempt_id` audit reference remains. Quote deletion cascades to
its slot children only; the booking-attempt ledger is not modified.

`GET /api/salons/:id/customers`

Returns ManleAI-owned customer records plus owner-scoped activity from internal
appointments, booking attempts, call sessions, and handoff requests. This is not
a full Square customer directory sync. A row with no `id` is activity-only and
can be converted into a local customer record by the owner. POS customer links
are optional mappings used when the active provider requires a customer ID for
booking. The optional `limit` query parameter defaults to 50 and is capped at
100. The optional `offset` query parameter defaults to 0. Responses include
`customers`, `summary`, `limit`, `offset`, and `has_more`; `has_more` is
computed by requesting one extra row and does not require an exact total count.
`summary` describes the full owner-scoped customer/activity set, not only the
current page.

```json
{
  "customers": [
    {
      "id": "...",
      "salon_id": "...",
      "key": "phone:+13125550101",
      "name": "Linh Tran",
      "phone": "+13125550101",
      "email": "linh@example.com",
      "notes": "Prefers quiet appointments.",
      "active": true,
      "sync_status": "synced",
      "last_synced_at": "2026-06-10T15:00:00Z",
      "sync_error": "",
      "source": "local",
      "pos_linked": true,
      "last_activity_at": "2026-06-10T15:00:00Z",
      "last_activity_source": "appointment",
      "last_outcome": "confirmed",
      "confirmed_appointments": 1,
      "pending_requests": 0,
      "call_count": 2,
      "handoff_count": 0,
      "appointment_ids": ["..."],
      "booking_attempt_ids": ["..."],
      "call_session_ids": ["..."]
    }
  ],
  "summary": {
    "total_known_customers": 1,
    "active_customers": 1,
    "pos_linked_customers": 1,
    "confirmed_appointments": 1,
    "pending_requests": 0,
    "customers_with_calls": 1,
    "last_customer_activity_at": "2026-06-10T15:00:00Z"
  },
  "limit": 10,
  "offset": 0,
  "has_more": false
}
```

`POST /api/salons/:id/customers`

Creates a local ManleAI customer. The request must include `name` and at least
one contact field. Active, non-archived customers are deduped by normalized
phone and normalized email per salon.

```json
{
  "name": "Linh Tran",
  "phone": "+13125550101",
  "email": "linh@example.com",
  "notes": "Prefers quiet appointments.",
  "active": true
}
```

Returns `201` with `{ "customer": { ... } }`. Duplicate active phone or email
returns `409`.

`PUT /api/salons/:id/customers/:customer_id`

Updates a non-archived local customer record. This does not edit Square
Appointments records or expose provider payloads.

`POST /api/salons/:id/customers/:customer_id/archive`

Soft-archives a customer by setting `active=false`, `sync_status=archived`, and
`archived_at`. Existing activity remains visible in customer history.

`GET /api/salons/:id/customers/search?phone=<phone>&provider=square`

Searches the active provider by phone through `POSProvider.SearchCustomerByPhone` and returns only the normalized provider-neutral customer DTO. It does not create customers, sync the Square directory, or expose Square payloads/tokens.
Provider failures return bounded `POS_CUSTOMER_LOOKUP_FAILED` copy and never
include the provider response or wrapped error text.

```json
{
  "found": true,
  "provider": "square",
  "customer": {
    "pos_customer_id": "CUSTOMER_ID",
    "name": "Linh Tran",
    "phone": "+13125550101",
    "email": "linh@example.com"
  }
}
```

`GET /api/salons/:id/appointments`

Returns appointments recorded after authority-native success, including
confirmed, rescheduled, and cancelled statuses. Internal rows expose
`authority_appointment_version`, `party_size`, and only the current version's
unreleased ordered service/resource plan. A rescheduled root therefore exposes
the replacement plan; a cancelled root exposes zero active segments while its
released historical plans remain in lifecycle evidence. Persisted provider
columns are null, while legacy compatibility DTO fields may serialize empty
strings. The optional
`limit` query parameter defaults to 50 and is capped at 200. The optional
`offset` query parameter defaults to 0. Responses include `appointments`,
`limit`, `offset`, and `has_more`; `has_more` is computed by requesting one
extra row and does not require an exact total count. Each item includes
`scheduling_authority`, `staff_selection_mode` and, when available, ordered
`segments[]` from `appointment_services` with service names, assigned
technician names, durations, and segment-level staff selection mode.
`staff_selection_mode=anyone` means the customer did not request a named
technician even though the committed appointment stores the concrete staff
assignment.

Phase 0 appointment responses also expose additive authority snapshots while
retaining every legacy POS field:

```json
{
  "scheduling_authority": "external_provider",
  "authority_provider": "square",
  "authority_appointment_id": "square-booking-id",
  "authority_appointment_version": 4,
  "segments": [
    {
      "scheduling_authority": "external_provider",
      "authority_provider": "square"
    }
  ]
}
```

For an internal row, `scheduling_authority` is `manleai_calendar`,
`confirmation_source` is `manleai_calendar`, and internal IDs/segments are the
durable evidence. Clients must hide empty legacy provider fields and must not
invent provider labels, booking IDs, versions, or POS errors.

`confirmed_at` and `confirmation_source` are optional output-only confirmation
provenance. `confirmed_by_user_id` is persisted internal audit provenance and is
intentionally redacted from client JSON until authenticated owner-manual read
semantics exist. V46 leaves all three values null for historical rows because
old provider metadata does not prove the exact confirmation time or actor.
Missing public provenance does not change current confirmed status, and future
provenance must never replace authority-specific durable success evidence.

`GET /api/salons/:id/calendar?start=<date-or-rfc3339>&end=<date-or-rfc3339>&view=<day|week|month|agenda>`

Returns a range view for the standalone POS Calendar app. The response includes
mixed-origin authority-confirmed `appointments`, external `pending_requests`
from `pos_pending` and `fallback_pending` booking attempts, and a `warnings`
summary. Calendar
appointment items include the
normal appointment fields plus `pos_sync_status`, `last_pos_synced_at`,
`pos_sync_error`, `sync_warning`, `can_edit`, and `can_delete`. A warning means
the item is not verified as cleanly synced from the active POS calendar, or the
record is a pending booking request that must not be treated as confirmed.

```json
{
  "salon_id": "...",
  "start_time": "2026-07-08T00:00:00Z",
  "end_time": "2026-07-15T00:00:00Z",
  "view": "week",
  "appointments": [],
  "pending_requests": [],
  "warnings": {
    "not_synced": 0,
    "sync_failed": 0,
    "pending_pos_sync": 0,
    "fallback_pending": 0
  }
}
```

`GET /api/salons/:id/calendar/events/stream?cursor=<created_at|notification_id>`

Streams authenticated `text/event-stream` frames for new booking events used by
the standalone POS Calendar app. The request uses the same bearer token auth as
other protected salon APIs; access tokens must not be passed in the query
string. The optional `cursor` is the last event cursor returned by the stream.
When no cursor is supplied, the stream starts from the current server time so
old notifications do not replay as fresh toasts.

Each `calendar.booking` event includes a durable notification `id`, replay
`cursor`, `type` (`booking_confirmed` or `booking_fallback_pending`),
`booking_attempt_id`, optional `appointment_id`, booking status, source,
customer display name, start/end time, and created timestamp. `booking_confirmed`
means the active POS returned a booking ID. `booking_fallback_pending` and any
calendar `pos_pending` item must be shown as pending, not confirmed.

`POST /api/salons/:id/calendar/sync`

Imports appointments from the active POS provider for the requested range. For
Square Appointments, this uses the Square Bookings list path and upserts local
appointment mirrors by `(salon_id, pos_provider, pos_appointment_id)`. The
import does not bypass the current external-provider confirmation rule; it mirrors provider
records and records POS errors when sync fails. One owner-scoped selected
location/snapshot-generation fence is captured before pagination and reused for
every page. The persistence transaction revalidates that exact fence before
its first write, so a provider, location, or generation change fails the import
without partial mirror writes. A returned Square booking with a different
non-empty `location_id` also fails the page.

```json
{
  "start_time": "2026-07-08",
  "end_time": "2026-07-15"
}
```

```json
{
  "provider": "square",
  "summary": {
    "imported": 2,
    "updated": 8,
    "skipped": 0
  },
  "range": {
    "start_time": "2026-07-08T00:00:00Z",
    "end_time": "2026-07-15T00:00:00Z"
  }
}
```

`POST /api/salons/:id/appointments/:appointment_id/reschedule`

The scheduling facade resolves the target appointment's persisted authority
and requires it to equal any authority already owned by `operation_key` or
`retry_of_attempt_id`. A mismatch returns `409 BOOKING_OPERATION_CONFLICT`
before executor/provider dispatch; the salon's current authority setting does
not reinterpret the target.

```json
{
	"operation_key": "dashboard-reschedule-7f60e4bf",
  "retry_of_attempt_id": "optional-safe-fallback-attempt-uuid",
  "availability_quote_id": "availability-quote-uuid",
  "slot_fingerprint": "sha256-hex",
  "start_time": "2026-06-11T16:00:00Z",
  "staff_id": "...",
  "notes": "Customer requested later time"
}
```

Returns `200` with the updated appointment only when the active `POSProvider` successfully reschedules the exact target booking and returns a version newer than the pre-dispatch baseline. The appointment's immutable originating provider location must match both the currently selected synchronized location and the fresh quote; a switched location or legacy missing origin fails before dispatch. Same-location generation advancement is allowed only when current raw service/staff mappings and the target version remain valid. Direct persistence is serialized with calendar import and cannot overwrite a newer stored provider version; an equal or newer authoritative mirror is accepted only when its version is at least the direct response, remains newer than baseline, and its synchronized time plus ordered canonical/raw assignment match exactly. Returns `202` with the existing `pos_pending` attempt when the same operation is already executing, or with an unconfirmed fallback/provider-pending attempt when the POS provider does not confirm the requested change or a conflicting mirror wins the persistence race; the internal appointment remains unchanged. Reusing an `operation_key` with a different normalized payload returns `409 BOOKING_OPERATION_CONFLICT`. A retry may name `retry_of_attempt_id` only when the prior attempt is a non-superseded `fallback_pending` row with `retry_policy=safe` and the new normalized request fingerprint is identical. A changed time, notes, service order, or technician assignment must start a new logical request instead of reusing that retry lineage.

`POST /api/salons/:id/appointments/:appointment_id/cancel`

Cancellation uses the same origin rule: target appointment, existing operation
key, and retry attempt must resolve to one authority. A cross-origin request
returns `409 BOOKING_OPERATION_CONFLICT` before provider dispatch.

```json
{
	"operation_key": "dashboard-cancel-9aa95f18",
  "retry_of_attempt_id": "optional-safe-fallback-attempt-uuid",
  "reason": "Customer requested cancellation"
}
```

Returns `200` with the cancelled appointment only when the active `POSProvider` successfully cancels the exact target booking and returns a version newer than the pre-dispatch baseline. Its immutable originating provider location must match the currently selected synchronized location, and cancellation carries that current exact provider fence into the adapter; a location switch or legacy missing origin makes zero provider calls. The monotonic persistence guard never overwrites a newer calendar/webhook version; a current cancelled mirror at least as new as the direct response and newer than baseline is authoritative success. Returns `202` with the existing `pos_pending` attempt when the same operation is already executing, or with status `fallback_pending` when the POS provider fails or the direct result conflicts with stored provider truth; the internal appointment remains unchanged. Reusing an `operation_key` with a different normalized payload returns `409 BOOKING_OPERATION_CONFLICT`.

`GET /api/salons/:id/booking-attempts`

Returns booking attempts, including transient `pos_pending` records and `fallback_pending` records that need owner review. The optional `status` query parameter filters by attempt status such as `fallback_pending`. The optional `limit` query parameter defaults to 50 and is capped at 200. The optional `offset` query parameter defaults to 0. Responses include `booking_attempts`, `limit`, `offset`, `has_more`, and `status` when a filter is applied; `has_more` is computed by requesting one extra row and does not require an exact total count.

Each item includes `staff_selection_mode` and, when available, ordered `segments[]` from `booking_attempt_segments` so owner dashboards can distinguish the customer preference (`anyone` or `specific`) from the staff assignment attempted through the POS provider. Fallback action rows include backend-owned `booking_action` (`book`, `reschedule`, or `cancel`), plus `target_appointment_id` and `appointment` when a failed reschedule or cancellation has a POS-confirmed appointment target. Dashboard retry actions must still call the booking service and active POS provider; a fallback request must not be marked confirmed, rescheduled, or cancelled from the list response alone.

Phase 0 attempt responses additionally include `scheduling_authority`,
`authority_provider`, optional `authority_appointment_id`, optional
`authority_appointment_version`, and authority/provider fields on ordered
segments. These are compatibility snapshots of current external-provider
evidence. Legacy POS fields still drive current routing, fingerprints, retries,
reconciliation, provider dispatch, and confirmation.

Booking attempts also expose backend-owned operation integrity state:

- `operation_type`: `book`, `reschedule`, or `cancel`.
- `provider_outcome`: `not_started`, `in_flight`, `succeeded`, `failed`, or `unknown`.
- `retry_policy`: `none`, `safe`, or `blocked`.
- `reconciliation_status`: `not_required`, `required`, or `resolved`.
- `processing_lease_expires_at`: present while one worker owns the durable operation claim.
- `can_retry`: authoritative backend retry gate. Dashboard clients must not infer retry safety only from `fallback_pending`. Book/reschedule retries require a non-legacy stored provider fence, the same currently active and synchronized provider location, exact current ordered provider service/staff mappings and versions, and an unchanged appointment target/version for reschedule. Cancel retry requires the unchanged target booking and baseline version. A newer snapshot generation requires a fresh quote but does not by itself change the logical request.
- `retry_blocked_reason`: owner-facing reason when reconciliation, legacy evidence, provider readiness, catalog mapping, or target-version drift blocks retry.

`provider_outcome=unknown` means a provider write timed out, was cancelled in flight, returned HTTP 5xx, lost or truncated its response, failed to decode a success response, failed during a post-write provider lookup, or returned insufficient booking metadata after it may have succeeded. Such attempts use `retry_policy=blocked` and `reconciliation_status=required`; they must be verified in the active POS before another operation is submitted. Only a typed definitive provider rejection is eligible for `retry_policy=safe`; untyped provider-write errors default to unknown.

Lease expiry is phase-sensitive. Recovery first checks authoritative calendar
truth under the same salon-scoped lock. An exact create/reschedule/cancel mirror
terminalizes the attempt without a fallback. Otherwise an expired `not_started`
operation proves the processing owner stopped before provider dispatch: it
becomes `fallback_pending` with `provider_outcome=failed`,
`retry_policy=safe`, and `reconciliation_status=not_required`. An expired
`in_flight` operation may already have reached the provider, so it becomes
`fallback_pending` with `provider_outcome=unknown`, `retry_policy=blocked`, and
`reconciliation_status=required`.

For an exact external create mirror, canonicalization reassigns the mirror to
the canonical attempt and records durable confirmation provenance. It
preserves existing `confirmed_at` and `confirmation_source`, fills only missing
values with the canonicalization transaction time and `external_provider`, and
does not invent `confirmed_by_user_id`. A repeated recovery does not change the
stored confirmation or update timestamps. This active evidence transition is
separate from the V46 historical backfill, which leaves unprovable legacy
provenance null.

Owner booking-attempt/calendar reads and the background worker use the same
atomic recovery transaction. The worker selects a bounded candidate list
without row locks, then handles each row with calendar advisory lock first and
appointment/attempt row locks afterward. It clears the processing token/lease, records one
`POS_TIMEOUT`, and creates one deduplicated owner-notification outbox row. Only
the `in_flight` branch creates reconciliation work. Repeated or concurrent
sweeps do not duplicate POS errors, notifications, or reconciliation tasks and
report no processed salon after its expired rows have already transitioned.
A same-operation-key replay that wins before lease recovery may safely resume
an expired `not_started` claim because provider dispatch has not begun; after
the recovery transaction finalizes it as fallback, a new attempt must use the
explicit safe-retry lineage.

Owner notifications remain durable deduplicated in-product/outbox records;
queueing alone is not delivery proof. V56
`backend/modules/notification_delivery` provides the provider-neutral worker
claim, bounded retry/dead-letter policy, immutable attempt/event/action audit,
and monotonic callback transitions. `backend/modules/notification_twilio`
provides the Twilio Messaging transport and signed callback translation using
strict salon-scoped integration configuration. Delivery runs only when the
salon-owned operational SMS configuration and consent attestation are ready;
provider acceptance is distinct from `sent` and `delivered`, and ambiguous
post-dispatch outcomes are not retried automatically. None of these delivery
states confirms, reschedules, or cancels an appointment.

`GET /api/salons/:id/booking-reconciliations?status=<open|resolved|escalated>&limit=100&offset=0`

Returns a paginated owner-scoped reconciliation queue. Each task includes
`booking_attempt_id` and an optional embedded `booking_attempt`. Unknown or
provider-pending writes remain blocked until resolution. Only historical
duplicates proven pre-dispatch with `provider_outcome=not_started` may be
normalized automatically as superseded; their task is closed with
`status=resolved` and `resolution=superseded`, and their candidate and
resolution endpoints return `409 RECONCILIATION_CONFLICT`. V39 fails instead of
collapsing a fingerprint group with more than one dispatched/unknown outcome.

`GET /api/salons/:id/booking-reconciliations/:attempt_id/candidates`

Returns `{ "candidates": [...] }` containing only tenant-scoped,
provider-synced appointments that satisfy the same exact provider ID,
operation, target appointment, status, version, time, service, and customer
matching predicate used during resolution. Clients must use this endpoint
rather than infer candidates from a truncated appointment list.

For reschedule and cancel attempts, `version` means a provider appointment
version strictly newer than the target version captured before the outbound
write. Reschedule candidates must also match the requested start/end and the
ordered service/staff assignment. The customer technician-preference flag is
not compared because a provider calendar mirror records the assigned
technician, while the original request may have used `staff_selection_mode=anyone`.

`POST /api/salons/:id/booking-reconciliations/:attempt_id/resolve`

```json
{
  "action_key": "owner-resolution-uuid",
  "action": "provider_attached",
  "provider_appointment_id": "square-booking-id",
  "provider_appointment_version": 4,
  "provider_status": "accepted",
  "note": "Verified against the latest provider calendar sync."
}
```

Actions are `provider_attached`, `not_created`, and `escalated`.
`provider_attached` locks and revalidates the exact provider-synced candidate
before linking it; `not_created` makes a definitive failure retryable only after
owner verification. For reschedule/cancel, the existing target provider booking
ID is not proof that the requested mutation succeeded. `not_created` is
rejected whenever an exact provider-synced booking or newer appointment
mutation is already visible.
`escalated` keeps retry blocked. `action_key` is
payload-bound and replay-safe. Resolution is serialized with provider calendar
imports for the salon before either path takes row locks, preventing a
concurrent mirror insert from passing a `not_created` absence check.

`POST /api/salons/:id/booking-attempts`

This is the legacy external-provider compatibility create route. It keeps the
existing booking-attempt response contract for persisted external operations.
New authority-neutral callers use `/scheduling-actions`; owner-manual work is
stored in `scheduling_requests`, never mirrored into this ledger.

```json
{
	"operation_key": "dashboard-booking-0dfd",
  "retry_of_attempt_id": "optional-safe-fallback-attempt-uuid",
  "availability_quote_id": "availability-quote-uuid",
  "slot_fingerprint": "sha256-hex",
  "customer_name": "Linh Tran",
  "customer_phone": "+13125550101",
  "customer_email": "linh@example.com",
  "service_id": "...",
  "staff_id": "...",
  "staff_selection_mode": "specific",
  "segments": [
    {
      "service_id": "...",
      "staff_id": "...",
      "staff_selection_mode": "specific"
    }
  ],
  "start_time": "2026-06-10T15:00:00Z",
  "notes": "First visit"
}
```

`operation_key` is required and identifies one logical booking operation. It is generated once per dashboard action or deterministically from the conversation session. The backend stores a normalized request fingerprint under a salon-scoped unique operation claim before customer or POS side effects. Replaying the same key and logical intent returns the existing attempt and reuses its POS idempotency key without a second POS writer. Ephemeral availability quote IDs and slot-proof fingerprints may be refreshed for the same logical request after response loss and are not replay-identity fields; they remain mandatory and exact for the first claim that can dispatch. Reusing the same key with different customer, retry lineage, target, time, notes, or ordered service/staff intent returns `409 BOOKING_OPERATION_CONFLICT`.

For a V86 external claim, an overlapping different operation returns
`409 SLOT_COMMIT_CONFLICT`. An exact operation whose claim is still processing
returns `202 SLOT_CLAIM_IN_PROGRESS`; an outcome that may have reached the
provider but cannot yet be proved returns `202 SLOT_OUTCOME_UNKNOWN`. None of
these responses is confirmation evidence. The booking and neutral scheduling
handlers use the same status/code contract.

The scheduling facade resolves an existing `operation_key` across persisted
booking attempts and owner-manual scheduling requests, and requires
`retry_of_attempt_id`, when present, to have the same originating authority.
Only when neither origin exists does it read the current
`salon_settings.scheduling_authority` for a genuinely new create. A key already
owned by an owner-manual request cannot be reinterpreted as an external booking
attempt.

For this authenticated HTTP endpoint, the handler assigns
`source=owner_dashboard`; caller-supplied source values are not trusted. Quote
evidence is required on this HTTP surface. Provider-neutral internal callers
retain their own source and conversation authorization gates, but conversation
scheduling actions enter through the same authority facade. Square-specific
test, webhook, sync, and repair operations use the established external booking
service where they are explicitly operating on external-provider state.

After authority resolution chooses `external_provider`, the booking service
requires matching adapter capability and current exact persisted evidence. It
then creates the backend booking attempt, consumes the quote, and installs the
V86 claim in one transaction before calling the active `POSProvider`. For
multi-service booking, each segment is resolved to provider-neutral
service/staff records and persisted in `booking_attempt_segments`; confirmed
appointments snapshot the same ordered segments in `appointment_services`.
Booking resolution translates ManleAI canonical IDs through valid
active-provider links before calling the POS
adapter. Local-only, unmapped, sync-failed, or archived records must not be
booked. `staff_selection_mode=anyone` records that the customer did not request
a named technician; the backend still stores the POS-compatible staff assignment
used for the booking attempt. Returns `201` with status `confirmed` only when
the POS provider returns a POS booking ID and booking version. Returns `202`
with the existing `pos_pending` attempt for an in-flight duplicate. A typed
definitive provider rejection returns `fallback_pending`,
`provider_outcome=failed`, and `retry_policy=safe`. A transport error, HTTP 5xx,
response/decode ambiguity, post-write lookup failure, timeout, or missing
required provider metadata returns
`fallback_pending`, `provider_outcome=unknown`, `retry_policy=blocked`, and
`reconciliation_status=required`; it must not be automatically retried or
described as confirmed.

## Conversation Sessions

`GET /api/salons/:id/conversation-sessions`

Returns recent conversation sessions for the authenticated owner, including `simulator` and `phone` channels. The optional `lifecycle_status` query parameter accepts `active`, `archived`, or `redacted`; the default is `active`. The optional `limit` query parameter defaults to 25 and is capped at 100. The optional `offset` query parameter defaults to 0. Responses include `sessions`, `limit`, `offset`, and `has_more`; `has_more` is computed by requesting one extra row and does not require an exact total count. Active lifecycle sessions receive a 90-day `retention_expires_at` timestamp. The worker redacts expired sessions by clearing customer PII, transcript bodies, handoff summaries, webhook payloads, and temporary voice audio while preserving booking, handoff, outcome, provider call, and timestamp audit links.

`POST /api/salons/:id/conversation-sessions`

```json
{
  "channel": "simulator"
}
```

Creates a simulator session and writes the initial AI transcript message. The initial message identifies the salon when a salon name is configured, includes the recording disclosure from runtime defaults or salon greeting configuration, and uses an open-ended help prompt instead of assuming the caller wants to book.

`GET /api/salons/:id/conversation-sessions/:session_id`

Returns one conversation session with transcript messages and the latest handoff request when present. Booking state includes `requested_date` when the customer has provided a day but not a specific time, and `requested_start_time` only after a concrete start time or offered slot is selected. A successfully persisted owner-manual request sets `scheduling_request_id` and `outcome=owner_review_pending` while leaving `appointment_id` and `booking_attempt_id` empty. `dialog_state` is a versioned operational state object containing phase, pending typed clarification, bounded mutation history, no-progress count, `draft_revision`, `reviewed_revision`, `authorized_revision`, and optional `consultation` and `guidance` state. Consultation state includes controlled caller needs such as desired finish, candidate and recommended service IDs, selected service ID, last asked field, profile revisions, recommendation reasons, bounded no-progress count, resume phase, and exit reason. Guidance state includes `stage`, dynamically derived `offered_actions`, `awaiting_action_choice`, separate `no_progress_count` and `provider_failure_count`, `progress_fingerprint`, and `last_provider_outcome`. Existing legacy guidance prompt/counter fields are normalized into this version 5 nested object on read without a database migration. `awaiting_action_choice` enables only the bounded choices from the immediately preceding provider-failure prompt; it is not a general caller-intent classifier and is cleared on progress or terminal handoff. A bounded semantic-provider outage uses handoff reason `guidance_provider_unavailable`; caller ambiguity continues to use `service_clarification_unresolved`. Transcript messages may include PII-reduced turn-understanding diagnostics, validated acts/questions, revision transitions, slot state, event keys, guardrail outcomes, answer sources, consultation or guidance audit metadata, and next required field.

Optional detail children use a compatibility projection. Valid legacy
transcript `metadata` that is not an object and valid legacy party guest data
that is not the current array shape are omitted and surfaced in
`detail_warnings`; the raw stored record is not changed. Database or scan
failures remain fatal and return the sanitized section-specific error codes
listed for the normalized Platform route above.

Both the list and detail responses include `scheduling_result_evidence`, an
owner-scoped backend projection whose default is `complete=false`. Clients must
not infer confirmation from the session `outcome`, `appointment_id`, or
`booking_attempt_id`. An `owner_manual` request can produce only
`kind=pending_owner_review`; later request states such as `resolved` or
`dismissed` are still not appointment confirmation. Its optional validated
`target_scheduling_authority` preserves the selected internal/external target
for a pending-approval request without changing the request's nonconfirming
`scheduling_authority=owner_manual` origin. A completed
`manleai_calendar` operation requires the exact internal attempt, execution
event, root version/status, durable result child graph, valid current active
child graph, and absence of provider evidence. A completed
`external_provider` operation requires a successful provider outcome with the
required provider booking ID/version and a matching external appointment
mirror. A current external book/reschedule result additionally requires exact
ordered attempt-to-appointment child-graph equality. Because the external
appointment child table is a current, non-versioned mirror, a later valid
lifecycle version can retain the immutable earlier result as complete
historical evidence without requiring its old child graph to equal the newer
current graph; it remains noncurrent. Split party evidence is complete only when every expected operation
key, attempt, appointment root, and child result is present and valid.
`result_status` and `authority_appointment_version` describe the exact
historical operation; `current_status`,
`current_authority_appointment_version`, and `is_current` describe the present
root after later lifecycle operations. Therefore an exact historical success
remains auditable with `complete=true` but is not a current confirmation when
`is_current=false`. Unknown, malformed, provider-ambiguous, partially
persisted, or legacy-incomplete evidence fails closed with `kind=incomplete`.

Phase 5H also returns `dialog_state.reviewed_booking_mode` and
`dialog_state.selected_scheduling_authority`. They bind the final review to the
exact conversation policy and selected authority; either setting changing
requires a new full review before origin-free execution. Under
`pending_approval`, verified internal/provider slots are selection evidence
only and the final result is one `owner_review_pending` request with no
appointment or booking-attempt ID. Under `disabled`, origin-free conversation
scheduling stops before availability, request, executor, or provider actions.

Production-created sessions use `dialog_state.review_required=true`. When every booking field is complete, the runtime sets `reviewed_revision=draft_revision`, returns `phase=review`, and asks the caller to review the draft. A later explicit authorization sets `authorized_revision` only for that same revision. Booking requires `draft_revision == reviewed_revision == authorized_revision`; review acceptance is not booking confirmation.

`POST /api/salons/:id/conversation-sessions/:session_id/archive`

Archives an owner-scoped conversation session so it no longer appears in the default active call log. Archiving is idempotent and keeps transcript text available until retention redaction.

`POST /api/salons/:id/conversation-sessions/:session_id/redact`

Irreversibly redacts a completed or otherwise non-active owner-scoped conversation session. Active sessions return `409 CONVERSATION_LIFECYCLE_CONFLICT`. Redaction clears customer PII and transcript text but keeps POS booking evidence and owner handoff links for audit.

`POST /api/salons/:id/conversation-sessions/:session_id/messages`

```json
{
  "message": "My name is Linh Tran, phone 312-555-0199, classic manicure with Mai on 2026-06-10 at 3pm.",
  "event_key": "optional-provider-event-key"
}
```

The conversation runtime supports all three registered executors within their
operation contracts. `owner_manual` is request-only, `external_provider`
retains its provider behavior, and Phase 4C `manleai_calendar` supports
structured multi-guest, multi-service staff-only/pooled availability, atomic
create, and whole-root reschedule/cancel.

Under `owner_manual`, answer context uses active, non-archived, AI-bookable
canonical services with positive duration and active, non-archived,
AI-bookable canonical staff without requiring a POS link. Informational
business-hours questions use only owner-managed `local_override` periods; if
none exist, the response says business hours have not been configured and does
not claim that POS sync is missing. Informational hours are not availability
and do not call availability, booking, or POS actions. Availability is
`request_only`: the engine asks for a preferred time and does not claim an
opening. After final review and authorization, book, reschedule, and cancel
persist one call-linked scheduling request and end with concise pending-owner-
review wording. Book/party wording explicitly says the request is not a
confirmed appointment; reschedule/cancel wording says the original appointment
has not been changed. Persistence failure enters safe handoff and does not
claim that the request was recorded, sent, delivered, confirmed, rescheduled,
or cancelled. An external-origin target remains on its external executor even
when the salon's current setting is `owner_manual`; without external history,
a durable target description identifies the owner-manual reschedule/cancel
request.

Under `manleai_calendar`, answer context uses canonical active AI-bookable
services/staff, activated V48 policies, weekly staff schedules,
`local_override` hours, and resource policy. It derives readiness from backend
`readiness.capabilities`, not an optimistic catalog query. A complete party
plan becomes one aggregate availability request and one reviewed quote that
preserves every guest reference, ordered service unit, concrete staff
assignment, timing range, and resource allocation. The booking action enters
the same neutral atomic executor used by dashboard create. Confirmation
requires durable internal root IDs and a complete child graph matching that
quote; partial children and provider/POS-shaped evidence are rejected. Exact
response-loss replay keeps the same root operation identity and returns the
same committed graph. Typed quote-stale/resource conflicts reopen the complete
draft with explicit zero-confirmation behavior. Internal reschedule and cancel
use the selected candidate's persisted internal origin and exact version even
after the salon's current authority changes. Reschedule uses one target-aware
aggregate quote and a persisted `internal_reschedule_confirmation` act before
the action call. Cancel persists `internal_cancel_reason` followed by
`internal_cancel_confirmation` and sends no quote/replacement plan. Only a
state-scoped affirmative can authorize either reviewed lifecycle act. Exact
operation replay preserves the historical lifecycle result after later root
mutations; stale target/version refreshes candidates and reoffers without
success wording; cutoff/not-ready, partial, or provider-shaped results never
produce rescheduled/cancelled wording. External-provider party behavior is
unchanged.

The detailed provider mutation behavior below applies to the Square-backed
`external_provider` path.

The implemented Phase 6 fuzzy-service slice is authority-neutral conversation
state, not a new scheduling executor. Exact catalog and alias selections keep
their established behavior. A fuzzy result with exactly one current catalog
candidate persists
`dialog_state.pending.prompt_key=fuzzy_service_confirmation`, the candidate
ID, source service IDs, fuzzy provenance/token, and add/replace/set or party
scope; it does not mutate the draft, clear availability, or call booking. Only
a state-scoped affirmative or new exact/alias catalog evidence accepts that
identity. Negative input rejects it, unclear input repeats the prompt, and a
stale catalog candidate, changed source selection, or wrong-state input fails
safely. Event-key replay returns the persisted turn without applying the
confirmation twice. The bounded affirmative grammar is valid only while that
one pending catalog candidate owns the expected input; it is not a general
caller-intent classifier. No other Phase 6 scope is implied here.

Processes one simulated customer message through the deterministic conversation engine. `event_key` is optional for simulator callers and is used by voice adapters to dedupe provider retries. The simulator asks one question at a time, preserves already-collected booking slots, handles greeting-only or connection-check turns without replaying the full welcome or forcing booking intent, handles date-only turns such as weekdays before a time is known, can create owner handoffs for human requests or disabled AI booking, and resolves provider-neutral availability before selecting a booking time. The selected authority returns either request-only behavior or verified slots; execution starts only after the caller selects a slot when one is required and the required customer details are collected. Deterministic date, time, staff, customer, and explicit availability-question evidence is applied before a model-proposed standalone summary can return, so a concrete correction or request for openings cannot be dropped or misrouted by semantic classification. If a new-booking caller proposes a different date or time after availability slots were offered, the engine preserves those slots and stores a typed pending correction until the caller confirms; rejection or a renewed availability question keeps and repeats the prior slots, while confirmation invalidates them and performs a fresh authority-neutral availability check. Service utterances are interpreted against the active salon catalog, active `service_aliases`, active `service_categories`, and active `service_category_aliases`; exact catalog service names win over aliases, alias matches can select one service, category/category-alias matches ask the caller to choose a real service in that group, and generic or fuzzy family matches ask for catalog-backed clarification instead of selecting a service. A structured semantic act cannot narrow a category/category-alias candidate set to one service without concrete service evidence. Exact family evidence takes precedence over fuzzy service guessing unless another caller token distinctly identifies one catalog service, so conversational wording such as "manicure as well" cannot be misread as "Gel Manicure." If the caller mentions a different service after a service is already selected, the engine distinguishes adding from replacing before mutating the booking; a bare concrete service switch uses confirmation before clearing slots or changing the draft. A generic request such as another service first asks whether to add or replace; an ambiguous service family then asks for one concrete catalog service; and a multi-service replacement also asks which current service to replace. While a family target is pending, short replies are interpreted against that candidate set first, with full-catalog fallback for a clearly different service. The engine preserves the selected services and offered slots until the operation and concrete target are resolved, never applies every candidate from an ambiguous family, and only then clears stale offered slots and rechecks availability when date or time context already exists. Non-booking answers are routed from structured sources before knowledge: authority-eligible AI-bookable services, authority-eligible business hours, authority-eligible staff, booking availability prompts, then active knowledge. Informational service menu and count questions are answered from the full matched bookable catalog without selecting a service, clearing pending candidates, or calling availability/booking tools; if a booking is already in progress, the reply then resumes the unresolved service question. Transcript metadata may include `service_understanding_status`, `service_understanding_reason`, `service_understanding_confidence`, candidate service IDs/names, selected service, alias source, alias ID, category ID, category name, `answer_source`, `answer_source_reason`, `answer_source_confidence`, `router_intent`, `source_record_ids`, and `answer_context_cache_hit` for debugging. Supported group or party booking requests resolve party size and guest service counts into ordered `booking_segments`, call authority-neutral availability, and execute through the same scheduling boundary after the caller selects a required slot and provides required customer details. Party parsing distinguishes person-count phrases such as "for two guests" from service-count phrases such as "two manicures"; session `party_plan` may include optional `parse_source`, `parse_confidence`, `clarify_reason`, group `source`, and `evidence` fields for debugging and review. Ambiguous party service families and party-size/service-count mismatches ask for catalog-backed clarification before availability. Offered slots may include ordered `segments` with provider-neutral service/staff assignments. The engine can select a unique offered slot from ordinal replies, spoken times such as "one p.m.", or a "Yes" reply to a prompt that confirmed one specific offered time; unclear time fragments repeat the existing offered slots instead of rerunning availability. Once a customer selects a slot, the session stores selected `booking_segments` and `staff_selection_mode` so simulator and phone flows can submit one multi-service or supported all-or-none party scheduling operation. When `staff_selection_mode=anyone`, the customer did not choose a named technician, so the conversation avoids presenting an assigned technician as customer-selected. Reschedule and cancellation requests use `booking_action` values `reschedule` and `cancel`, resolve upcoming appointments by captured origin and caller phone, ask the caller to select or confirm the target appointment, and then enter the provider-neutral scheduling boundary. `owner_manual` returns one durable pending owner-review request and never appointment-confirmed wording. `manleai_calendar` returns confirmed/rescheduled/cancelled wording only after its atomic internal commit returns the required durable root, attempt, version, and child evidence. Square-backed `external_provider` returns that wording only after provider success with the required provider booking ID/version and matching appointment evidence. External provider failures create `booking_fallback_pending` wording and do not create confirmed, rescheduled, or cancelled appointment language.

Session responses include a persisted `state_revision`. Production message and
typed voice-recovery execution acquire one session-scoped database lock before
the first state read and retain it through any availability/POS side effect and
the final transcript/state commit. `event_key` is persisted on both sides of
the turn; replay returns that event's exact historical AI reply without
reinterpreting the request or replacing a newer current session state. The repository also compares `state_revision` during `SaveTurn`; after a
bounded retry, an unresolved stale write returns
`409 CONVERSATION_STATE_CONFLICT` without silently overwriting newer state.

When `consultation_enabled=true`, a caller may enter AI Consultation without a booking request. The semantic lane extracts controlled consultation needs but does not choose a service. Field-level `set`, `replace`, `add`, `remove`, and `clear` mutations are validated against controlled values and are the sole persistence authority for consultation need fields, so a free-standing semantic snapshot cannot overwrite scalar or list state and corrections do not accumulate stale preferences. Backend ranking uses only complete `ready` profiles from the eligible active-provider service catalog, stores the profile revisions and reasons in dialog state, and asks one useful question per turn. Consultation never calls availability or booking tools. A recommendation with one result asks whether the caller wants booking help; multiple results ask for a concrete service choice, and that choice still requires a separate booking-intent confirmation. Deterministic health signals are checked before normal turn routing; a validated structured safety assessment is also handled before any draft, availability, consultation, or POS action. A caller may end with `outcome=consultation_completed`, return to an unchanged booking draft, or enter owner handoff for safety or repeated unresolved input.

Structured service, alias, category, consultation-profile, staff, knowledge,
and business-hour answer context is guarded by a database-owned fence on every
turn. Persisted collection versions cover the common structured sources.
`owner_manual` additionally includes the salon-wide
`business_resource_versions` `business_hours` version; `manleai_calendar` uses
persisted config and activated versions; and `external_provider` uses
active provider/location/snapshot generation/sync readiness. Provider evidence
does not participate in either owner-first fence, and local-hours versions do
not participate where those rows are not the selected authority's informational
hours source. A local cache hit is accepted only when the complete relevant
fence matches. The `manleai_calendar` per-turn fence reads only persisted
authority/config/activation versions; it does not hydrate the calendar aggregate
on a stable cache hit. A cache miss loads the authoritative aggregate and calls
`EvaluateReadiness`; conversation accepts that readiness only when the aggregate
evidence matches the lightweight fence. Cache misses double-read the fence
around context loading, and concurrent source-of-truth changes retry. A
non-ready external snapshot fails closed by hiding provider-owned structured
data while retaining eligible salon-authored knowledge. This is internal
freshness behavior and adds no API field.

Configured production turns first enter the state-driven Turn Kernel. The kernel derives `expected_input`, measures deterministic coverage, and selects one explicit route: `fast_lane`, `answer_lane`, `action_lane`, `recovery_lane`, or `semantic_lane`. Unambiguous expected-field evidence, offered-slot choices, state-scoped confirmations, structured questions, and operational actions avoid a reply-model round trip. For a new booking with no selected service, an exact catalog service advances directly to the next missing field and a category asks for one concrete catalog option; neither path asks whether to add or replace. An add-or-replace operation choice is valid only after a service has been selected. The semantic lane selects one of two strict contracts from operational state, not caller wording. `guidance_turn` is limited to initial caller-goal or service-guidance state with no booking progress and returns a typed `guidance_action`, bounded explicit `guidance_party_size`, extraction-only consultation needs, confidence, reason, and safety without a separately model-authored goal, acts, or questions. The model always receives the complete stable `recognizable_guidance_actions` protocol (`book`, `service_catalog`, `consultation`, `salon_question`, `name_service`, `reschedule`, `cancel`, and `human_handoff`), so recognition of a caller's request is not disabled when a catalog or recommendation profile is unavailable. The backend separately derives `turn_available_guidance_actions` and the runtime service-guidance capability from the current catalog, salon consultation toggle, and ready owner-approved profiles. It validates the recognized action, derives the general goal, rejects party sizes outside 2-20 or attached to a non-book action, and then resolves whether that action can be fulfilled. A valid initial party size is translated into the same guest-count act consumed by the reducer. A recognized consultation request remains consultation even when personalized recommendation is disabled or unavailable: the reply truthfully offers catalog-backed grouping when possible, otherwise explains the technical service-guide limitation and offers owner help without guessing a service or blaming the caller. The shorter `dialog_state.guidance.offered_actions` remains the state-owned choice set used only during bounded dependency recovery. A semantic `service_catalog` action always renders from the active bookable service catalog; active knowledge text cannot become the service-menu source. `full_turn` is required for corrections, multi-signal or partial-coverage turns, pending/review/party state, and any existing booking progress; it may return zero or more ordered acts and questions. Full-contract acts cover add/replace/remove/undo plus set/clear corrections for service, staff, date/time, customer, and guest state. One utterance may contain both a correction and a question; the reducer applies validated correction semantics first, answers the question from structured sources, then resumes one useful pending question. Replacement source and target remain separate. Pending candidates are context rather than a closed vocabulary, current-draft questions do not become catalog-count questions, and repeated unresolved clarification is bounded. For a completed `party_plan`, service correction pending state uses `party_service_target`, `party_service_guest`, `party_service_operation`, and `party_service_source` with exact `guest_ref`; short replies resolve deterministically, redundant `guest_scope` is cleared, only the selected party group may change, and offered slots/review authorization are invalidated only after the correction resolves. Unresolved party-correction pending state blocks availability and booking even when semantic interpretation is unavailable.

The configured OpenAI reply model interprets only `semantic_lane` turns selected from state and deterministic coverage, never from a keyword-only gate. Input contains `expected_input`, `semantic_contract`, a PII-reduced utterance, selected or context-relevant service and staff identities, current party guest references/counts, pending act, booking action/stage, boolean customer-field presence, and current revision. Existing per-guest service assignments are not model input; they remain backend state so the model cannot manufacture replacement sources from draft layout. Initial guidance carries the full stable recognition vocabulary, while capability stays backend-owned. The accepted guidance action deterministically derives its general goal and protocol-owned companion fields: catalog questions own `catalog` subject, salon questions own their operational subject, and other actions cannot retain irrelevant catalog mode/subject decoration. Consultation profiles are never sent to the semantic model; owner-approved ready profiles remain backend-only inputs to consultation question planning and recommendation ranking. During an active consultation, model-authored booking/service acts are discarded before validation. Party service mutations must name an existing guest reference, and replacement sources must be grounded in the caller's current utterance; an initial multi-act party request may construct counted groups without flattening repeated services. A structured time preference is normalized to availability when the state is awaiting date/time, regardless of a model-authored staff or current-booking subject. Both structured contracts carry extraction-only consultation mutations and a global safety assessment; neither has side-effect authority. The backend rejects low-confidence goals/questions/acts, invalid guidance actions, invalid mutation semantics, malformed party counts, safety categories outside the controlled contract, and IDs outside the salon's active catalog. The interpreter inherits the simulator or phone request context and no longer installs a private 2.5-second deadline; the OpenAI adapter retains a 30-second HTTP transport ceiling. A caller-context timeout, provider failure, empty output, invalid schema, low-confidence result, or rejected catalog reference preserves the draft and may fall back to independently validated catalog and already-captured field evidence before asking the next missing-field question. Without such evidence, the runtime safely clarifies or hands off. Guidance provider-failure wording is selected only from typed `dialog_state.guidance.offered_actions`; the immediately following bounded choice can resolve one of those still-active actions without another provider call. It may offer profile-backed consultation only when consultation is enabled and at least one eligible profile is ready, and it never offers a service menu when the runtime catalog is empty. The model cannot call tools, mutate session state, or create confirmed wording.

New scheduling execution requires revision-bound final review. Service, staff,
date/time, guest/party, or customer changes advance the draft revision;
dependency-bearing changes also invalidate offered availability. Under
`external_provider`, confirmed wording requires provider success plus the
required booking evidence. Under `owner_manual`, the result remains pending
owner review and never uses confirmed wording. Under Phase 4C
`manleai_calendar`, create and reschedule require current exact quote proof;
reschedule additionally requires the persisted reviewed whole-root target/
version, while cancel requires the persisted target/version, reason, and
explicit lifecycle confirmation with no quote. Confirmed lifecycle wording
requires durable internal root/attempt IDs, exact target/result versions,
authoritative status/active-child evidence, and the complete operation-native
child snapshot from the atomic commit.

Primary act/question/guidance validation is isolated from auxiliary consultation extraction. `unknown` is treated as absent, while malformed, low-confidence, catalog-invalid, and state-no-op consultation snapshots or mutations are dropped with `turn_consultation_profile_dropped` / `turn_consultation_mutations_dropped` diagnostics instead of rejecting valid primary meaning. Consultation mutation schema values come from the controlled protocol vocabulary plus current request catalog service IDs; runtime field/state validation remains authoritative. A syntactic question containing an expected-field value is partial coverage rather than an automatic fast-lane completion, so semantic constraints such as date plus time window are retained. Once guidance resolves to booking, the reply asks for the next missing booking field; a resolved salon question returns the structured answer without appending the generic caller-goal menu.

Phone channel sessions are created by Twilio webhooks and use the same conversation engine. Phone bookings use source `ai_voice_call`; simulator bookings use source `ai_conversation_simulator`.

`GET /api/salons/:id/party-booking-requests?status=pending`

Returns owner-scoped party booking requests created from group booking handoffs.
The optional `status` filter accepts `pending`, `contacted`, `resolved`, or
`dismissed`; the optional `limit` defaults to 25 and is capped at 100. The
optional `offset` defaults to 0. Responses include `party_booking_requests`,
`limit`, `offset`, and `has_more`; `has_more` is computed by requesting one
extra row and does not require an exact total count.

```json
{
  "party_booking_requests": [
    {
      "id": "party-request-id",
      "salon_id": "...",
      "call_session_id": "session-id",
      "event_key": "provider-turn-key",
      "status": "pending",
      "party_size": 3,
      "representative_name": "Linh Tran",
      "representative_phone": "+13125550101",
      "requested_date": "2026-07-06",
      "requested_time_window": "afternoon",
      "guest_service_requests": [
        {"service_id": "service-gel-id", "service_name": "Gel Manicure"}
      ],
      "flexibility_notes": "Caller can move later if needed.",
      "summary": "Group booking request for three manicures.",
      "created_at": "2026-07-04T15:00:00Z",
      "updated_at": "2026-07-04T15:00:00Z"
    }
  ],
  "limit": 25,
  "offset": 0,
  "has_more": false
}
```

`PATCH /api/salons/:id/party-booking-requests/:request_id/status`

Updates owner workflow status for a party request. This does not create,
confirm, reschedule, or cancel a POS appointment.

```json
{
  "status": "contacted"
}
```

## AI Training

`GET /api/salons/:id/knowledge-items`

Returns owner-scoped salon knowledge items.

`POST /api/salons/:id/knowledge-items`

```json
{
  "title": "Late arrival policy",
  "category": "policy",
  "body": "Customers can arrive up to 10 minutes late before the owner needs to review the appointment.",
  "status": "active"
}
```

`PUT /api/salons/:id/knowledge-items/:item_id`

Updates title, category, body, and status.

`DELETE /api/salons/:id/knowledge-items/:item_id`

Deletes an owner-scoped knowledge item.

`GET /api/salons/:id/service-aliases`

Returns owner-scoped service aliases used by the conversation service-understanding layer. Runtime use is limited to active aliases whose target service is active and AI-bookable.

`POST /api/salons/:id/service-aliases`

Creates or updates a service alias. The backend normalizes `alias` into
`normalized_alias` and upserts by `(salon_id, normalized_alias)`, so retrying
the same request updates one alias instead of creating duplicate learned
behavior. The backend rejects an active service alias if the normalized phrase
already belongs to an active service category alias, because one caller phrase
cannot safely mean both a concrete service and a category.

```json
{
  "service_id": "...",
  "alias": "shell manicure",
  "source": "owner",
  "status": "active",
  "confidence": 0.94
}
```

`GET /api/salons/:id/owner-corrections`

Returns recent owner corrections.

`POST /api/salons/:id/owner-corrections`

```json
{
  "call_session_id": "...",
  "transcript_message_id": "...",
  "correction": "Mention the group deposit policy before collecting booking details."
}
```

When `transcript_message_id` is provided, `call_session_id` is required so the backend can validate the correction source against the owner-scoped call session.

`POST /api/salons/:id/owner-corrections/:correction_id/apply`

Creates a knowledge item from the correction and marks the correction as `applied`.

`POST /api/salons/:id/owner-corrections/:correction_id/apply-service-alias`

Applies a correction into a structured service alias and marks the correction as `applied`. This is for service-understanding fixes such as mapping a repeated caller/STT phrase to a real service. It does not alter POS services and does not confirm appointments.

```json
{
  "service_id": "...",
  "alias": "shell manicure",
  "confidence": 0.96
}
```

`POST /api/salons/:id/owner-corrections/:correction_id/dismiss`

Marks the correction as `dismissed`.

`POST /api/salons/:id/training/evaluate`

```json
{
  "message": "Do you take walk-ins?"
}
```

Returns a read-only preview that uses active salon knowledge without creating a
call session, writing transcript rows, calling voice providers, calling a
scheduling executor, or calling the booking service. The endpoint resolves the
owner-scoped selected `salon_settings.scheduling_authority` through
`scheduling.Service.CurrentSchedulingAuthority` and returns the confirmation
contract for that exact authority.

```json
{
  "message": "Do you take walk-ins?",
  "reply": "Walk-ins are accepted when staff is available. Would you like help with an appointment?",
  "matched_knowledge": {
    "title": "Walk-in policy",
    "category": "policy",
    "body": "Walk-ins are accepted when staff is available."
  },
  "outcome": "knowledge_answer",
  "booking_action": "none",
  "scheduling_authority": "owner_manual",
  "confirmation_requirement": "pending_owner_review",
  "confirmation_guardrail": "This preview never reserves or confirms an appointment. Owner manual scheduling is non-reserving, and every request remains pending for owner review.",
  "pos_confirmation_required": false
}
```

`confirmation_requirement` is one of:

- `pending_owner_review` for `owner_manual`; evaluation is non-reserving and
  cannot turn a request into an appointment.
- `atomic_internal_commit` for `manleai_calendar`; confirmation requires an
  atomic internal commit with durable root appointment and attempt IDs, an
  authoritative appointment version, and complete child service/resource
  evidence.
- `provider_booking_success` for `external_provider`; the current Square-backed
  executor requires a successful provider booking ID and status plus persisted
  booking evidence.

`confirmation_guardrail` is backend-owned authority-aware presentation copy.
Clients must render it instead of inferring confirmation rules from provider
fields. `pos_confirmation_required` remains for backward compatibility and is
true only for the currently Square-backed `external_provider` path; it is false
for `owner_manual` and `manleai_calendar`. Unknown authority values and
authority-read failures fail closed as generic `TRAINING_EVALUATION_FAILED`
responses. A salon outside the authenticated owner's scope remains
`404 SALON_NOT_FOUND`.

## Voice

`GET /api/salons/:id/voice/status`

Returns owner-scoped live voice, scheduling, and external AI-provider readiness
without exposing Twilio, OpenAI, or scheduling-provider token secrets. The
response separates three dimensions:

- `phone_answering_ready`: the configured telephony path can accept an inbound
  call for the salon. Legacy `ready` mirrors this dimension.
- `request_capture_ready`: phone answering, salon AI enablement, booking mode,
  authority/version fence, and the selected authority's availability or
  request-capture path are ready.
- `automated_booking_ready`: request capture is ready, `booking_mode` is
  `confirmed_booking`, and the selected authority can produce its required
  durable confirmation evidence. Legacy `phone_booking_ready` mirrors this
  dimension.

`scheduling_authority`, `scheduling_authority_version`, and `booking_mode`
identify the exact decision fence. Each dimension includes safe typed
`blockers`. `owner_manual` may make request capture ready but never automatic
booking ready. `manleai_calendar` uses current internal activation and
capability evidence. `external_provider` uses the selected adapter's provider
readiness, including write safety for automatic confirmation. Square setup is
not a prerequisite for `owner_manual` or `manleai_calendar`.

```json
{
  "provider": "twilio",
  "configured": true,
  "signature_verification": true,
  "inbound_webhook_url": "https://api.example.com/api/voice/twilio/3f17f690-7de4-4b26-91b8-2763ca15489d/incoming",
  "turn_webhook_url": "https://api.example.com/api/voice/twilio/3f17f690-7de4-4b26-91b8-2763ca15489d/turn",
  "recording_webhook_url": "https://api.example.com/api/voice/twilio/3f17f690-7de4-4b26-91b8-2763ca15489d/recording",
  "stream_webhook_url": "wss://api.example.com/api/voice/twilio/3f17f690-7de4-4b26-91b8-2763ca15489d/stream",
  "salon_phone": "+16292536211",
  "ready": true,
  "phone_answering_ready": true,
  "request_capture_ready": true,
  "automated_booking_ready": true,
  "phone_booking_ready": true,
  "scheduling_authority": "external_provider",
  "scheduling_authority_version": 3,
  "booking_mode": "confirmed_booking",
  "phone_answering": {
    "ready": true,
    "blockers": []
  },
  "request_capture": {
    "ready": true,
    "blockers": []
  },
  "automated_booking": {
    "ready": true,
    "blockers": []
  },
  "input_mode": "recording",
  "booking": {
    "ready": true,
    "ai_enabled": true,
    "active_provider": "square",
    "provider_connected": true,
    "provider_synced": true,
    "square_connected": true,
    "square_synced": true,
    "test_booking_cancelled": true,
    "booking_write_blocked": false,
    "guidance_service_count": 4,
    "service_count": 4,
    "consultation_enabled": true,
    "consultation_ready_service_count": 2,
    "service_guidance": {
      "status": "recommendation_ready",
      "catalog_available": true,
      "consultation_enabled": true,
      "recommendation_ready": true,
      "ready_service_count": 2
    },
    "staff_count": 3,
    "business_hours_count": 6,
    "checks": [
      {"key": "connect_square", "label": "Connect Square Appointments", "complete": true},
      {"key": "sync_square", "label": "Sync Square calendar", "complete": true},
      {"key": "bookable_services", "label": "AI-bookable services", "complete": true},
      {"key": "bookable_staff", "label": "AI-bookable staff", "complete": true},
      {"key": "business_hours", "label": "Business hours", "complete": true},
      {"key": "enable_ai_booking", "label": "Enable AI booking", "complete": true}
    ]
  },
  "ai": {
    "provider": "openai",
    "configured": true,
    "ready": true,
    "stt": {
      "provider": "openai",
      "configured": true,
      "ready": true,
      "model": "gpt-4o-mini-transcribe"
    },
    "llm": {
      "provider": "openai",
      "configured": true,
      "ready": true,
      "model": "gpt-4.1-mini"
    },
    "tts": {
      "provider": "openai",
      "configured": true,
      "ready": true,
      "model": "tts-1",
      "voice": "alloy"
    },
    "realtime": {
      "provider": "openai",
      "configured": true,
      "ready": true,
      "model": "gpt-realtime-2",
      "voice": "alloy"
    }
  }
}
```

`booking.guidance_service_count` counts canonical active-provider, linked,
versioned, synced, active, non-archived AI-bookable services without requiring
the current provider connection snapshot to be complete. It is the source for
service menus and consultation. `booking.service_count` and
`booking.staff_count` additionally require the strict active provider,
selected location, completed snapshot generation, and `last_sync_at` fence used
for availability and booking. `booking.service_guidance.status` is one of
`recommendation_ready`, `catalog_only`, `consultation_disabled`, or
`catalog_unavailable`.
It reports whether the runtime can show the canonical guidance catalog and
whether enough owner-approved consultation profiles exist to make a
personalized recommendation. Guidance may be ready while
`request_capture_ready=false` or `automated_booking_ready=false`; that state
permits consultation but no scheduling call beyond the capability proved by
the selected authority. The legacy `booking` object remains external-provider
guidance and Square diagnostic evidence. It does not gate owner-managed or
internal-calendar readiness. Guidance is independent of Twilio transport
readiness.

`POST /api/salons/:id/voice/semantic-check`

Runs an authenticated, owner-scoped OpenAI semantic-contract probe using both
the strict `full_turn` and compact `guidance_turn` schemas with the configured
reply model used by live turn interpretation. The returned fingerprint covers
the ordered schema set. The synthetic requests contain no caller transcript, create no
conversation turn, does not mutate a booking draft, and cannot call
availability or POS booking tools.

Provider rejection is returned as `200` with `verified=false` so the endpoint
can be used as an operational diagnostic. Diagnostics are restricted to safe
bounded fields; provider response bodies and error messages are never exposed.

```json
{
  "provider": "openai",
  "configured": true,
  "verified": false,
  "schema_fingerprint": "sha256:4d8f3a1c2b7e9f01a2345678",
  "request_id": "req_123",
  "diagnostics": {
    "failure_stage": "turn_interpretation_response",
    "http_status": "400",
    "http_status_class": "4xx",
    "error_type": "invalid_request_error",
    "error_code": "invalid_json_schema",
    "error_param": "text.format.schema",
    "schema_fingerprint": "sha256:4d8f3a1c2b7e9f01a2345678"
  }
}
```

`configured=false` means the salon-scoped OpenAI integration is not enabled or
does not have the required model/key configuration. A successful live-contract
request set returns `verified=true`. A successful probe also closes the matching
local semantic-contract circuits for the same salon and current
provider/model/schema configurations.

`POST /api/salons/:id/voice/semantic-evaluate`

Runs one authenticated owner-scoped semantic scenario through the same
salon-scoped turn model used by live phone and simulator turns. This is a
read-only evaluation surface: it creates no conversation session or transcript,
does not call availability or booking tools, and cannot produce a confirmed
appointment. The request is bounded to 100 catalog services, 500 aliases, 100
categories, and 100 staff. Every selected, pending, draft, alias, category, and
party-group reference must point into the supplied catalog or the request is
rejected before the provider call.

```json
{
  "scenario_id": "guidance_catalog-001",
  "channel": "phone",
  "customer_message": "Could you walk me through the services you offer?",
  "expected_input": "caller_goal",
  "semantic_contract": "guidance_turn",
  "recognizable_guidance_actions": ["book", "service_catalog", "consultation", "salon_question", "name_service", "human_handoff", "reschedule", "cancel"],
  "booking_action": "book",
  "catalog_services": [
    {"service_id": "svc_luna", "service_name": "Luna Renewal", "category_id": "cat_ritual", "category_name": "Signature Ritual"}
  ],
  "catalog_service_aliases": [
    {"service_id": "svc_luna", "alias": "moon refresh"}
  ],
  "catalog_categories": [
    {"category_id": "cat_ritual", "category_name": "Signature Ritual", "aliases": ["renewal"], "service_ids": ["svc_luna"]}
  ]
}
```

The response contains `scenario_id`, the validated structured `result`, and
`duration_ms`. Provider-disabled returns `503`; caller-context timeout returns
`504`; provider failure returns `502`. Provider response bodies and secrets are
never exposed.

The generated corpus and deterministic review live at
`backend/modules/conversation/testdata/receptionist_semantic_scenarios.json`
and `receptionist_semantic_review.json`. The schema-v2 corpus contains 196
authored base utterances: each has an exact phone and simulator instance, then
transparent generated delivery variants expand the set to 1,000 single-turn
semantic-contract scenarios. The 100 review records are deterministic corpus,
source-of-truth, state, safety, and hardcoding audits; they are not model
executions and are not semantic pass results. Regenerate and validate them with:

```bash
cd backend
GOCACHE=/private/tmp/manleai-go-cache go run ./cmd/conversation-eval-corpus
GOCACHE=/private/tmp/manleai-go-cache go run ./cmd/conversation-eval -mode offline
```

Offline mode writes
`receptionist_semantic_offline_report.json` with
`contract_validated_count=1000`, `model_evaluated_count=0`, `passed_count=0`,
and `not_run_count=1000`. It proves corpus and scoring-contract validity only;
it does not claim that a model passed any scenario.

Live evaluation requires an authenticated owner token file and deliberately
does not read provider configuration from environment files:

```bash
GOCACHE=/private/tmp/manleai-go-cache go run ./cmd/conversation-eval \
  -mode live \
  -api-base https://example.com/api \
  -salon-id SALON_UUID \
  -token-file /secure/path/owner-token \
  -scenario-ids guidance_catalog-002,guidance_catalog-022,guidance_consultation-001,guidance_consultation-002,guidance_consultation-021,guidance_consultation-022 \
  -output /tmp/conversation-eval-report.json
```

Live mode requires `-output` and is hard-capped at 12 selected scenarios. Use
`-scenario-ids id-1,id-2` for targeted canaries or `-limit N` for a bounded
prefix. `-sample-per-family N` remains available for deterministic offline
subsets, but the current 14-family corpus exceeds the live cap even at one per
family. The unselected 1,000-case corpus cannot be sent as a paid live batch.
Selectors cannot be combined with each other or with `-limit`.
`-transient-retries N` permits zero to three
evaluator-only retries for endpoint `502`, `503`, or `504`; semantic mismatches
are never retried. Reports retain the validated model result and duration for
every pass and failure and expose both `transient_retry_count` and
`recovered_transient_count`, so infrastructure recovery cannot be mistaken for
semantic quality. This evaluator policy does not alter runtime phone or
simulator retry behavior.

For the separately bounded direct-model pilot, the generated artifact is
`backend/modules/conversation/testdata/receptionist_semantic_pilot_50.json`.
It contains 50 directly authored executions across 45 distinct nail-salon
situations, with five core situations represented once for phone and once for
simulator. It contains no generated paraphrase filler. The operational-question
slice covers distinct hours, staff, and policy meanings rather than spending
all three pilot positions on hours; its staff and policy cases include
non-native/ASR-like wording. Direct-model mode calls
the stored OpenAI model once for recognition, then lets the production
Conversation Service decide whether that turn needs a guarded style reply, a
profile-backed consultation question, or no output-model call. It does not
force a universal second rewrite. Review batches contain at most five retained
outputs; the complete 50 therefore has 10 review rounds. With no retries the
complete-run hard ceiling remains 110, while operational/terminal turns may use
fewer calls. Evaluation contract `production-flow-v9` rejects final replies
that expose any dynamic fixture identifier, uses separate typed salon-local
`hour` and `minute` model fields so the provider never computes minutes after
midnight, converts those clock components before production slot filtering,
and deterministically verifies every retained offered slot against
the recorded `before`, `after`, or `exact` constraint. It also treats expected
consultation extraction as exact, rejects invented booking/completion flags,
and rejects positive consultation mutations not represented in the same-turn
structured snapshot. Protocol `unknown` is normalized to field absence, and
guidance actions remain the only initial workflow-transition authority.
Reviewer contract `evidence-review-v9` explicitly checks
those consultation facts, the recorded local slot minutes and timezone, as well
as machine-facing labels, silent service/staff/date/time mutations, and
unnecessarily broad hours answers. Version 9 also accepts explicit multi-turn
journey evidence and requires the reviewer to inspect every retained
transcript/state/tool turn rather than only the terminal reply.

Direct-model mode is a local CLI workflow, not an API endpoint. Before any
model call, the supplied salon ID selects both the encrypted OpenAI row in
`salon_integration_configs` and a zero-model-call database readiness preflight.
The preflight requires `service_guidance.status=recommendation_ready` and
records guidance/ready-profile counts plus the separate provider/booking state.
`ResolveOpenAIConfigStrict` fails closed if that
database row is absent, disabled, undecryptable, or incomplete; it never falls
back to environment provider values. Scenario state, catalogs, aliases,
consultation profiles, staff, and availability are isolated evaluation
fixtures. The runner does not use an owner token, does not create conversation
records, does not call the internal semantic-evaluation API, and blocks every
booking, reschedule, or cancellation write. Synthetic availability may be
returned to exercise the caller-facing flow, but it is reported as a tool
attempt and never reaches the POS. Direct reports therefore set
`context_source=isolated_fixture` and `runtime_readiness_verified=false`; the
separate `runtime_preflight` object is database evidence, not proof that the
fixture scenarios executed against that salon's runtime catalog.

Run the exact pilot with an explicit paid-call ceiling and report path:

```bash
cd backend
GOCACHE=/private/tmp/manleai-go-cache go run ./cmd/conversation-eval \
  -mode direct-model \
  -salon-id CONFIG_SALON_UUID \
  -max-model-calls 110 \
  -transient-retries 0 \
  -output /tmp/conversation-eval-pilot-50.json
```

Run the approved ten-scenario canary before the complete pilot:

```bash
GOCACHE=/private/tmp/manleai-go-cache go run ./cmd/conversation-eval \
  -mode direct-model \
  -salon-id CONFIG_SALON_UUID \
  -scenario-ids pilot-003,pilot-007,pilot-014,pilot-015,pilot-019,pilot-028,pilot-032,pilot-038,pilot-042,pilot-050 \
  -max-model-calls 22 \
  -transient-retries 0 \
  -output /tmp/conversation-eval-canary-10.json
```

The complete pilot corpus is contract-validated before selectors are applied.
`-scenario-ids`, `-limit`, and `-sample-per-family` are mutually exclusive.
Direct review round count is derived from the selected results in batches of at
most five, so the ten-scenario canary performs two review calls.

The default checkpoint is
`/tmp/conversation-eval-pilot-50.json.checkpoint.json`. It is written atomically
with owner-only permissions before and after every model call. A matching
checkpoint resumes completed recognition, service-selected output generation,
and review work without repeating it. The run key includes explicit evaluation
and reviewer contract versions, so a changed harness cannot resume incompatible
evidence. If a process stops while a paid call is marked in flight, resume
fails closed instead of silently repeating a possibly billed request. Raising
`-transient-retries` from zero permits only bounded transport/provider retries;
every retry consumes the same `-max-model-calls` budget and semantic mismatches
are never retried. The report retains per-scenario structured recognition,
backend-safe reply, final reply, structured backend route/transition/reply-source
evidence, booking-confirmed and provider-ID facts, owner-request handoff mode,
model calls, token usage, tool attempts, durations, failures, and the selected
review records. Repository tests validate the
harness and fixtures without making a paid model call; they do not claim that
the current stored model passes the pilot. Review booking safety uses backend
facts: the absence of a provider booking ID is safe when the backend did not
confirm, and a recognition-only `booking_requested` flag is not confirmation.
An `owner_request` handoff must not be rewritten as a live transfer. Each review
round passes only when all five scores are at least 4/5; the report counts review
passes and failures separately, and the CLI exits unsuccessfully when any review
round fails. These model scores remain a secondary review layer: deterministic
identifier-leak and booking-safety checks can fail a scenario before reviewer
scoring.

The independent multi-turn salon suite uses a different local command and
artifact:

```bash
cd backend
GOCACHE=/private/tmp/manleai-go-cache go run ./cmd/conversation-eval-real \
  -mode structural \
  -corpus modules/conversation/testdata/receptionist_real_salon_100.json \
  -output modules/conversation/testdata/receptionist_real_salon_structural_report.json

GOCACHE=/private/tmp/manleai-go-cache go run ./cmd/conversation-eval-real \
  -mode deterministic-runtime \
  -corpus modules/conversation/testdata/receptionist_real_salon_100.json \
  -output /tmp/receptionist-real-salon-runtime.json
```

The first command validates exactly 100 independently authored journeys and
must retain `passed=false`, `model_executed_count=0`, and
`review_passed_count=0`. The second keeps one isolated production Conversation
Service session across every turn, executes all 100 with retained scripted
semantics, and retains complete transcripts, state transitions, tool attempts,
handoff, confirmation facts, turn route/reason, deterministic coverage,
interpreter outcome, answer source/reason, router intent, and reply source/policy.
Corpus, production-flow, and transcript-review
contract versions are part of the run key, including the shared reviewer
contract, so an incompatible checkpoint cannot be resumed. It still cannot
claim a model pass. A paid
canary is explicit and database-configured:

```bash
GOCACHE=/private/tmp/manleai-go-cache go run ./cmd/conversation-eval-real \
  -mode live-canary \
  -config-salon-id CONFIG_SALON_UUID \
  -max-model-calls 60 \
  -transient-retries 0 \
  -output /tmp/receptionist-real-salon-live-10.json
```

The config salon selects only the encrypted database OpenAI integration; it
does not own the isolated fixtures. Exactly ten corpus journeys are marked as
live canaries: `advice-001`, `consult-001`, `question-003`, `question-005`,
`question-008`, `question-010`, `booking-001`, `repair-003`, `safety-001`, and
`failure-002`. They cover consultation, service catalog, informational
hours/staff/policy, booking, an informational detour during booking, safety,
and dependency failure. The runner stops on the first journey failure, never
exceeds 60 combined recognition/reply/consultation/review calls, and cannot
pass until all ten journeys are model-executed and both five-journey
multi-turn reviews pass.
No live canary is run by repository tests or corpus generation. Failure
ownership and the prompt-change gate are documented in
`docs/operations/conversation-semantic-quality.md`.

`POST /api/voice/twilio/:route_id/incoming`

Public tenant-bound webhook for a new inbound call. The opaque `route_id`
locates only an enabled Twilio integration row and returns only its tenant ID;
the request then binds provider database context to that tenant, reloads the
exact row under RLS, and verifies `To`, `AccountSid`, existing CallSid identity,
and the official Twilio signature over the exact stored public URL and every
form parameter. Only the resulting server-owned verified route proof may create
or exactly reuse a phone session. All lookup, identity, config, decrypt, and
signature failures return the same `403 TWILIO_WEBHOOK_REJECTED` with no
tenant-scoped side effect.

Expected Twilio form fields include:

```txt
CallSid
AccountSid
From
To
```

The shared `/api/voice/twilio/incoming|turn|recording|stream*` routes remain
mounted only for expand-release migration and rollback. They still use the
legacy phone-routing path and therefore are not evidence of the tenant-bound
guarantee. A later contract migration removes them only after every live
tenant has a matching verified inbound event.

`POST /api/voice/twilio/:route_id/turn`

Public Twilio Programmable Voice webhook for gathered speech turns. Requires a valid `X-Twilio-Signature`. The webhook finds the `phone` session by `CallSid`, appends the customer speech to the transcript, and returns TwiML. The response continues gathering only while the session remains active. Completed, fallback, and handoff outcomes return final TwiML and hang up.

Expected Twilio form fields include:

```txt
CallSid
From
To
SpeechResult
```

`GET /api/salons/:id/conversation-sessions/:session_id/realtime-events?limit=100&offset=0`

Owner-authenticated debug endpoint for the selected phone call session. It
returns a page from the latest realtime lifecycle and timing audit events
recorded in `voice_webhook_events`, scoped by salon ownership and session ID
and ordered chronologically within the page. `limit` defaults to 50 and is
bounded to 100; `offset` defaults to 0. `has_more=true` means older events are
available at `offset + limit`. The Calls dashboard follows those pages,
deduplicates immutable event IDs, and sorts the complete selected-call timeline
chronologically. The response intentionally exposes only debug-safe fields
extracted from provider payloads, including transcript admission decisions,
configured/effective audio handling, confidence/VAD measurements, response correlation IDs, and
salted canonical-output hashes. Raw transcript, audio, and provider payloads
are not returned. Twilio stream callbacks are persisted from an explicit
allowlist (`stage`, stream identity/event, terminal state, and stable error
code), not by copying the callback form. Provider/transport failures use a
fixed message and bounded typed diagnostic fields; raw OpenAI/Twilio messages
are neither persisted nor returned.

Example response:

```json
{
  "events": [
    {
      "id": "b87c6...",
      "provider": "twilio",
      "provider_call_id": "CA2a47b5e8e5777820f84d6447132b1574",
      "event_type": "realtime_failed",
      "stage": "openai_spoken_fact_mismatch",
      "stream_sid": "MZ...",
      "error": "Realtime operation failed.",
      "diagnostics": {
        "request_id": "manleai-reply-2",
        "response_id": "resp_...",
        "match_classification": "canonical_fact_mismatch",
        "expected_hash": "d0be2dc421be4fcd",
        "actual_hash": "78b3fbc972cf74f1"
      },
      "created_at": "2026-06-29T00:13:10Z"
    }
  ],
  "limit": 100,
  "offset": 0,
  "has_more": true
}
```

`POST /api/voice/twilio/:route_id/recording`

Public Twilio Programmable Voice webhook for recording-mode turns when external STT is configured. Requires a valid `X-Twilio-Signature`. The webhook downloads the Twilio recording, sends audio through the configured STT provider behind `modules/voice`, then routes the resulting text through the same conversation engine and booking service. STT failures produce safe reprompt/fallback behavior and do not fabricate customer intent.

Expected Twilio form fields include:

```txt
CallSid
AccountSid
From
To
RecordingUrl
```

`GET /api/voice/twilio/:route_id/stream`

Public Twilio Media Streams WebSocket endpoint for realtime audio mode. The endpoint is not configured directly in Twilio Console; the incoming webhook returns `<Connect><Stream>` with signed custom parameters for the existing call session. The stream forwards Twilio g711 audio frames to the configured OpenAI Realtime adapter for VAD and transcription, then routes accepted completed transcripts through the same backend conversation engine and booking service. GA sessions request `item.input_audio_transcription.logprobs`; `automatic`, `standard`, and `strong_noise_rejection` enable near-field input noise reduction, while `minimal_processing` does not force input noise reduction. Admission is policy-aware and fail-closed: missing/invalid confidence metadata, low mean confidence, low-tail token confidence, or transcript density inconsistent with VAD duration is audited as `transcript_rejected_low_confidence` and leaves the conversation draft unchanged. Automatic mode begins with standard admission and, after structured low-confidence or VAD-coherence evidence, switches the current call to stronger admission for later turns. It does not inspect transcript wording or infer the caller's location; missing provider metadata is not treated as noise evidence. Replayed transcript-completion events with the same provider item/transcript key are ignored before admission, recovery, or conversation mutation. Each admitted transcript advances an input generation; nonterminal backend replies from superseded generations are discarded before speech. Consecutive rejected transcripts use bounded in-stream recovery: a short retry, a one-answer retry that repeats the last backend-approved question, then background-noise coaching. Recovery output waits behind any accepted backend turn and is removed when that backend reply becomes available. A fourth consecutive rejection calls a typed conversation action instead of fabricating a customer message; it creates one `voice_input_unintelligible` owner callback handoff when the session has a callable caller number, or completes the call with a quieter-place retry instruction when it does not. The handoff uses a stable per-session event key so repeated or concurrent execution does not create duplicate owner work, and it must persist before callback wording is spoken. A successfully admitted transcript resets the rejection streak. Confidence rejection never switches the call to recording/gather; that fallback remains reserved for terminal Realtime/provider failures. Accepted and rejected transcript timing events record PII-free `item_id`, `input_generation`, decision/reason, configured `profile`, `effective_profile`, `adaptive`, `runtime_action`, `audio_quality_signal`, mean/min logprob, token count, VAD duration, `rejection_streak`, and `recovery_action` when available; timing payloads do not store transcript or audio bodies. Transcription steering uses concise salon, catalog, pending-candidate, and alias keyword lists instead of example booking sentences.

With dashboard setting `speech_output_mode=streaming_tts`, backend-approved text is sent to the dedicated Speech API with raw PCM output. OpenAI PCM is signed little-endian mono audio at 24 kHz; the adapter incrementally applies a stateful anti-aliasing 3:1 FIR resampler and encodes raw PCMU 8 kHz frames. The bridge sends one bounded 200 ms startup block, then releases one 160-byte frame every 20 ms from a bounded queue. Queue saturation applies producer backpressure, so provider HTTP chunk speed cannot create a WebSocket burst or reorder/drop audio. A reply shorter than the startup target flushes when its Speech stream completes. Speech provider completion is recorded at `tts_stream_done`, while the reply remains active until the local playout queue drains and records `tts_playout_done`; only then may the typed scheduler start the next eligible reply or emit a terminal Twilio `mark`. Output kinds are ordered by workflow authority: terminal, current backend turn, initial reply, input recovery, then progress. No new TTS request starts while caller speech is active or a stopped-speech item is awaiting its transcript. Caller barge-in before or after startup clears local queued audio and Twilio playback, cancels streaming speech immediately even inside the legacy playback guard, and rejects late chunks by generation. Input-recovery speech has a four-second first-provider-byte budget; on expiry it is canceled so current backend output can advance. A terminal backend or typed handoff reply latches the call, clears nonterminal pending output, ignores later transcripts, and closes exactly once after playout, interruption, or timeout. Provider failure clears any partial playback and enters terminal fallback.

PII-free stages include `speech_stopped`, `transcript_admitted`, `backend_turn_start`, `backend_turn_done`, `tts_request_start`, `tts_first_provider_chunk`, `tts_first_byte_timeout`, `tts_startup_buffer_ready`, `twilio_first_media_sent`, `tts_stream_done`, `tts_playout_done`, and playback completion through the terminal mark. Speech diagnostics expose `input_generation`, `reply_kind`, stale-reply suppression, input/output sample rates, post-first-chunk producer wall/active/audio durations, producer rate excluding local backpressure, observed provider emit gap, producer backpressure duration/count, maximum queue depth, playout duration/frame/batch counts, underrun count, and WebSocket write latency. Backpressure metrics distinguish an upstream provider gap from time intentionally spent waiting for local queue capacity. `backend_turn_done` also exposes whitelisted substage durations for route/config resolution, session load, answer-context load, turn routing, turn interpretation, availability/POS calls, and turn persistence. Answer-context timing adds only bounded `answer_context_authority`, `answer_context_cache_status`, `answer_context_refresh_reason`, `answer_context_retry_reason`, `answer_context_attempts`, `answer_context_outcome`, and optional `answer_context_ready` fields so operators can distinguish cache hits, TTL/fence refreshes, readiness/concurrent retries, fail-closed refreshes, load failures, and retry exhaustion. Router diagnostics include `turn_route`, `turn_expected_input`, `turn_route_reason`, `turn_deterministic_coverage`, `turn_semantic_contract`, `turn_recognizable_guidance_actions`, `turn_available_guidance_actions`, `turn_guidance_action`, `service_guidance_capability`, `service_guidance_catalog_available`, `service_guidance_recommendation_ready`, `turn_model_service_count`, and `turn_model_staff_count`. `turn_interpreter_path`, `turn_interpreter_outcome`, `turn_interpreter_ms`, and `turn_interpreter_schema_fingerprint` distinguish and time a skipped model call, accepted structured interpretation, timeout, provider failure, empty or invalid output, confidence rejection, and catalog rejection. Provider failures may also expose the bounded fields `turn_interpreter_provider`, `turn_interpreter_failure_stage`, `turn_interpreter_http_status`, `turn_interpreter_http_status_class`, and `turn_interpreter_request_id`. Request and response bodies are never copied into these diagnostics. These diagnostics contain no transcript, audio, customer, salon ID, raw error, or provider-secret values. Speech output failures close through the signed recording/gather fallback without changing booking state or creating a second booking attempt.

`speech_output_mode=buffered_realtime` is a legacy fallback. Only that mode uses Realtime `response.create`, response identity binding, complete output-transcript validation, and release after `response.done`. Operational facts are never allowed to bypass backend/POS ownership in either mode. If realtime configuration is missing, voice status falls back to the recording or gather path.

`POST /api/voice/twilio/:route_id/stream/status` and
`POST /api/voice/twilio/:route_id/stream/fallback` use the same tenant route,
Account SID, exact signature, and existing CallSid ownership. Twilio Stream
status callbacks do not include `To`; for that endpoint only, the durable
tenant-bound call session supplies the previously verified inbound number.
The callback still fails closed if the route, Account SID, CallSid tenant, or
stored number does not agree.

`GET /api/voice/audio/:id`

Public short-lived audio output endpoint for Twilio `<Play>` responses. Access
requires both `expires=<unix-seconds>` and `signature=<base64url-hmac>` query
parameters. The HMAC-SHA256 capability is signed with the salon's current,
database-backed Twilio auth token and binds the exact audio ID, salon ID,
telephony provider, provider call ID, call-session ID, and expiry. Only expiry
and signature appear in the query; salon, call, session, phone, provider, and
secret values are never placed in the URL.

The persisted `voice_audio_outputs.expires_at` value is authoritative and the
capability may never exceed either that value or 15 minutes from verification.
The handler loads non-media metadata first, verifies the database expiry and
capability, then loads audio bytes. Valid capabilities may be fetched more than
once during their TTL. Rotating the stored Twilio auth token invalidates
outstanding URLs. Missing stored signing configuration produces no unsigned
audio URL, so TwiML retains the safe text-speech fallback. Missing, unknown,
tampered, expired, repository-failed, and signing-config-failed requests all
return the same non-enumerating `404 VOICE_AUDIO_UNAVAILABLE` response.

The current phone path never confirms an appointment from conversation state
alone. `owner_manual` remains pending owner review;
`manleai_calendar` requires the atomic durable internal root/attempt/child
evidence; and Square-backed `external_provider` requires provider success plus
the required booking ID/version and matching appointment evidence.

## POS Provider Switch Readiness

`GET /api/salons/:id/pos/provider-switch-readiness`

Returns the active POS provider, installed adapters, unavailable adapter gate,
provider mapping counts, and readiness checks. This endpoint is read-only in
the current production release. It does not activate a new provider or create fake provider
support.

```json
{
  "salon_id": "...",
  "active_provider": "square",
  "active_provider_label": "Square Appointments",
  "installed_providers": [
    {
      "provider": "square",
      "label": "Square Appointments",
      "installed": true,
      "active": true,
      "status": "active",
      "capabilities": {
        "service_upsert": false,
        "service_archive": false,
        "staff_upsert": false,
        "staff_archive": false,
        "customer_upsert": false
      }
    }
  ],
  "unavailable_providers": [
    {
      "provider": "",
      "label": "No alternate POS adapter installed",
      "installed": false,
      "active": false,
      "status": "disabled",
      "blocked_reason": "Square Appointments is the only native POS integration in the current production release."
    }
  ],
  "mapping": {
    "service_count": 4,
    "staff_count": 3,
    "customer_count": 12,
    "bookable_service_count": 4,
    "bookable_staff_count": 3,
    "linked_service_count": 4,
    "linked_staff_count": 3,
    "linked_customer_count": 6,
    "unmapped_service_count": 0,
    "unmapped_staff_count": 0,
    "sync_failed_count": 0
  },
  "checks": [
    {"key": "active_provider", "label": "Active provider selected", "complete": true},
    {"key": "alternate_adapter", "label": "Alternate provider adapter installed", "complete": false}
  ],
  "dry_run_booking_ready": true,
  "can_start_switch": false,
  "can_activate_provider": false,
  "blocked_reason": "No alternate production POS adapter is installed in this deployment."
}
```

`POST /api/salons/:id/pos/provider-switch-runs`

Creates a provider switch run record for a requested target provider. If the
target adapter is not installed, the run is persisted with `status=blocked` and
no import/match work is started. If a real target adapter is installed in a
future slice, the backend can populate service/staff/customer match candidates,
but activation still remains disabled in this phase. The current dashboard
import wizard shell does not call this endpoint while no alternate native POS
adapter is installed.

```json
{
  "to_provider": "future_provider"
}
```

Response:

```json
{
  "run": {
    "id": "...",
    "salon_id": "...",
    "from_provider": "square",
    "to_provider": "future_provider",
    "status": "blocked",
    "blocked_reason": "The requested POS provider adapter is not installed in this deployment.",
    "dry_run_ready": false,
    "can_activate": false,
    "match_summary": {
      "total": 0,
      "suggested": 0,
      "unmatched": 0,
      "conflicts": 0,
      "confirmed": 0,
      "skipped": 0
    },
    "matches": []
  }
}
```

`GET /api/salons/:id/pos/provider-switch-runs/latest`

Returns the latest switch run for the salon, or `{"run": null}` when no run has
been created.

`GET /api/salons/:id/pos/provider-switch-runs/:run_id`

Returns one owner-scoped switch run with its match summary and match rows.
Match rows are provider-neutral and may include `service`, `staff`, or
`customer` entities. There is intentionally no activation endpoint in the
current production release.

`GET /api/salons/:id/pos/provider-switch-runs/:run_id/dry-run-readiness`

Returns the dry-run checklist for one owner-scoped provider switch run. This
endpoint is read-only. It does not call a target POS provider, create
appointments, create provider links, mark `dry_run_ready=true`, or activate a
provider. In the current production release it remains blocked because no alternate native
POS adapter and no alternate-provider dry-run executor exist.

```json
{
  "run_id": "...",
  "salon_id": "...",
  "from_provider": "square",
  "to_provider": "future_provider",
  "status": "ready",
  "checks": [
    {"key": "target_adapter", "label": "Target provider adapter installed", "complete": false, "message": "The target POS provider adapter is not installed in this deployment."},
    {"key": "switch_run_reviewable", "label": "Switch run is reviewable", "complete": true},
    {"key": "imported_records", "label": "Imported provider records exist", "complete": false, "message": "Import records from a real alternate POS provider before dry-run checks can pass."},
    {"key": "matches_resolved", "label": "Match conflicts resolved", "complete": false, "message": "Resolve suggested, unmatched, or conflicting provider matches before dry-run."},
    {"key": "current_provider_booking_ready", "label": "Current provider booking readiness passed", "complete": true},
    {"key": "dry_run_execution_available", "label": "Alternate-provider dry-run execution available", "complete": false, "message": "Alternate-provider dry-run execution is not available in the current production release."}
  ],
  "can_run_dry_run": false,
  "dry_run_ready": false,
  "can_activate": false,
  "blocked_reason": "The target POS provider adapter is not installed in this deployment."
}
```

`PATCH /api/salons/:id/pos/provider-switch-runs/:run_id/matches/:match_id`

Updates one match review decision for an owner-scoped switch run. Allowed
`match_status` values are `confirmed`, `unmatched`, and `skipped`. Confirming a
match records the owner's review decision only; it does not create provider
links, run a dry-run booking, or activate a provider.

```json
{
  "match_status": "confirmed"
}
```

Response:

```json
{
  "run": {
    "id": "...",
    "status": "ready",
    "can_activate": false,
    "match_summary": {
      "total": 1,
      "suggested": 0,
      "unmatched": 0,
      "conflicts": 0,
      "confirmed": 1,
      "skipped": 0
    },
    "matches": [
      {
        "id": "...",
        "entity_type": "service",
        "canonical_entity_id": "...",
        "canonical_name": "Classic Manicure",
        "provider_entity_id": "...",
        "provider_name": "Classic Manicure",
        "match_status": "confirmed",
        "match_confidence": 100,
        "match_reason": "Owner confirmed this provider match."
      }
    ]
  }
}
```

## Square

`POST /api/integrations/square/webhook`

Public Square booking-event receiver. The handler verifies HMAC-SHA256 against
the exact dashboard-stored HTTPS notification URL plus raw request body, using
the encrypted salon-scoped signature key and constant-time comparison. Event
IDs are durably deduped before `200`; invalid signatures or bodies are rejected.
If both envelope and nested booking location IDs are present, they must be
identical before tenant routing. The worker fetches current Square booking truth
rather than trusting event payload status, and scheduled calendar repair remains
a backstop. Event and repair completions are claim-token fenced, so an expired
worker cannot complete or overwrite a newer lease. A locally
stored verifier configuration is not evidence of an active Square subscription
or successful recent delivery. Tenant routing requires an exact
merchant/location match and Square as the salon's active provider. Recoverable
connection states (`connected`, `syncing`, `active`, `error`, and
`expired_token`) may enqueue a valid signed event; `not_connected` and
`disabled` connections cannot.

Webhook target and scheduled-repair selection intentionally does not read the
salon's current scheduling-authority setting. It remains Square
provider/connection-scoped so historical external-provider appointments can
continue to converge after a later switch. Booking-calendar persistence,
matching, and reconciliation explicitly require
`scheduling_authority=external_provider` and skip or protect internal-origin
rows; webhook processing therefore cannot manufacture an internal-authority
appointment mutation.

`GET /api/platform/tenants/:tenant_id/operations/square-webhooks?status=&limit=25&offset=0`

Returns authenticated, Platform-Operations-scoped Square webhook evidence. The
optional `status` filter accepts `pending`, `processing`, `failed`,
`dead_letter`, or `succeeded`; an empty filter returns every persisted event,
including read-only `ignored` events. `ignored` is intentionally not a filter
value and never authorizes requeue. `limit` defaults to 25, is capped at 100,
and `has_more` is derived from one extra row.

```json
{
  "webhook_configured": true,
  "events": [
    {
      "id": "webhook-record-uuid",
      "event_type": "booking.updated",
      "processing_status": "dead_letter",
      "processing_attempts": 10,
      "requeue_count": 0,
      "last_error_class": "dependency",
      "last_error_code": "SQUARE_WEBHOOK_ATTEMPTS_EXHAUSTED",
      "can_requeue": true,
      "next_attempt_at": "2026-07-24T10:00:00Z",
      "delivered_at": "2026-07-24T09:50:00Z",
      "dead_lettered_at": "2026-07-24T10:00:00Z",
      "created_at": "2026-07-24T09:50:00Z",
      "updated_at": "2026-07-24T10:00:00Z"
    }
  ],
  "metrics": {
    "pending": 0,
    "processing": 0,
    "failed": 0,
    "dead_letter": 1,
    "succeeded_recent": 12,
    "recent_window_hours": 168,
    "last_delivered_at": "2026-07-24T09:50:00Z",
    "last_succeeded_at": "2026-07-24T09:45:00Z"
  },
  "calendar_repair": {
    "relevant": true,
    "status": "degraded",
    "repair_attempts": 2,
    "last_error_class": "dependency",
    "last_error_code": "SQUARE_CALENDAR_REPAIR_FAILED",
    "next_repair_at": "2026-07-24T10:05:00Z",
    "updated_at": "2026-07-24T10:00:00Z"
  },
  "limit": 25,
  "offset": 0,
  "has_more": false
}
```

`webhook_configured` is a non-secret, salon-scoped readiness boolean derived
only from the stored notification URL and encrypted signature-key presence. It
does not use environment fallback and does not prove that Square has created a
subscription or delivered an event.

The response is operationally safe: it omits Square merchant/location/booking
identifiers, raw webhook payloads, signature material, processing claim tokens,
provider responses, customer data, and raw errors. Metrics are independent of
the current page and `succeeded_recent` uses the returned
`recent_window_hours`. Calendar-repair health is a separate backstop and is not
OAuth, catalog-sync, scheduling-authority, or appointment-confirmation state.

`GET /api/platform/tenants/:tenant_id/operations/square-webhooks/:webhook_event_id`

Returns `{"event": {...}}` with the same safe event fields for one tenant-owned
record. The dashboard derives its timestamp timeline only from those returned
fields; this endpoint does not expose the raw delivery or provider payload.

`POST /api/platform/tenants/:tenant_id/operations/square-webhooks/:webhook_event_id/requeue`

```json
{
  "action_key": "stable-owner-action-uuid"
}
```

Requeues only when the backend event response has `can_requeue=true`. The
action key is stable for one owner intent. Exact reuse for the same event
returns the current safe event and response header
`X-Idempotent-Replay: true`; changed reuse returns
`409 SQUARE_WEBHOOK_ACTION_CONFLICT`. A new accepted action returns the header
as `false`. Requeue clears only the bounded processing failure state and queues
the existing durable event; it does not create an appointment or claim that a
webhook, repair, or booking succeeded.

Errors are `400 SQUARE_WEBHOOK_OPERATIONS_INVALID`,
`404 SALON_NOT_FOUND`, `404 SQUARE_WEBHOOK_EVENT_NOT_FOUND`,
`409 SQUARE_WEBHOOK_ACTION_CONFLICT`,
`409 SQUARE_WEBHOOK_REQUEUE_BLOCKED`, or sanitized
`500 SQUARE_WEBHOOK_OPERATIONS_FAILED`.

`GET /api/platform/tenants/:tenant_id/technical/square/connect-url`

Returns a Square OAuth URL and state.

All authenticated Square technical operations take the tenant only from the
fixed Platform route and require `technical.read` or `technical.write`. They do
not accept a salon selector in query or JSON and never impersonate the owner.

`GET /api/integrations/square/callback?code=<code>&state=<state>`

Exchanges the Square OAuth code and stores encrypted tokens.

`GET /api/platform/tenants/:tenant_id/technical/square/status`

Returns the Square connection, recent sync logs, and AI booking readiness checks,
including `business_hour_period_count` for the selected Square location import.
Readiness includes the Platform-authorized tenant's current
`scheduling_authority`.
`can_test_booking` and `can_enable_ai_booking` require
`scheduling_authority=external_provider` in addition to their existing Square
gates. The readiness-check list includes `atomic_slot_commit`; V87 completes it
only for current buyer-level single-create evidence. The response also exposes
`automatic_single_create`, `automatic_reschedule`, `automatic_party_create`,
`resource_capacity`, `write_permission_mode`, `reconnect_required`,
`evidence_current`, `evidence_verified_at`, `evidence_expires_at`,
`blocker_code`, `connection_capability_version`, and
`integration_config_version`. Square reschedule, party, and resource-capacity
values are always false. `can_enable_ai_booking` is a backward-compatible
single-create readiness signal, not a lifecycle or party capability. There is
no UI/API bypass for this capability evidence. `can_cancel_test_booking` remains based on the persisted latest external
test appointment so cleanup is not orphaned after a later authority switch.
Readiness also includes `booking_write_blocked`, `booking_write_blocked_code`,
`booking_write_blocked_reason`, and `booking_write_blocked_at` when the latest
Square create-booking permission or write-unsupported error still has not been
cleared by a later successful Square test booking.

Connection, location, sync, test-booking, and cancel responses never include a
Square response body or error detail. Provider errors are classified to stable
POS codes and fixed safe messages before persistence and before the handler
builds an authenticated response. Migration V63 applies the same contract to
legacy POS/Square error text and payloads.

`GET /api/platform/tenants/:tenant_id/technical/square/locations`

Lists Square locations through the Square adapter.

`POST /api/platform/tenants/:tenant_id/technical/square/select-location`

```json
{
  "location_id": "..."
}
```

`POST /api/platform/tenants/:tenant_id/technical/square/sync`

Imports Square services, staff, selected-location business hour periods, and
customers into local canonical tables. The button label is `Sync` in the owner
dashboard because the action is no longer limited to services and staff.
Calendar appointment imports are handled separately by
`POST /api/salons/:id/calendar/sync`, scoped to a requested calendar range.
Catalog and customer pagination continue until cursor exhaustion and reject a
repeated cursor. Services are imported only when the Square variation is
available for booking and has a positive normalized duration. Staff are the
intersection of active selected-location Team members and bookable Booking
Profiles. The provider snapshot is applied transactionally; missing previously
imported rows are disabled/unmapped, and sync never automatically re-enables an
owner-disabled `ai_bookable` flag.

The request body is empty. Provider-write tenant quota is consumed before the
Square call; exhaustion returns `429 TENANT_QUOTA_EXCEEDED` with `Retry-After`.

```json
{
  "ok": true,
  "summary": {
    "services_synced": 12,
    "staff_synced": 4,
    "business_hour_periods_synced": 8,
    "customers_synced": 250,
    "customers_skipped": 0
  }
}
```

`POST /api/platform/tenants/:tenant_id/technical/square/scheduling-capability/re-evaluate`

```json
{
  "action_key": "square-scheduling-capability-review-uuid",
  "expected_connection_capability_version": 7,
  "expected_integration_config_version": 12
}
```

Re-evaluates Square scheduling safety from persisted tenant-scoped connection
and integration configuration only. It makes no Square create, update, cancel,
or other provider request. The client cannot submit capability booleans.
`APPOINTMENTS_WRITE` without `APPOINTMENTS_ALL_WRITE` can produce current
buyer-write evidence for concrete-staff single create. Seller-write, missing,
unknown, or malformed scopes fail closed; reconnect or any booking-relevant
connection/config/location/API-version change makes prior evidence stale.
Exact action replay returns the same result with
`X-Idempotent-Replay: true`; changed payload reuse, stale expected versions,
and cross-tenant access fail without replacing immutable historical evidence.
Only Platform Admin or an exactly delegated Platform Ops principal with
`technical.write` can call this route, and the actual Platform actor is audited.

`POST /api/integrations/square/test-booking` (retired; not registered)

Safe-retry example; an initial write omits `retry_of_attempt_id`:

```json
{
  "operation_key": "square-test-booking-4fa6c4e2",
  "retry_of_attempt_id": "a2a86a35-95d4-4ad1-a3ab-0dc4c71b1163",
  "availability_quote_id": "availability-quote-uuid",
  "slot_fingerprint": "sha256-hex",
  "salon_id": "...",
  "customer_name": "ManleAI Test Customer",
  "customer_phone": "+13125550199",
  "customer_email": "",
  "service_id": "...",
  "staff_id": "...",
  "start_time": "2026-06-11T15:00:00Z",
  "notes": "AI booking readiness test."
}
```

Creates a real Square booking through the authority-neutral scheduling facade,
which reaches the provider-neutral booking service only after resolving the
operation origin. A response-loss replay is read through provider-free
`ReplayCreate`. If no replay exists, the read-only
`ResolveCreateSchedulingAuthority` resolves the operation key and optional
retry attempt with the same origin-equality and tenant checks used by create
dispatch; only origin-free work falls back to the current salon mode. A valid
persisted external safe retry can therefore continue after a later switch,
subject to the underlying Square provider-readiness gates, even though the
public readiness response keeps `can_test_booking=false` for the internal
current mode. A current internal authority rejects an origin-free new Square
test creation with
`409 SCHEDULING_AUTHORITY_NOT_READY` before readiness/provider dispatch. The
selected slot must come from the availability endpoint and include current
quote evidence. Square is accepted by the `atomic_slot_commit` gate only for
current V87 buyer-write single-create evidence and a concrete staff assignment.
Seller-write or stale evidence is rejected before dispatch. A capability-
verified provider endpoint returns `201` only when it returns an accepted booking ID;
returns `202` for unconfirmed `pos_pending`, `provider_pending`, or
`fallback_pending` outcomes.

`retry_of_attempt_id` is omitted on the initial write. After a definitive
failure returns a safe `fallback_pending` attempt, an explicit retry uses a new
operation key and supplies that exact attempt ID. A response-loss replay keeps
the same operation key and logical payload, including the same retry lineage
when the lost request was itself a retry; a freshly revalidated quote proof does
not turn that replay into a new logical operation. The availability quote remains single-use for
unrelated requests; the repository can atomically transfer its consumer only
when `consumed_by_attempt_id` exactly matches `retry_of_attempt_id` and the
quote has not expired.

The test-booking response and `latest_test_booking` include `operation_type`,
`provider_outcome`, `retry_policy`, `reconciliation_status`, `can_retry`, and
`retry_blocked_reason`. The dashboard selects retry lineage only from a safe
fallback with the matching operation type (`book` for create and `cancel` for
cancel), including after a page reload. It disables another test write while
the previous operation is `pos_pending`, in-flight, or requires
reconciliation. Square status reads recover only stale external-provider test
operation leases before returning `latest_test_booking`.

Scheduling-authority failure returns the sanitized
`409 SCHEDULING_AUTHORITY_NOT_READY` message “Scheduling is not ready for this
salon.” Readiness and unknown Square gate failures likewise use bounded public
messages; the handler does not expose wrapped internal diagnostic text.

`POST /api/integrations/square/cancel-test-booking` (retired; not registered)

Safe-retry example; an initial cancellation omits `retry_of_attempt_id`:

```json
{
  "operation_key": "square-test-cancel-b8c8f001",
  "retry_of_attempt_id": "069171f5-27a7-44ea-a759-8a986a10cd05",
  "salon_id": "...",
  "appointment_id": "...",
  "reason": "AI booking readiness test cleanup"
}
```

Cancels the latest Square test booking through the authority-neutral scheduling
facade. `ReplayCancel` is a provider-free persisted-history lookup; an actual
cancellation write resolves and validates the operation/retry/target origins
before reaching the provider-neutral booking service. An external-origin test
appointment can therefore be replayed or cancelled after the salon's current
authority changes. Returns `200` when Square
cancels the booking, or `202` with `fallback_pending` when POS cancellation
fails. As with create, the optional `retry_of_attempt_id` is sent only for an
explicit safe retry and must identify the matching prior `cancel` attempt; the
retry uses a new operation key.

`POST /api/platform/tenants/:tenant_id/technical/square/ai-booking/enable`

```json
{
  "action_key": "platform-ai-runtime-enable-uuid",
  "expected_version": 3
}
```

Sets `salons.ai_enabled=true` only when the tenant's current
`scheduling_authority` is `external_provider`, Square is connected, a location
is selected, services/staff/business hours are synced, and at least one service
and staff member are AI-bookable, and buyer-level single-create
`atomic_slot_commit` evidence is current. This does not enable automated Square
reschedule, party create, or resource-capacity execution. A current internal authority returns
`409 SCHEDULING_AUTHORITY_NOT_READY`. Square test booking create/cancel remains
an optional POS write smoke test and is not an AI enablement gate.

`POST /api/platform/tenants/:tenant_id/technical/square/ai-booking/disable`

```json
{
  "action_key": "platform-ai-runtime-disable-uuid",
  "expected_version": 4
}
```

Sets `salons.ai_enabled=false`. Both actions use V70 technical resource
versioning, exact replay, changed-request conflict detection, and actual
Platform actor audit.

## Customer Appointment SMS

Customer SMS is a separate, default-disabled owner policy and consent/delivery
ledger. A stored caller phone is not consent. `owner_manual` request messages
state that the request is pending owner review; confirmed/rescheduled/cancelled
copy is created only from the matching durable appointment lifecycle commit.
Every message includes STOP instructions.

- `GET /api/salons/:id/customer-sms-policy` and
  `PUT /api/salons/:id/customer-sms-policy` read/update the owner-scoped enable,
  quiet-hours, salon-timezone, and optimistic policy-version contract.
- `POST /api/salons/:id/customer-sms-consents/attest` requires
  `{"destination":"...","attested":true,"action_key":"..."}`. An unchecked
  or false attestation is invalid. Local conversation/owner evidence cannot
  lift `opted_out`; only a signed Twilio `OptOutType=START` callback can do so.
- `GET /api/salons/:id/appointments/:appointment_id/customer-notifications`
  and the corresponding
  `POST .../:delivery_id/requeue` expose the appointment child ledger.
- `GET /api/salons/:id/scheduling-requests/:request_id/customer-notifications`
  and the corresponding
  `POST .../:delivery_id/requeue` expose `request_received` evidence inside the
  owner-review request workflow.

Reads expose only masked destination, consent state/source/timestamps, safe
delivery status/error codes, attempt count, and event history. They never
return message body, full destination, destination hash, provider message ID,
or credentials. `can_requeue=true` requires a definitive dead letter, current
consent and policy snapshots, the exact current request/appointment source
version and status, no unknown send outcome, unredacted content, and remaining
bounded owner-requeue allowance. Requeue is action-key idempotent. Provider
accepted or sent is not handset delivery; only `delivered` is delivery proof.

After V61 retention, delivery DTOs include `redacted`, `redacted_at`, and
`redaction_version`. Redaction clears destination/body data and forces
`can_requeue=false` while preserving safe status/version/attempt/event/time
evidence. Consent and STOP routing keys remain outside delivery-content
redaction.

`POST /api/notifications/twilio/inbound/:salon_id` consumes only signed Twilio
Advanced Opt-Out `OptOutType` values (`STOP`, `START`, `HELP`); it never parses
`Body` or emits a duplicate reply. In addition to exact-URL/all-form signature
verification, it binds `AccountSid` and the configured Messaging Service SID,
or exact sender `To`, to the route salon. The shared status callback similarly
binds `AccountSid` before a monotonic delivery mutation.

## Operations Health

`GET /api/platform/tenants/:tenant_id/operations/status`

Returns authenticated, Platform-Operations-scoped recurring-worker health plus safe queue
aggregates for the requested salon. The response never includes worker
instance/run IDs, raw errors, payloads, provider entity IDs, secrets, customer
data, or cross-salon counts. Provider-specific Square rows are omitted when no
relevant Square connection exists.

V86 adds queue keys `external_slot_claims_pre_dispatch` and
`external_slot_claims_unknown`. They expose only tenant-scoped counts and the
oldest claim timestamp. The former is releasable only when recovery proves
provider dispatch never started; the latter must remain claimed until exact
reconciliation confirms provider creation or verifies non-creation.

```json
{
  "status": "degraded",
  "evaluated_at": "2026-07-24T10:00:00Z",
  "jobs": [
    {
      "key": "notification_delivery",
      "label": "Owner notification delivery",
      "status": "healthy",
      "last_success_at": "2026-07-24T09:59:45Z",
      "last_heartbeat_at": "2026-07-24T09:59:45Z",
      "last_duration_ms": 82,
      "last_processed_count": 3,
      "stale_after_seconds": 120,
      "links": [{"label": "Open", "href": "/platform/tenants/tenant-uuid/operations"}]
    }
  ],
  "queues": [
    {
      "key": "notification_delivery",
      "label": "Owner notification delivery",
      "status": "degraded",
      "backlog_count": 2,
      "oldest_at": "2026-07-24T09:30:00Z",
      "dead_letter_count": 1,
      "links": [{"label": "Open", "href": "/platform/tenants/tenant-uuid/operations"}]
    }
  ]
}
```

Job and queue status values are `healthy`, `running`, `degraded`, `stale`, and
`unknown`. Missing evidence is `unknown`; an expired live lease or old heartbeat
is `stale`. Safe `error_class` and `error_code` may be present. Missing optional
V56 notification metrics return an unknown queue with
`NOTIFICATION_METRICS_UNAVAILABLE`; other aggregate failures use
`QUEUE_METRICS_UNAVAILABLE`. This read does not replay or mutate any job.

The stable job set includes `scheduling_pii_retention`. Its queue row reports
only the requested salon's due-record count and oldest due timestamp across the
V61 retention classes; it never returns content, destinations, audio, provider
identifiers, or raw errors. These metrics are technical execution evidence, not
proof that a production legal/compliance retention policy has been approved.
