# Phone Booking Demo Configuration Memo

This memo tells future agents how to guide the owner through configuring the live phone booking demo step by step.

The target use case is strict:

Customer calls -> AI answers through Twilio -> customer asks for a booking -> AI checks Booking Calendar / Square Appointments availability -> AI offers only available slots inside valid salon business hours -> customer chooses a slot -> AI confirms only after Square returns a successful booking ID -> appointment is stored and synced through the Square-backed `external_provider` booking path.

## Agent Rules

- Guide one configuration step at a time. Do not dump every step unless the user asks for the full checklist.
- After each step, ask the user to confirm what is done before moving on.
- Never ask the user to paste secrets into chat. Tell them which Integrations dashboard field or provider console field needs the secret. Do not inspect or use env files to configure or diagnose active provider behavior.
- Never say this external-provider confirmation demo is ready until `GET /api/salons/:id/voice/status` returns `scheduling_authority: "external_provider"`, `booking_mode: "confirmed_booking"`, and all three readiness dimensions true: `phone_answering_ready`, `request_capture_ready`, and `automated_booking_ready`. `phone_booking_ready` is only the compatibility alias for the last dimension.
- Keep Square as the first real POS provider. Do not claim generic POS support for this demo.
- Keep the Square-backed `external_provider` confirmation invariant explicit:
  no confirmed appointment unless Square returns the required booking ID and
  metadata.
- If Square fails, the expected behavior is fallback pending owner review, not confirmation.

## Source Of Truth

- Backend env files: infrastructure only; they do not contain provider configuration.
- Dashboard provider config: `/dashboard/integrations` and `GET /api/salons/:id/integration-configs` are the source of truth.
- Runtime readiness/debug evidence: `GET /api/salons/:id/voice/status` and the owner-scoped Calls realtime timeline.
- Local commands: `docs/local-development.md`
- Deployment secrets: `docs/deployment.md`
- Square behavior: `docs/square-integration.md`
- Voice/Twilio API: `docs/api.md`
- POS boundary: `docs/pos-adapter-layer.md`
- AI receptionist tone: `/dashboard/settings` and `GET/PUT /api/salons/:id/settings`

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

For ngrok/local demo, tell the user to expose the backend API port and then set
the public origin in the Integrations dashboard:

- Square tab: `Redirect URL`
- Twilio tab: `Public API base URL`

Use these values:

```txt
Twilio public API base URL: https://<public-api-url>
Square redirect URL: https://<public-api-url>/api/integrations/square/callback
```

If deployed, use the deployed API origin for both values.

Do not use the frontend URL for these two dashboard fields and do not copy the
values into env files. Save them in the Integrations dashboard.

## Step 3 - Core Backend Env

Minimum backend env values are infrastructure/runtime bootstrap only:

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
Do not put Square, Twilio, or OpenAI provider settings or secrets in env files.
Save them through the Integrations dashboard.

## Step 4 - Square Configuration

In Square Developer:

- Use sandbox first.
- Set OAuth redirect URL to:

```txt
https://<public-api-url>/api/integrations/square/callback
```

In the ManleAI Integrations dashboard, Square tab:

- `Environment`: `Sandbox`
- `Square client ID`: sandbox application ID
- `Square client secret`: sandbox application secret
- `Redirect URL`: `https://<public-api-url>/api/integrations/square/callback`
- `Square API version`: `2026-05-20`
- `Square API base URL`: blank, or `https://connect.squareupsandbox.com`

The adapter requests these Square scopes:

```txt
APPOINTMENTS_READ
APPOINTMENTS_ALL_READ
APPOINTMENTS_WRITE
APPOINTMENTS_ALL_WRITE
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
`session=false`; production OAuth includes `session=false`. If sandbox OAuth
opens a blank page, first open the sandbox seller dashboard:

```txt
https://app.squareupsandbox.com/dashboard/
```

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
  `APPOINTMENTS_READ`, `APPOINTMENTS_ALL_READ`, `APPOINTMENTS_WRITE`,
  `APPOINTMENTS_ALL_WRITE`,
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

In the ManleAI Integrations dashboard, Twilio tab:

- `Public API base URL`: `https://<public-api-url>`
- `Twilio auth token`: Twilio Auth Token
- `Incoming path`: `/api/voice/twilio/incoming`
- `Turn path`: `/api/voice/twilio/turn`
- `Recording path`: `/api/voice/twilio/recording`
- `Stream path`: `/api/voice/twilio/stream`
- `Voice transport`: `recording` for the legacy recording loop, or `realtime_stream` for Twilio Media Streams + OpenAI Realtime.

