# Phone Booking Demo Configuration Memo

This memo tells future agents how to guide the owner through configuring the live phone booking demo step by step.

The target use case is strict:

Customer calls -> AI answers through Twilio -> customer asks for a booking -> AI checks Booking Calendar / Square Appointments availability -> AI offers only available slots inside valid salon business hours -> customer chooses a slot -> AI confirms only after Square returns a successful booking ID -> appointment is stored and synced through the POS-first booking path.

## Agent Rules

- Guide one configuration step at a time. Do not dump every step unless the user asks for the full checklist.
- After each step, ask the user to confirm what is done before moving on.
- Never ask the user to paste secrets into chat. Tell them which env variable or provider console field needs the secret.
- Never say the demo is ready until `GET /api/salons/:id/voice/status` returns both `ready: true` and `phone_booking_ready: true`.
- Keep Square as the first real POS provider. Do not claim generic POS support for this demo.
- Keep the POS-first invariant explicit: no confirmed appointment unless Square returns a POS booking ID.
- If Square fails, the expected behavior is fallback pending owner review, not confirmation.

## Source Of Truth

- Backend env names: `backend/.env.example`
- Local commands: `docs/local-development.md`
- Deployment secrets: `docs/deployment.md`
- Square behavior: `docs/square-integration.md`
- Voice/Twilio API: `docs/api.md`
- POS boundary: `docs/pos-adapter-layer.md`

## Step 0 - Establish Current State

Ask the user which mode they are configuring:

1. Local simulator only
2. Local backend exposed with ngrok for Twilio/Square callbacks
3. Deployed/staging backend with real public URL

Then ask what is already done:

- Backend running?
- Frontend running?
- Square app created?
- Twilio number purchased/configured?
- OpenAI API key available?
- Salon record phone number known?

Do not proceed to provider setup before backend and public URL are clear.

## Step 1 - Local App Baseline

Expected local startup:

```bash
cp .env.example .env
docker compose up -d --build
```

Seed local owner only for local development:

```bash
cd backend
DATABASE_URL=postgres://ai_receptionist:ai_receptionist@localhost:55432/ai_receptionist?sslmode=disable make seed-local
```

Run API:

```bash
cp backend/.env.example backend/.env
cd backend
make run-api
```

Run frontend:

```bash
cd frontend
npm install
npm run dev
```

Local login:

```txt
owner@lotusnails.example / password123
```

Seeded local phones:

```txt
Salon inbound phone: +16292536211
Owner handoff phone: +13125550102
```

Use the salon inbound phone as Twilio `To` / simulator `-to`.

## Step 2 - Public API URL

For live Twilio or Square OAuth callback, the backend must have a public HTTPS origin.

For ngrok/local demo, tell the user to expose the backend API port and then set:

```bash
VOICE_PUBLIC_BASE_URL=https://<public-api-url>
SQUARE_REDIRECT_URL=https://<public-api-url>/api/integrations/square/callback
```

If deployed, use the deployed API origin for both values.

Do not use the frontend URL for these two variables.

## Step 3 - Core Backend Env

Minimum backend env values:

```bash
APP_ENV=local
SERVER_PORT=8080
DATABASE_URL=postgres://ai_receptionist:ai_receptionist@localhost:55432/ai_receptionist?sslmode=disable
JWT_SECRET=<long-secret>
TOKEN_ENCRYPTION_KEY_BASE64=<32-byte-base64>
CORS_ALLOWED_ORIGINS=http://localhost:3088
FRONTEND_URL=http://localhost:3088
AUTO_MIGRATE=true
```

For deployed/staging, replace `CORS_ALLOWED_ORIGINS` and `FRONTEND_URL` with the deployed frontend origin.

## Step 4 - Square Configuration

In Square Developer:

- Use sandbox first.
- Set OAuth redirect URL to:

```txt
https://<public-api-url>/api/integrations/square/callback
```

Backend env:

```bash
SQUARE_ENVIRONMENT=sandbox
SQUARE_CLIENT_ID=<square-client-id>
SQUARE_CLIENT_SECRET=<square-client-secret>
SQUARE_REDIRECT_URL=https://<public-api-url>/api/integrations/square/callback
SQUARE_API_VERSION=2026-05-20
```

The adapter requests these Square scopes:

```txt
APPOINTMENTS_READ
APPOINTMENTS_WRITE
APPOINTMENTS_BUSINESS_SETTINGS_READ
CUSTOMERS_READ
CUSTOMERS_WRITE
ITEMS_READ
ITEMS_WRITE
MERCHANT_PROFILE_READ
EMPLOYEES_READ
EMPLOYEES_WRITE
```

