# Backend

Go/Fiber API for the AI Receptionist pilot foundation.

## Stack

- Go
- Fiber
- PostgreSQL
- Startup SQL migrations from `backend/migrations`
- Ent schema definitions
- Redis connection target reserved for later async work
- JWT access tokens and persisted refresh tokens

## Commands

```bash
make run-api
make run-worker
make test
make seed-local
```

## Implemented Endpoints

Auth:

- `POST /api/auth/login`
- `POST /api/auth/logout`
- `POST /api/auth/refresh-token`
- `GET /api/auth/me`

Salon:

- `GET /api/salons`
- `POST /api/salons`
- `GET /api/salons/:id`
- `PUT /api/salons/:id`
- `GET /api/salons/:id/settings`
- `PUT /api/salons/:id/settings`
- `GET /api/salons/:id/business-hours`
- `PUT /api/salons/:id/business-hours`
- `GET /api/salons/:id/services`
- `PATCH /api/salons/:id/services/:service_id/ai-bookable`
- `GET /api/salons/:id/staff`
- `PATCH /api/salons/:id/staff/:staff_id/ai-bookable`
- `GET /api/salons/:id/customers`
- `GET /api/salons/:id/customers/search`
- `GET /api/salons/:id/appointments`
- `POST /api/salons/:id/appointments/:appointment_id/reschedule`
- `POST /api/salons/:id/appointments/:appointment_id/cancel`
- `GET /api/salons/:id/booking-attempts`
- `POST /api/salons/:id/booking-attempts`

Square integration:

- `GET /api/integrations/square/connect-url`
- `GET /api/integrations/square/callback`
- `GET /api/integrations/square/status`
- `GET /api/integrations/square/locations`
- `POST /api/integrations/square/select-location`
- `POST /api/integrations/square/sync`
- `POST /api/integrations/square/test-booking`
- `POST /api/integrations/square/cancel-test-booking`
- `POST /api/integrations/square/enable-ai-booking`
- `POST /api/integrations/square/disable-ai-booking`

## Architecture

Handlers parse HTTP requests only. Services own business rules and tenant checks. Repositories own SQL. The API runs startup migrations before registering repositories. The `modules/pos` package defines the POSProvider boundary; `modules/pos_square` is the only concrete provider.

Booking code calls `POSProvider` only. Square payloads and API URLs must remain inside `modules/pos_square`. Booking workflows create backend `pos_pending` attempts before POS writes, then finalize the same attempts as confirmed/rescheduled/cancelled only on POS success or `fallback_pending` on POS failure. POS failures must not create confirmed appointments.