When buffered TTS is used, the backend generates a short-lived signed
`/api/voice/audio/:id` URL for Twilio `<Play>`. The URL query contains only an
expiry and HMAC signature; the signature is bound to the exact salon, call,
session, provider, audio row, and database expiry. A missing stored auth token
keeps the text fallback and never emits an unsigned URL. Rotating the dashboard
Twilio auth token invalidates playback URLs generated with the prior token.

In Twilio phone number voice settings:

```txt
When a call comes in:
POST https://<public-api-url>/api/voice/twilio/incoming
```

Do not paste the realtime stream URL into the Twilio phone number settings. The
backend returns `<Connect><Stream>` from the incoming webhook with signed
call-session parameters when `Voice transport` is `realtime_stream` and OpenAI
Realtime is ready.

Twilio webhook signature verification uses the dashboard-saved Twilio auth
token. If the token is wrong, Twilio webhooks return
`TWILIO_SIGNATURE_INVALID`.

The salon phone in the dashboard/database must match the Twilio number receiving the call. The backend routes inbound calls by matching Twilio `To` to `salons.phone`.

## Step 6 - OpenAI Voice AI Configuration

The local app can run Twilio speech `<Gather>` without OpenAI STT/TTS. For a full voice demo with recording-mode turns, transcription, safer AI reply rewriting, and generated audio playback, configure OpenAI in the ManleAI Integrations dashboard, OpenAI tab:

- Enable OpenAI voice AI
- `OpenAI API key`: OpenAI API key
- `Base URL`: `https://api.openai.com/v1`
- `Transcription model`: `gpt-4o-mini-transcribe`
- `Reply model`: `gpt-4.1-mini`
- `Speech model`: `tts-1`
- `Speech voice`: `alloy`
- `Speech output mode`: `Low-latency streaming TTS (recommended)`; use buffered Realtime only as a legacy rollback mode
- Optional realtime mode:
  - Enable realtime
  - `Realtime model`: `gpt-realtime-2`
  - `Realtime voice`: `alloy`
  - `Background-noise handling`: `Automatic (recommended)`; this adapts each call from incoming audio evidence and does not assume where the caller is
  - `Realtime instructions`: optional non-secret operating notes for the audio bridge

Readiness meaning:

- STT ready enables recording mode.
- LLM ready allows safe reply rewriting, but it must not override Square-backed
  `external_provider` confirmation safety.
- Before testing freeform semantic turns, run the owner-authenticated
  `POST /api/salons/:id/voice/semantic-check`. Require `configured=true` and
  `verified=true`; a failed check returns only safe provider/status/schema
  diagnostics and performs no conversation, availability, or POS writes.
- TTS ready allows Twilio `<Play>` audio responses.
- Realtime ready enables Twilio Media Streams input mode when the Twilio tab also selects `realtime_stream`. Completed transcripts still go through the backend Turn Kernel, conversation reducer/planner, and booking service. Deterministically complete turns use a fast/answer/action/recovery lane; only ambiguous, corrective, multi-intent, or partially covered turns enter the bounded semantic lane. OpenAI Realtime must not confirm bookings on its own.

AI receptionist speaking style is configured in Settings, not in the OpenAI tab.
Use `/dashboard/settings` -> `AI receptionist` -> `Speaking style` to set
`ai_tone`. Tone is passed into LLM reply requests when available, but it is
style-only: it must not change required booking slots, handoff decisions,
availability checks, or Square-backed `external_provider` confirmation wording. Realtime should speak
backend-approved replies and should not rely on independent realtime
instructions for tone changes.
Realtime reply order is backend-owned: each queued reply has an application
request ID and ignores late audio from a cancelled or prior generation. GA input requires confidence metadata and applies
the selected background-noise policy to mean, low-tail, and VAD-coherence admission; automatic mode can strengthen admission for the current call after structured degraded-audio evidence; a
rejected transcript is reprompted without changing conversation state. Barge-in
before first playback cancels the active streaming reply immediately; after
playback begins it must pass the playback guard before Twilio clear/cancel. In
`streaming_tts` mode, OpenAI Realtime is input-only;
backend-approved text is sent to the Speech endpoint as raw PCM 24 kHz, converted
through a stateful anti-aliasing resampler to PCMU 8 kHz, and held in a bounded
200 ms startup buffer before the first Twilio media message. The remaining audio
uses a bounded backpressure queue and a monotonic 20 ms frame cadence instead of
provider HTTP chunk timing. Short replies flush at Speech completion; longer
replies keep draining after provider completion, and FIFO advancement waits for
local playout completion. A provider or conversion failure uses terminal
fallback and never retries booking. `buffered_realtime` is the legacy rollback
mode: it still binds response identity, buffers complete output, and validates
the final audio transcript before release.

