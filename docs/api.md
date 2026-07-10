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

Returns and updates owner-scoped AI receptionist settings. `ai_tone` accepts
`professional_warm`, `natural_human`, `friendly_young`, or `concise_calm`.
The tone controls spoken reply style only; booking guardrails and POS-first
confirmation rules still override style.

```json
{
  "ai_greeting": "Thanks for calling Lotus Nails Studio.",
  "ai_voice": "professional_female",
  "ai_tone": "natural_human",
  "booking_mode": "pending_approval",
  "recording_enabled": true,
  "recording_consent_message": "This call may be recorded.",
  "sms_confirmation_enabled": true,
  "sms_reminder_enabled": true,
  "reminder_hours_before": 24,
  "handoff_enabled": true
}
```

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
- Service category taxonomy and service category aliases
- Service aliases that can be matched to existing target services
- AI Training knowledge base

Secret-bearing integrations return only configuration values and secret status
metadata such as `client_secret_configured`, `auth_token_configured`,
`api_key_configured`, and `*_source`. It does not export services, staff,
customers, appointments, booking attempts, fallback requests, call sessions,
transcripts, recordings, summaries, owner corrections, POS entity links, POS
sync jobs/logs/errors, provider switch runs/matches, synced business hour
periods, party booking requests, voice webhook/audio records, POS OAuth tokens,
API keys, client secrets, encrypted secrets, or POS connection token state.

```json
{
  "schema_version": "manleai.salon_configuration.v5",
  "exported_at": "2026-06-26T15:00:00Z",
  "secrets_exported": false,
  "operational_data_exported": false,
  "excluded_data": ["services", "staff", "customers", "appointments", "call_sessions", "pos_entity_links", "pos_sync_jobs", "pos_sync_logs", "pos_errors", "provider_switch_runs", "provider_switch_matches", "salon_business_hour_periods", "party_booking_requests", "voice_webhook_events", "voice_audio_outputs", "pos_oauth_tokens", "api_keys", "client_secrets", "encrypted_secrets"],
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
    "ai_tone": "natural_human",
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
      "realtime_model": "gpt-realtime-2",
      "realtime_voice": "alloy",
      "realtime_noise_profile": "noisy_salon",
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
    "scopes": ["APPOINTMENTS_READ", "APPOINTMENTS_ALL_READ", "APPOINTMENTS_WRITE", "APPOINTMENTS_ALL_WRITE"],
    "last_sync_at": "2026-06-25T14:30:00Z",
    "updated_at": "2026-06-25T14:30:00Z"
  },
  "service_categories": {
    "count": 1,
    "items": [
      {
        "source_key": "service_category:manicure",
        "name": "Manicure",
        "slug": "manicure",
        "description": "Hand nail services.",
        "status": "active",
        "source": "manual",
        "sort_order": 10,
        "aliases": [
          {
            "source_key": "service_category_alias:mani",
            "alias": "mani",
            "normalized_alias": "mani",
            "source": "owner",
            "status": "active",
            "confidence": 0.94,
            "created_at": "2026-06-25T14:30:00Z",
            "updated_at": "2026-06-25T14:30:00Z"
          }
        ],
        "created_at": "2026-06-25T14:30:00Z",
        "updated_at": "2026-06-25T14:30:00Z"
      }
    ]
  },
  "service_aliases": {
    "count": 1,
    "items": [
      {
        "source_key": "service_alias:overlay",
        "alias": "overlay",
        "normalized_alias": "overlay",
        "target_service": {
          "name": "Builder Gel",
          "duration_minutes": 75,
          "price_display": "$70.00"
        },
        "source": "owner",
        "status": "active",
        "confidence": 0.94,
        "created_at": "2026-06-25T14:30:00Z",
        "updated_at": "2026-06-25T14:30:00Z"
      }
    ]
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
    "schema_version": "manleai.salon_configuration.v5",
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
  "schema_version": "manleai.salon_configuration.v5",
  "can_apply": true,
  "summary": [
    {"section": "salon_profile", "created": 0, "updated": 6, "unchanged": 5, "skipped": 0, "conflicts": 0},
    {"section": "service_categories", "created": 2, "updated": 1, "unchanged": 4, "skipped": 0, "conflicts": 0},
    {"section": "service_aliases", "created": 3, "updated": 1, "unchanged": 4, "skipped": 2, "conflicts": 0},
    {"section": "knowledge_base", "created": 4, "updated": 2, "unchanged": 8, "skipped": 0, "conflicts": 0}
  ],
  "warnings": [
    {"section": "integrations", "code": "secret_reentry_required", "message": "square secret values are not included in the export. Re-enter secrets or reconnect this provider after import.", "field": "square"}
  ],
  "conflicts": [],
  "excluded_data": ["services", "staff", "customers", "appointments", "call_sessions", "pos_entity_links", "pos_sync_jobs", "pos_sync_logs", "pos_errors", "provider_switch_runs", "provider_switch_matches", "salon_business_hour_periods", "party_booking_requests", "voice_webhook_events", "voice_audio_outputs", "pos_oauth_tokens", "api_keys", "client_secrets", "encrypted_secrets"],
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
    "schema_version": "manleai.salon_configuration.v5",
    "...": "full exported configuration bundle"
  }
}
```

