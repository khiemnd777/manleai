# Square Integration

## Supported in Milestone 2

- OAuth authorization URL
- OAuth callback token exchange
- Encrypted token storage
- Connection status
- Location listing
- Location selection
- Services sync
- Staff sync
- Sync logs
- POS error logs

## Not Implemented Until Milestone 3

- Customer search/create
- Availability checks
- Create appointment
- Reschedule appointment
- Cancel appointment
- Create test booking
- Cancel test booking
- Enable AI booking gate

These operations return explicit Milestone 3 errors today. The system must not fake booking success.

## Environment

```txt
SQUARE_ENVIRONMENT=sandbox
SQUARE_CLIENT_ID=
SQUARE_CLIENT_SECRET=
SQUARE_REDIRECT_URL=http://localhost:18080/api/integrations/square/callback
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
