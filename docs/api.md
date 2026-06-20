# API

Base URL: `http://localhost:18089`

All endpoints except login, refresh, logout, health, the Square OAuth callback, and Twilio voice webhooks require:

```txt
Authorization: Bearer <access_token>
```

## Auth

`POST /api/auth/login`

```json
{
  "email": "owner@lotusnails.example",
  "password": "password123"
}
```

Returns access token, refresh token, user, roles, and primary salon ID.

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

## Salons

`GET /api/salons`

Returns salons owned by the authenticated user.

`POST /api/salons`

```json
{
  "name": "Lotus Nails Studio",
  "phone": "+13125550101",
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

`GET /api/salons/:id/business-hours`

`PUT /api/salons/:id/business-hours`

`GET /api/salons/:id/services`

Returns synced provider-neutral services for dashboard tables.

`PATCH /api/salons/:id/services/:service_id/ai-bookable`

```json
{
  "ai_bookable": false
}
```

Updates only the internal AI booking eligibility flag for a synced service. Square service records are not edited. Inactive Square services cannot be enabled for AI booking.

`GET /api/salons/:id/staff`

Returns synced provider-neutral staff for dashboard tables.

`PATCH /api/salons/:id/staff/:staff_id/ai-bookable`

```json
{
  "ai_bookable": true
}
```

Updates only the internal AI booking eligibility flag for a synced staff member. Square staff records are not edited. Inactive Square staff cannot be enabled for AI booking.

`POST /api/salons/:id/availability`

Returns provider-neutral available booking slots from the active POS provider for an AI-bookable service and optional AI-bookable staff member. Results are filtered to the salon's configured business hours in the salon timezone. This endpoint does not create a booking attempt or confirm an appointment.

```json
{
  "service_id": "...",
  "staff_id": "...",
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
  "preferred_date": "2026-06-10",
  "duration_minutes": 45,
  "timezone": "America/Chicago",
  "slots": [
    {
      "start_time": "2026-06-10T15:00:00Z",
      "end_time": "2026-06-10T15:45:00Z",
      "staff_id": "...",
      "staff_name": "Mai Nguyen"
    }
  ]
}
```

`GET /api/salons/:id/customers`

Returns an owner-scoped operational customer list aggregated from internal appointments, booking attempts, call sessions, and handoff requests. This is not a full Square customer directory sync.

```json
{
  "customers": [
    {
      "key": "phone:+13125550101",
      "name": "Linh Tran",
      "phone": "+13125550101",
      "email": "linh@example.com",
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

Returns appointments recorded after POS success, including confirmed, rescheduled, and cancelled statuses.

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

Returns booking attempts, including transient `pos_pending` records and `fallback_pending` records that need owner review.

`POST /api/salons/:id/booking-attempts`

```json
{
  "customer_name": "Linh Tran",
  "customer_phone": "+13125550101",
  "customer_email": "linh@example.com",
  "service_id": "...",
  "staff_id": "...",
  "start_time": "2026-06-10T15:00:00Z",
  "notes": "First visit"
}
```

Creates a backend booking attempt before calling the active `POSProvider`. Returns `201` with status `confirmed` only when the POS provider returns a POS booking ID and booking version. Returns `202` with status `fallback_pending` when the POS provider fails, times out, or does not return required booking metadata.

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

Processes one simulated customer message through the deterministic conversation engine. The simulator asks one question at a time, can create owner handoffs for human requests or disabled AI booking, checks provider-neutral availability before selecting a booking time, offers available slots from Square Appointments, and calls the provider-neutral booking service only after the customer selects a slot and required customer details are collected. A simulator booking is marked `booking_confirmed` only when the booking service returns a confirmed booking attempt with a POS booking ID and appointment. POS failures create `booking_fallback_pending` wording and do not create confirmed appointment language.

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

Returns owner-scoped live voice, phone booking, and external AI provider readiness without exposing Twilio, OpenAI, or POS token secrets. `ready` means Twilio can route live phone webhooks; `phone_booking_ready` means the phone path also has the booking prerequisites needed to offer available Square Appointments slots and attempt POS-first confirmation.

```json
{
  "provider": "twilio",
  "configured": true,
  "signature_verification": true,
  "inbound_webhook_url": "https://api.example.com/api/voice/twilio/incoming",
  "turn_webhook_url": "https://api.example.com/api/voice/twilio/turn",
  "recording_webhook_url": "https://api.example.com/api/voice/twilio/recording",
  "salon_phone": "+13125550101",
  "ready": true,
  "phone_booking_ready": true,
  "input_mode": "recording",
  "booking": {
    "ready": true,
    "ai_enabled": true,
    "square_connected": true,
    "square_synced": true,
    "service_count": 4,
    "staff_count": 3,
    "business_hours_count": 6,
    "checks": [
      {"key": "enable_ai_booking", "label": "Enable AI booking", "complete": true},
      {"key": "connect_square", "label": "Connect Square Appointments", "complete": true},
      {"key": "sync_square", "label": "Sync Square calendar", "complete": true},
      {"key": "bookable_services", "label": "AI-bookable services", "complete": true},
      {"key": "bookable_staff", "label": "AI-bookable staff", "complete": true},
      {"key": "business_hours", "label": "Business hours", "complete": true}
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
    }
  }
}
```

`POST /api/voice/twilio/incoming`

Public Twilio Programmable Voice webhook for a new inbound call. Requires a valid `X-Twilio-Signature` generated with `VOICE_TWILIO_AUTH_TOKEN`. The webhook matches Twilio `To` to the salon phone, creates or reuses a `phone` conversation session, records a `voice_webhook_events` audit row, and returns TwiML with a speech `<Gather>`.

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

`GET /api/voice/audio/:id`

Public short-lived audio output endpoint for Twilio `<Play>` responses. IDs are unguessable runtime UUIDs, expire quickly, and never expose POS tokens or provider secrets.

The phone path never confirms an appointment unless the booking service returns a POS-confirmed booking attempt with a POS booking ID and appointment.

## Square

`GET /api/integrations/square/connect-url?salon_id=<id>`

Returns a Square OAuth URL and state.

`GET /api/integrations/square/callback?code=<code>&state=<state>`

Exchanges the Square OAuth code and stores encrypted tokens.

`GET /api/integrations/square/status?salon_id=<id>`

Returns the Square connection, recent sync logs, and AI booking readiness checks.

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

```json
{
  "salon_id": "..."
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

Sets `salons.ai_enabled=true` only after Square is connected, a location is selected, services/staff are synced, at least one service and staff member are AI-bookable, and the latest test booking was created and cancelled successfully.

`POST /api/integrations/square/disable-ai-booking`

```json
{
  "salon_id": "..."
}
```

Sets `salons.ai_enabled=false`.
