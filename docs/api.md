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
confirmation rules still override style. `consultation_enabled` is the salon-wide
runtime gate for AI service consultation. It does not make any service eligible by
itself; eligibility still requires an active-provider link, AI booking eligibility,
and a complete `ready` service consultation profile with at least one recommended
outcome and one compatible current system. The setting defaults to `false`, and an
enable request is rejected until at least one service meets that full eligibility
contract.

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
  "handoff_enabled": true,
  "consultation_enabled": true
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
- Service consultation profiles with portable target-service references
- AI Training knowledge base

Secret-bearing integrations return only configuration values and secret status
metadata such as `client_secret_configured`, `auth_token_configured`,
`api_key_configured`, and `*_source`. It does not export services, staff,
customers, appointments, booking attempts, fallback requests, call sessions,
transcripts, recordings, summaries, owner corrections, POS entity links, POS
sync jobs/logs/errors, provider switch runs/matches, synced business hour
periods, party booking requests, voice webhook/audio records, POS OAuth tokens,
API keys, client secrets, encrypted secrets, or POS connection token state.
Schema v7 adds portable `service_consultation_profiles` and
`included_sections`. A full export lists every supported configuration section;
a curated pack may list only taxonomy, service aliases, and consultation
profiles so it cannot overwrite salon, provider, or AI runtime configuration.
Import remains backward compatible with v1-v6 bundles. Bundles from v1-v5 do
not contain the consultation toggle and therefore default it to disabled;
schema v6 and later preserve the explicit value subject to profile readiness.

```json
{
  "schema_version": "manleai.salon_configuration.v7",
  "exported_at": "2026-06-26T15:00:00Z",
  "secrets_exported": false,
  "operational_data_exported": false,
  "included_sections": ["salon_profile", "ai_receptionist", "public_booking_page", "integrations", "service_categories", "service_aliases", "service_consultation_profiles", "knowledge_base"],
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
    "consultation_enabled": true,
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
      "speech_model": "tts-1",
      "speech_voice": "alloy",
      "speech_output_mode": "streaming_tts",
      "realtime_enabled": true,
      "realtime_model": "gpt-realtime-2",
      "realtime_voice": "alloy",
      "realtime_noise_profile": "automatic",
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
  "service_consultation_profiles": {
    "count": 1,
    "items": [
      {
        "source_key": "service_consultation_profile:builder gel|75",
        "target_service": {
          "name": "Builder Gel",
          "duration_minutes": 75,
          "price_display": "$70.00"
        },
        "status": "ready",
        "recommended_outcomes": ["maintain", "add_strength"],
        "compatible_current_systems": ["natural", "gel"],
        "length_capabilities": ["keep", "shorten"],
        "priority_tags": ["durability", "lower_maintenance"],
        "finish_options": ["gel_polish", "glossy"],
        "maintenance_note": "Return for professional maintenance as growth becomes visible.",
        "owner_approved_summary": "A structured gel service for clients who want strength and long wear."
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

Request shape, with a full exported bundle or scoped v7 pack in
`configuration`:

```json
{
  "configuration": {
    "schema_version": "manleai.salon_configuration.v7",
    "...": "full export or scoped configuration pack"
  }
}
```

Response:

```json
{
  "request_id": "import-preview-id",
  "dry_run": true,
  "status": "previewed",
  "schema_version": "manleai.salon_configuration.v7",
  "can_apply": true,
  "summary": [
    {"section": "salon_profile", "created": 0, "updated": 6, "unchanged": 5, "skipped": 0, "conflicts": 0},
    {"section": "service_categories", "created": 2, "updated": 1, "unchanged": 4, "skipped": 0, "conflicts": 0},
    {"section": "service_aliases", "created": 3, "updated": 1, "unchanged": 4, "skipped": 2, "conflicts": 0},
    {"section": "service_consultation_profiles", "created": 7, "updated": 0, "unchanged": 0, "skipped": 0, "conflicts": 0},
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
    "schema_version": "manleai.salon_configuration.v7",
    "...": "full export or scoped configuration pack"
  }
}
```

Import idempotency:

- Salon profile, AI receptionist, public booking page, and integration settings update the existing salon-scoped rows.
- Integration secrets are preserved if already present and are never imported from the transfer file.
- Service categories upsert by stable slug; category aliases upsert by normalized alias and conflict with active service aliases.
- Service aliases upsert by normalized alias only when their target service resolves to exactly one existing target-salon service by name and duration; missing targets are skipped with warnings, and active category-alias conflicts block apply.
- Consultation profiles resolve by normalized service name plus duration and upsert by `(salon_id, service_id)`. Missing or ambiguous targets block apply. A `ready` profile additionally requires an active-provider, POS-linked, synced, AI-bookable target service. The transfer never creates services or POS mappings.
- Identical consultation profile imports remain `unchanged`; they do not increment profile revision.
- `included_sections` limits both preview and apply. Scoped consultation packs can leave salon profile, integrations, AI runtime settings, and knowledge untouched.
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
    "schema_version": "manleai.salon_configuration.v7",
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
  "schema_version": "manleai.salon_configuration.v7",
  "can_apply": true,
  "summary": [],
  "warnings": [],
  "conflicts": [],
  "excluded_data": ["services", "staff", "customers", "appointments", "call_sessions", "pos_entity_links", "pos_sync_jobs", "pos_sync_logs", "pos_errors", "provider_switch_runs", "provider_switch_matches", "salon_business_hour_periods", "party_booking_requests", "voice_webhook_events", "voice_audio_outputs", "pos_oauth_tokens", "api_keys", "client_secrets", "encrypted_secrets"],
  "requires_secret_reentry": ["square", "twilio", "openai"]
}
```

Onboarding import requires a full bundle; scoped v7 data packs are imported from
Settings after the salon exists. Onboarding never imports services, staff,
customers, appointments, POS tokens, API keys, client secrets, or encrypted
secrets. It can import service category taxonomy and aliases because those are
understanding configuration, not service records or provider state. Service
aliases and consultation profiles are deferred because target services do not
exist before Square sync, and consultation remains disabled until a post-sync
Settings import resolves the profile pack. If the bundle requests `ai_enabled=true`,
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
    "client_secret_source": "database",
    "webhook_notification_url": "https://api.example.com/api/integrations/square/webhook",
    "webhook_configured": true,
    "webhook_signature_key_configured": true,
    "webhook_signature_key_source": "database"
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
    "speech_model": "tts-1",
    "speech_voice": "alloy",
    "speech_output_mode": "streaming_tts",
    "realtime_enabled": true,
    "realtime_model": "gpt-realtime-2",
    "realtime_voice": "alloy",
    "realtime_noise_profile": "automatic",
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
  "api_base_url": "",
  "webhook_notification_url": "https://api.example.com/api/integrations/square/webhook",
  "webhook_signature_key": "new-signature-key-or-empty-to-keep-existing",
  "clear_webhook_signature_key": false
}
```

`webhook_configured=true` means only that an HTTPS notification URL and an
encrypted signature key are stored for inbound verification. It does not prove
that a Square subscription exists or that recent deliveries succeeded. Secret
encryption failures abort the update and preserve previously stored secrets.
Configuration transfer preserves the target salon's deployment URL and never
exports a webhook signature key; that key must be re-entered at the target.

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
  "speech_model": "tts-1",
  "speech_voice": "alloy",
  "speech_output_mode": "streaming_tts",
  "realtime_enabled": true,
  "realtime_model": "gpt-realtime-2",
  "realtime_voice": "alloy",
  "realtime_noise_profile": "automatic",
  "realtime_instructions": ""
}
```

