# Square Integration

## Scheduling Authority Applicability

Square Appointments is the installed implementation of `external_provider`.
The runtime uses Square for scheduling only while the
captured scheduling authority is `external_provider` and the selected external
adapter is `square`. V86 additionally requires Atomic Slot Commit capability;
V87 resolves it from the exact persisted Square scope/configuration fence.
Buyer-level `APPOINTMENTS_WRITE` without `APPOINTMENTS_ALL_WRITE` enables new
single create only. Seller-write, reschedule, party, and resource-capacity
automation remain fail-closed.

All Square writes remain behind the authority-neutral scheduling facade.
A genuinely new availability/action uses the current salon setting; persisted
operation origin is resolved across external `booking_attempts` and owner-
manual `scheduling_requests`, while retry/target origin owns historical
dispatch, and all origins present must agree. `owner_manual` is now an active
request-only executor outside Square: it creates pending owner-review work and
never enters this adapter, creates a POS error, or starts reconciliation.
`manleai_calendar` is also implemented outside Square: Phase 4B provides
structured multi-guest, multi-service staff-only/pooled availability and
all-or-none atomic create, and Phase 4C provides target-origin whole-root
reschedule/cancel with same-root versioned events, released old-plan evidence,
exact historical replay, and no POS/provider fields. Those internal lifecycle
actions never enter the Square adapter. Explicit authority switching is
implemented outside Square through the owner-scoped V52-V55 preview,
latest/detail, and commit workflow. It uses operation/action replay, expected
authority-version and readiness fences, a live external-execution gate, an
immutable audit trail, and an explicit reverse-switch reference rather than an
implicit rollback.

Connecting, selecting a location, syncing, or receiving a Square webhook must
never implicitly switch scheduling authority. Current provider-calendar
persistence/matching/reconciliation explicitly fences writes to
`external_provider` and protects internal-origin rows. These
applicability rules do not weaken any Square confirmation, idempotency, fence,
unknown-outcome, retry, or reconciliation safeguard below.

## Supported Through Current Backend Foundation

- OAuth authorization URL
- Signed and persisted OAuth state nonce
- OAuth callback token exchange
- Encrypted token storage
- Pinned Square API version through `Square-Version`
- Connection status
- Location listing
- Location selection
- Services sync
- Staff sync
- Business hour period sync from the selected Square location
- Customer sync from Square Customers search pagination
- Calendar appointment list sync from Square Bookings into local appointment mirrors
- Signed booking webhook ingestion with durable event dedupe and fenced worker claims
- Scheduled calendar repair as a webhook backstop
- Authenticated owner-scoped webhook event metrics/list/detail plus backend-
  gated, action-key-idempotent safe requeue inside the connected Square card
- Customer search/create
- Availability checks
- Buyer-write single-create appointment path, gated before dispatch by Atomic Slot Commit
- Reschedule appointment code path, retained as request-only for Square
- Cancel appointment
- Buyer-write single create test-booking path, gated before dispatch by Atomic Slot Commit
- Cancel test booking
- AI booking readiness checks with current `scheduling_authority` and
  external-only new-test/AI-enable gates
- Sync logs
- POS error logs
- Provider capability reporting for POS sync jobs
- Booking-time customer link persistence through `pos_entity_links`
- Active-provider switch readiness gate, currently blocked because Square is
  the only installed native POS adapter
- Provider switch run persistence and service/staff/customer match skeleton for
  future real adapters
- Provider switch match review skeleton for future real adapter imports
- Provider switch dry-run readiness checklist, still gated because no alternate
  native POS adapter exists
- Provider switch/import wizard shell, still gated because no alternate native
  POS adapter exists
- Availability quote consumption for owner/test booking writes
- Unknown-result reconciliation queue with exact provider-synced candidates

## Not Implemented In The Current Square Slice

- Real sandbox payload verification against a Square Appointments account
- Witnessed representative-environment throughput/capacity evidence
- Verified atomic Square reschedule no-overlap evidence
- Atomic whole-party Square booking and external pooled-resource capacity proof
- Outbox-driven Square service/staff writes from ManleAI canonical records
- Outbox-driven Square customer writes outside the booking flow
- Executable provider switch import, executable dry-run, and activation workflow
  for a second real POS adapter

