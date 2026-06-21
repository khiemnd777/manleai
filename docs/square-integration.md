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
- Customer search/create
- Availability checks
- Create appointment
- Reschedule appointment
- Cancel appointment
- Create test booking
- Cancel test booking
- AI booking readiness gate
- Sync logs
- POS error logs

## Not Implemented In The Current Square Slice

- Real sandbox payload verification against a Square Appointments account

The provider-neutral booking service records backend `pos_pending` attempts before calling Square, passes backend-owned idempotency keys into the adapter, and must not fake booking success. Reschedule, cancel, test booking, test booking cancellation, and simulator booking requests leave internal confirmed appointment state unchanged unless Square succeeds. AI booking can only be enabled after the latest Square test booking was created and cancelled successfully.

Booking attempts and confirmed appointments snapshot service/staff segments in backend tables before or after the Square call as appropriate. Provider-neutral POS DTOs now carry segment arrays, and `SquareAdapter` maps those arrays into Square booking `appointment_segments` and availability `segment_filters`. `staff_selection_mode=anyone` is retained as internal/customer preference metadata; Square-specific appointment payload requirements remain isolated inside `SquareAdapter`.

## Environment

```txt
SQUARE_ENVIRONMENT=sandbox
SQUARE_CLIENT_ID=
SQUARE_CLIENT_SECRET=
SQUARE_REDIRECT_URL=http://localhost:18089/api/integrations/square/callback
SQUARE_API_VERSION=2026-05-20
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
