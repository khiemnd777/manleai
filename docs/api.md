# API

Base URL: `http://localhost:18089`

All endpoints except login, bootstrap owner setup, refresh, logout, health,
public catalog endpoints, the Square OAuth callback, and Twilio voice webhooks
require:

```txt
Authorization: Bearer <access_token>
```

## Canonical Entity Semantics

Services, staff, and customers are ManleAI-owned canonical records.
The active POS provider remains the authority for real availability and booking
execution. API clients should treat ManleAI `id` values as product identity and
provider IDs as mappings used behind the provider-neutral booking contract.

Some endpoints still expose legacy Square provider fields alongside canonical
IDs because Square Appointments is the first real POS integration. API
responses include provider link and sync fields such as:

```json
{
  "sync_status": "synced",
  "pos_provider": "square",
  "pos_linked": true,
  "last_synced_at": "2026-06-10T15:00:00Z",
  "sync_error": ""
}
```

Sync status meanings:

- `local_only`: ManleAI record exists without an active-provider mapping.
- `syncing`: a provider sync or projection operation is in progress.
- `synced`: canonical record has a valid active-provider mapping.
- `sync_failed`: the latest provider sync failed and booking must be gated.
- `unmapped`: record needs an owner or sync workflow to link it to the provider.
- `archived`: record is retained for history but unavailable for new booking.

Local-only, unmapped, sync-failed, or archived services/staff may be visible in
management views after the canonical migration, but they must not be used for
availability checks or booking attempts. Confirmed, rescheduled, or cancelled
appointment responses still require active POS provider success and required
provider booking metadata.

## Auth

`POST /api/auth/login`

```json
{
  "email": "owner@example.com",
  "password": "your-password"
}
```

Returns access token, refresh token, user, roles, and primary salon ID.

`GET /api/auth/bootstrap/status`

Unauthenticated endpoint used by a fresh deployment to determine whether first
owner setup is still open.

```json
{
  "available": true
}
```

`POST /api/auth/bootstrap-owner`

Unauthenticated one-time endpoint for creating the first dashboard owner. It is
available only while the `users` table is empty. The backend assigns the
`salon_owner` role; clients do not send a role.

```json
{
  "email": "owner@example.com",
  "full_name": "Owner Name",
  "password": "minimum-8-characters"
}
```

Returns access token, refresh token, user, roles, and an empty primary salon ID
until onboarding creates the first salon. After any user exists, this endpoint
returns `409 BOOTSTRAP_CLOSED`.

`POST /api/auth/refresh-token`

```json
{
  "refresh_token": "..."
}
```

`POST /api/auth/logout`

```json
{
  "refresh_token": "..."
}
```

`GET /api/auth/me`

## Public Catalog

`GET /api/public/salon`

Public unauthenticated endpoint for the landing root. Returns the first
published salon catalog by `salons.created_at ASC, salons.id ASC`, so
`http://localhost:3090` can redirect to its canonical `/s/:slug` page.

`GET /api/public/salons/:slug`

Public unauthenticated endpoint for the customer-facing `landing/` app. Returns
only salons whose owner has enabled the public catalog. The response is
public-safe: no POS IDs, provider tokens, owner IDs, staff phone/email, sync
errors, or raw provider payloads are returned.

Only active, synced, POS-linked, AI-bookable services and staff for the active
POS provider are included. This endpoint does not check availability, create a
booking attempt, or confirm appointments.

```json
{
  "salon": {
    "slug": "lotus-nails-studio",
    "name": "Lotus Nails Studio",
    "phone": "+16292536211",
    "address": "1200 W Sample Ave",
    "city": "Chicago",
    "state": "IL",
    "zip_code": "60601",
    "timezone": "America/Chicago",
    "primary_language": "en",
    "secondary_language": "vi",
    "active_pos_provider": "square"
  },
  "services": [
    {
      "name": "Classic Manicure",
      "description": "Trim, shape, cuticle care, and polish.",
      "ai_description": "Classic manicure",
      "duration_minutes": 45,
      "price_from": 35,
      "price_display": "$35.00"
    }
  ],
  "staff": [
    {
      "name": "Mai Nguyen"
    }
  ],
  "hours": [
    {
      "day_of_week": 1,
      "start_local_time": "09:30:00",
      "end_local_time": "19:00:00",
      "source": "imported",
      "provider": "square"
    }
  ],
  "booking_note": "Appointments are confirmed only after Square Appointments completes the booking."
}
```

## Salons

`GET /api/salons`

Returns salons owned by the authenticated user.

`POST /api/salons`

```json
{
  "name": "Lotus Nails Studio",
  "phone": "+16292536211",
  "address": "1200 W Sample Ave",
  "city": "Chicago",
  "state": "IL",
  "zip_code": "60601",
  "timezone": "America/Chicago",
  "primary_language": "en",
  "secondary_language": "vi",
  "handoff_phone": "+13125550102"
}
```

`GET /api/salons/:id/settings`

`PUT /api/salons/:id/settings`

`GET /api/salons/:id/public-catalog`

Returns owner-scoped public page settings and publish readiness.

```json
{
  "salon_id": "...",
  "public_slug": "lotus-nails-studio",
  "public_catalog_enabled": true,
  "public_path": "/s/lotus-nails-studio",
  "bookable_service_count": 3,
  "bookable_staff_count": 2,
  "can_publish": true,
  "updated_at": "2026-06-25T14:30:00Z"
}
```

`PUT /api/salons/:id/public-catalog`

```json
{
  "public_slug": "lotus-nails-studio",
  "public_catalog_enabled": true
}
```

Enabling publish requires a valid unique slug plus at least one active,
synced, POS-linked, AI-bookable service and staff member for the active POS
provider. Disabling remains allowed so owners can take a page offline.

`GET /api/salons/:id/business-hours`

Returns Square-style business hour periods currently imported for the salon.
Owner-facing editing happens in Square Appointments; the local dashboard reads
these periods and can trigger Square sync.