`realtime_noise_profile` is the compatibility field name for the location-neutral
background-noise handling policy. Canonical values are `automatic` (default),
`standard`, `strong_noise_rejection`, and `minimal_processing`. Automatic mode
starts each call with standard transcript admission and switches only that call
to stronger admission after structured confidence/VAD evidence of degraded
audio. Missing provider confidence metadata fails closed but does not count as
noise evidence. Legacy imports are canonicalized without losing their prior
behavior: `noisy_salon` becomes `strong_noise_rejection`, `balanced` becomes
`standard`, and `quiet_room` becomes `minimal_processing`.

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
      "category_reviewed_at": "2026-06-25T14:30:00Z",
      "field_authority": {
        "operational_source": "provider",
        "provider": "square",
        "provider_label": "Square Appointments",
        "operational_write_mode": "provider_read_only"
      },
      "consultation_profile": {
        "status": "ready",
        "recommended_outcomes": ["maintain", "color_refresh"],
        "compatible_current_systems": ["natural", "regular_polish"],
        "length_capabilities": ["keep", "shorten"],
        "priority_tags": ["shorter_visit", "lower_cost"],
        "finish_options": ["regular_polish", "glossy"],
        "maintenance_note": "Owner-approved upkeep guidance.",
        "owner_approved_summary": "Classic nail care with regular polish.",
        "revision": 3
      }
    }
  ]
}
```

`field_authority` is the backend-owned field-level write contract. Clients must
not infer editability from `source`, `sync_status`, or `pos_linked`. Current
Square-imported services return `provider_read_only`; local-only services return
`operational_source=manleai` and `operational_write_mode=local`.

`POST /api/salons/:id/services`

Creates a local ManleAI service. Local-only services are visible in the Services
dashboard but cannot be used for availability or booking until an active POS
provider link exists. The API always creates local services with
`ai_bookable=false`, `sync_status=local_only`, and `pos_linked=false`.
If the active provider later declares service write support, the mutation can
queue a `pos_sync_jobs` outbox job and move through `syncing`, `synced`, or
`sync_failed`. Square Appointments service writes are not enabled in the current
slice.

`consultation_profile` is the optional owner-managed child record for this
service. Its controlled arrays accept outcomes (`maintain`, `shorten`,
`add_length`, `add_strength`, `repair`, `removal`, `color_refresh`), current
systems (`natural`, `regular_polish`, `gel`, `dip`, `acrylic`, `extension`),
length capabilities (`keep`, `shorten`, `add_length`), priorities
(`durability`, `lower_maintenance`, `lower_cost`, `shorter_visit`), and finishes
(`natural`, `regular_polish`, `gel_polish`, `glossy`, `matte`, `nail_art`). A
`ready` profile requires at least one recommended outcome and at least one
compatible current system. Optional length, priority, and finish fields refine
ranking but cannot publish an otherwise incomplete profile. Summary and
maintenance text are trimmed and limited to 320 Unicode characters. Identical
retry payloads are no-ops; a changed profile increments `revision`.

Only `ready` profiles attached to active-provider, POS-linked, AI-bookable
services participate in consultation ranking. `ai_description`, then
`description`, remains a display-summary fallback for legacy records; neither
field can make a draft or disabled profile recommendation-eligible. Health or
medical suitability questions are handed to the owner.

Active consultation control state lives in `call_sessions.dialog_state`, with
transcript metadata retained only as audit evidence. Appointment cancel,
reschedule, human handoff, and active party-plan actions take precedence over
consultation. Selecting a service during consultation does not start booking
until the caller also gives explicit booking intent.

```json
{
  "name": "Gel Removal",
  "description": "Removal service for existing gel polish.",
  "ai_description": "Removes existing gel polish before a new service.",
  "duration_minutes": 20,
  "price_from": 10,
  "active": true,
  "service_category_id": "category-removal-id",
  "consultation_profile": {
    "status": "draft",
    "recommended_outcomes": ["removal"],
    "compatible_current_systems": ["gel"],
    "length_capabilities": [],
    "priority_tags": [],
    "finish_options": [],
    "maintenance_note": "",
    "owner_approved_summary": "Removes existing gel polish before a new service."
  }
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
    "category_source": "manual",
    "consultation_profile": {
      "status": "draft",
      "recommended_outcomes": ["removal"],
      "compatible_current_systems": ["gel"],
      "length_capabilities": [],
      "priority_tags": [],
      "finish_options": [],
      "owner_approved_summary": "Removes existing gel polish before a new service.",
      "revision": 1
    }
  }
}
```

`PUT /api/salons/:id/services/:service_id`

Updates a non-archived service's operational fields only when
`field_authority.operational_write_mode` is `local` or `provider_sync`. A
`provider_read_only` service returns `409 PROVIDER_MANAGED_FIELDS`; the API does
not create a local override that would be overwritten by the next provider
import. If `active=false` is saved on an editable record, `ai_bookable` is also
disabled.

```json
{
  "name": "Classic Manicure",
  "description": "Trim, shape, cuticle care, and polish.",
  "ai_description": "Includes nail shaping, cuticle care, and regular polish.",
  "duration_minutes": 45,
  "price_from": 35,
  "active": true,
  "service_category_id": "category-manicure-id",
  "consultation_profile": {
    "status": "ready",
    "recommended_outcomes": ["maintain", "color_refresh"],
    "compatible_current_systems": ["natural", "regular_polish"],
    "length_capabilities": ["keep", "shorten"],
    "priority_tags": ["shorter_visit"],
    "finish_options": ["regular_polish"],
    "maintenance_note": "Owner-approved upkeep guidance.",
    "owner_approved_summary": "Includes nail shaping, cuticle care, and regular polish."
  }
}
```

`PATCH /api/salons/:id/services/:service_id/owner-controls`

Atomically updates ManleAI-owned service enrichment without mutating
provider-managed operational fields. Repeating the same consultation profile is
idempotent and does not create another profile row or increment its revision.

```json
{
  "ai_description": "Owner-approved comparison guidance.",
  "service_category_id": "category-manicure-id",
  "consultation_profile": {
    "status": "ready",
    "recommended_outcomes": ["maintain"],
    "compatible_current_systems": ["natural"],
    "length_capabilities": ["keep"],
    "priority_tags": [],
    "finish_options": ["regular_polish"],
    "maintenance_note": "Owner-approved upkeep guidance.",
    "owner_approved_summary": "Owner-approved comparison guidance."
  }
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

Idempotently materializes the active `en-US` database-owned nail taxonomy
release into salon system categories and aliases. Exact normalized local
service names may receive a taxonomy category suggestion and service aliases
only when the taxonomy concept resolves to one unique local service target.
Unknown or ambiguous local services remain unassigned; system aliases that
conflict with owner/imported aliases are skipped. The refresh never creates a
service or POS entity link and never overrides manual/imported categories or
owner/imported aliases. Taxonomy releases and their category/concept/alias
children are versioned database data seeded by migration V44, not Go phrase
maps or API request constants.

```json
{
  "refresh": {
    "created_categories": 0,
    "restored_system_categories": 0,
    "created_aliases": 0,
    "updated_system_aliases": 0,
    "skipped_alias_conflicts": 0,
    "created_service_aliases": 12,
    "updated_system_service_aliases": 0,
    "skipped_service_alias_conflicts": 1,
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
      "pos_linked": true,
      "field_authority": {
        "operational_source": "provider",
        "provider": "square",
        "provider_label": "Square Appointments",
        "operational_write_mode": "provider_read_only"
      }
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

Updates a non-archived staff member's operational fields only when
`field_authority.operational_write_mode` is `local` or `provider_sync`. A
`provider_read_only` staff member returns `409 PROVIDER_MANAGED_FIELDS`; current
Square Appointments records must be edited in Square and imported through sync.
If `active=false` is saved on an editable record, `ai_bookable` is also disabled.

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
The backend resolves all requested services and staff against
`salons.active_pos_provider`. The provider request covers the requested
salon-local calendar day, including daylight-saving boundaries; API clients do
not send a provider timezone or construct UTC day ranges.
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
  "quote_id": "availability-quote-uuid",
  "request_fingerprint": "sha256-hex",
  "expires_at": "2026-06-10T14:05:00Z",
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
      "fingerprint": "sha256-hex",
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

When at least one usable slot is returned, the backend persists a short-lived,
single-use availability quote and returns `quote_id`, `request_fingerprint`,
`expires_at`, and one `fingerprint` per slot. These fields are intentionally
omitted when `slots` is empty. Owner-facing HTTP create, reschedule, and Square
test-booking writes must submit the quote ID and selected slot fingerprint;
missing evidence returns `409 AVAILABILITY_QUOTE_REQUIRED`, while expired,
consumed, provider-snapshot-stale, or payload-mismatched evidence returns
`409 AVAILABILITY_QUOTE_STALE`.

Conversation callers do not accept client-supplied HTTP quote fields. They
retain the backend quote proof attached to the selected offered slot and
re-query provider availability immediately before create or reschedule. The
runtime replaces the proof only when the refreshed slot has the same start,
end, and ordered service/staff assignment. A changed or missing slot is
re-offered and no POS write is dispatched. Party booking refreshes every child
slot and quote before the first child write; a failed child preflight produces
zero child writes for that attempt.

Availability quotes are ephemeral authorization evidence, not the booking
audit source of truth. The worker deletes unconsumed quotes only after expiry
plus a 24-hour grace period and deletes orphaned consumed quotes only after a
30-day audit window. Cleanup runs every five minutes and drains at most eight
lock-skipping batches of 250 quotes per run; it never deletes a quote while any
`booking_attempts.availability_quote_id` or
`consumed_by_attempt_id` audit reference remains. Quote deletion cascades to
its slot children only; the booking-attempt ledger is not modified.

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
records and records POS errors when sync fails. One owner-scoped selected
location/snapshot-generation fence is captured before pagination and reused for
every page. The persistence transaction revalidates that exact fence before
its first write, so a provider, location, or generation change fails the import
without partial mirror writes. A returned Square booking with a different
non-empty `location_id` also fails the page.

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
	"operation_key": "dashboard-reschedule-7f60e4bf",
  "retry_of_attempt_id": "optional-safe-fallback-attempt-uuid",
  "availability_quote_id": "availability-quote-uuid",
  "slot_fingerprint": "sha256-hex",
  "start_time": "2026-06-11T16:00:00Z",
  "staff_id": "...",
  "notes": "Customer requested later time"
}
```

Returns `200` with the updated appointment only when the active `POSProvider` successfully reschedules the exact target booking and returns a version newer than the pre-dispatch baseline. The appointment's immutable originating provider location must match both the currently selected synchronized location and the fresh quote; a switched location or legacy missing origin fails before dispatch. Same-location generation advancement is allowed only when current raw service/staff mappings and the target version remain valid. Direct persistence is serialized with calendar import and cannot overwrite a newer stored provider version; an equal or newer authoritative mirror is accepted only when its version is at least the direct response, remains newer than baseline, and its synchronized time plus ordered canonical/raw assignment match exactly. Returns `202` with the existing `pos_pending` attempt when the same operation is already executing, or with an unconfirmed fallback/provider-pending attempt when the POS provider does not confirm the requested change or a conflicting mirror wins the persistence race; the internal appointment remains unchanged. Reusing an `operation_key` with a different normalized payload returns `409 BOOKING_OPERATION_CONFLICT`. A retry may name `retry_of_attempt_id` only when the prior attempt is a non-superseded `fallback_pending` row with `retry_policy=safe` and the new normalized request fingerprint is identical. A changed time, notes, service order, or technician assignment must start a new logical request instead of reusing that retry lineage.

`POST /api/salons/:id/appointments/:appointment_id/cancel`

```json
{
	"operation_key": "dashboard-cancel-9aa95f18",
  "retry_of_attempt_id": "optional-safe-fallback-attempt-uuid",
  "reason": "Customer requested cancellation"
}
```

Returns `200` with the cancelled appointment only when the active `POSProvider` successfully cancels the exact target booking and returns a version newer than the pre-dispatch baseline. Its immutable originating provider location must match the currently selected synchronized location, and cancellation carries that current exact provider fence into the adapter; a location switch or legacy missing origin makes zero provider calls. The monotonic persistence guard never overwrites a newer calendar/webhook version; a current cancelled mirror at least as new as the direct response and newer than baseline is authoritative success. Returns `202` with the existing `pos_pending` attempt when the same operation is already executing, or with status `fallback_pending` when the POS provider fails or the direct result conflicts with stored provider truth; the internal appointment remains unchanged. Reusing an `operation_key` with a different normalized payload returns `409 BOOKING_OPERATION_CONFLICT`.

`GET /api/salons/:id/booking-attempts`

Returns booking attempts, including transient `pos_pending` records and `fallback_pending` records that need owner review. The optional `status` query parameter filters by attempt status such as `fallback_pending`. The optional `limit` query parameter defaults to 50 and is capped at 200. The optional `offset` query parameter defaults to 0. Responses include `booking_attempts`, `limit`, `offset`, `has_more`, and `status` when a filter is applied; `has_more` is computed by requesting one extra row and does not require an exact total count.

Each item includes `staff_selection_mode` and, when available, ordered `segments[]` from `booking_attempt_segments` so owner dashboards can distinguish the customer preference (`anyone` or `specific`) from the staff assignment attempted through the POS provider. Fallback action rows include backend-owned `booking_action` (`book`, `reschedule`, or `cancel`), plus `target_appointment_id` and `appointment` when a failed reschedule or cancellation has a POS-confirmed appointment target. Dashboard retry actions must still call the booking service and active POS provider; a fallback request must not be marked confirmed, rescheduled, or cancelled from the list response alone.

Booking attempts also expose backend-owned operation integrity state:

- `operation_type`: `book`, `reschedule`, or `cancel`.
- `provider_outcome`: `not_started`, `in_flight`, `succeeded`, `failed`, or `unknown`.
- `retry_policy`: `none`, `safe`, or `blocked`.
- `reconciliation_status`: `not_required`, `required`, or `resolved`.
- `processing_lease_expires_at`: present while one worker owns the durable operation claim.
- `can_retry`: authoritative backend retry gate. Dashboard clients must not infer retry safety only from `fallback_pending`. Book/reschedule retries require a non-legacy stored provider fence, the same currently active and synchronized provider location, exact current ordered provider service/staff mappings and versions, and an unchanged appointment target/version for reschedule. Cancel retry requires the unchanged target booking and baseline version. A newer snapshot generation requires a fresh quote but does not by itself change the logical request.
- `retry_blocked_reason`: owner-facing reason when reconciliation, legacy evidence, provider readiness, catalog mapping, or target-version drift blocks retry.

`provider_outcome=unknown` means a provider write timed out, was cancelled in flight, returned HTTP 5xx, lost or truncated its response, failed to decode a success response, failed during a post-write provider lookup, or returned insufficient booking metadata after it may have succeeded. Such attempts use `retry_policy=blocked` and `reconciliation_status=required`; they must be verified in the active POS before another operation is submitted. Only a typed definitive provider rejection is eligible for `retry_policy=safe`; untyped provider-write errors default to unknown.

Lease expiry is phase-sensitive. Recovery first checks authoritative calendar
truth under the same salon-scoped lock. An exact create/reschedule/cancel mirror
terminalizes the attempt without a fallback. Otherwise an expired `not_started`
operation proves the processing owner stopped before provider dispatch: it
becomes `fallback_pending` with `provider_outcome=failed`,
`retry_policy=safe`, and `reconciliation_status=not_required`. An expired
`in_flight` operation may already have reached the provider, so it becomes
`fallback_pending` with `provider_outcome=unknown`, `retry_policy=blocked`, and
`reconciliation_status=required`.

Owner booking-attempt/calendar reads and the background worker use the same
atomic recovery transaction. The worker selects a bounded candidate list
without row locks, then handles each row with calendar advisory lock first and
appointment/attempt row locks afterward. It clears the processing token/lease, records one
`POS_TIMEOUT`, and creates one deduplicated owner-notification outbox row. Only
the `in_flight` branch creates reconciliation work. Repeated or concurrent
sweeps do not duplicate POS errors, notifications, or reconciliation tasks and
report no processed salon after its expired rows have already transitioned.
A same-operation-key replay that wins before lease recovery may safely resume
an expired `not_started` claim because provider dispatch has not begun; after
the recovery transaction finalizes it as fallback, a new attempt must use the
explicit safe-retry lineage.

Owner notifications are currently durable in-product/outbox records with
dedupe, payload, attempt count, and delivery metadata. This repository does not
yet contain an external SMS/email/push delivery consumer for those records.

`GET /api/salons/:id/booking-reconciliations?status=<open|resolved|escalated>&limit=100&offset=0`

Returns a paginated owner-scoped reconciliation queue. Each task includes
`booking_attempt_id` and an optional embedded `booking_attempt`. Unknown or
provider-pending writes remain blocked until resolution. Only historical
duplicates proven pre-dispatch with `provider_outcome=not_started` may be
normalized automatically as superseded; their task is closed with
`status=resolved` and `resolution=superseded`, and their candidate and
resolution endpoints return `409 RECONCILIATION_CONFLICT`. V39 fails instead of
collapsing a fingerprint group with more than one dispatched/unknown outcome.

`GET /api/salons/:id/booking-reconciliations/:attempt_id/candidates`

Returns `{ "candidates": [...] }` containing only tenant-scoped,
provider-synced appointments that satisfy the same exact provider ID,
operation, target appointment, status, version, time, service, and customer
matching predicate used during resolution. Clients must use this endpoint
rather than infer candidates from a truncated appointment list.

For reschedule and cancel attempts, `version` means a provider appointment
version strictly newer than the target version captured before the outbound
write. Reschedule candidates must also match the requested start/end and the
ordered service/staff assignment. The customer technician-preference flag is
not compared because a provider calendar mirror records the assigned
technician, while the original request may have used `staff_selection_mode=anyone`.

`POST /api/salons/:id/booking-reconciliations/:attempt_id/resolve`

```json
{
  "action_key": "owner-resolution-uuid",
  "action": "provider_attached",
  "provider_appointment_id": "square-booking-id",
  "provider_appointment_version": 4,
  "provider_status": "accepted",
  "note": "Verified against the latest provider calendar sync."
}
```

Actions are `provider_attached`, `not_created`, and `escalated`.
`provider_attached` locks and revalidates the exact provider-synced candidate
before linking it; `not_created` makes a definitive failure retryable only after
owner verification. For reschedule/cancel, the existing target provider booking
ID is not proof that the requested mutation succeeded. `not_created` is
rejected whenever an exact provider-synced booking or newer appointment
mutation is already visible.
`escalated` keeps retry blocked. `action_key` is
payload-bound and replay-safe. Resolution is serialized with provider calendar
imports for the salon before either path takes row locks, preventing a
concurrent mirror insert from passing a `not_created` absence check.

`POST /api/salons/:id/booking-attempts`

```json
{
	"operation_key": "dashboard-booking-0dfd",
  "retry_of_attempt_id": "optional-safe-fallback-attempt-uuid",
  "availability_quote_id": "availability-quote-uuid",
  "slot_fingerprint": "sha256-hex",
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

`operation_key` is required and identifies one logical booking operation. It is generated once per dashboard action or deterministically from the conversation session. The backend stores a normalized request fingerprint under a salon-scoped unique operation claim before customer or POS side effects. Replaying the same key and logical intent returns the existing attempt and reuses its POS idempotency key without a second POS writer. Ephemeral availability quote IDs and slot-proof fingerprints may be refreshed for the same logical request after response loss and are not replay-identity fields; they remain mandatory and exact for the first claim that can dispatch. Reusing the same key with different customer, retry lineage, target, time, notes, or ordered service/staff intent returns `409 BOOKING_OPERATION_CONFLICT`.

For this authenticated HTTP endpoint, the handler assigns
`source=owner_dashboard`; caller-supplied source values are not trusted. Quote
evidence is required on this HTTP surface. Provider-neutral internal callers
use the service directly and retain their own source and conversation
authorization gates.

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
with the existing `pos_pending` attempt for an in-flight duplicate. A typed
definitive provider rejection returns `fallback_pending`,
`provider_outcome=failed`, and `retry_policy=safe`. A transport error, HTTP 5xx,
response/decode ambiguity, post-write lookup failure, timeout, or missing
required provider metadata returns
`fallback_pending`, `provider_outcome=unknown`, `retry_policy=blocked`, and
`reconciliation_status=required`; it must not be automatically retried or
described as confirmed.

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

Returns one conversation session with transcript messages and the latest handoff request when present. Booking state includes `requested_date` when the customer has provided a day but not a specific time, and `requested_start_time` only after a concrete start time or offered slot is selected. `dialog_state` is a versioned operational state object containing phase, pending typed clarification, bounded mutation history, no-progress count, `draft_revision`, `reviewed_revision`, `authorized_revision`, and optional `consultation` and `guidance` state. Consultation state includes controlled caller needs such as desired finish, candidate and recommended service IDs, selected service ID, last asked field, profile revisions, recommendation reasons, bounded no-progress count, resume phase, and exit reason. Guidance state includes `stage`, dynamically derived `offered_actions`, `awaiting_action_choice`, separate `no_progress_count` and `provider_failure_count`, `progress_fingerprint`, and `last_provider_outcome`. Existing legacy guidance prompt/counter fields are normalized into this version 5 nested object on read without a database migration. `awaiting_action_choice` enables only the bounded choices from the immediately preceding provider-failure prompt; it is not a general caller-intent classifier and is cleared on progress or terminal handoff. A bounded semantic-provider outage uses handoff reason `guidance_provider_unavailable`; caller ambiguity continues to use `service_clarification_unresolved`. Transcript messages may include PII-reduced turn-understanding diagnostics, validated acts/questions, revision transitions, slot state, event keys, guardrail outcomes, answer sources, consultation or guidance audit metadata, and next required field.

Production-created sessions use `dialog_state.review_required=true`. When every booking field is complete, the runtime sets `reviewed_revision=draft_revision`, returns `phase=review`, and asks the caller to review the draft. A later explicit authorization sets `authorized_revision` only for that same revision. Booking requires `draft_revision == reviewed_revision == authorized_revision`; review acceptance is not booking confirmation.

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

Processes one simulated customer message through the deterministic conversation engine. `event_key` is optional for simulator callers and is used by voice adapters to dedupe provider retries. The simulator asks one question at a time, preserves already-collected booking slots, handles greeting-only or connection-check turns without replaying the full welcome or forcing booking intent, handles date-only turns such as weekdays before a time is known, can create owner handoffs for human requests or disabled AI booking, checks provider-neutral availability before selecting a booking time, offers available slots from Square Appointments, and calls the provider-neutral booking service only after the customer selects a slot and required customer details are collected. Deterministic date, time, staff, customer, and explicit availability-question evidence is applied before a model-proposed standalone summary can return, so a concrete correction or request for openings cannot be dropped or misrouted by semantic classification. If a new-booking caller proposes a different date or time after availability slots were offered, the engine preserves those slots and stores a typed pending correction until the caller confirms; rejection or a renewed availability question keeps and repeats the prior slots, while confirmation invalidates them and performs a fresh provider availability check. Service utterances are interpreted against the active salon catalog, active `service_aliases`, active `service_categories`, and active `service_category_aliases`; exact catalog service names win over aliases, alias matches can select one service, category/category-alias matches ask the caller to choose a real service in that group, and generic or fuzzy family matches ask for catalog-backed clarification instead of selecting a service. A structured semantic act cannot narrow a category/category-alias candidate set to one service without concrete service evidence. Exact family evidence takes precedence over fuzzy service guessing unless another caller token distinctly identifies one catalog service, so conversational wording such as "manicure as well" cannot be misread as "Gel Manicure." If the caller mentions a different service after a service is already selected, the engine distinguishes adding from replacing before mutating the booking; a bare concrete service switch uses confirmation before clearing slots or changing the draft. A generic request such as another service first asks whether to add or replace; an ambiguous service family then asks for one concrete catalog service; and a multi-service replacement also asks which current service to replace. While a family target is pending, short replies are interpreted against that candidate set first, with full-catalog fallback for a clearly different service. The engine preserves the selected services and offered slots until the operation and concrete target are resolved, never applies every candidate from an ambiguous family, and only then clears stale offered slots and rechecks availability when date or time context already exists. Non-booking answers are routed from structured sources before knowledge: active-provider AI-bookable services, imported business hour periods, active-provider staff, booking availability prompts, then active knowledge. Informational service menu and count questions are answered from the full matched bookable catalog without selecting a service, clearing pending candidates, or calling availability/booking tools; if a booking is already in progress, the reply then resumes the unresolved service question. Transcript metadata may include `service_understanding_status`, `service_understanding_reason`, `service_understanding_confidence`, candidate service IDs/names, selected service, alias source, alias ID, category ID, category name, `answer_source`, `answer_source_reason`, `answer_source_confidence`, `router_intent`, `source_record_ids`, and `answer_context_cache_hit` for debugging. Supported group or party booking requests resolve party size and guest service counts into ordered `booking_segments`, call availability, and can be booked through the same POS-first booking service after the caller selects a slot and provides required customer details. Party parsing distinguishes person-count phrases such as "for two guests" from service-count phrases such as "two manicures"; session `party_plan` may include optional `parse_source`, `parse_confidence`, `clarify_reason`, group `source`, and `evidence` fields for debugging and review. Ambiguous party service families and party-size/service-count mismatches ask for catalog-backed clarification before availability. Offered slots may include ordered `segments` with provider-neutral service/staff assignments. The engine can select a unique offered slot from ordinal replies, spoken times such as "one p.m.", or a "Yes" reply to a prompt that confirmed one specific offered time; unclear time fragments repeat the existing offered slots instead of rerunning availability. Once a customer selects a slot, the session stores selected `booking_segments` and `staff_selection_mode` so simulator and phone flows can create one multi-service or supported party POS-first booking request. When `staff_selection_mode=anyone`, the customer did not choose a named technician, so the conversation avoids presenting the POS staff assignment as a customer-selected technician. Reschedule and cancellation requests use `booking_action` values `reschedule` and `cancel`, look up upcoming POS-backed appointments by caller phone, ask the caller to select or confirm the target appointment, and then call the provider-neutral booking service. A simulator booking is marked `booking_confirmed` only when the booking service returns a confirmed booking attempt with a POS booking ID and appointment. Cancellation is marked `booking_cancelled` only when the booking service returns a POS-cancelled appointment. POS failures create `booking_fallback_pending` wording and do not create confirmed, rescheduled, or cancelled appointment language.

Session responses include a persisted `state_revision`. Production message and
typed voice-recovery execution acquire one session-scoped database lock before
the first state read and retain it through any availability/POS side effect and
the final transcript/state commit. `event_key` is persisted on both sides of
the turn; replay returns that event's exact historical AI reply without
reinterpreting the request or replacing a newer current session state. The repository also compares `state_revision` during `SaveTurn`; after a
bounded retry, an unresolved stale write returns
`409 CONVERSATION_STATE_CONFLICT` without silently overwriting newer state.

When `consultation_enabled=true`, a caller may enter AI Consultation without a booking request. The semantic lane extracts controlled consultation needs but does not choose a service. Field-level `set`, `replace`, `add`, `remove`, and `clear` mutations are validated against controlled values and are the sole persistence authority for consultation need fields, so a free-standing semantic snapshot cannot overwrite scalar or list state and corrections do not accumulate stale preferences. Backend ranking uses only complete `ready` profiles from the eligible active-provider service catalog, stores the profile revisions and reasons in dialog state, and asks one useful question per turn. Consultation never calls availability or booking tools. A recommendation with one result asks whether the caller wants booking help; multiple results ask for a concrete service choice, and that choice still requires a separate booking-intent confirmation. Deterministic health signals are checked before normal turn routing; a validated structured safety assessment is also handled before any draft, availability, consultation, or POS action. A caller may end with `outcome=consultation_completed`, return to an unchanged booking draft, or enter owner handoff for safety or repeated unresolved input.

Structured service, alias, staff, and business-hour answer context is guarded by
a database-owned active-provider/location/generation/readiness fence on every
turn. A local cache hit is accepted only when that current fence matches. Cache
misses double-read the fence around context loading; a concurrent switch/sync
retries, and a non-ready snapshot fails closed by hiding provider-owned
structured data while retaining eligible salon-authored knowledge.

Configured production turns first enter the state-driven Turn Kernel. The kernel derives `expected_input`, measures deterministic coverage, and selects one explicit route: `fast_lane`, `answer_lane`, `action_lane`, `recovery_lane`, or `semantic_lane`. Unambiguous expected-field evidence, offered-slot choices, state-scoped confirmations, structured questions, and operational actions avoid a reply-model round trip. For a new booking with no selected service, an exact catalog service advances directly to the next missing field and a category asks for one concrete catalog option; neither path asks whether to add or replace. An add-or-replace operation choice is valid only after a service has been selected. The semantic lane selects one of two strict contracts from operational state, not caller wording. `guidance_turn` is limited to initial caller-goal or service-guidance state with no booking progress and returns a typed `guidance_action`, bounded explicit `guidance_party_size`, extraction-only consultation needs, confidence, reason, and safety without a separately model-authored goal, acts, or questions. The model always receives the complete stable `recognizable_guidance_actions` protocol (`book`, `service_catalog`, `consultation`, `salon_question`, `name_service`, `reschedule`, `cancel`, and `human_handoff`), so recognition of a caller's request is not disabled when a catalog or recommendation profile is unavailable. The backend separately derives `turn_available_guidance_actions` and the runtime service-guidance capability from the current catalog, salon consultation toggle, and ready owner-approved profiles. It validates the recognized action, derives the general goal, rejects party sizes outside 2-20 or attached to a non-book action, and then resolves whether that action can be fulfilled. A valid initial party size is translated into the same guest-count act consumed by the reducer. A recognized consultation request remains consultation even when personalized recommendation is disabled or unavailable: the reply truthfully offers catalog-backed grouping when possible, otherwise explains the technical service-guide limitation and offers owner help without guessing a service or blaming the caller. The shorter `dialog_state.guidance.offered_actions` remains the state-owned choice set used only during bounded dependency recovery. A semantic `service_catalog` action always renders from the active bookable service catalog; active knowledge text cannot become the service-menu source. `full_turn` is required for corrections, multi-signal or partial-coverage turns, pending/review/party state, and any existing booking progress; it may return zero or more ordered acts and questions. Full-contract acts cover add/replace/remove/undo plus set/clear corrections for service, staff, date/time, customer, and guest state. One utterance may contain both a correction and a question; the reducer applies validated correction semantics first, answers the question from structured sources, then resumes one useful pending question. Replacement source and target remain separate. Pending candidates are context rather than a closed vocabulary, current-draft questions do not become catalog-count questions, and repeated unresolved clarification is bounded. For a completed `party_plan`, service correction pending state uses `party_service_target`, `party_service_guest`, `party_service_operation`, and `party_service_source` with exact `guest_ref`; short replies resolve deterministically, redundant `guest_scope` is cleared, only the selected party group may change, and offered slots/review authorization are invalidated only after the correction resolves. Unresolved party-correction pending state blocks availability and booking even when semantic interpretation is unavailable.

The configured OpenAI reply model interprets only `semantic_lane` turns selected from state and deterministic coverage, never from a keyword-only gate. Input contains `expected_input`, `semantic_contract`, a PII-reduced utterance, selected or context-relevant service and staff identities, current party guest references/counts, pending act, booking action/stage, boolean customer-field presence, and current revision. Existing per-guest service assignments are not model input; they remain backend state so the model cannot manufacture replacement sources from draft layout. Initial guidance carries the full stable recognition vocabulary, while capability stays backend-owned. The accepted guidance action deterministically derives its general goal and protocol-owned companion fields: catalog questions own `catalog` subject, salon questions own their operational subject, and other actions cannot retain irrelevant catalog mode/subject decoration. Consultation profiles are never sent to the semantic model; owner-approved ready profiles remain backend-only inputs to consultation question planning and recommendation ranking. During an active consultation, model-authored booking/service acts are discarded before validation. Party service mutations must name an existing guest reference, and replacement sources must be grounded in the caller's current utterance; an initial multi-act party request may construct counted groups without flattening repeated services. A structured time preference is normalized to availability when the state is awaiting date/time, regardless of a model-authored staff or current-booking subject. Both structured contracts carry extraction-only consultation mutations and a global safety assessment; neither has side-effect authority. The backend rejects low-confidence goals/questions/acts, invalid guidance actions, invalid mutation semantics, malformed party counts, safety categories outside the controlled contract, and IDs outside the salon's active catalog. The interpreter inherits the simulator or phone request context and no longer installs a private 2.5-second deadline; the OpenAI adapter retains a 30-second HTTP transport ceiling. A caller-context timeout, provider failure, empty output, invalid schema, low-confidence result, or rejected catalog reference preserves the draft and may fall back to independently validated catalog and already-captured field evidence before asking the next missing-field question. Without such evidence, the runtime safely clarifies or hands off. Guidance provider-failure wording is selected only from typed `dialog_state.guidance.offered_actions`; the immediately following bounded choice can resolve one of those still-active actions without another provider call. It may offer profile-backed consultation only when consultation is enabled and at least one eligible profile is ready, and it never offers a service menu when the runtime catalog is empty. The model cannot call tools, mutate session state, or create confirmed wording.

New booking execution requires revision-bound final review. Service, staff, date/time, guest/party, or customer changes advance the draft revision; dependency-bearing changes also invalidate offered availability. The active POS provider still owns booking execution, and confirmed wording still requires POS success plus a booking ID.

Primary act/question/guidance validation is isolated from auxiliary consultation extraction. `unknown` is treated as absent, while malformed, low-confidence, catalog-invalid, and state-no-op consultation snapshots or mutations are dropped with `turn_consultation_profile_dropped` / `turn_consultation_mutations_dropped` diagnostics instead of rejecting valid primary meaning. Consultation mutation schema values come from the controlled protocol vocabulary plus current request catalog service IDs; runtime field/state validation remains authoritative. A syntactic question containing an expected-field value is partial coverage rather than an automatic fast-lane completion, so semantic constraints such as date plus time window are retained. Once guidance resolves to booking, the reply asks for the next missing booking field; a resolved salon question returns the structured answer without appending the generic caller-goal menu.

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
    "guidance_service_count": 4,
    "service_count": 4,
    "consultation_enabled": true,
    "consultation_ready_service_count": 2,
    "service_guidance": {
      "status": "recommendation_ready",
      "catalog_available": true,
      "consultation_enabled": true,
      "recommendation_ready": true,
      "ready_service_count": 2
    },
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
      "model": "tts-1",
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

`booking.guidance_service_count` counts canonical active-provider, linked,
versioned, synced, active, non-archived AI-bookable services without requiring
the current provider connection snapshot to be complete. It is the source for
service menus and consultation. `booking.service_count` and
`booking.staff_count` additionally require the strict active provider,
selected location, completed snapshot generation, and `last_sync_at` fence used
for availability and booking. `booking.service_guidance.status` is one of
`recommendation_ready`, `catalog_only`, `consultation_disabled`, or
`catalog_unavailable`.
It reports whether the runtime can show the canonical guidance catalog and
whether enough owner-approved consultation profiles exist to make a
personalized recommendation. Guidance may be ready while
`phone_booking_ready=false`; that state permits consultation but no availability
or booking provider call. It is independent of Twilio transport readiness.

`POST /api/salons/:id/voice/semantic-check`

Runs an authenticated, owner-scoped OpenAI semantic-contract probe using both
the strict `full_turn` and compact `guidance_turn` schemas with the configured
reply model used by live turn interpretation. The returned fingerprint covers
the ordered schema set. The synthetic requests contain no caller transcript, create no
conversation turn, does not mutate a booking draft, and cannot call
availability or POS booking tools.

Provider rejection is returned as `200` with `verified=false` so the endpoint
can be used as an operational diagnostic. Diagnostics are restricted to safe
bounded fields; provider response bodies and error messages are never exposed.

```json
{
  "provider": "openai",
  "configured": true,
  "verified": false,
  "schema_fingerprint": "sha256:4d8f3a1c2b7e9f01a2345678",
  "request_id": "req_123",
  "diagnostics": {
    "failure_stage": "turn_interpretation_response",
    "http_status": "400",
    "http_status_class": "4xx",
    "error_type": "invalid_request_error",
    "error_code": "invalid_json_schema",
    "error_param": "text.format.schema",
    "schema_fingerprint": "sha256:4d8f3a1c2b7e9f01a2345678"
  }
}
```

`configured=false` means the salon-scoped OpenAI integration is not enabled or
does not have the required model/key configuration. A successful live-contract
request set returns `verified=true`. A successful probe also closes the matching
local semantic-contract circuits for the same salon and current
provider/model/schema configurations.

`POST /api/salons/:id/voice/semantic-evaluate`

Runs one authenticated owner-scoped semantic scenario through the same
salon-scoped turn model used by live phone and simulator turns. This is a
read-only evaluation surface: it creates no conversation session or transcript,
does not call availability or booking tools, and cannot produce a confirmed
appointment. The request is bounded to 100 catalog services, 500 aliases, 100
categories, and 100 staff. Every selected, pending, draft, alias, category, and
party-group reference must point into the supplied catalog or the request is
rejected before the provider call.

```json
{
  "scenario_id": "guidance_catalog-001",
  "channel": "phone",
  "customer_message": "Could you walk me through the services you offer?",
  "expected_input": "caller_goal",
  "semantic_contract": "guidance_turn",
  "recognizable_guidance_actions": ["book", "service_catalog", "consultation", "salon_question", "name_service", "human_handoff", "reschedule", "cancel"],
  "booking_action": "book",
  "catalog_services": [
    {"service_id": "svc_luna", "service_name": "Luna Renewal", "category_id": "cat_ritual", "category_name": "Signature Ritual"}
  ],
  "catalog_service_aliases": [
    {"service_id": "svc_luna", "alias": "moon refresh"}
  ],
  "catalog_categories": [
    {"category_id": "cat_ritual", "category_name": "Signature Ritual", "aliases": ["renewal"], "service_ids": ["svc_luna"]}
  ]
}
```

The response contains `scenario_id`, the validated structured `result`, and
`duration_ms`. Provider-disabled returns `503`; caller-context timeout returns
`504`; provider failure returns `502`. Provider response bodies and secrets are
never exposed.

The generated corpus and deterministic review live at
`backend/modules/conversation/testdata/receptionist_semantic_scenarios.json`
and `receptionist_semantic_review.json`. The schema-v2 corpus contains 196
authored base utterances: each has an exact phone and simulator instance, then
transparent generated delivery variants expand the set to 1,000 single-turn
semantic-contract scenarios. The 100 review records are deterministic corpus,
source-of-truth, state, safety, and hardcoding audits; they are not model
executions and are not semantic pass results. Regenerate and validate them with:

```bash
cd backend
GOCACHE=/private/tmp/manleai-go-cache go run ./cmd/conversation-eval-corpus
GOCACHE=/private/tmp/manleai-go-cache go run ./cmd/conversation-eval -mode offline
```

Offline mode writes
`receptionist_semantic_offline_report.json` with
`contract_validated_count=1000`, `model_evaluated_count=0`, `passed_count=0`,
and `not_run_count=1000`. It proves corpus and scoring-contract validity only;
it does not claim that a model passed any scenario.

Live evaluation requires an authenticated owner token file and deliberately
does not read provider configuration from environment files:

```bash
GOCACHE=/private/tmp/manleai-go-cache go run ./cmd/conversation-eval \
  -mode live \
  -api-base https://example.com/api \
  -salon-id SALON_UUID \
  -token-file /secure/path/owner-token \
  -scenario-ids guidance_catalog-002,guidance_catalog-022,guidance_consultation-001,guidance_consultation-002,guidance_consultation-021,guidance_consultation-022 \
  -output /tmp/conversation-eval-report.json
```

Live mode requires `-output` and is hard-capped at 12 selected scenarios. Use
`-scenario-ids id-1,id-2` for targeted canaries or `-limit N` for a bounded
prefix. `-sample-per-family N` remains available for deterministic offline
subsets, but the current 14-family corpus exceeds the live cap even at one per
family. The unselected 1,000-case corpus cannot be sent as a paid live batch.
Selectors cannot be combined with each other or with `-limit`.
`-transient-retries N` permits zero to three
evaluator-only retries for endpoint `502`, `503`, or `504`; semantic mismatches
are never retried. Reports retain the validated model result and duration for
every pass and failure and expose both `transient_retry_count` and
`recovered_transient_count`, so infrastructure recovery cannot be mistaken for
semantic quality. This evaluator policy does not alter runtime phone or
simulator retry behavior.

For the separately bounded direct-model pilot, the generated artifact is
`backend/modules/conversation/testdata/receptionist_semantic_pilot_50.json`.
It contains 50 directly authored executions across 45 distinct nail-salon
situations, with five core situations represented once for phone and once for
simulator. It contains no generated paraphrase filler. Direct-model mode calls
the stored OpenAI model once for recognition, then lets the production
Conversation Service decide whether that turn needs a guarded style reply, a
profile-backed consultation question, or no output-model call. It does not
force a universal second rewrite. Review batches contain at most five retained
outputs; the complete 50 therefore has 10 review rounds. With no retries the
complete-run hard ceiling remains 110, while operational/terminal turns may use
fewer calls. Evaluation contract `production-flow-v8` rejects final replies
that expose any dynamic fixture identifier, uses separate typed salon-local
`hour` and `minute` model fields so the provider never computes minutes after
midnight, converts those clock components before production slot filtering,
and deterministically verifies every retained offered slot against
the recorded `before`, `after`, or `exact` constraint. It also treats expected
consultation extraction as exact, rejects invented booking/completion flags,
and rejects positive consultation mutations not represented in the same-turn
structured snapshot. Protocol `unknown` is normalized to field absence, and
guidance actions remain the only initial workflow-transition authority.
Reviewer contract `evidence-review-v9` explicitly checks
those consultation facts, the recorded local slot minutes and timezone, as well
as machine-facing labels, silent service/staff/date/time mutations, and
unnecessarily broad hours answers. Version 9 also accepts explicit multi-turn
journey evidence and requires the reviewer to inspect every retained
transcript/state/tool turn rather than only the terminal reply.

Direct-model mode is a local CLI workflow, not an API endpoint. Before any
model call, the supplied salon ID selects both the encrypted OpenAI row in
`salon_integration_configs` and a zero-model-call database readiness preflight.
The preflight requires `service_guidance.status=recommendation_ready` and
records guidance/ready-profile counts plus the separate provider/booking state.
`ResolveOpenAIConfigStrict` fails closed if that
database row is absent, disabled, undecryptable, or incomplete; it never falls
back to environment provider values. Scenario state, catalogs, aliases,
consultation profiles, staff, and availability are isolated evaluation
fixtures. The runner does not use an owner token, does not create conversation
records, does not call the internal semantic-evaluation API, and blocks every
booking, reschedule, or cancellation write. Synthetic availability may be
returned to exercise the caller-facing flow, but it is reported as a tool
attempt and never reaches the POS. Direct reports therefore set
`context_source=isolated_fixture` and `runtime_readiness_verified=false`; the
separate `runtime_preflight` object is database evidence, not proof that the
fixture scenarios executed against that salon's runtime catalog.

Run the exact pilot with an explicit paid-call ceiling and report path:

```bash
cd backend
GOCACHE=/private/tmp/manleai-go-cache go run ./cmd/conversation-eval \
  -mode direct-model \
  -salon-id CONFIG_SALON_UUID \
  -max-model-calls 110 \
  -transient-retries 0 \
  -output /tmp/conversation-eval-pilot-50.json
```

Run the approved ten-scenario canary before the complete pilot:

```bash
GOCACHE=/private/tmp/manleai-go-cache go run ./cmd/conversation-eval \
  -mode direct-model \
  -salon-id CONFIG_SALON_UUID \
  -scenario-ids pilot-003,pilot-005,pilot-007,pilot-015,pilot-019,pilot-032,pilot-038,pilot-042,pilot-047,pilot-050 \
  -max-model-calls 22 \
  -transient-retries 0 \
  -output /tmp/conversation-eval-canary-10.json
```

The complete pilot corpus is contract-validated before selectors are applied.
`-scenario-ids`, `-limit`, and `-sample-per-family` are mutually exclusive.
Direct review round count is derived from the selected results in batches of at
most five, so the ten-scenario canary performs two review calls.

The default checkpoint is
`/tmp/conversation-eval-pilot-50.json.checkpoint.json`. It is written atomically
with owner-only permissions before and after every model call. A matching
checkpoint resumes completed recognition, service-selected output generation,
and review work without repeating it. The run key includes explicit evaluation
and reviewer contract versions, so a changed harness cannot resume incompatible
evidence. If a process stops while a paid call is marked in flight, resume
fails closed instead of silently repeating a possibly billed request. Raising
`-transient-retries` from zero permits only bounded transport/provider retries;
every retry consumes the same `-max-model-calls` budget and semantic mismatches
are never retried. The report retains per-scenario structured recognition,
backend-safe reply, final reply, structured backend route/transition/reply-source
evidence, booking-confirmed and provider-ID facts, owner-request handoff mode,
model calls, token usage, tool attempts, durations, failures, and the selected
review records. Repository tests validate the
harness and fixtures without making a paid model call; they do not claim that
the current stored model passes the pilot. Review booking safety uses backend
facts: the absence of a provider booking ID is safe when the backend did not
confirm, and a recognition-only `booking_requested` flag is not confirmation.
An `owner_request` handoff must not be rewritten as a live transfer. Each review
round passes only when all five scores are at least 4/5; the report counts review
passes and failures separately, and the CLI exits unsuccessfully when any review
round fails. These model scores remain a secondary review layer: deterministic
identifier-leak and booking-safety checks can fail a scenario before reviewer
scoring.

The independent multi-turn salon suite uses a different local command and
artifact:

```bash
cd backend
GOCACHE=/private/tmp/manleai-go-cache go run ./cmd/conversation-eval-real \
  -mode structural \
  -corpus modules/conversation/testdata/receptionist_real_salon_100.json \
  -output modules/conversation/testdata/receptionist_real_salon_structural_report.json

GOCACHE=/private/tmp/manleai-go-cache go run ./cmd/conversation-eval-real \
  -mode deterministic-runtime \
  -corpus modules/conversation/testdata/receptionist_real_salon_100.json \
  -output /tmp/receptionist-real-salon-runtime.json
```

The first command validates exactly 100 independently authored journeys and
must retain `passed=false`, `model_executed_count=0`, and
`review_passed_count=0`. The second keeps one isolated production Conversation
Service session across every turn, executes all 100 with retained scripted
semantics, and retains complete transcripts, state transitions, tool attempts,
handoff, and confirmation facts. Corpus, production-flow, and transcript-review
contract versions are part of the run key, including the shared reviewer
contract, so an incompatible checkpoint cannot be resumed. It still cannot
claim a model pass. A paid
canary is explicit and database-configured:

```bash
GOCACHE=/private/tmp/manleai-go-cache go run ./cmd/conversation-eval-real \
  -mode live-canary \
  -config-salon-id CONFIG_SALON_UUID \
  -max-model-calls 60 \
  -transient-retries 0 \
  -output /tmp/receptionist-real-salon-live-10.json
```

The config salon selects only the encrypted database OpenAI integration; it
does not own the isolated fixtures. Exactly ten corpus journeys are marked as
live canaries. The runner stops on the first journey failure, never exceeds 60
combined recognition/reply/consultation/review calls, and cannot pass until all
ten journeys are model-executed and both five-journey multi-turn reviews pass.
No live canary is run by repository tests or corpus generation.

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

`GET /api/salons/:id/conversation-sessions/:session_id/realtime-events?limit=100&offset=0`

Owner-authenticated debug endpoint for the selected phone call session. It
returns a page from the latest realtime lifecycle and timing audit events
recorded in `voice_webhook_events`, scoped by salon ownership and session ID
and ordered chronologically within the page. `limit` defaults to 50 and is
bounded to 100; `offset` defaults to 0. `has_more=true` means older events are
available at `offset + limit`. The Calls dashboard follows those pages,
deduplicates immutable event IDs, and sorts the complete selected-call timeline
chronologically. The response intentionally exposes only debug-safe fields
extracted from provider payloads, including transcript admission decisions,
configured/effective audio handling, confidence/VAD measurements, response correlation IDs, and
salted canonical-output hashes. Raw transcript, audio, and provider payloads
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
      "stage": "openai_spoken_fact_mismatch",
      "stream_sid": "MZ...",
      "error": "realtime audio transcript did not match backend reply",
      "diagnostics": {
        "request_id": "manleai-reply-2",
        "response_id": "resp_...",
        "match_classification": "canonical_fact_mismatch",
        "expected_hash": "d0be2dc421be4fcd",
        "actual_hash": "78b3fbc972cf74f1"
      },
      "created_at": "2026-06-29T00:13:10Z"
    }
  ],
  "limit": 100,
  "offset": 0,
  "has_more": true
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

Public Twilio Media Streams WebSocket endpoint for realtime audio mode. The endpoint is not configured directly in Twilio Console; the incoming webhook returns `<Connect><Stream>` with signed custom parameters for the existing call session. The stream forwards Twilio g711 audio frames to the configured OpenAI Realtime adapter for VAD and transcription, then routes accepted completed transcripts through the same backend conversation engine and booking service. GA sessions request `item.input_audio_transcription.logprobs`; `automatic`, `standard`, and `strong_noise_rejection` enable near-field input noise reduction, while `minimal_processing` does not force input noise reduction. Admission is policy-aware and fail-closed: missing/invalid confidence metadata, low mean confidence, low-tail token confidence, or transcript density inconsistent with VAD duration is audited as `transcript_rejected_low_confidence` and leaves the conversation draft unchanged. Automatic mode begins with standard admission and, after structured low-confidence or VAD-coherence evidence, switches the current call to stronger admission for later turns. It does not inspect transcript wording or infer the caller's location; missing provider metadata is not treated as noise evidence. Replayed transcript-completion events with the same provider item/transcript key are ignored before admission, recovery, or conversation mutation. Each admitted transcript advances an input generation; nonterminal backend replies from superseded generations are discarded before speech. Consecutive rejected transcripts use bounded in-stream recovery: a short retry, a one-answer retry that repeats the last backend-approved question, then background-noise coaching. Recovery output waits behind any accepted backend turn and is removed when that backend reply becomes available. A fourth consecutive rejection calls a typed conversation action instead of fabricating a customer message; it creates one `voice_input_unintelligible` owner callback handoff when the session has a callable caller number, or completes the call with a quieter-place retry instruction when it does not. The handoff uses a stable per-session event key so repeated or concurrent execution does not create duplicate owner work, and it must persist before callback wording is spoken. A successfully admitted transcript resets the rejection streak. Confidence rejection never switches the call to recording/gather; that fallback remains reserved for terminal Realtime/provider failures. Accepted and rejected transcript timing events record PII-free `item_id`, `input_generation`, decision/reason, configured `profile`, `effective_profile`, `adaptive`, `runtime_action`, `audio_quality_signal`, mean/min logprob, token count, VAD duration, `rejection_streak`, and `recovery_action` when available; timing payloads do not store transcript or audio bodies. Transcription steering uses concise salon, catalog, pending-candidate, and alias keyword lists instead of example booking sentences.

With dashboard setting `speech_output_mode=streaming_tts`, backend-approved text is sent to the dedicated Speech API with raw PCM output. OpenAI PCM is signed little-endian mono audio at 24 kHz; the adapter incrementally applies a stateful anti-aliasing 3:1 FIR resampler and encodes raw PCMU 8 kHz frames. The bridge sends one bounded 200 ms startup block, then releases one 160-byte frame every 20 ms from a bounded queue. Queue saturation applies producer backpressure, so provider HTTP chunk speed cannot create a WebSocket burst or reorder/drop audio. A reply shorter than the startup target flushes when its Speech stream completes. Speech provider completion is recorded at `tts_stream_done`, while the reply remains active until the local playout queue drains and records `tts_playout_done`; only then may the typed scheduler start the next eligible reply or emit a terminal Twilio `mark`. Output kinds are ordered by workflow authority: terminal, current backend turn, initial reply, input recovery, then progress. No new TTS request starts while caller speech is active or a stopped-speech item is awaiting its transcript. Caller barge-in before or after startup clears local queued audio and Twilio playback, cancels streaming speech immediately even inside the legacy playback guard, and rejects late chunks by generation. Input-recovery speech has a four-second first-provider-byte budget; on expiry it is canceled so current backend output can advance. A terminal backend or typed handoff reply latches the call, clears nonterminal pending output, ignores later transcripts, and closes exactly once after playout, interruption, or timeout. Provider failure clears any partial playback and enters terminal fallback.

PII-free stages include `speech_stopped`, `transcript_admitted`, `backend_turn_start`, `backend_turn_done`, `tts_request_start`, `tts_first_provider_chunk`, `tts_first_byte_timeout`, `tts_startup_buffer_ready`, `twilio_first_media_sent`, `tts_stream_done`, `tts_playout_done`, and playback completion through the terminal mark. Speech diagnostics expose `input_generation`, `reply_kind`, stale-reply suppression, input/output sample rates, post-first-chunk producer wall/active/audio durations, producer rate excluding local backpressure, observed provider emit gap, producer backpressure duration/count, maximum queue depth, playout duration/frame/batch counts, underrun count, and WebSocket write latency. Backpressure metrics distinguish an upstream provider gap from time intentionally spent waiting for local queue capacity. `backend_turn_done` also exposes whitelisted substage durations for route/config resolution, session load, answer-context load, turn routing, turn interpretation, availability/POS calls, and turn persistence. Router diagnostics include `turn_route`, `turn_expected_input`, `turn_route_reason`, `turn_deterministic_coverage`, `turn_semantic_contract`, `turn_recognizable_guidance_actions`, `turn_available_guidance_actions`, `turn_guidance_action`, `service_guidance_capability`, `service_guidance_catalog_available`, `service_guidance_recommendation_ready`, `turn_model_service_count`, and `turn_model_staff_count`. `turn_interpreter_path`, `turn_interpreter_outcome`, `turn_interpreter_ms`, and `turn_interpreter_schema_fingerprint` distinguish and time a skipped model call, accepted structured interpretation, timeout, provider failure, empty or invalid output, confidence rejection, and catalog rejection. Provider failures may also expose the bounded fields `turn_interpreter_provider`, `turn_interpreter_failure_stage`, `turn_interpreter_http_status`, `turn_interpreter_http_status_class`, and `turn_interpreter_request_id`. Request and response bodies are never copied into these diagnostics. These diagnostics contain no transcript, audio, customer, or provider-secret values. Speech output failures close through the signed recording/gather fallback without changing booking state or creating a second booking attempt.

`speech_output_mode=buffered_realtime` is a legacy fallback. Only that mode uses Realtime `response.create`, response identity binding, complete output-transcript validation, and release after `response.done`. Operational facts are never allowed to bypass backend/POS ownership in either mode. If realtime configuration is missing, voice status falls back to the recording or gather path.

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

`POST /api/integrations/square/webhook`

Public Square booking-event receiver. The handler verifies HMAC-SHA256 against
the exact dashboard-stored HTTPS notification URL plus raw request body, using
the encrypted salon-scoped signature key and constant-time comparison. Event
IDs are durably deduped before `200`; invalid signatures or bodies are rejected.
If both envelope and nested booking location IDs are present, they must be
identical before tenant routing. The worker fetches current Square booking truth
rather than trusting event payload status, and scheduled calendar repair remains
a backstop. Event and repair completions are claim-token fenced, so an expired
worker cannot complete or overwrite a newer lease. A locally
stored verifier configuration is not evidence of an active Square subscription
or successful recent delivery. Tenant routing requires an exact
merchant/location match and Square as the salon's active provider. Recoverable
connection states (`connected`, `syncing`, `active`, `error`, and
`expired_token`) may enqueue a valid signed event; `not_connected` and
`disabled` connections cannot.

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
Catalog and customer pagination continue until cursor exhaustion and reject a
repeated cursor. Services are imported only when the Square variation is
available for booking and has a positive normalized duration. Staff are the
intersection of active selected-location Team members and bookable Booking
Profiles. The provider snapshot is applied transactionally; missing previously
imported rows are disabled/unmapped, and sync never automatically re-enables an
owner-disabled `ai_bookable` flag.

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

Safe-retry example; an initial write omits `retry_of_attempt_id`:

```json
{
  "operation_key": "square-test-booking-4fa6c4e2",
  "retry_of_attempt_id": "a2a86a35-95d4-4ad1-a3ab-0dc4c71b1163",
  "availability_quote_id": "availability-quote-uuid",
  "slot_fingerprint": "sha256-hex",
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

Creates a real Square booking through the provider-neutral booking service.
The selected slot must come from the availability endpoint and include current
quote evidence. Returns `201` only when Square returns an accepted booking ID;
returns `202` for unconfirmed `pos_pending`, `provider_pending`, or
`fallback_pending` outcomes.

`retry_of_attempt_id` is omitted on the initial write. After a definitive
failure returns a safe `fallback_pending` attempt, an explicit retry uses a new
operation key and supplies that exact attempt ID. A response-loss replay keeps
the same operation key and logical payload, including the same retry lineage
when the lost request was itself a retry; a freshly revalidated quote proof does
not turn that replay into a new logical operation. The availability quote remains single-use for
unrelated requests; the repository can atomically transfer its consumer only
when `consumed_by_attempt_id` exactly matches `retry_of_attempt_id` and the
quote has not expired.

The test-booking response and `latest_test_booking` include `operation_type`,
`provider_outcome`, `retry_policy`, `reconciliation_status`, `can_retry`, and
`retry_blocked_reason`. The dashboard selects retry lineage only from a safe
fallback with the matching operation type (`book` for create and `cancel` for
cancel), including after a page reload. It disables another test write while
the previous operation is `pos_pending`, in-flight, or requires
reconciliation. Square status reads recover stale test-operation leases before
returning `latest_test_booking`.

`POST /api/integrations/square/cancel-test-booking`

Safe-retry example; an initial cancellation omits `retry_of_attempt_id`:

```json
{
  "operation_key": "square-test-cancel-b8c8f001",
  "retry_of_attempt_id": "069171f5-27a7-44ea-a759-8a986a10cd05",
  "salon_id": "...",
  "appointment_id": "...",
  "reason": "AI booking readiness test cleanup"
}
```

Cancels the latest Square test booking through the provider-neutral booking
service. Returns `200` when Square cancels the booking, or `202` with
`fallback_pending` when POS cancellation fails. As with create, the optional
`retry_of_attempt_id` is sent only for an explicit safe retry and must identify
the matching prior `cancel` attempt; the retry uses a new operation key.

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