Sandbox OAuth uses the Square sandbox dashboard session and does not include
`session=false`; production OAuth includes `session=false`.

Dashboard flow:

1. Open `/dashboard/integrations`.
2. Connect Square.
3. Select location.
4. Sync services, staff, business hour periods, and customers.
5. Confirm at least one active AI-bookable service.
6. Confirm at least one active AI-bookable staff member.
7. Enable AI booking.
8. Optional smoke test: create a real Square test booking.
9. Optional cleanup: cancel that test booking.

Do not proceed to live phone booking until the Square readiness checks pass.

## Current Demo State - Square Sandbox Nail Salon

Use these notes when rebuilding or repairing the current Square sandbox demo.

Salon:

- Local salon: `Lotus Nails Studio`
- Square location: `L3QRV6AZKFKVQ`
- The active Square connection must include:
  `APPOINTMENTS_READ`, `APPOINTMENTS_WRITE`,
  `APPOINTMENTS_BUSINESS_SETTINGS_READ`, `CUSTOMERS_READ`,
  `CUSTOMERS_WRITE`, `ITEMS_READ`, `ITEMS_WRITE`,
  `MERCHANT_PROFILE_READ`, `EMPLOYEES_READ`, `EMPLOYEES_WRITE`

Sandbox token recovery:

- Sandbox browser OAuth can fail with a blank Square page or `HTTP 403`.
- If browser OAuth fails, use Square Developer Console -> Sandbox -> OAuth -> Test account authorizations.
- Copy the sandbox access token locally only. Do not paste the token into chat or docs.
- Import the token only after encrypting it with `TOKEN_ENCRYPTION_KEY_BASE64`, then write the encrypted value to `pos_connections.access_token_encrypted`.

Demo service menu:

- `Classic Manicure` - 30 minutes - `$25`
- `Gel Manicure` - 45 minutes - `$38`
- `Classic Pedicure` - 45 minutes - `$40`
- `Spa Pedicure` - 60 minutes - `$55`
- `Dip Powder Manicure` - 60 minutes - `$50`
- `Acrylic Full Set` - 75 minutes - `$65`
- `Gel Removal` - 20 minutes - `$15`

Square service bookability requirements:

- `item_data.product_type` must be `APPOINTMENTS_SERVICE`.
- Each service variation must include `service_duration`.
- Each service variation must set `available_for_booking=true`.
- Each service variation must set `transition_time=0`.
- Each service variation must set `team_member_ids` to Square Appointments-bookable team members.
- If these fields are missing, Square availability returns:
  `INVALID_VALUE: Service variation is not bookable`.

Demo staff:

- `Mai Nguyen`
- `Tina Tran`
- `Kelly Pham`
- `Jenny Le`

Current staff limitation:

- Square Bookings API currently reports only `Mai Nguyen` as Appointments-bookable.
- Local `staff.ai_bookable` should be `true` only for `Mai Nguyen` until Square Dashboard creates or enables booking profiles for the other demo staff.
- `Tina Tran`, `Kelly Pham`, and `Jenny Le` may remain active staff records, but they should not be offered for Square availability yet.
- If a non-bookable staff member is sent to Square availability, Square returns:
  `INVALID_VALUE: Invalid team member <id>`.

Expected validation:

- `2026-06-21`: no availability slots.
- `2026-06-22`: availability slots return; the first known slot was `2026-06-22T17:00:00Z`.
- The dashboard availability staff dropdown should show only `Mai Nguyen` until the other staff have Square Appointments booking profiles.

Recovery checklist:

1. Verify scopes in `pos_connections.scopes`.
2. Verify Square catalog has 7 active nail services.
3. Verify each Square service variation has `available_for_booking=true`, `transition_time=0`, and `team_member_ids`.
4. Verify local `services.pos_service_version` is synced after Square catalog updates.
5. Verify only Square Appointments-bookable staff have local `staff.ai_bookable=true`.
6. Test Square `/v2/bookings/availability/search`.
7. Test local `/api/salons/:id/availability`.

## Step 5 - Twilio Configuration

Backend env:

```bash
VOICE_PROVIDER=twilio
VOICE_PUBLIC_BASE_URL=https://<public-api-url>
VOICE_TWILIO_AUTH_TOKEN=<twilio-auth-token>
VOICE_TWILIO_INCOMING_PATH=/api/voice/twilio/incoming
VOICE_TWILIO_TURN_PATH=/api/voice/twilio/turn
VOICE_TWILIO_RECORDING_PATH=/api/voice/twilio/recording
```

In Twilio phone number voice settings:

```txt
When a call comes in:
POST https://<public-api-url>/api/voice/twilio/incoming
```

