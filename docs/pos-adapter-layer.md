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