```json
{
  "periods": [
    {
      "day_of_week": 1,
      "start_local_time": "09:30:00",
      "end_local_time": "13:00:00",
      "source": "imported",
      "provider": "square",
      "provider_location_id": "L123",
      "provider_period_index": 0,
      "last_synced_at": "2026-06-25T14:30:00Z"
    },
    {
      "day_of_week": 1,
      "start_local_time": "14:00:00",
      "end_local_time": "19:00:00",
      "source": "imported",
      "provider": "square",
      "provider_location_id": "L123",
      "provider_period_index": 1,
      "last_synced_at": "2026-06-25T14:30:00Z"
    }
  ]
}
```

`PUT /api/salons/:id/business-hours`

Returns `409 BUSINESS_HOURS_POS_MANAGED`. Business hours are managed in Square
and imported through `POST /api/integrations/square/sync`.

## Configuration Transfer

`GET /api/salons/:id/configuration-export`

Returns a sanitized, owner-scoped JSON snapshot for moving setup information to
another system or importing into another salon. This transfer bundle includes
configuration only:

- Salon profile
- AI receptionist settings
- Public booking page settings
- Integration runtime settings without secrets
- AI Training knowledge base

Secret-bearing integrations return only configuration values and secret status
metadata such as `client_secret_configured`, `auth_token_configured`,
`api_key_configured`, and `*_source`. It does not export services, staff,
customers, appointments, booking attempts, fallback requests, call sessions,
transcripts, recordings, summaries, owner corrections, POS OAuth tokens, API
keys, client secrets, encrypted secrets, or POS connection token state.

```json
{
  "schema_version": "manleai.salon_configuration.v2",
  "exported_at": "2026-06-26T15:00:00Z",
  "secrets_exported": false,
  "operational_data_exported": false,
  "excluded_data": ["services", "staff", "customers", "appointments", "call_sessions", "pos_oauth_tokens", "api_keys", "client_secrets"],
  "requires_secret_reentry": ["square", "twilio", "openai"],
  "salon_profile": {
    "name": "Lotus Nails Studio",
    "phone": "+16292536211",
    "address": "1200 W Sample Ave",
    "city": "Chicago",
    "state": "IL",
    "zip_code": "60601",
    "timezone": "America/Chicago",
    "primary_language": "en",
    "secondary_language": "vi",
    "handoff_phone": "+13125550102",
    "ai_enabled": true,
    "active_pos_provider": "square",
    "updated_at": "2026-06-25T14:30:00Z"
  },
  "ai_receptionist": {
    "ai_greeting": "Thanks for calling Lotus Nails Studio.",
    "ai_voice": "professional_female",
    "booking_mode": "confirmed_booking",
    "recording_enabled": true,
    "recording_consent_message": "This call may be recorded.",
    "sms_confirmation_enabled": true,
    "sms_reminder_enabled": true,
    "reminder_hours_before": 24,
    "handoff_enabled": true,
    "updated_at": "2026-06-25T14:30:00Z"
  },
  "public_booking_page": {
    "public_slug": "lotus-nails-studio",
    "public_catalog_enabled": true,
    "public_path": "/s/lotus-nails-studio",
    "updated_at": "2026-06-25T14:30:00Z"
  },
  "integrations": {
    "square": {
      "provider": "square",
      "configured": true,
      "environment": "sandbox",
      "client_id": "square-app-id",
      "redirect_url": "https://api.example.com/api/integrations/square/callback",
      "api_version": "2026-05-20",
      "client_secret_configured": true,
      "client_secret_source": "database"
    },
    "twilio": {
      "provider": "twilio",
      "configured": true,
      "public_base_url": "https://api.example.com",
      "incoming_path": "/api/voice/twilio/incoming",
      "turn_path": "/api/voice/twilio/turn",
      "recording_path": "/api/voice/twilio/recording",
      "stream_path": "/api/voice/twilio/stream",
      "voice_transport": "realtime_stream",
      "inbound_webhook_url": "https://api.example.com/api/voice/twilio/incoming",
      "turn_webhook_url": "https://api.example.com/api/voice/twilio/turn",
      "recording_webhook_url": "https://api.example.com/api/voice/twilio/recording",
      "stream_webhook_url": "wss://api.example.com/api/voice/twilio/stream",
      "auth_token_configured": true,
      "auth_token_source": "database"
    },
    "openai": {
      "provider": "openai",
      "enabled": true,
      "configured": true,
      "base_url": "https://api.openai.com/v1",
      "transcription_model": "gpt-4o-mini-transcribe",
      "reply_model": "gpt-4.1-mini",
      "speech_model": "gpt-4o-mini-tts",
      "speech_voice": "alloy",
      "realtime_enabled": true,
      "realtime_model": "gpt-4o-realtime-preview",
      "realtime_voice": "alloy",
      "realtime_instructions": "",
      "api_key_configured": true,
      "api_key_source": "database"
    }
  },
  "pos_connection": {
    "provider": "square",
    "status": "active",
    "merchant_id": "merchant-id",
    "location_id": "location-id",
    "scopes": ["APPOINTMENTS_READ", "APPOINTMENTS_WRITE"],
    "last_sync_at": "2026-06-25T14:30:00Z",
    "updated_at": "2026-06-25T14:30:00Z"
  },
  "knowledge_base": {
    "count": 1,
    "items": [
      {
        "source_key": "knowledge:3d9df47b6f8f4b97a7fcaf3e8d0f14be",
        "title": "Deposit policy",
        "category": "policy",
        "body": "Deposits are required for groups of four or more.",
        "status": "active",
        "source": "owner",
        "created_at": "2026-06-25T14:30:00Z",
        "updated_at": "2026-06-25T14:30:00Z"
      }
    ]
  }
}
```

`POST /api/salons/:id/configuration-import/preview`

Validates a transfer bundle and returns a dry-run summary. Preview does not
write to the database.

Request shape, with the full exported bundle in `configuration`:

```json
{
  "configuration": {
    "schema_version": "manleai.salon_configuration.v2",
    "...": "full exported configuration bundle"
  }
}
```

Response:

```json
{
  "request_id": "import-preview-id",
  "dry_run": true,
  "status": "previewed",
  "schema_version": "manleai.salon_configuration.v2",
  "can_apply": true,
  "summary": [
    {"section": "salon_profile", "created": 0, "updated": 6, "unchanged": 5, "skipped": 0, "conflicts": 0},
    {"section": "knowledge_base", "created": 4, "updated": 2, "unchanged": 8, "skipped": 0, "conflicts": 0}
  ],
  "warnings": [
    {"section": "integrations", "code": "secret_reentry_required", "message": "square secret values are not included in the export. Re-enter secrets or reconnect this provider after import.", "field": "square"}
  ],
  "conflicts": [],
  "excluded_data": ["services", "staff", "customers", "appointments", "call_sessions", "pos_oauth_tokens", "api_keys", "client_secrets"],
  "requires_secret_reentry": ["square", "twilio", "openai"]
}
```

`POST /api/salons/:id/configuration-import`

Applies a previously previewed transfer bundle. The request should include a
client-generated `request_id`. The backend stores an import run with
`payload_fingerprint`; reusing the same `request_id` with the same payload is
idempotent, while reusing it with a different payload returns a conflict.

```json
{
  "request_id": "2c8a6d65-b7ac-4e26-8d11-c2b37d2b8908",
  "configuration": {
    "schema_version": "manleai.salon_configuration.v2",
    "...": "full exported configuration bundle"
  }
}
```

Import idempotency:

- Salon profile, AI receptionist, public booking page, and integration settings update the existing salon-scoped rows.
- Integration secrets are preserved if already present and are never imported from the transfer file.
- Knowledge base entries upsert by `source_key`, backed by a unique `(salon_id, import_key)` index.
- Re-importing the same file reports `unchanged` or `updated`; it does not create duplicate knowledge rows.
- If the bundle requests `ai_enabled=true` or `booking_mode=confirmed_booking`, those fields are skipped unless the target salon has passed Square booking readiness.
- If the bundle requests public page publishing, the publish state is skipped unless the target salon has synced AI-bookable services and staff.

`POST /api/onboarding/configuration-import/preview`

Validates a transfer bundle before the owner has created a salon. This endpoint
is auth-only and does not require `salon_id`. It returns the same dry-run shape
as salon-scoped import preview, but `can_apply=false` with an
`owner_salon_exists` conflict when the owner already has a salon.

`POST /api/onboarding/configuration-import`

Creates the owner's first salon from a transfer bundle and applies supported
configuration in one transaction. The request should include a client-generated
`request_id`; reusing the same `request_id` and same payload is idempotent and
returns the same `salon_id` and `import_run_id`.

```json
{
  "request_id": "2c8a6d65-b7ac-4e26-8d11-c2b37d2b8908",
  "configuration": {
    "schema_version": "manleai.salon_configuration.v2",
    "...": "full exported configuration bundle"
  }
}
```

Response includes the created salon id:

```json
{
  "import_run_id": "...",
  "salon_id": "...",
  "request_id": "2c8a6d65-b7ac-4e26-8d11-c2b37d2b8908",
  "dry_run": false,
  "status": "applied",
  "schema_version": "manleai.salon_configuration.v2",
  "can_apply": true,
  "summary": [],
  "warnings": [],
  "conflicts": [],
  "excluded_data": ["services", "staff", "customers", "appointments", "call_sessions", "pos_oauth_tokens", "api_keys", "client_secrets"],
  "requires_secret_reentry": ["square", "twilio", "openai"]
}
```

Onboarding import never imports services, staff, customers, appointments, POS
tokens, API keys, client secrets, or encrypted secrets. If the bundle requests
`ai_enabled=true`, `booking_mode=confirmed_booking`, or public catalog
publishing, those live states are skipped until Square Appointments is
connected, synced, and booking-ready.

## Integration Configuration

Provider credentials and runtime settings are salon-scoped and owner-scoped.
Secret values are write-only: responses only expose whether a secret is
configured and whether it came from dashboard storage or environment fallback.

`GET /api/salons/:id/integration-configs`

```json
{
  "square": {
    "provider": "square",
    "configured": true,
    "environment": "sandbox",
    "client_id": "square-app-id",
    "redirect_url": "https://api.example.com/api/integrations/square/callback",
    "api_version": "2026-05-20",
    "client_secret_configured": true,
    "client_secret_source": "database"
  },
  "twilio": {
    "provider": "twilio",
    "configured": true,
    "public_base_url": "https://api.example.com",
    "incoming_path": "/api/voice/twilio/incoming",
    "turn_path": "/api/voice/twilio/turn",
    "recording_path": "/api/voice/twilio/recording",
    "stream_path": "/api/voice/twilio/stream",
    "voice_transport": "realtime_stream",
    "inbound_webhook_url": "https://api.example.com/api/voice/twilio/incoming",
    "turn_webhook_url": "https://api.example.com/api/voice/twilio/turn",
    "recording_webhook_url": "https://api.example.com/api/voice/twilio/recording",
    "stream_webhook_url": "wss://api.example.com/api/voice/twilio/stream",
    "auth_token_configured": true,
    "auth_token_source": "database"
  },
  "openai": {
    "provider": "openai",
    "enabled": true,
    "configured": true,
    "base_url": "https://api.openai.com/v1",
    "transcription_model": "gpt-4o-mini-transcribe",
    "reply_model": "gpt-4.1-mini",
    "speech_model": "gpt-4o-mini-tts",
    "speech_voice": "alloy",
    "realtime_enabled": true,
    "realtime_model": "gpt-4o-realtime-preview",
    "realtime_voice": "alloy",
    "realtime_instructions": "",
    "api_key_configured": true,
    "api_key_source": "database"
  }
}
```

`PUT /api/salons/:id/integration-configs/square`

```json
{
  "environment": "sandbox",
  "client_id": "square-app-id",
  "client_secret": "new-secret-or-empty-to-keep-existing",
  "clear_client_secret": false,
  "redirect_url": "https://api.example.com/api/integrations/square/callback",
  "api_version": "2026-05-20",
  "api_base_url": ""
}
```

`PUT /api/salons/:id/integration-configs/twilio`