Twilio webhook signature verification uses `VOICE_TWILIO_AUTH_TOKEN`. If the token is wrong, Twilio webhooks return `TWILIO_SIGNATURE_INVALID`.

The salon phone in the dashboard/database must match the Twilio number receiving the call. The backend routes inbound calls by matching Twilio `To` to `salons.phone`.

## Step 6 - OpenAI Voice AI Configuration

The local app can run Twilio speech `<Gather>` without OpenAI STT/TTS. For a full voice demo with recording-mode turns, transcription, safer AI reply rewriting, and generated audio playback, configure OpenAI:

```bash
VOICE_AI_PROVIDER=openai
VOICE_OPENAI_API_KEY=<openai-api-key>
VOICE_OPENAI_BASE_URL=https://api.openai.com/v1
VOICE_OPENAI_TRANSCRIPTION_MODEL=gpt-4o-mini-transcribe
VOICE_OPENAI_REPLY_MODEL=gpt-4.1-mini
VOICE_OPENAI_SPEECH_MODEL=gpt-4o-mini-tts
VOICE_OPENAI_SPEECH_VOICE=alloy
```

Readiness meaning:

- STT ready enables recording mode.
- LLM ready allows safe reply rewriting, but it must not override POS-first confirmation safety.
- TTS ready allows Twilio `<Play>` audio responses.

If OpenAI is not configured, do not block the whole phone webhook demo. Use Twilio Gather / deterministic safe replies for initial testing.

## Step 7 - Readiness Check

Use the Calls dashboard or the API:

```txt
GET /api/salons/:id/voice/status
```

Required for live phone booking:

```json
{
  "ready": true,
  "phone_booking_ready": true
}
```

Interpretation:

- `ready: true` means Twilio can route calls into the voice webhook.
- `phone_booking_ready: true` means the call path has enough Square booking setup to check availability and attempt POS-first booking.

If `phone_booking_ready` is false, inspect `booking.checks`:

- `enable_ai_booking`
- `connect_square`
- `sync_square`
- `bookable_services`
- `bookable_staff`
- `business_hours`

Fix checks in order.

## Step 8 - Simulator Test

Use the simulator before placing a live call:

```bash
cd backend
go run ./cmd/twilio-sim \
  -auth-token "$VOICE_TWILIO_AUTH_TOKEN" \
  -to "+16292536211" \
  -turn "I need a classic manicure tomorrow." \
  -turn "The first one works. My name is Linh Tran and my phone is 312-555-0199."
```

Expected flow:

1. Incoming call returns TwiML `<Gather>` greeting.
2. First turn returns available slot offers, not confirmation.
3. Second turn either confirms in Square Appointments if POS booking succeeds, or creates fallback pending owner review.

If the simulator fails signature verification, compare:

- `VOICE_PUBLIC_BASE_URL`
- simulator `-base-url`
- simulator `-signature-base-url`
- `VOICE_TWILIO_AUTH_TOKEN`

## Step 9 - Live Call Test

When simulator passes:

1. Call the Twilio number.
2. Ask for a specific service and day.
3. Confirm one of the offered slots.
4. Provide name and phone.
5. Verify the call only says confirmed if Square returns a POS booking ID.
6. Verify the appointment appears in Booking Calendar / Square Appointments.
7. Verify Calls dashboard shows the session, offered slots, selected slot, and booking outcome.

## Common Failure Map

`TWILIO_SIGNATURE_INVALID`

- Wrong Twilio auth token.
- Public URL mismatch between Twilio request URL and backend verification URL.
- Webhook path differs from env path.

`VOICE_PROVIDER_NOT_CONFIGURED`

- `VOICE_PROVIDER` is not `twilio`.
- `VOICE_TWILIO_AUTH_TOKEN` is empty.

`We could not route this call to a salon`

- Twilio `To` phone does not match `salons.phone`.
- Dashboard salon phone is missing or wrong.

`phone_booking_ready: false`

- Square not connected.
- Square location not selected.
- Services/staff not synced.
- No active AI-bookable service/staff.
- Business hours missing.
- AI booking not enabled.

AI offers no slots

- Square availability returned no slots.
- Requested day is outside business hours.
- Staff/service mapping is not AI-bookable.
- Service duration/version data missing from sync.

AI creates fallback pending instead of confirmation

- This is expected when Square booking fails.
- Check `booking_attempts`, `owner_notifications`, and `pos_errors`.
- Do not treat fallback pending as confirmed.

## Agent Handoff Prompt

When the user resumes configuration, start with:

```txt
Which step are you on now: backend/public URL, Square, Twilio, OpenAI, readiness, simulator, or live call?
```

Then guide only that step, verify it, and wait before moving forward.