Owner and customer notification delivery are implemented outside the Square
adapter. V56 registers the stable `notification_delivery` worker, whose
provider-neutral processor consumes durable `owner_notifications` rows and
resolves the salon-scoped Twilio transport only through
`modules/notification_twilio`. V59 separately registers the stable
`customer_notification_delivery` worker and keeps customer consent, policy,
source-version fences, attempts, callbacks, and delivery state in
`modules/customer_notification`. Both paths are provider-isolated from
`modules/pos_square`: they do not call Square, select scheduling authority, or
turn notification acceptance/delivery into appointment confirmation. Their
operational readiness still depends on the required salon-scoped Twilio
configuration, consent/policy evidence, callback reachability, worker health,
and monitoring.

Square Appointments service/staff/customer outbox writes remain disabled by
provider capability flags until the exact catalog/team/customer payloads are
verified against a real sandbox account. Local ManleAI service/staff/customer
CRUD can exist, but the Square adapter must not pretend those records were
projected to Square outside supported booking-time customer operations.

The owner-facing `Sync` action imports Square services, staff, business hour
periods, and customers. Business hours are edited in Square Appointments and
stored locally as `salon_business_hour_periods` using Square-style
`day_of_week`, `start_local_time`, and `end_local_time` periods. The local
Settings page is read-only for business hours and does not collapse split-day
periods. Availability filtering only offers slots that fit inside one synced
period.

Catalog, Team/Booking Profile, and Customer reads paginate until cursor
exhaustion and fail on a repeated cursor. A service variation is imported as
bookable only when Square marks it available for booking and its normalized
duration is at least one minute. Imported staff are the intersection of active
selected-location Team members and bookable Booking Profiles; contact data
comes from Team, with profile display name as a fallback. The provider snapshot
is applied atomically. Previously imported services/staff absent from the new
snapshot are disabled/unmapped, and sync does not automatically re-enable an
owner-disabled `ai_bookable` flag. Before remote reads begin, the backend
reserves a monotonically increasing snapshot generation for the selected
location. Commit revalidates both location and generation under a
salon/provider lock, so a location switch or a newer concurrent sync makes the
older result stale instead of allowing it to overwrite or unmap newer data.
Changing locations also clears completed-sync readiness until that location is
successfully imported. Starting any full snapshot clears `last_sync_at`; only
an `active` successful completion restores it, while a failed generation keeps
the timestamp empty and remains fail-closed for readiness.

The POS Calendar `Sync Square` action imports Square Bookings for the selected
calendar range through the provider-neutral `ListAppointments` capability. The
import upserts local appointment mirrors by Square booking ID, stores segment
service/staff mappings when those provider IDs can be resolved, and marks each
imported row `pos_sync_status=synced`. Failed or unavailable calendar sync
paths must write POS errors and surface appointment-level warnings instead of
marking records confirmed from UI state alone.
The service captures the active Square location and snapshot generation once,
uses that exact fence for every pagination request, rejects a booking whose
non-empty Square location differs, and revalidates the same fence in the local
mirror transaction before its first write. Provider/location changes during
the remote read therefore fail the whole import without partially mixing
locations or exposing another location's customer details in the salon mirror.

The provider-neutral booking service records backend `pos_pending` attempts
before calling Square, passes backend-owned idempotency keys into the adapter,
and must not fake booking success. Before appointment creation, booking resolves
the canonical ManleAI customer, reuses an active Square customer link when one
exists, or calls Square customer search/create and stores the resulting link.
Customer search/create/link failures keep the attempt in fallback pending state
instead of producing a confirmed appointment. Reschedule, cancel, test booking,
test booking cancellation, and simulator booking requests leave internal
confirmed appointment state unchanged unless Square succeeds. Square readiness
exposes the owner-scoped current `scheduling_authority`; new test creation and AI
booking enablement require `external_provider` in addition to connection,
location, sync, bookable service/staff, business-hour, write-permission, and
`atomic_slot_commit` gates. With current buyer-write evidence,
`can_test_booking` and backward-compatible `can_enable_ai_booking` describe
supported single create only; action-specific reschedule and party gates remain
false. Exact external replay and external-target test cancellation remain
available after a later authority switch, as does a persisted external safe
retry. The public readiness fields still report new test creation as
unavailable in an internal current mode; the persisted retry is separately
authorized by its external operation/retry lineage and the underlying provider-
readiness gates. Square test booking create/cancel is an optional POS write
smoke test and the recovery path for clearing an older write-permission blocker
after reconnecting or updating the seller account.

`external_provider_scheduling_capability_evidence` is immutable and bound to
the exact connection capability version, stored salon integration config and
version, Square location, Square API version, and normalized OAuth-scope
fingerprint. Platform Technical exposes an idempotent persisted-state-only
`POST /api/platform/tenants/:tenant_id/technical/square/scheduling-capability/re-evaluate`
action. The client supplies only an action key and expected versions; it cannot
submit capability booleans or override seller-write safety.