```json
{
  "public_base_url": "https://api.example.com",
  "auth_token": "new-token-or-empty-to-keep-existing",
  "clear_auth_token": false,
  "incoming_path": "/api/voice/twilio/incoming",
  "turn_path": "/api/voice/twilio/turn",
  "recording_path": "/api/voice/twilio/recording",
  "stream_path": "/api/voice/twilio/stream",
  "voice_transport": "realtime_stream"
}
```

`PUT /api/salons/:id/integration-configs/openai`

```json
{
  "enabled": true,
  "api_key": "new-key-or-empty-to-keep-existing",
  "clear_api_key": false,
  "base_url": "https://api.openai.com/v1",
  "transcription_model": "gpt-4o-mini-transcribe",
  "reply_model": "gpt-4.1-mini",
  "speech_model": "gpt-4o-mini-tts",
  "speech_voice": "alloy",
  "realtime_enabled": true,
  "realtime_model": "gpt-4o-realtime-preview",
  "realtime_voice": "alloy",
  "realtime_instructions": ""
}
```

`GET /api/salons/:id/services`

Returns ManleAI-owned services for dashboard tables, including Square-imported
services and local-only services. A service without a valid active-provider link
is manageable by the owner but not eligible for availability or booking.

```json
{
  "services": [
    {
      "id": "...",
      "salon_id": "...",
      "pos_provider": "square",
      "pos_service_id": "SQUARE_SERVICE_VARIATION_ID",
      "pos_service_version": 123,
      "name": "Classic Manicure",
      "description": "Trim, shape, cuticle care, and polish.",
      "ai_description": "Classic manicure",
      "duration_minutes": 45,
      "price_from": 35,
      "price_display": "$35.00",
      "ai_bookable": true,
      "active": true,
      "sync_status": "synced",
      "last_synced_at": "2026-06-10T15:00:00Z",
      "sync_error": "",
      "source": "imported",
      "pos_linked": true
    }
  ]
}
```

`POST /api/salons/:id/services`

Creates a local ManleAI service. Local-only services are visible in the Services
dashboard but cannot be used for availability or booking until an active POS
provider link exists. The API always creates local services with
`ai_bookable=false`, `sync_status=local_only`, and `pos_linked=false`.
If the active provider later declares service write support, the mutation can
queue a `pos_sync_jobs` outbox job and move through `syncing`, `synced`, or
`sync_failed`. Square Appointments service writes are not enabled in the current
slice.

```json
{
  "name": "Gel Removal",
  "description": "Removal service for existing gel polish.",
  "ai_description": "Gel polish removal",
  "duration_minutes": 20,
  "price_from": 10,
  "active": true
}
```

Returns `201`:

```json
{
  "service": {
    "id": "...",
    "name": "Gel Removal",
    "duration_minutes": 20,
    "price_from": 10,
    "ai_bookable": false,
    "active": true,
    "sync_status": "local_only",
    "source": "local",
    "pos_linked": false
  }
}
```

`PUT /api/salons/:id/services/:service_id`

Updates a non-archived service's canonical ManleAI fields. This does not edit
Square Appointments records or push changes to a POS provider. If `active=false`
is saved, `ai_bookable` is also disabled.

```json
{
  "name": "Classic Manicure",
  "description": "Trim, shape, cuticle care, and polish.",
  "ai_description": "Classic manicure",
  "duration_minutes": 45,
  "price_from": 35,
  "active": true
}
```

`POST /api/salons/:id/services/:service_id/archive`

Soft-archives a service by setting `active=false`, `ai_bookable=false`,
`sync_status=archived`, and `archived_at`. Archived services remain visible for
history but cannot be used for availability or booking.

`PATCH /api/salons/:id/services/:service_id/ai-bookable`

```json
{
  "ai_bookable": false
}
```

Updates only the internal AI booking eligibility flag for a service. Square
service records are not edited. A service cannot be enabled for AI booking when
it is inactive, archived, local-only, unmapped, sync-failed, or missing a valid
link for the active POS provider.

`GET /api/salons/:id/staff`

Returns ManleAI-owned staff records for dashboard tables, including
Square-imported staff and local-only staff. A staff member without a valid
active-provider link is manageable by the owner but not eligible for
availability or booking.

```json
{
  "staff": [
    {
      "id": "...",
      "salon_id": "...",
      "pos_provider": "square",
      "pos_staff_id": "SQUARE_TEAM_MEMBER_ID",
      "name": "Mai Nguyen",
      "phone": "+13125550101",
      "email": "mai@example.com",
      "ai_bookable": true,
      "active": true,
      "sync_status": "synced",
      "last_synced_at": "2026-06-10T15:00:00Z",
      "sync_error": "",
      "source": "imported",
      "pos_linked": true
    }
  ]
}
```

`POST /api/salons/:id/staff`

Creates a local ManleAI staff member. Local-only staff are visible in the Staff
dashboard but cannot be used for availability or booking until an active POS
provider link exists. The API always creates local staff with
`ai_bookable=false`, `sync_status=local_only`, and `pos_linked=false`.
If the active provider later declares staff write support, the mutation can
queue a `pos_sync_jobs` outbox job and move through `syncing`, `synced`, or
`sync_failed`. Square Appointments staff writes are not enabled in the current
slice.

```json
{
  "name": "Linh Tran",
  "phone": "+13125550102",
  "email": "linh@example.com",
  "active": true
}
```

Returns `201`:

```json
{
  "staff_member": {
    "id": "...",
    "name": "Linh Tran",
    "phone": "+13125550102",
    "email": "linh@example.com",
    "ai_bookable": false,
    "active": true,
    "sync_status": "local_only",
    "source": "local",
    "pos_linked": false
  }
}
```

`PUT /api/salons/:id/staff/:staff_id`

Updates a non-archived staff member's canonical ManleAI fields. This does not
edit Square Appointments records or push changes to a POS provider. If
`active=false` is saved, `ai_bookable` is also disabled.

```json
{
  "name": "Mai Nguyen",
  "phone": "+13125550101",
  "email": "mai@example.com",
  "active": true
}
```

`POST /api/salons/:id/staff/:staff_id/archive`

Soft-archives a staff member by setting `active=false`, `ai_bookable=false`,
`sync_status=archived`, and `archived_at`. Archived staff remain visible for
history but cannot be used for availability or booking.

