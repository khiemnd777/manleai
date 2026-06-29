# Backend

Go/Fiber API for the AI Receptionist pilot foundation.

## Stack

- Go
- Fiber
- PostgreSQL
- Startup SQL migrations from `backend/migrations`
- Ent schema definitions
- Redis connection target reserved for async/runtime work
- Worker backed by `pos_sync_jobs` and conversation retention redaction
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
- `GET /api/salons/:id/public-catalog`
- `PUT /api/salons/:id/public-catalog`
- `GET /api/salons/:id/business-hours`
- `PUT /api/salons/:id/business-hours`
- `POST /api/salons/:id/availability`
- `GET /api/salons/:id/services`
- `POST /api/salons/:id/services`
- `PUT /api/salons/:id/services/:service_id`
- `POST /api/salons/:id/services/:service_id/archive`
- `PATCH /api/salons/:id/services/:service_id/ai-bookable`
- `GET /api/salons/:id/staff`
- `POST /api/salons/:id/staff`
- `PUT /api/salons/:id/staff/:staff_id`
- `POST /api/salons/:id/staff/:staff_id/archive`
- `PATCH /api/salons/:id/staff/:staff_id/ai-bookable`
- `GET /api/salons/:id/customers`
- `POST /api/salons/:id/customers`
- `PUT /api/salons/:id/customers/:customer_id`
- `POST /api/salons/:id/customers/:customer_id/archive`
- `GET /api/salons/:id/customers/search`
- `GET /api/salons/:id/appointments`
- `POST /api/salons/:id/appointments/:appointment_id/reschedule`
- `POST /api/salons/:id/appointments/:appointment_id/cancel`
- `GET /api/salons/:id/booking-attempts`
- `POST /api/salons/:id/booking-attempts`
- `GET /api/salons/:id/conversation-sessions`
- `POST /api/salons/:id/conversation-sessions`
- `GET /api/salons/:id/conversation-sessions/:session_id`
- `GET /api/salons/:id/conversation-sessions/:session_id/realtime-events`
- `POST /api/salons/:id/conversation-sessions/:session_id/archive`
- `POST /api/salons/:id/conversation-sessions/:session_id/redact`
- `POST /api/salons/:id/conversation-sessions/:session_id/messages`
- `GET /api/salons/:id/voice/status`
- `GET /api/salons/:id/knowledge-items`
- `POST /api/salons/:id/knowledge-items`
- `PUT /api/salons/:id/knowledge-items/:item_id`
- `DELETE /api/salons/:id/knowledge-items/:item_id`
- `GET /api/salons/:id/owner-corrections`
- `POST /api/salons/:id/owner-corrections`
- `POST /api/salons/:id/owner-corrections/:correction_id/apply`
- `POST /api/salons/:id/owner-corrections/:correction_id/dismiss`
- `POST /api/salons/:id/training/evaluate`
- `GET /api/salons/:id/integration-configs`
- `PUT /api/salons/:id/integration-configs/square`
- `PUT /api/salons/:id/integration-configs/twilio`
- `PUT /api/salons/:id/integration-configs/openai`
- `GET /api/salons/:id/configuration-export`
- `POST /api/salons/:id/configuration-import/preview`
- `POST /api/salons/:id/configuration-import`
- `GET /api/salons/:id/pos/provider-switch-readiness`
- `GET /api/salons/:id/pos/provider-switch-runs/latest`
- `POST /api/salons/:id/pos/provider-switch-runs`
- `GET /api/salons/:id/pos/provider-switch-runs/:run_id`
- `GET /api/salons/:id/pos/provider-switch-runs/:run_id/dry-run-readiness`
- `PATCH /api/salons/:id/pos/provider-switch-runs/:run_id/matches/:match_id`

Public:

- `GET /api/public/salon`
- `GET /api/public/salons/:slug`
- `GET /api/voice/audio/:id`

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

Twilio voice:

- `POST /api/voice/twilio/incoming`
- `POST /api/voice/twilio/turn`
- `POST /api/voice/twilio/recording`
- `POST /api/voice/twilio/stream/status`
- `POST /api/voice/twilio/stream/fallback`
- `GET /api/voice/twilio/stream`

Onboarding configuration transfer:

- `POST /api/onboarding/configuration-import/preview`
- `POST /api/onboarding/configuration-import`

Worker:

- `make run-worker` processes capability-gated POS sync jobs from
  `pos_sync_jobs` and redacts expired call sessions. Unsupported provider
  writes are not faked as successful.

## Architecture

Handlers parse HTTP requests only. Services own business rules and tenant checks. Repositories own SQL. The API runs startup migrations before registering repositories. The `modules/pos` package defines the POSProvider boundary; `modules/pos_square` is the only concrete provider.

Booking code calls `POSProvider` only. Square payloads and API URLs must remain inside `modules/pos_square`. Booking workflows create backend `pos_pending` attempts before POS writes, then finalize the same attempts as confirmed/rescheduled/cancelled only on POS success or `fallback_pending` on POS failure. POS failures must not create confirmed appointments.