The automated-create OAuth request uses `APPOINTMENTS_WRITE` and does not
request `APPOINTMENTS_ALL_WRITE`. An existing seller-write connection is never
silently downscoped: readiness reports `seller_write`,
`reconnect_required=true`, and every automated confirmation capability false
until an explicit reconnect establishes the buyer-write fence and safety is
re-evaluated.

These historical-origin safeguards support data whose current authority token
has changed. The owner-facing authority-switch workflow exists, but it does not
make Square integration setup an authority choice and it does not bypass the
persisted origin of historical external or internal operations.

Booking attempts and confirmed appointments snapshot service/staff segments in backend tables before or after the Square call as appropriate. Provider-neutral POS DTOs now carry segment arrays, and `SquareAdapter` maps those arrays into Square booking `appointment_segments` and availability `segment_filters`. `staff_selection_mode=anyone` is retained as internal/customer preference metadata; Square-specific appointment payload requirements remain isolated inside `SquareAdapter`.

Square availability ranges are built from the requested salon-local calendar
day. The adapter converts local midnight and the following local midnight to
RFC3339 timestamps, including 23-hour and 25-hour daylight-saving transition
days, instead of querying a fixed UTC day.

Square mutation failures are returned as typed provider-write errors. Only an
allowlist of explicit rejection statuses (`400`, `401`, `403`, `404`, `409`,
`422`, and `429`) is treated as definitive failure. Other 4xx responses,
transport errors, request timeout, HTTP 5xx, response decode failures, missing
provider metadata, and failures while retrieving a just-created booking are
unknown outcomes. Unknown outcomes remain `fallback_pending` with retry blocked
until the owner reconciles the action in Square Appointments. Square's official
Create, Update, and Cancel Booking references define idempotency keys for these
mutations: <https://developer.squareup.com/reference/square/bookings-api/CreateBooking>,
<https://developer.squareup.com/reference/square/bookings-api/update-booking>,
and <https://developer.squareup.com/reference/square/bookings-api/CancelBooking>.

The optional dashboard smoke test preserves one operation key across response
loss. Its service first performs provider-free `ReplayCreate`/`ReplayCancel` so
an exact historical external result remains recoverable after a salon switch.
For a non-replay create, the service calls the facade's read-only
`ResolveCreateSchedulingAuthority`, which validates persisted operation/retry
origin equality and falls back to the current token only when neither origin
exists; it does not dispatch an executor or provider. A valid external safe
retry can therefore proceed through provider readiness after a current-mode
switch, while an origin-free create is rejected as authority-not-ready when
the current token is internal. Actual create/cancel writes still use the
scheduling facade, and cancellation uses the persisted external target origin.
A pending or unknown latest test write gates additional test create/cancel
actions, and Square status reads expire only stale external-provider processing
leases before exposing the latest test state. This temporary test-action gate
does not make the optional smoke test a prerequisite for enabling AI booking.

SaaS Refactor Phases 4-6 expose Square connection, status, location, sync, and
AI-runtime controls only below
`/api/platform/tenants/:tenant_id/technical/square`. The route fixes the tenant;
Platform authorization requires the exact Technical capability, and service
methods operate directly on that tenant without resolving or impersonating a
salon owner. Tenant POS Calendar reads only
`/api/salons/:id/business/external-scheduling-readiness`, whose projection
omits connection IDs, merchant/location IDs, scopes, sync logs, diagnostics,
tokens, and test-booking evidence. The legacy authenticated owner Square
technical route group is not registered.

Square gate handlers map scheduling-authority failures to sanitized
`409 SCHEDULING_AUTHORITY_NOT_READY` and map other gate failures to bounded
public messages. Wrapped executor or provider diagnostic text is not returned
to the client.

For a definitive test create/cancel failure, the Square test DTO forwards the
exact safe fallback attempt as `retry_of_attempt_id` into the provider-neutral
booking service and uses a new operation key. `latest_test_booking` exposes
`operation_type`, so a reload can recover only the lineage matching the action
being retried. Replaying a request whose response was lost keeps both its
operation key and logical payload unchanged; refreshed ephemeral quote proof is
ignored for replay identity and does not create a new retry lineage.