`PATCH /api/salons/:id/staff/:staff_id/ai-bookable`

```json
{
  "ai_bookable": true
}
```

Updates only the internal AI booking eligibility flag for a staff member. Square
staff records are not edited. A staff member cannot be enabled for AI booking
when inactive, archived, local-only, unmapped, sync-failed, or missing a valid
link for the active POS provider.

`POST /api/salons/:id/availability`

Returns provider-neutral available booking slots from the active POS provider
for one or more AI-bookable services and optional AI-bookable staff members. Use
`segments` for multi-service booking; the legacy `service_id` and `staff_id`
fields remain supported for single-service booking. These IDs are ManleAI
canonical IDs that must resolve through a valid active-provider link before the
POS adapter is called.
`staff_selection_mode` is either `specific` or `anyone`; `anyone` means the
customer did not request a named technician. Results are filtered to synced
business hour periods in the salon timezone. A slot must fit inside one imported
period; split-day periods are not collapsed. This endpoint does not create a
booking attempt or confirm an appointment.

```json
{
  "service_id": "...",
  "staff_id": "...",
  "staff_selection_mode": "specific",
  "segments": [
    {
      "service_id": "...",
      "staff_id": "...",
      "staff_selection_mode": "specific"
    }
  ],
  "preferred_date": "2026-06-10",
  "limit": 5
}
```

Returns:

```json
{
  "service_id": "...",
  "service_name": "Classic Manicure",
  "staff_id": "...",
  "staff_name": "Mai Nguyen",
  "staff_selection_mode": "specific",
  "segments": [
    {
      "service_id": "...",
      "service_name": "Classic Manicure",
      "staff_id": "...",
      "staff_name": "Mai Nguyen",
      "staff_selection_mode": "specific",
      "duration_minutes": 45
    }
  ],
  "preferred_date": "2026-06-10",
  "duration_minutes": 45,
  "timezone": "America/Chicago",
  "slots": [
    {
      "start_time": "2026-06-10T15:00:00Z",
      "end_time": "2026-06-10T15:45:00Z",
      "staff_id": "...",
      "staff_name": "Mai Nguyen",
      "staff_selection_mode": "specific",
      "segments": [
        {
          "service_id": "...",
          "service_name": "Classic Manicure",
          "staff_id": "...",
          "staff_name": "Mai Nguyen",
          "staff_selection_mode": "specific",
          "duration_minutes": 45
        }
      ]
    }
  ]
}
```

`GET /api/salons/:id/customers`

Returns ManleAI-owned customer records plus owner-scoped activity from internal
appointments, booking attempts, call sessions, and handoff requests. This is not
a full Square customer directory sync. A row with no `id` is activity-only and
can be converted into a local customer record by the owner. POS customer links
are optional mappings used when the active provider requires a customer ID for
booking.

```json
{
  "customers": [
    {
      "id": "...",
      "salon_id": "...",
      "key": "phone:+13125550101",
      "name": "Linh Tran",
      "phone": "+13125550101",
      "email": "linh@example.com",
      "notes": "Prefers quiet appointments.",
      "active": true,
      "sync_status": "synced",
      "last_synced_at": "2026-06-10T15:00:00Z",
      "sync_error": "",
      "source": "local",
      "pos_linked": true,
      "last_activity_at": "2026-06-10T15:00:00Z",
      "last_activity_source": "appointment",
      "last_outcome": "confirmed",
      "confirmed_appointments": 1,
      "pending_requests": 0,
      "call_count": 2,
      "handoff_count": 0,
      "appointment_ids": ["..."],
      "booking_attempt_ids": ["..."],
      "call_session_ids": ["..."]
    }
  ],
  "summary": {
    "total_known_customers": 1,
    "confirmed_appointments": 1,
    "pending_requests": 0,
    "customers_with_calls": 1,
    "last_customer_activity_at": "2026-06-10T15:00:00Z"
  }
}
```

`POST /api/salons/:id/customers`

Creates a local ManleAI customer. The request must include `name` and at least
one contact field. Active, non-archived customers are deduped by normalized
phone and normalized email per salon.

```json
{
  "name": "Linh Tran",
  "phone": "+13125550101",
  "email": "linh@example.com",
  "notes": "Prefers quiet appointments.",
  "active": true
}
```

Returns `201` with `{ "customer": { ... } }`. Duplicate active phone or email
returns `409`.

`PUT /api/salons/:id/customers/:customer_id`

Updates a non-archived local customer record. This does not edit Square
Appointments records or expose provider payloads.

`POST /api/salons/:id/customers/:customer_id/archive`

Soft-archives a customer by setting `active=false`, `sync_status=archived`, and
`archived_at`. Existing activity remains visible in customer history.

`GET /api/salons/:id/customers/search?phone=<phone>&provider=square`

Searches the active provider by phone through `POSProvider.SearchCustomerByPhone` and returns only the normalized provider-neutral customer DTO. It does not create customers, sync the Square directory, or expose Square payloads/tokens.

```json
{
  "found": true,
  "provider": "square",
  "customer": {
    "pos_customer_id": "CUSTOMER_ID",
    "name": "Linh Tran",
    "phone": "+13125550101",
    "email": "linh@example.com"
  }
}
```

`GET /api/salons/:id/appointments`

Returns appointments recorded after POS success, including confirmed, rescheduled, and cancelled statuses. Each item includes `staff_selection_mode` and, when available, ordered `segments[]` from `appointment_services` with service names, assigned technician names, durations, and segment-level staff selection mode. `staff_selection_mode=anyone` means the customer did not request a named technician even though the POS-confirmed appointment stores the staff assignment used to book.

`POST /api/salons/:id/appointments/:appointment_id/reschedule`

```json
{
  "start_time": "2026-06-11T16:00:00Z",
  "staff_id": "...",
  "notes": "Customer requested later time"
}
```

Returns `200` with the updated appointment only when the active `POSProvider` successfully reschedules the POS booking. Returns `202` with status `fallback_pending` when the POS provider fails; the internal appointment remains unchanged.

`POST /api/salons/:id/appointments/:appointment_id/cancel`

```json
{
  "reason": "Customer requested cancellation"
}
```

