# POS Adapter Layer

The project is POS-first, but ManleAI owns canonical salon operational data.
POS providers are external projections and booking execution layers. Internal
booking behavior must never depend directly on Square, Vagaro, GlossGenius,
Fresha, Booksy, Mindbody, Boulevard, Zenoti, or any other provider payload. POS
adapters are outbound writers/readers behind the provider-neutral contract.

## Ownership Model

ManleAI-owned canonical records include services, staff, customers, AI controls,
owner workflow state, fallback pending requests, logs, and training data. The
active POS provider owns real-time availability and booking execution.
`salons.active_pos_provider` is the backend source of truth for which provider
is evaluated by management lists, booking readiness, and future switch flows.

Provider IDs are mappings, not primary product identity. The target model stores
those mappings in `pos_entity_links` with:

```txt
entity_type: service | staff | customer
entity_id: ManleAI canonical record ID
provider: square | another implemented provider
provider_entity_id: provider-side ID
sync_status: local_only | syncing | synced | sync_failed | unmapped | archived
```

During the migration, existing `services.pos_service_id` and
`staff.pos_staff_id` fields are legacy provider links for Square-imported data.
Customer links are stored in `pos_entity_links`. New provider-facing work
should converge on separate link records rather than treating provider IDs as
the service, staff, or customer identity.

Booking eligibility depends on both canonical state and the active-provider
link. A local-only, unmapped, archived, or sync-failed record may be visible to
owners, but it must not be used for POS availability or POS booking. `AI
bookable` can only be enabled for an active canonical record with a valid link
for the active POS provider.

## Field-Level Operational Authority

Record origin, sync state, booking eligibility, and write authority are separate
contracts. Service/staff APIs expose derived `field_authority` metadata with:

- `operational_source`: `manleai | provider`
- `provider` and `provider_label` when an adapter participates
- `operational_write_mode`: `local | provider_read_only | provider_sync`

The provider-neutral POS service derives this metadata from the canonical
record's provider identity/link and the adapter's declared capabilities. A
provider-backed record is `provider_read_only` when the adapter does not support
the corresponding upsert operation. `sync_failed` and `unmapped` remain
provider-managed error states; they must not unlock local operational edits.

For services, ManleAI-managed owner controls use a separate atomic mutation so
category and consultation data can be saved without resubmitting stale
provider-managed name, description, duration, price, or active values. Direct
operational writes to a `provider_read_only` service or staff record return a
conflict instead of silently creating local/provider drift.

The booking service resolves every new-booking and availability service/staff
reference with an explicit provider scope obtained from
`salons.active_pos_provider`. Repository lookups enforce the same provider
scope. Reschedule and cancellation of an existing appointment intentionally use
the provider recorded on that appointment so a provider switch does not orphan
historical POS bookings.

Provider switch activation must be a gated workflow, not a status toggle. A new
provider cannot become active until its adapter exists, provider data has been
imported, canonical services/staff/customers have been matched or resolved,
booking readiness has passed, and previous provider links remain available for
appointment history.

The current backend can persist provider switch runs and service/staff/customer
match candidates for a real future adapter. Missing target adapters create
blocked runs. Owners can record match review decisions for imported candidates,
and the backend can return a dry-run readiness checklist for a switch run, but
the dashboard import wizard shell remains gated when no alternate native POS
adapter is installed. Activation remains unavailable until a real import, full
conflict resolution, and executable alternate-provider dry-run path exists.

## Interface

`backend/modules/pos/types.go` defines:

```go
type POSProvider interface {
    Name() string
    Connect(ctx context.Context, input ConnectInput) (*Connection, error)
    HealthCheck(ctx context.Context, salonID string) error
    ListLocations(ctx context.Context, salonID string) ([]Location, error)
    ListServices(ctx context.Context, salonID string) ([]Service, error)
    ListStaff(ctx context.Context, salonID string) ([]StaffMember, error)
    SearchCustomerByPhone(ctx context.Context, salonID string, phone string) (*Customer, error)
    CreateCustomer(ctx context.Context, salonID string, input CreateCustomerInput) (*Customer, error)
    CheckAvailability(ctx context.Context, salonID string, input AvailabilityInput) ([]TimeSlot, error)
    CreateAppointment(ctx context.Context, salonID string, input CreateAppointmentInput) (*Appointment, error)
    RescheduleAppointment(ctx context.Context, salonID string, appointmentID string, input RescheduleInput) (*Appointment, error)
    CancelAppointment(ctx context.Context, salonID string, appointmentID string, input CancelInput) (*Appointment, error)
    Sync(ctx context.Context, salonID string) error
}
```

Providers may also expose optional sync/write capabilities:

```go
type CapabilityProvider interface {
    Capabilities() ProviderCapabilities
}

type POSWriteProvider interface {
    UpsertService(ctx context.Context, salonID string, service Service) (*ProviderSyncResult, error)
    ArchiveService(ctx context.Context, salonID string, service Service) (*ProviderSyncResult, error)
    UpsertStaff(ctx context.Context, salonID string, staff StaffMember) (*ProviderSyncResult, error)
    ArchiveStaff(ctx context.Context, salonID string, staff StaffMember) (*ProviderSyncResult, error)
    UpsertCustomer(ctx context.Context, salonID string, customer Customer) (*ProviderSyncResult, error)
}
```

Capability flags are authoritative. If a provider does not declare support for
a write operation, the API and worker must keep that operation gated and must
not fake a synced state.

Provider `Sync(ctx, salonID)` is the pull/import path. For Square Appointments
it imports services, staff, selected-location business hour periods, and
customers. Business hours use provider-native period semantics instead of a
single open/close pair per day, and booking availability filters require a slot
to fit inside one imported period.

Square pull sync paginates Catalog, Team/Booking Profiles, and Customers until
cursor exhaustion and rejects a repeated cursor. Service variations must be
available for booking with a positive normalized duration. Staff must be both
an active Team member at the selected location and have a bookable Booking
Profile. A complete provider snapshot is applied transactionally; rows missing
from the snapshot are disabled/unmapped, and provider sync cannot re-enable an
owner-disabled `ai_bookable` choice. Full imports reserve a monotonic
salon/provider snapshot generation before provider reads. The transaction
revalidates both that generation and the selected location before mutating
canonical records; a location switch or a newer import rejects stale work.
Beginning a snapshot clears the prior `last_sync_at`, and only an active
successful completion restores it; a failed generation cannot remain visually
or operationally "synced" through an older timestamp.

Range-scoped calendar import uses the same fence discipline independently of a
full catalog sync. The booking service captures one owner-scoped active
provider/location/generation fence, the adapter revalidates it on every page,
and the booking repository revalidates it again in the mirror transaction
before any row mutation. Stale imports fail atomically and cannot mix bookings
or customer data from two provider locations.

Provider-neutral availability input carries the salon timezone. Date-only
queries represent one salon-local calendar day; adapters convert local midnight
and the next local midnight to provider timestamps. Calendar-day arithmetic is
required instead of a fixed 24-hour interval so daylight-saving transitions
remain correct.

## POS Sync Jobs

`pos_sync_jobs` is the provider-neutral outbox for projecting ManleAI-owned
canonical records to the active POS provider when that provider supports the
operation.

Current job operations are:

- `upsert_service`
- `archive_service`
- `upsert_staff`
- `archive_staff`
- `upsert_customer`

Jobs move through `queued`, `running`, `succeeded`, and `failed`. The worker
claims queued or retryable failed jobs, checks provider capabilities, calls
`POSWriteProvider` only for supported operations, updates `pos_entity_links`,
and writes `pos_sync_logs`. Provider failures are logged to `pos_errors` and
reflected on canonical sync fields when the operation owns a durable canonical
projection.

## First Provider

`backend/modules/pos_square` implements the first adapter:

- OAuth URL generation
- OAuth callback token exchange
- Encrypted token storage
- Location listing
- Catalog service normalization
- Catalog service version persistence for future Square booking payloads
- Team member normalization
- Import/projection into internal `services` and `staff`
- Legacy Square provider link persistence on service/staff rows until
  `pos_entity_links` is introduced
- Customer search/create
- Availability search
- Create booking
- Reschedule booking
- Cancel booking
- POS sync logs and POS error logs

Square currently declares no service/staff/customer outbox write capabilities.
It can import services/staff and perform booking/customer operations already
listed here, but service/staff/customer writes from ManleAI remain
capability-gated until real Square payloads are verified against a Square
Appointments sandbox account.

The provider-neutral booking service creates backend `booking_attempts` before
outbound POS writes, passes backend-owned idempotency keys into the adapter, and
finalizes the same attempts as confirmed/rescheduled/cancelled or fallback
pending. Before appointment creation, booking resolves the canonical customer
and reuses an active customer link, or asks the provider to search/create a
customer and then stores the `pos_entity_links` mapping. Failure while
searching, creating, or linking the provider customer produces fallback pending
state instead of confirmed wording. Booking attempts and confirmed appointments
also snapshot one or more service/staff segments using provider-neutral fields.
`CreateAppointmentInput`, `RescheduleInput`, and `AvailabilityInput` expose
segment arrays so providers can map multi-service booking payloads without
leaking provider-specific shapes into booking services. The legacy
single-service fields remain populated for compatibility during the migration to
customer-facing multi-service booking. `staff_selection_mode=anyone` is stored
as the customer's technician preference while the provider adapter receives
whatever staff assignment the provider contract requires. Real Square payloads
must remain inside `SquareAdapter`, not in handlers or booking services.