Import idempotency:

- Salon profile, AI receptionist, public booking page, and integration settings update the existing salon-scoped rows.
- Integration secrets are preserved if already present and are never imported from the transfer file.
- Service categories upsert by stable slug; category aliases upsert by normalized alias and conflict with active service aliases.
- Service aliases upsert by normalized alias only when their target service resolves to exactly one existing target-salon service by name and duration; missing targets are skipped with warnings, and active category-alias conflicts block apply.
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
    "schema_version": "manleai.salon_configuration.v5",
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
  "schema_version": "manleai.salon_configuration.v5",
  "can_apply": true,
  "summary": [],
  "warnings": [],
  "conflicts": [],
  "excluded_data": ["services", "staff", "customers", "appointments", "call_sessions", "pos_entity_links", "pos_sync_jobs", "pos_sync_logs", "pos_errors", "provider_switch_runs", "provider_switch_matches", "salon_business_hour_periods", "party_booking_requests", "voice_webhook_events", "voice_audio_outputs", "pos_oauth_tokens", "api_keys", "client_secrets", "encrypted_secrets"],
  "requires_secret_reentry": ["square", "twilio", "openai"]
}
```

Onboarding import never imports services, staff, customers, appointments, POS
tokens, API keys, client secrets, or encrypted secrets. It can import service
category taxonomy and aliases because those are understanding configuration,
not service records or provider state. Service aliases whose target services do
not exist yet are skipped with warnings during onboarding import. If the bundle requests `ai_enabled=true`,
`booking_mode=confirmed_booking`, or public catalog publishing, those live
states are skipped until Square Appointments is connected, synced, and
booking-ready.

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
    "realtime_model": "gpt-realtime-2",
    "realtime_voice": "alloy",
    "realtime_noise_profile": "noisy_salon",
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
  "realtime_model": "gpt-realtime-2",
  "realtime_voice": "alloy",
  "realtime_noise_profile": "noisy_salon",
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
      "pos_linked": true,
      "service_category_id": "category-manicure-id",
      "category_name": "Manicure",
      "category_slug": "manicure",
      "category_source": "manual",
      "category_confidence": 1,
      "category_reviewed_at": "2026-06-25T14:30:00Z"
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
  "active": true,
  "service_category_id": "category-removal-id"
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
    "pos_linked": false,
    "service_category_id": "category-removal-id",
    "category_source": "manual"
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
  "active": true,
  "service_category_id": "category-manicure-id"
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

`PATCH /api/salons/:id/services/:service_id/category`

Assigns or clears a service category. Passing a category ID marks the assignment
as owner-reviewed `manual` with confidence `1`; passing an empty
`service_category_id` clears the assignment and returns it to `unassigned`.

```json
{
  "service_category_id": "category-manicure-id"
}
```

`GET /api/salons/:id/service-categories`

Returns owner-scoped service categories with service counts and aliases. These
groups help the AI clarify service requests but are not directly bookable.

```json
{
  "service_categories": [
    {
      "id": "category-manicure-id",
      "salon_id": "...",
      "name": "Manicure",
      "slug": "manicure",
      "description": "Hand nail services.",
      "status": "active",
      "sort_order": 10,
      "source": "manual",
      "service_count": 4,
      "aliases": [
        {
          "id": "category-alias-id",
          "category_id": "category-manicure-id",
          "category_name": "Manicure",
          "alias": "mani",
          "normalized_alias": "mani",
          "source": "owner",
          "status": "active",
          "confidence": 0.94
        }
      ]
    }
  ]
}
```

`POST /api/salons/:id/service-categories`

Creates an owner-managed category. Slugs are derived from names and are unique
per salon.

```json
{
  "name": "Waxing",
  "description": "Waxing services.",
  "sort_order": 80
}
```

`PUT /api/salons/:id/service-categories/:category_id`

Updates category name, description, and sort order.

`POST /api/salons/:id/service-categories/:category_id/archive`

Archives a category, clears matching service assignments back to `unassigned`,
and archives its category aliases.

`POST /api/salons/:id/service-categories/:category_id/restore`

Restores an archived category.

`POST /api/salons/:id/service-categories/:category_id/aliases`

Creates or updates a category alias by `(salon_id, normalized_alias)`. The
backend rejects aliases that conflict with active service aliases.

```json
{
  "alias": "mani",
  "confidence": 0.94
}
```

`POST /api/salons/:id/service-category-aliases/:alias_id/archive`

Archives one category alias.

`POST /api/salons/:id/service-categories/suggestions/refresh`

Idempotently seeds common nail-salon categories such as Manicure, Pedicure,
Acrylic, Dip Powder, and Removal; creates system category aliases when they do
not conflict with service aliases; and suggests categories only for services
whose category source is `unassigned` or `suggested`. It does not override
manual or imported assignments.

```json
{
  "refresh": {
    "created_categories": 0,
    "restored_system_categories": 0,
    "created_aliases": 0,
    "updated_system_aliases": 0,
    "skipped_alias_conflicts": 0,
    "suggested_services": 3,
    "skipped_reviewed_services": 5,
    "skipped_ambiguous_services": 0,
    "unmatched_unreviewed_services": 1
  }
}
```

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
booking. The optional `limit` query parameter defaults to 50 and is capped at
100. The optional `offset` query parameter defaults to 0. Responses include
`customers`, `summary`, `limit`, `offset`, and `has_more`; `has_more` is
computed by requesting one extra row and does not require an exact total count.
`summary` describes the full owner-scoped customer/activity set, not only the
current page.

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
    "active_customers": 1,
    "pos_linked_customers": 1,
    "confirmed_appointments": 1,
    "pending_requests": 0,
    "customers_with_calls": 1,
    "last_customer_activity_at": "2026-06-10T15:00:00Z"
  },
  "limit": 10,
  "offset": 0,
  "has_more": false
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

Returns appointments recorded after POS success, including confirmed, rescheduled, and cancelled statuses. The optional `limit` query parameter defaults to 50 and is capped at 200. The optional `offset` query parameter defaults to 0. Responses include `appointments`, `limit`, `offset`, and `has_more`; `has_more` is computed by requesting one extra row and does not require an exact total count. Each item includes `staff_selection_mode` and, when available, ordered `segments[]` from `appointment_services` with service names, assigned technician names, durations, and segment-level staff selection mode. `staff_selection_mode=anyone` means the customer did not request a named technician even though the POS-confirmed appointment stores the staff assignment used to book.

`GET /api/salons/:id/calendar?start=<date-or-rfc3339>&end=<date-or-rfc3339>&view=<day|week|month|agenda>`

Returns a range view for the standalone POS Calendar app. The response includes
POS-confirmed `appointments`, `pending_requests` from `pos_pending` and
`fallback_pending` booking attempts, and a `warnings` summary. Calendar
appointment items include the
normal appointment fields plus `pos_sync_status`, `last_pos_synced_at`,
`pos_sync_error`, `sync_warning`, `can_edit`, and `can_delete`. A warning means
the item is not verified as cleanly synced from the active POS calendar, or the
record is a pending booking request that must not be treated as confirmed.

```json
{
  "salon_id": "...",
  "start_time": "2026-07-08T00:00:00Z",
  "end_time": "2026-07-15T00:00:00Z",
  "view": "week",
  "appointments": [],
  "pending_requests": [],
  "warnings": {
    "not_synced": 0,
    "sync_failed": 0,
    "pending_pos_sync": 0,
    "fallback_pending": 0
  }
}
```

`GET /api/salons/:id/calendar/events/stream?cursor=<created_at|notification_id>`

Streams authenticated `text/event-stream` frames for new booking events used by
the standalone POS Calendar app. The request uses the same bearer token auth as
other protected salon APIs; access tokens must not be passed in the query
string. The optional `cursor` is the last event cursor returned by the stream.
When no cursor is supplied, the stream starts from the current server time so
old notifications do not replay as fresh toasts.

Each `calendar.booking` event includes a durable notification `id`, replay
`cursor`, `type` (`booking_confirmed` or `booking_fallback_pending`),
`booking_attempt_id`, optional `appointment_id`, booking status, source,
customer display name, start/end time, and created timestamp. `booking_confirmed`
means the active POS returned a booking ID. `booking_fallback_pending` and any
calendar `pos_pending` item must be shown as pending, not confirmed.

`POST /api/salons/:id/calendar/sync`

Imports appointments from the active POS provider for the requested range. For
Square Appointments, this uses the Square Bookings list path and upserts local
appointment mirrors by `(salon_id, pos_provider, pos_appointment_id)`. The
import does not bypass the POS-first confirmation rule; it mirrors provider
records and records POS errors when sync fails.

```json
{
  "start_time": "2026-07-08",
  "end_time": "2026-07-15"
}
```

```json
{
  "provider": "square",
  "summary": {
    "imported": 2,
    "updated": 8,
    "skipped": 0
  },
  "range": {
    "start_time": "2026-07-08T00:00:00Z",
    "end_time": "2026-07-15T00:00:00Z"
  }
}
```

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

Returns booking attempts, including transient `pos_pending` records and `fallback_pending` records that need owner review. The optional `status` query parameter filters by attempt status such as `fallback_pending`. The optional `limit` query parameter defaults to 50 and is capped at 200. The optional `offset` query parameter defaults to 0. Responses include `booking_attempts`, `limit`, `offset`, `has_more`, and `status` when a filter is applied; `has_more` is computed by requesting one extra row and does not require an exact total count.

Each item includes `staff_selection_mode` and, when available, ordered `segments[]` from `booking_attempt_segments` so owner dashboards can distinguish the customer preference (`anyone` or `specific`) from the staff assignment attempted through the POS provider. Fallback action rows include `booking_action` (`book`, `reschedule`, or `cancel`) derived from backend notification/audit state, plus `target_appointment_id` and `appointment` when a failed reschedule or cancellation has a POS-confirmed appointment target. Dashboard retry actions must still call the booking service and active POS provider; a fallback request must not be marked confirmed, rescheduled, or cancelled from the list response alone.

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

Returns recent conversation sessions for the authenticated owner, including `simulator` and `phone` channels. The optional `lifecycle_status` query parameter accepts `active`, `archived`, or `redacted`; the default is `active`. The optional `limit` query parameter defaults to 25 and is capped at 100. The optional `offset` query parameter defaults to 0. Responses include `sessions`, `limit`, `offset`, and `has_more`; `has_more` is computed by requesting one extra row and does not require an exact total count. Active lifecycle sessions receive a 90-day `retention_expires_at` timestamp. The worker redacts expired sessions by clearing customer PII, transcript bodies, handoff summaries, webhook payloads, and temporary voice audio while preserving booking, handoff, outcome, provider call, and timestamp audit links.

`POST /api/salons/:id/conversation-sessions`

```json
{
  "channel": "simulator"
}
```

Creates a simulator session and writes the initial AI transcript message. The initial message identifies the salon when a salon name is configured, includes the recording disclosure from runtime defaults or salon greeting configuration, and uses an open-ended help prompt instead of assuming the caller wants to book.

`GET /api/salons/:id/conversation-sessions/:session_id`

Returns one conversation session with transcript messages and the latest handoff request when present. Booking state includes `requested_date` when the customer has provided a day but not a specific time, and `requested_start_time` only after a concrete start time or offered slot is selected. Transcript messages may include backend metadata for turn diagnostics such as slot state before/after, event keys, guardrail outcomes, answer source, source reason, router intent, source record IDs, answer-context cache hit state, and next required booking field.

`POST /api/salons/:id/conversation-sessions/:session_id/archive`

Archives an owner-scoped conversation session so it no longer appears in the default active call log. Archiving is idempotent and keeps transcript text available until retention redaction.

`POST /api/salons/:id/conversation-sessions/:session_id/redact`

Irreversibly redacts a completed or otherwise non-active owner-scoped conversation session. Active sessions return `409 CONVERSATION_LIFECYCLE_CONFLICT`. Redaction clears customer PII and transcript text but keeps POS booking evidence and owner handoff links for audit.

`POST /api/salons/:id/conversation-sessions/:session_id/messages`

```json
{
  "message": "My name is Linh Tran, phone 312-555-0199, classic manicure with Mai on 2026-06-10 at 3pm.",
  "event_key": "optional-provider-event-key"
}
```

Processes one simulated customer message through the deterministic conversation engine. `event_key` is optional for simulator callers and is used by voice adapters to dedupe provider retries. The simulator asks one question at a time, preserves already-collected booking slots, handles greeting-only or connection-check turns without replaying the full welcome or forcing booking intent, handles date-only turns such as weekdays before a time is known, can create owner handoffs for human requests or disabled AI booking, checks provider-neutral availability before selecting a booking time, offers available slots from Square Appointments, and calls the provider-neutral booking service only after the customer selects a slot and required customer details are collected. Service utterances are interpreted against the active salon catalog, active `service_aliases`, active `service_categories`, and active `service_category_aliases`; exact catalog service names win over aliases, alias matches can select one service, category/category-alias matches ask the caller to choose a real service in that group, and generic or fuzzy family matches ask for catalog-backed clarification instead of selecting a service. If the caller mentions a different service after a service is already selected, the engine asks the caller to confirm the switch, keeps the current service unless the caller confirms, and only then clears stale offered slots and rechecks availability when date or time context already exists. Non-booking answers are routed from structured sources before knowledge: active-provider AI-bookable services, imported business hour periods, active-provider staff, booking availability prompts, then active knowledge. Informational service menu and count questions are answered from the full matched bookable catalog without selecting a service, clearing pending candidates, or calling availability/booking tools; if a booking is already in progress, the reply then resumes the unresolved service question. Transcript metadata may include `service_understanding_status`, `service_understanding_reason`, `service_understanding_confidence`, candidate service IDs/names, selected service, alias source, alias ID, category ID, category name, `answer_source`, `answer_source_reason`, `answer_source_confidence`, `router_intent`, `source_record_ids`, and `answer_context_cache_hit` for debugging. Supported group or party booking requests resolve party size and guest service counts into ordered `booking_segments`, call availability, and can be booked through the same POS-first booking service after the caller selects a slot and provides required customer details. Party parsing distinguishes person-count phrases such as "for two guests" from service-count phrases such as "two manicures"; session `party_plan` may include optional `parse_source`, `parse_confidence`, `clarify_reason`, group `source`, and `evidence` fields for debugging and review. Ambiguous party service families and party-size/service-count mismatches ask for catalog-backed clarification before availability. Offered slots may include ordered `segments` with provider-neutral service/staff assignments. The engine can select a unique offered slot from ordinal replies, spoken times such as "one p.m.", or a "Yes" reply to a prompt that confirmed one specific offered time; unclear time fragments repeat the existing offered slots instead of rerunning availability. Once a customer selects a slot, the session stores selected `booking_segments` and `staff_selection_mode` so simulator and phone flows can create one multi-service or supported party POS-first booking request. When `staff_selection_mode=anyone`, the customer did not choose a named technician, so the conversation avoids presenting the POS staff assignment as a customer-selected technician. Reschedule and cancellation requests use `booking_action` values `reschedule` and `cancel`, look up upcoming POS-backed appointments by caller phone, ask the caller to select or confirm the target appointment, and then call the provider-neutral booking service. A simulator booking is marked `booking_confirmed` only when the booking service returns a confirmed booking attempt with a POS booking ID and appointment. Cancellation is marked `booking_cancelled` only when the booking service returns a POS-cancelled appointment. POS failures create `booking_fallback_pending` wording and do not create confirmed, rescheduled, or cancelled appointment language.

Phone channel sessions are created by Twilio webhooks and use the same conversation engine. Phone bookings use source `ai_voice_call`; simulator bookings use source `ai_conversation_simulator`.

`GET /api/salons/:id/party-booking-requests?status=pending`

Returns owner-scoped party booking requests created from group booking handoffs.
The optional `status` filter accepts `pending`, `contacted`, `resolved`, or
`dismissed`; the optional `limit` defaults to 25 and is capped at 100. The
optional `offset` defaults to 0. Responses include `party_booking_requests`,
`limit`, `offset`, and `has_more`; `has_more` is computed by requesting one
extra row and does not require an exact total count.

```json
{
  "party_booking_requests": [
    {
      "id": "party-request-id",
      "salon_id": "...",
      "call_session_id": "session-id",
      "event_key": "provider-turn-key",
      "status": "pending",
      "party_size": 3,
      "representative_name": "Linh Tran",
      "representative_phone": "+13125550101",
      "requested_date": "2026-07-06",
      "requested_time_window": "afternoon",
      "guest_service_requests": [
        {"service_id": "service-gel-id", "service_name": "Gel Manicure"}
      ],
      "flexibility_notes": "Caller can move later if needed.",
      "summary": "Group booking request for three manicures.",
      "created_at": "2026-07-04T15:00:00Z",
      "updated_at": "2026-07-04T15:00:00Z"
    }
  ],
  "limit": 25,
  "offset": 0,
  "has_more": false
}
```

`PATCH /api/salons/:id/party-booking-requests/:request_id/status`

Updates owner workflow status for a party request. This does not create,
confirm, reschedule, or cancel a POS appointment.

```json
{
  "status": "contacted"
}
```

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

`GET /api/salons/:id/service-aliases`

Returns owner-scoped service aliases used by the conversation service-understanding layer. Runtime use is limited to active aliases whose target service is active and AI-bookable.

`POST /api/salons/:id/service-aliases`

Creates or updates a service alias. The backend normalizes `alias` into
`normalized_alias` and upserts by `(salon_id, normalized_alias)`, so retrying
the same request updates one alias instead of creating duplicate learned
behavior. The backend rejects an active service alias if the normalized phrase
already belongs to an active service category alias, because one caller phrase
cannot safely mean both a concrete service and a category.

```json
{
  "service_id": "...",
  "alias": "shell manicure",
  "source": "owner",
  "status": "active",
  "confidence": 0.94
}
```

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

`POST /api/salons/:id/owner-corrections/:correction_id/apply-service-alias`

Applies a correction into a structured service alias and marks the correction as `applied`. This is for service-understanding fixes such as mapping a repeated caller/STT phrase to a real service. It does not alter POS services and does not confirm appointments.

```json
{
  "service_id": "...",
  "alias": "shell manicure",
  "confidence": 0.96
}
```

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

Returns owner-scoped live voice, phone booking, and external AI provider readiness without exposing Twilio, OpenAI, or POS token secrets. `ready` means Twilio can route live phone webhooks; `phone_booking_ready` means the phone path also has the booking prerequisites needed to offer available slots from the active POS provider and attempt POS-first confirmation. For Square Appointments, a current create-booking permission blocker returns `booking.booking_write_blocked=true` and keeps `phone_booking_ready=false`.

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
    "booking_write_blocked": false,
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
      "model": "gpt-realtime-2",
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

`GET /api/salons/:id/conversation-sessions/:session_id/realtime-events`

Owner-authenticated debug endpoint for the selected phone call session. It
returns realtime stream audit events recorded in `voice_webhook_events`, scoped
by salon ownership and session ID. The response intentionally exposes only
debug-safe fields extracted from provider payloads; raw Twilio/OpenAI payloads
are not returned.

Example response:

```json
{
  "events": [
    {
      "id": "b87c6...",
      "provider": "twilio",
      "provider_call_id": "CA2a47b5e8e5777820f84d6447132b1574",
      "event_type": "realtime_failed",
      "stage": "openai_event",
      "stream_sid": "MZ...",
      "error": "invalid_request_error: invalid_value: session.audio.input.format: Unsupported audio format.",
      "created_at": "2026-06-29T00:13:10Z"
    }
  ]
}
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