Returns `200` with the cancelled appointment only when the active `POSProvider` successfully cancels the POS booking. Returns `202` with status `fallback_pending` when the POS provider fails; the internal appointment remains unchanged.

`GET /api/salons/:id/booking-attempts`

Returns booking attempts, including transient `pos_pending` records and `fallback_pending` records that need owner review. Each item includes `staff_selection_mode` and, when available, ordered `segments[]` from `booking_attempt_segments` so owner dashboards can distinguish the customer preference (`anyone` or `specific`) from the staff assignment attempted through the POS provider.

`POST /api/salons/:id/booking-attempts`

```json
{
  "customer_name": "Linh Tran",
  "customer_phone": "+13125550101",
  "customer_email": "linh@example.com",
  "service_id": "...",
  "staff_id": "...",
  "staff_selection_mode": "specific",
  "segments": [
    {
      "service_id": "...",
      "staff_id": "...",
      "staff_selection_mode": "specific"
    }
  ],
  "start_time": "2026-06-10T15:00:00Z",
  "notes": "First visit"
}
```

Creates a backend booking attempt before calling the active `POSProvider`. For
multi-service booking, each segment is resolved to provider-neutral
service/staff records and persisted in `booking_attempt_segments`; confirmed
appointments snapshot the same ordered segments in `appointment_services`.
Booking resolution translates ManleAI canonical IDs through valid
active-provider links before calling the POS
adapter. Local-only, unmapped, sync-failed, or archived records must not be
booked. `staff_selection_mode=anyone` records that the customer did not request
a named technician; the backend still stores the POS-compatible staff assignment
used for the booking attempt. Returns `201` with status `confirmed` only when
the POS provider returns a POS booking ID and booking version. Returns `202`
with status `fallback_pending` when the POS provider fails, times out, or does
not return required booking metadata.

## Conversation Sessions

`GET /api/salons/:id/conversation-sessions`

Returns recent conversation sessions for the authenticated owner, including `simulator` and `phone` channels.

`POST /api/salons/:id/conversation-sessions`

```json
{
  "channel": "simulator"
}
```

Creates a simulator session and writes the initial AI transcript message.

`GET /api/salons/:id/conversation-sessions/:session_id`

Returns one conversation session with transcript messages and the latest handoff request when present.

`POST /api/salons/:id/conversation-sessions/:session_id/messages`

```json
{
  "message": "My name is Linh Tran, phone 312-555-0199, classic manicure with Mai on 2026-06-10 at 3pm."
}
```

Processes one simulated customer message through the deterministic conversation engine. The simulator asks one question at a time, can create owner handoffs for human requests or disabled AI booking, checks provider-neutral availability before selecting a booking time, offers available slots from Square Appointments, and calls the provider-neutral booking service only after the customer selects a slot and required customer details are collected. Offered slots may include ordered `segments` with provider-neutral service/staff assignments. Once a customer selects a slot, the session stores selected `booking_segments` and `staff_selection_mode` so simulator and phone flows can create one multi-service POS-first booking request. When `staff_selection_mode=anyone`, the customer did not choose a named technician, so the conversation avoids presenting the POS staff assignment as a customer-selected technician. A simulator booking is marked `booking_confirmed` only when the booking service returns a confirmed booking attempt with a POS booking ID and appointment. POS failures create `booking_fallback_pending` wording and do not create confirmed appointment language.

Phone channel sessions are created by Twilio webhooks and use the same conversation engine. Phone bookings use source `ai_voice_call`; simulator bookings use source `ai_conversation_simulator`.

## AI Training

`GET /api/salons/:id/knowledge-items`

Returns owner-scoped salon knowledge items.

`POST /api/salons/:id/knowledge-items`

```json
{
  "title": "Late arrival policy",
  "category": "policy",
  "body": "Customers can arrive up to 10 minutes late before the owner needs to review the appointment.",
  "status": "active"
}
```

`PUT /api/salons/:id/knowledge-items/:item_id`

Updates title, category, body, and status.

`DELETE /api/salons/:id/knowledge-items/:item_id`

Deletes an owner-scoped knowledge item.

`GET /api/salons/:id/owner-corrections`

Returns recent owner corrections.

`POST /api/salons/:id/owner-corrections`

```json
{
  "call_session_id": "...",
  "transcript_message_id": "...",
  "correction": "Mention the group deposit policy before collecting booking details."
}
```

When `transcript_message_id` is provided, `call_session_id` is required so the backend can validate the correction source against the owner-scoped call session.

`POST /api/salons/:id/owner-corrections/:correction_id/apply`

Creates a knowledge item from the correction and marks the correction as `applied`.

`POST /api/salons/:id/owner-corrections/:correction_id/dismiss`

Marks the correction as `dismissed`.

`POST /api/salons/:id/training/evaluate`

```json
{
  "message": "Do you take walk-ins?"
}
```

Returns a read-only preview that uses active salon knowledge without creating a call session, writing transcript rows, calling voice providers, or calling the booking service.

```json
{
  "message": "Do you take walk-ins?",
  "reply": "Walk-ins are accepted when staff is available. Would you like help with an appointment?",
  "matched_knowledge": {
    "title": "Walk-in policy",
    "category": "policy",
    "body": "Walk-ins are accepted when staff is available."
  },
  "outcome": "knowledge_answer",
  "booking_action": "none",
  "pos_confirmation_required": true
}
```

## Voice

`GET /api/salons/:id/voice/status`

Returns owner-scoped live voice, phone booking, and external AI provider readiness without exposing Twilio, OpenAI, or POS token secrets. `ready` means Twilio can route live phone webhooks; `phone_booking_ready` means the phone path also has the booking prerequisites needed to offer available slots from the active POS provider and attempt POS-first confirmation.