If OpenAI is not configured, do not block the whole phone webhook demo. Use Twilio Gather / deterministic safe replies for initial testing.

## Step 7 - Readiness Check

Use the Calls dashboard or the API:

```txt
GET /api/salons/:id/voice/status
```

Required for this Square-backed live confirmation demo:

```json
{
  "scheduling_authority": "external_provider",
  "booking_mode": "confirmed_booking",
  "phone_answering_ready": true,
  "request_capture_ready": true,
  "automated_booking_ready": true
}
```

Interpretation:

- `phone_answering_ready: true` means the configured Twilio path can accept the salon's inbound call.
- `request_capture_ready: true` means the selected external authority can collect a structured request and offer provider-backed availability.
- `automated_booking_ready: true` means confirmed mode is selected and the external authority passes every execution/write-safety gate required for Square confirmation. `phone_booking_ready` mirrors this value for compatibility.

If a dimension is false, inspect that dimension's typed `blockers` first. For
`external_provider`, the legacy `booking.checks` remain useful Square-specific
diagnostics:

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
read -s TWILIO_SIM_AUTH_TOKEN
go run ./cmd/twilio-sim \
  -auth-token "$TWILIO_SIM_AUTH_TOKEN" \
  -to "+16292536211" \
  -turn "I need a classic manicure tomorrow." \
  -turn "The first one works. My name is Linh Tran and my phone is 312-555-0199." \
  -turn "Yes, please book it."
unset TWILIO_SIM_AUTH_TOKEN
```

Enter the Twilio auth token locally when prompted. This transient shell value
is only for signing the simulator request; do not store it in an env file or
paste it into chat.

Expected flow:

1. Incoming call returns TwiML `<Gather>` greeting.
2. First turn returns available slot offers, not confirmation.
3. After service, slot, and customer details are complete, the AI reads a final review and asks whether it should book.
4. Only the caller's explicit final-review authorization may trigger the POS booking attempt.
5. Square success with a booking ID produces booked/confirmed wording; POS failure creates fallback pending owner review.

If the simulator fails signature verification, compare:

- dashboard Twilio `Public API base URL`
- simulator `-base-url`
- simulator `-signature-base-url`
- dashboard Twilio auth token entered locally for `TWILIO_SIM_AUTH_TOKEN`

## Step 9 - Live Call Test

When simulator passes:

1. Call the Twilio number.
2. Ask for a specific service and day.
3. Select one of the offered slots.
4. Provide name and phone.
5. Listen to the final review and explicitly authorize booking.
6. Verify the call only says confirmed if Square returns a POS booking ID.
7. Verify the appointment appears in Booking Calendar / Square Appointments.
8. Verify Calls dashboard shows the session, offered slots, selected slot, draft/review revisions, typed turn-understanding metadata, and booking outcome.

## Common Failure Map

`TWILIO_SIGNATURE_INVALID`

- Wrong Twilio auth token.
- Public URL mismatch between Twilio request URL and backend verification URL.
- Webhook path differs from the dashboard Twilio path.

`VOICE_PROVIDER_NOT_CONFIGURED`

- Dashboard Twilio config is missing, disabled, or incomplete.
- Dashboard Twilio auth token is empty.

`We could not route this call to a salon`

- Twilio `To` phone does not match `salons.phone`.
- Dashboard salon phone is missing or wrong.

`request_capture_ready: false` or `automated_booking_ready: false`

- The dimension's `blockers` identify the exact authority, mode, fence, catalog,
  availability, or execution prerequisite.
- For this external-provider demo, Square-specific causes include no connection,
  no selected location, stale/incomplete sync, missing AI-bookable service or
  staff, missing imported business hours, and blocked booking writes.

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