V86 Atomic Slot Commit makes the local PostgreSQL claim the outbound
linearization point for a capability-approved ManleAI external mutation. Quote
consumption, attempt creation, and all concrete half-open `[start,end)` staff
intervals commit together before any Square customer or booking request. Two
calls that race for the same Square location/staff/time therefore produce one
local winner and one `SLOT_COMMIT_CONFLICT`; the loser performs zero Square
writes and must fetch fresh availability. This protects only writes that enter
ManleAI. It does not serialize a concurrent seller Dashboard, Square app, or
another third-party writer. The
[official Square Bookings overview](https://developer.squareup.com/docs/bookings-api/what-it-is)
allows seller-level writers to create double bookings; therefore seller-write is never
auto-confirm capable and this system does not claim a global no-double-booking
invariant against seller-side writers. Use `manleai_calendar` or
`owner_manual` when that global constraint is required.

Once dispatch starts, an ambiguous Square outcome retains the active claim and
enters reconciliation. A worker may release an expired pre-dispatch claim only
when the persisted outcome proves `not_started`. It must not release or retry a
dispatch-started/unknown claim based on lease age. Verified provider
non-creation releases it; an exact authoritative provider match confirms it.
The operations-health API exposes bounded pre-dispatch and unknown claim queue
counts without customer or provider payloads.

Owner-facing booking, reschedule, and test-booking HTTP flows first request a
short-lived availability quote. Conversation create/reschedule paths retain the
backend-owned quote for the offered slot, refresh its exact assignment
immediately before dispatch. External party create makes no sequential child
writes while whole-party atomic capability is unavailable. The selected quote/slot is single-use and must match salon,
provider, selected location and snapshot generation, ordered service/staff
segments, time range, and request fingerprint. A safe fallback retry preserves
the original normalized logical request exactly but obtains a current quote;
any changed customer, location, provider service/staff identity or version,
appointment baseline, time, or notes payload is rejected from that lineage.
A generation-only change expires the prior proof without changing the logical
operation fingerprint. Unknown or provider-pending results remain unconfirmed and create a
reconciliation task. Candidate lookup and resolution use the same exact
tenant-scoped matching predicate, including provider booking ID/version/status
where present. For reschedule/cancel, the
attempt stores the Square booking version observed before dispatch and only a
newer synchronized version can prove the mutation. A reschedule must also match
the requested range and ordered service/staff assignment. The original Square
booking ID does not by itself block an owner-verified `not_created` resolution.
The direct Square mutation response must name that same booking and advance the
captured version. Persistence is serialized with calendar import across direct
success, fallback, and lease recovery. An exact equal/newer authoritative mirror
is preserved and can terminalize the operation: create requires the returned
booking ID, range, and ordered canonical/raw segments; reschedule requires a
version newer than baseline, at least the returned response version, requested
range, and exact segments; cancel requires a cancelled version newer than
baseline and at least the returned response version. A conflicting mirror is
not overwritten and the operation remains unconfirmed pending reconciliation.
When exact external create evidence is canonicalized, the appointment preserves
existing confirmation provenance, fills only missing `confirmed_at` and
`confirmation_source` with the canonicalization time and
`external_provider`, and leaves `confirmed_by_user_id` unset unless an actual
actor was already recorded. Repeated recovery is timestamp-idempotent.
The creating/mirror attempt stores the booking's originating Square location.
Reschedule and cancel are allowed only while that immutable origin matches the
currently selected synchronized location; reschedule must also use a quote from
that location, and cancellation dispatch validates the same current provider
fence. A same-location newer generation is acceptable only when the target and
ordered raw service/staff mappings remain current. Legacy unknown origins and
location switches fail before any Square mutation.
Calendar re-import at the same provider version may fill only missing customer
and internal service/staff mappings after the locked persisted status, time,
version, raw provider customer identity, and ordered raw service/staff
ID/version/duration snapshot matches exactly.
A conflicting equal-version payload is skipped and cannot resolve a pending
reschedule or cancellation.

An availability quote remains single-use across unrelated booking intents. A
safe retry may atomically move the quote consumer from the exact
`retry_of_attempt_id` to its successor while the quote is still valid; any
missing, mismatched, expired, or non-lineage reuse remains stale and cannot
reach Square.

The public Square booking webhook verifies HMAC-SHA256 over the exact stored
notification URL plus raw body using the write-only encrypted signature key.
Event IDs are durably deduped before acknowledgement. Processing fetches current
Square booking truth instead of trusting payload status. Envelope and nested
booking location IDs must match when both are present. Event processing and
scheduled repair use per-claim tokens, and completion requires the same token,
so a stale worker cannot complete or overwrite a newer lease. Processing applies
provider-version-aware mirror updates so an older event cannot overwrite a
newer appointment revision. Scheduled repair is a separate
calendar-health backstop and does not turn a transient repair failure into a
Square OAuth/catalog readiness failure. A configured local verifier does not
prove that a Square subscription exists or that deliveries are healthy.
Tenant routing accepts exact merchant/location matches while a selected Square
connection is connected, syncing, active, in error, or token-expired so signed
deliveries are not discarded during a sync or recoverable outage. It excludes
not-connected and disabled connections and still requires Square to be the
salon's active POS provider.

This target/repair predicate intentionally does not read the current scheduling
authority so historical external-provider mirrors can converge after a later
salon switch. Downstream booking-calendar persistence, matching, and
reconciliation currently require the originating authority to be
`external_provider`; they skip/protect internal-origin rows and cannot change an internal
appointment's authoritative lifecycle state.

V60 adds owner operations without broadening webhook authority. The owner-
scoped API returns only event type, bounded processing status/attempt/requeue
counts, backend-owned `can_requeue`, safe error class/code, timestamps,
salon-wide backlog/recent-success metrics, and calendar-repair health. It omits
provider entity IDs, raw payloads, signature/token/claim material, customer
data, provider responses, and raw errors. Public filters accept `pending`,
`processing`, `failed`, `dead_letter`, and `succeeded`; an unfiltered list may
include a read-only `ignored` event. Requeue uses one stable owner action key,
is limited by backend terminal-failure evidence and bounded counts, and returns
`X-Idempotent-Replay` for exact recovery. The connected Square child panel
never treats webhook receipt, replay, repair, or configuration as appointment
confirmation or scheduling-authority selection.

## Configuration

Square Appointments app credentials are configured in the Integrations
dashboard per salon. The backend stores the Square client secret encrypted and
never returns it to the frontend. The dashboard also stores an HTTPS webhook
notification URL and encrypted write-only webhook signature key. Active runtime
configuration is resolved only from the exact enabled
`salon_integration_configs` row for the requested salon; `.env`,
`project.env`, templates, Compose defaults, and deployment secrets are not
evidence of a salon's active provider configuration. Configuration writes
surface encryption/decryption failures and preserve the last valid stored
secret. Repository misses, access failures, decryption failures, malformed or
incomplete stored configuration, and disabled stored configuration fail closed
before OAuth or a Square API call. They never inherit process environment
settings or secrets.

V88 also requires every Square adapter call to carry a non-empty salon ID and a
constructor-injected tenant config resolver. A complete Square
provider/merchant/location identity may belong to only one salon. Connecting,
selecting a location, and syncing still do not choose `active_pos_provider`.
When that field is explicitly blank, Platform Technical exposes a reviewed
`active-provider/activate` action only after the stored config and current
connection/location/sync evidence pass their exact version fences. Release A
keeps the historical schema default of `square`; changing new-tenant
provisioning to blank is deferred to the replica-drained Release B contract.
Saving these credentials, completing OAuth, or selecting a location configures
the external adapter only. It is not scheduling-authority consent and must not
perform an implicit authority switch.
Configuration transfer preserves the target deployment's webhook URL and never
exports the signature key, which must be re-entered at the target.

## Sandbox OAuth Troubleshooting

If Square sandbox OAuth opens a blank page or returns the error
`To start the OAuth flow for a sandbox account, first launch the seller test
account from the Developer Console.`, open the sandbox seller dashboard first:

```txt
https://app.squareupsandbox.com/dashboard/
```

Do not use the production Square dashboard for sandbox OAuth setup:

```txt
https://app.squareup.com/dashboard/
```

After the sandbox dashboard loads, return to the ManleAI Platform tenant
Technical tab and retry `Connect Square`. For Platform-managed sandbox credentials, keep the
Square environment set to `Sandbox` and use the sandbox application ID and
secret. The optional Square API base URL may be blank or set to:

```txt
https://connect.squareupsandbox.com
```

## Token Security

Square tokens are encrypted with AES-GCM before being persisted in `pos_connections`. The API never returns access or refresh tokens to the frontend.

## Error Logging

Square API failures are normalized and written to `pos_errors` with:

- `salon_id`
- `provider`
- `operation`
- `error_code`
- `error_message`

`error_message` is selected from a fixed internal mapping. Square response
details, bodies, credentials, and customer data are never copied into error
strings or payloads returned/stored by this path. `pos_errors.payload` is not
used for provider diagnostics, and V63 clears any value retained by older
versions while rewriting related historical POS/Square error fields.