```json
{
  "provider": "twilio",
  "configured": true,
  "signature_verification": true,
  "inbound_webhook_url": "https://api.example.com/api/voice/twilio/incoming",
  "turn_webhook_url": "https://api.example.com/api/voice/twilio/turn",
  "recording_webhook_url": "https://api.example.com/api/voice/twilio/recording",
  "stream_webhook_url": "wss://api.example.com/api/voice/twilio/stream",
  "salon_phone": "+16292536211",
  "ready": true,
  "phone_booking_ready": true,
  "input_mode": "recording",
  "booking": {
    "ready": true,
    "ai_enabled": true,
    "active_provider": "square",
    "provider_connected": true,
    "provider_synced": true,
    "square_connected": true,
    "square_synced": true,
    "test_booking_cancelled": true,
    "service_count": 4,
    "staff_count": 3,
    "business_hours_count": 6,
    "checks": [
      {"key": "connect_square", "label": "Connect Square Appointments", "complete": true},
      {"key": "sync_square", "label": "Sync Square calendar", "complete": true},
      {"key": "bookable_services", "label": "AI-bookable services", "complete": true},
      {"key": "bookable_staff", "label": "AI-bookable staff", "complete": true},
      {"key": "business_hours", "label": "Business hours", "complete": true},
      {"key": "enable_ai_booking", "label": "Enable AI booking", "complete": true}
    ]
  },
  "ai": {
    "provider": "openai",
    "configured": true,
    "ready": true,
    "stt": {
      "provider": "openai",
      "configured": true,
      "ready": true,
      "model": "gpt-4o-mini-transcribe"
    },
    "llm": {
      "provider": "openai",
      "configured": true,
      "ready": true,
      "model": "gpt-4.1-mini"
    },
    "tts": {
      "provider": "openai",
      "configured": true,
      "ready": true,
      "model": "gpt-4o-mini-tts",
      "voice": "alloy"
    },
    "realtime": {
      "provider": "openai",
      "configured": true,
      "ready": true,
      "model": "gpt-4o-realtime-preview",
      "voice": "alloy"
    }
  }
}
```

`POST /api/voice/twilio/incoming`

Public Twilio Programmable Voice webhook for a new inbound call. Requires a valid `X-Twilio-Signature` generated with the salon's configured Twilio auth token. The webhook matches Twilio `To` to the salon phone, creates or reuses a `phone` conversation session, records a `voice_webhook_events` audit row, and returns TwiML for the configured input mode: speech `<Gather>`, recording mode, or `<Connect><Stream>` when realtime streaming is enabled and ready.

Expected Twilio form fields include:

```txt
CallSid
From
To
```

`POST /api/voice/twilio/turn`

Public Twilio Programmable Voice webhook for gathered speech turns. Requires a valid `X-Twilio-Signature`. The webhook finds the `phone` session by `CallSid`, appends the customer speech to the transcript, and returns TwiML. The response continues gathering only while the session remains active. Completed, fallback, and handoff outcomes return final TwiML and hang up.

Expected Twilio form fields include:

```txt
CallSid
From
To
SpeechResult
```

`POST /api/voice/twilio/recording`

Public Twilio Programmable Voice webhook for recording-mode turns when external STT is configured. Requires a valid `X-Twilio-Signature`. The webhook downloads the Twilio recording, sends audio through the configured STT provider behind `modules/voice`, then routes the resulting text through the same conversation engine and booking service. STT failures produce safe reprompt/fallback behavior and do not fabricate customer intent.

Expected Twilio form fields include:

```txt
CallSid
AccountSid
From
To
RecordingUrl
```

`GET /api/voice/twilio/stream`

Public Twilio Media Streams WebSocket endpoint for realtime audio mode. The endpoint is not configured directly in Twilio Console; the incoming webhook returns `<Connect><Stream>` with signed custom parameters for the existing call session. The stream forwards Twilio g711 audio frames to the configured OpenAI Realtime adapter, routes completed transcripts through the same backend conversation engine and booking service, then streams backend-approved audio responses back to Twilio. If realtime configuration is missing, voice status falls back to the recording or gather path.

`GET /api/voice/audio/:id`

Public short-lived audio output endpoint for Twilio `<Play>` responses. IDs are unguessable runtime UUIDs, expire quickly, and never expose POS tokens or provider secrets.

The phone path never confirms an appointment unless the booking service returns a POS-confirmed booking attempt with a POS booking ID and appointment.

## POS Provider Switch Readiness

`GET /api/salons/:id/pos/provider-switch-readiness`

Returns the active POS provider, installed adapters, unavailable adapter gate,
provider mapping counts, and readiness checks. This endpoint is read-only in
the current pilot. It does not activate a new provider or create fake provider
support.

```json
{
  "salon_id": "...",
  "active_provider": "square",
  "active_provider_label": "Square Appointments",
  "installed_providers": [
    {
      "provider": "square",
      "label": "Square Appointments",
      "installed": true,
      "active": true,
      "status": "active",
      "capabilities": {
        "service_upsert": false,
        "service_archive": false,
        "staff_upsert": false,
        "staff_archive": false,
        "customer_upsert": false
      }
    }
  ],
  "unavailable_providers": [
    {
      "provider": "",
      "label": "No alternate POS adapter installed",
      "installed": false,
      "active": false,
      "status": "disabled",
      "blocked_reason": "Square Appointments is the only native POS integration in this pilot release."
    }
  ],
  "mapping": {
    "service_count": 4,
    "staff_count": 3,
    "customer_count": 12,
    "bookable_service_count": 4,
    "bookable_staff_count": 3,
    "linked_service_count": 4,
    "linked_staff_count": 3,
    "linked_customer_count": 6,
    "unmapped_service_count": 0,
    "unmapped_staff_count": 0,
    "sync_failed_count": 0
  },
  "checks": [
    {"key": "active_provider", "label": "Active provider selected", "complete": true},
    {"key": "alternate_adapter", "label": "Alternate provider adapter installed", "complete": false}
  ],
  "dry_run_booking_ready": true,
  "can_start_switch": false,
  "can_activate_provider": false,
  "blocked_reason": "No alternate POS adapter is installed in this pilot deployment."
}
```

`POST /api/salons/:id/pos/provider-switch-runs`

Creates a provider switch run record for a requested target provider. If the
target adapter is not installed, the run is persisted with `status=blocked` and
no import/match work is started. If a real target adapter is installed in a
future slice, the backend can populate service/staff/customer match candidates,
but activation still remains disabled in this phase. The current dashboard
import wizard shell does not call this endpoint while no alternate native POS
adapter is installed.

