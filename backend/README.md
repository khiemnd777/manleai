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

Square integration:

- `GET /api/integrations/square/connect-url`
- `GET /api/integrations/square/callback`
- `GET /api/integrations/square/status`
- `GET /api/integrations/square/locations`
- `POST /api/integrations/square/select-location`
- `POST /api/integrations/square/sync`
- `POST /api/integrations/square/test-booking` returns `501` until Milestone 3
- `POST /api/integrations/square/cancel-test-booking` returns `501` until Milestone 3
- `POST /api/integrations/square/enable-ai-booking` returns `501` until Milestone 3
- `POST /api/integrations/square/disable-ai-booking` returns `501` until Milestone 3

## Architecture

Handlers parse HTTP requests only. Services own business rules and tenant checks. Repositories own SQL. The API runs startup migrations before registering repositories. The `modules/pos` package defines the POSProvider boundary; `modules/pos_square` is the only concrete provider.

Booking code must call `POSProvider` only. Square payloads and API URLs must remain inside `modules/pos_square`.
