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

## Not Implemented In The Current Square Slice

- Real sandbox payload verification against a Square Appointments account
- Outbox-driven Square service/staff writes from ManleAI canonical records
- Outbox-driven Square customer writes outside the booking flow
- Executable provider switch import, executable dry-run, and activation workflow
  for a second real POS adapter

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

The provider-neutral booking service records backend `pos_pending` attempts
before calling Square, passes backend-owned idempotency keys into the adapter,
and must not fake booking success. Before appointment creation, booking resolves
the canonical ManleAI customer, reuses an active Square customer link when one
exists, or calls Square customer search/create and stores the resulting link.
Customer search/create/link failures keep the attempt in fallback pending state
instead of producing a confirmed appointment. Reschedule, cancel, test booking,
test booking cancellation, and simulator booking requests leave internal
confirmed appointment state unchanged unless Square succeeds. AI booking can
only be enabled after the latest Square test booking was created and cancelled
successfully.

Booking attempts and confirmed appointments snapshot service/staff segments in backend tables before or after the Square call as appropriate. Provider-neutral POS DTOs now carry segment arrays, and `SquareAdapter` maps those arrays into Square booking `appointment_segments` and availability `segment_filters`. `staff_selection_mode=anyone` is retained as internal/customer preference metadata; Square-specific appointment payload requirements remain isolated inside `SquareAdapter`.

## Configuration

Square Appointments app credentials are configured in the Integrations
dashboard per salon. The backend stores the Square client secret encrypted and
never returns it to the frontend. These legacy environment variables remain
optional local-development fallback values when a salon does not yet have
dashboard-saved Square configuration:

- `SQUARE_ENVIRONMENT`
- `SQUARE_CLIENT_ID`
- `SQUARE_CLIENT_SECRET`
- `SQUARE_REDIRECT_URL`
- `SQUARE_API_VERSION`

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