```json
{
  "to_provider": "future_provider"
}
```

Response:

```json
{
  "run": {
    "id": "...",
    "salon_id": "...",
    "from_provider": "square",
    "to_provider": "future_provider",
    "status": "blocked",
    "blocked_reason": "The requested POS provider adapter is not installed in this deployment.",
    "dry_run_ready": false,
    "can_activate": false,
    "match_summary": {
      "total": 0,
      "suggested": 0,
      "unmatched": 0,
      "conflicts": 0,
      "confirmed": 0,
      "skipped": 0
    },
    "matches": []
  }
}
```

`GET /api/salons/:id/pos/provider-switch-runs/latest`

Returns the latest switch run for the salon, or `{"run": null}` when no run has
been created.

`GET /api/salons/:id/pos/provider-switch-runs/:run_id`

Returns one owner-scoped switch run with its match summary and match rows.
Match rows are provider-neutral and may include `service`, `staff`, or
`customer` entities. There is intentionally no activation endpoint in the
current pilot.

`GET /api/salons/:id/pos/provider-switch-runs/:run_id/dry-run-readiness`

Returns the dry-run checklist for one owner-scoped provider switch run. This
endpoint is read-only. It does not call a target POS provider, create
appointments, create provider links, mark `dry_run_ready=true`, or activate a
provider. In the current pilot it remains blocked because no alternate native
POS adapter and no alternate-provider dry-run executor exist.

```json
{
  "run_id": "...",
  "salon_id": "...",
  "from_provider": "square",
  "to_provider": "future_provider",
  "status": "ready",
  "checks": [
    {"key": "target_adapter", "label": "Target provider adapter installed", "complete": false, "message": "The target POS provider adapter is not installed in this deployment."},
    {"key": "switch_run_reviewable", "label": "Switch run is reviewable", "complete": true},
    {"key": "imported_records", "label": "Imported provider records exist", "complete": false, "message": "Import records from a real alternate POS provider before dry-run checks can pass."},
    {"key": "matches_resolved", "label": "Match conflicts resolved", "complete": false, "message": "Resolve suggested, unmatched, or conflicting provider matches before dry-run."},
    {"key": "current_provider_booking_ready", "label": "Current provider booking readiness passed", "complete": true},
    {"key": "dry_run_execution_available", "label": "Alternate-provider dry-run execution available", "complete": false, "message": "Alternate-provider dry-run execution is not implemented in this pilot slice."}
  ],
  "can_run_dry_run": false,
  "dry_run_ready": false,
  "can_activate": false,
  "blocked_reason": "The target POS provider adapter is not installed in this deployment."
}
```

`PATCH /api/salons/:id/pos/provider-switch-runs/:run_id/matches/:match_id`

Updates one match review decision for an owner-scoped switch run. Allowed
`match_status` values are `confirmed`, `unmatched`, and `skipped`. Confirming a
match records the owner's review decision only; it does not create provider
links, run a dry-run booking, or activate a provider.

```json
{
  "match_status": "confirmed"
}
```

Response:

```json
{
  "run": {
    "id": "...",
    "status": "ready",
    "can_activate": false,
    "match_summary": {
      "total": 1,
      "suggested": 0,
      "unmatched": 0,
      "conflicts": 0,
      "confirmed": 1,
      "skipped": 0
    },
    "matches": [
      {
        "id": "...",
        "entity_type": "service",
        "canonical_entity_id": "...",
        "canonical_name": "Classic Manicure",
        "provider_entity_id": "...",
        "provider_name": "Classic Manicure",
        "match_status": "confirmed",
        "match_confidence": 100,
        "match_reason": "Owner confirmed this provider match."
      }
    ]
  }
}
```

## Square

`GET /api/integrations/square/connect-url?salon_id=<id>`

Returns a Square OAuth URL and state.

`GET /api/integrations/square/callback?code=<code>&state=<state>`

Exchanges the Square OAuth code and stores encrypted tokens.

`GET /api/integrations/square/status?salon_id=<id>`

Returns the Square connection, recent sync logs, and AI booking readiness checks,
including `business_hour_period_count` for the selected Square location import.

`GET /api/integrations/square/locations?salon_id=<id>`

Lists Square locations through the Square adapter.

`POST /api/integrations/square/select-location`

```json
{
  "salon_id": "...",
  "location_id": "..."
}
```

`POST /api/integrations/square/sync`

Imports Square services, staff, selected-location business hour periods, and
customers into local canonical tables. The button label is `Sync` in the owner
dashboard because the action is no longer limited to services and staff.

```json
{
  "salon_id": "..."
}
```

```json
{
  "ok": true,
  "summary": {
    "services_synced": 12,
    "staff_synced": 4,
    "business_hour_periods_synced": 8,
    "customers_synced": 250,
    "customers_skipped": 0
  }
}
```

`POST /api/integrations/square/test-booking`

```json
{
  "salon_id": "...",
  "customer_name": "ManleAI Test Customer",
  "customer_phone": "+13125550199",
  "customer_email": "",
  "service_id": "...",
  "staff_id": "...",
  "start_time": "2026-06-11T15:00:00Z",
  "notes": "AI booking readiness test."
}
```

Creates a real Square booking through the provider-neutral booking service. Returns `201` when Square confirms the booking, or `202` with `fallback_pending` when POS booking fails.

`POST /api/integrations/square/cancel-test-booking`

```json
{
  "salon_id": "...",
  "appointment_id": "...",
  "reason": "AI booking readiness test cleanup"
}
```

Cancels the latest Square test booking through the provider-neutral booking service. Returns `200` when Square cancels the booking, or `202` with `fallback_pending` when POS cancellation fails.

`POST /api/integrations/square/enable-ai-booking`

```json
{
  "salon_id": "..."
}
```

Sets `salons.ai_enabled=true` only after Square is connected, a location is selected, services/staff/business hours are synced, and at least one service and staff member are AI-bookable. Square test booking create/cancel remains an optional POS write smoke test and is not an AI enablement gate.

`POST /api/integrations/square/disable-ai-booking`

```json
{
  "salon_id": "..."
}
```

Sets `salons.ai_enabled=false`.