Public Twilio Media Streams WebSocket endpoint for realtime audio mode. The endpoint is not configured directly in Twilio Console; the incoming webhook returns `<Connect><Stream>` with signed custom parameters for the existing call session. The stream forwards Twilio g711 audio frames to the configured OpenAI Realtime adapter, routes completed transcripts through the same backend conversation engine and booking service, then streams backend-approved audio responses back to Twilio. Realtime replies are serialized so a new OpenAI `response.create` is not sent until the active response finishes; caller barge-in clears Twilio playback and suppresses stale audio after AI audio has started and passed the noise guard while preserving the latest queued backend reply. If realtime configuration is missing, voice status falls back to the recording or gather path.

`GET /api/voice/audio/:id`

Public short-lived audio output endpoint for Twilio `<Play>` responses. IDs are unguessable runtime UUIDs, expire quickly, and never expose POS tokens or provider secrets.

The phone path never confirms an appointment unless the booking service returns a POS-confirmed booking attempt with a POS booking ID and appointment.

## POS Provider Switch Readiness

`GET /api/salons/:id/pos/provider-switch-readiness`

Returns the active POS provider, installed adapters, unavailable adapter gate,
provider mapping counts, and readiness checks. This endpoint is read-only in
the current production release. It does not activate a new provider or create fake provider
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
      "blocked_reason": "Square Appointments is the only native POS integration in the current production release."
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
  "blocked_reason": "No alternate production POS adapter is installed in this deployment."
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
current production release.

`GET /api/salons/:id/pos/provider-switch-runs/:run_id/dry-run-readiness`

Returns the dry-run checklist for one owner-scoped provider switch run. This
endpoint is read-only. It does not call a target POS provider, create
appointments, create provider links, mark `dry_run_ready=true`, or activate a
provider. In the current production release it remains blocked because no alternate native
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
    {"key": "dry_run_execution_available", "label": "Alternate-provider dry-run execution available", "complete": false, "message": "Alternate-provider dry-run execution is not available in the current production release."}
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
Readiness also includes `booking_write_blocked`, `booking_write_blocked_code`,
`booking_write_blocked_reason`, and `booking_write_blocked_at` when the latest
Square create-booking permission error still has not been cleared by a later
successful Square test booking.

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
Calendar appointment imports are handled separately by
`POST /api/salons/:id/calendar/sync`, scoped to a requested calendar range.

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
