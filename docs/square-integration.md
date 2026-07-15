# Square Integration

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
- Customer search/create
- Availability checks
- Create appointment
- Reschedule appointment
- Cancel appointment
- Create test booking
- Cancel test booking
- AI booking readiness checks
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
- Outbox-driven Square service/staff writes from ManleAI canonical records
- Outbox-driven Square customer writes outside the booking flow
- Executable provider switch import, executable dry-run, and activation workflow
  for a second real POS adapter
- External SMS/email/push delivery consumer for durable owner-notification outbox rows

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
confirmed appointment state unchanged unless Square succeeds. AI booking can be
enabled after Square connection, location, sync, bookable service/staff, and
business hour readiness pass and no current Square create-booking permission
blocker is present; Square test booking create/cancel is an optional POS write
smoke test and the recovery path for clearing an older write-permission blocker
after reconnecting or updating the seller account.

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
loss. A pending or unknown latest test write gates additional test create/cancel
actions, and Square status reads expire stale processing leases before exposing
the latest test state. This temporary test-action gate does not make the
optional smoke test a prerequisite for enabling AI booking.

For a definitive test create/cancel failure, the Square test DTO forwards the
exact safe fallback attempt as `retry_of_attempt_id` into the provider-neutral
booking service and uses a new operation key. `latest_test_booking` exposes
`operation_type`, so a reload can recover only the lineage matching the action
being retried. Replaying a request whose response was lost keeps both its
operation key and logical payload unchanged; refreshed ephemeral quote proof is
ignored for replay identity and does not create a new retry lineage.

Owner-facing booking, reschedule, and test-booking HTTP flows first request a
short-lived availability quote. Conversation create/reschedule paths retain the
backend-owned quote for the offered slot, refresh its exact assignment
immediately before dispatch, and refresh every party child before the first
child write. The selected quote/slot is single-use and must match salon,
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

## Configuration

Square Appointments app credentials are configured in the Integrations
dashboard per salon. The backend stores the Square client secret encrypted and
never returns it to the frontend. The dashboard also stores an HTTPS webhook
notification URL and encrypted write-only webhook signature key. Active runtime
configuration is resolved from `salon_integration_configs`; `.env`,
`project.env`, templates, Compose defaults, and deployment secrets are not
evidence of a salon's active provider configuration. Configuration writes
surface encryption/decryption failures and preserve the last valid stored
secret. The pre-existing runtime resolver still falls back to legacy bootstrap
configuration on repository/decryption errors instead of only exact
not-found; that fail-open behavior is a known production-readiness gap and is
not evidence of the active salon configuration.
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

After the sandbox dashboard loads, return to the ManleAI Integrations dashboard
and retry `Connect Square`. For dashboard-managed sandbox credentials, keep the
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
- optional JSON payload