Provider write errors carry a provider-neutral outcome and phase. A definitive
provider rejection may be retry-safe. A transport failure, HTTP 5xx, truncated
success response, decode failure, or post-write lookup failure has an unknown
outcome unless the adapter can prove the mutation was rejected. Unknown writes
require POS reconciliation and must not be retried with a new operation key.

The durable processing lease preserves the same phase distinction across a
process crash. Recovery acquires the calendar advisory lock before row locks and
first converges an exact authoritative create/reschedule/cancel mirror when one
already exists. Without that proof, `provider_outcome=not_started` records a
definitive failed, retry-safe fallback without reconciliation, while
`provider_outcome=in_flight` records an unknown, retry-blocked fallback and
opens reconciliation. Owner reads and the background sweep use the same
idempotent transaction and deduplicated notification/outbox keys.

Historical fingerprint dedupe follows the same phase boundary. V39 may
auto-supersede only a duplicate with `provider_outcome=not_started`; it aborts
if one fingerprint group has multiple attempts whose dispatch may have begun.
Superseded attempts are terminal and cannot be reacquired, lease-swept,
started, or finalized by stale workers.

Every create/reschedule writer consumes one persisted availability quote. The
quote binds salon, provider, selected location and snapshot generation, request
fingerprint, expiry, one selected slot, ordered segments, time range, and one
consuming booking attempt. HTTP clients submit the proof; conversation paths
refresh the exact backend-owned proof immediately before dispatch and refresh
all party children before the first child write. A safe retry is a new operation
key with the exact prior logical request fingerprint, a fresh current provider
fence, and an atomic supersession link; changing booking details is a new
request, not a retry. A generation-only change invalidates an old quote but does
not change the logical fingerprint. Retry remains allowed only when the stored
location, ordered raw provider service/staff identities and versions, and target
appointment baseline still match current provider-synced state. An
unknown or provider-pending write creates a reconciliation task. Resolution may
attach only a tenant-scoped provider-synced candidate revalidated under lock,
record a verified definitive non-creation, or remain escalated and blocked.
Appointment mutations store the target provider version before dispatch.
Reschedule/cancel reconciliation requires a provider-synced version newer than
that baseline; reschedule also requires the exact requested range and ordered
service/staff assignments. `not_created` treats the existing target booking ID
as context, not proof that the requested mutation was applied. Calendar import
persists the originating location fence on its mirror attempt. Reschedule and
cancel owner lookup/action claims join that immutable origin to the current
active synchronized connection and require an exact location match; cancel now
carries the current fence through `pos.CancelInput`. Same-location generation
advancement is allowed only after current raw mappings and the target baseline
are revalidated. A location switch or legacy missing origin makes the action
unavailable with zero provider calls instead of implicitly moving a booking.
Calendar import
and reconciliation resolution share one salon-scoped transaction advisory lock
before either path locks attempt or appointment rows. Equal-version calendar
enrichment and action resolution require an exact match against the locked
persisted status, range, version, provider customer identity, and ordered raw
service/staff snapshot.
Direct success, fallback finalization, and lease recovery use that same lock
order. A newer authoritative mirror is preserved and may complete the operation
only when the create booking ID/range/segments, reschedule range and ordered
canonical/raw segments, or cancelled target state match exactly; mismatches
remain unconfirmed and reconciliation-required.

Availability quotes are bounded operational evidence. A worker cleanup keeps a
24-hour grace after unconsumed quote expiry, retains orphaned consumed quotes
for 30 days after consumption, and skips every quote still referenced by a
booking attempt or its recorded consumer. The cleanup uses bounded
`FOR UPDATE SKIP LOCKED` batches; deleting an eligible quote cascades only to
its quote-slot rows and never mutates the booking-attempt audit ledger.

Booking webhooks remain provider-specific inbound adapters. Their raw signature
validation, tenant routing, event dedupe, claim fencing, provider retrieval,
and error mapping stay inside `modules/pos_square`; the normalized calendar
mirror write remains in `modules/booking`. A webhook body is never direct
confirmation evidence.

## Adding Future Providers

Add a new package such as `modules/pos_vagaro` only when that provider is a real
implementation. Future provider names in docs are architecture targets, not
shipped support.

The provider must:

- Implement `pos.POSProvider`.
- Keep provider-specific auth and payload mapping inside the adapter package.
- Return provider-neutral DTOs.
- Normalize provider errors into the internal POS error codes.
- Store secrets only through encrypted `pos_connections` fields.
- Add only provider-specific docs and tests; do not change booking service logic unless the provider-neutral contract needs to evolve.
