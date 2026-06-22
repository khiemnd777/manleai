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
New provider-facing work should converge on separate link records rather than
treating provider IDs as the service, staff, or customer identity.

Booking eligibility depends on both canonical state and the active-provider
link. A local-only, unmapped, archived, or sync-failed record may be visible to
owners, but it must not be used for POS availability or POS booking. `AI
bookable` can only be enabled for an active canonical record with a valid link
for the active POS provider.

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

The provider-neutral booking service creates backend `booking_attempts` before outbound POS writes, passes backend-owned idempotency keys into the adapter, and finalizes the same attempts as confirmed/rescheduled/cancelled or fallback pending. Booking attempts and confirmed appointments also snapshot one or more service/staff segments using provider-neutral fields. `CreateAppointmentInput`, `RescheduleInput`, and `AvailabilityInput` expose segment arrays so providers can map multi-service booking payloads without leaking provider-specific shapes into booking services. The legacy single-service fields remain populated for compatibility during the migration to customer-facing multi-service booking. `staff_selection_mode=anyone` is stored as the customer's technician preference while the provider adapter receives whatever staff assignment the provider contract requires. Real Square payloads must remain inside `SquareAdapter`, not in handlers or booking services.

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
