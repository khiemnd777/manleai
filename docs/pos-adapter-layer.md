# POS Adapter Layer

The project is POS-first. Internal booking behavior must never depend directly on Square, Vagaro, GlossGenius, Fresha, Booksy, Mindbody, Boulevard, Zenoti, or any other provider payload.

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
    RescheduleAppointment(ctx context.Context, salonID string, appointmentID string, input RescheduleInput) error
    CancelAppointment(ctx context.Context, salonID string, appointmentID string, reason string) error
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
- Team member normalization
- Sync into internal `services` and `staff`
- POS sync logs and POS error logs

Booking operations intentionally return a Milestone 3 error today. They must be implemented inside `SquareAdapter`, not in handlers or booking services.

## Adding Future Providers

Add a new package such as `modules/pos_vagaro`.

The provider must:

- Implement `pos.POSProvider`.
- Keep provider-specific auth and payload mapping inside the adapter package.
- Return provider-neutral DTOs.
- Normalize provider errors into the internal POS error codes.
- Store secrets only through encrypted `pos_connections` fields.
- Add only provider-specific docs and tests; do not change booking service logic unless the provider-neutral contract needs to evolve.

