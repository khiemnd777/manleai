# Local Development

## Prerequisites

- Go 1.23+
- Node 20+
- Docker

## Start Services

```bash
make restart
```

`make restart` is the only local stack command. It creates `.env` with private
local infrastructure secrets when missing, builds all images, starts
PostgreSQL/Redis, checks migration checksums, starts the API, applies the
guarded `sample_test` fixture, and waits for every service to become healthy.

The first incompatible legacy sample volume is removed automatically after the
checksum preflight identifies it. A compatible volume is never reset. Repeated
restarts return `sample_migration_replayed=true` and preserve the exact one
`Lotus Nails Studio` tenant. Login values are stored in the ignored mode-`600`
file `.local/sample-data.env`; the fixed tenant-owner email is
`owner@lotusnails.example`.

## Run API

```bash
cp backend/.env.example backend/.env
cd backend
make run-api
```

Local rate limiting is opt-in. Set `RATE_LIMIT_ENABLED=true` to exercise the
same Redis-backed middleware against `REDIS_URL`; production defaults it on.
The local refresh cookie remains HttpOnly and SameSite-Strict but omits Secure
so `http://localhost` development can authenticate.

## Run Frontend

```bash
cd frontend
npm install
npm run dev
```

Visit `http://localhost:3088/login`.

## Run Landing

The public customer-facing app is separate from the admin dashboard.

```bash
cd landing
npm install
npm run dev
```

Visit a published salon page at `http://localhost:3090/s/<public-slug>`.
The landing app reads `LANDING_API_BASE_URL` or `NEXT_PUBLIC_API_BASE_URL` and
does not require an owner login for public catalog reads.

## External Voice AI

The local app can run without external AI providers. To exercise voice provider readiness and recording-mode turns, configure OpenAI in the Integrations dashboard. Provider models, voices, output mode, and secrets are dashboard/database configuration and must not be placed in env files.

When external STT is configured, Twilio can use `/api/voice/twilio/recording`; otherwise the existing `/api/voice/twilio/turn` speech gather path remains available. For lower latency, configure Twilio `voice_transport=realtime_stream`, OpenAI Realtime input, and `Low-latency streaming TTS` output in the Integrations dashboard.

## Simulate Twilio Phone Booking Webhooks

Use the local Twilio simulator to exercise the real phone webhook path without hand-writing signed requests. The API must be running, `-auth-token` must match the salon's configured Twilio auth token, and `-to` must be the salon phone number stored for routing inbound calls.

```bash
cd backend
go run ./cmd/twilio-sim \
  -auth-token "$VOICE_TWILIO_AUTH_TOKEN" \
  -to "+16292536211" \
  -turn "I need a classic manicure tomorrow." \
  -turn "The first one works. My name is Linh Tran and my phone is 312-555-0199."
```

The simulator posts to `/api/voice/twilio/incoming`, then posts each customer
utterance to `/api/voice/twilio/turn` with a valid `X-Twilio-Signature`. The
seeded exercise is a Square-backed `external_provider` call: AI greeting,
availability lookup inside valid booking hours, slot negotiation, and Square-
backed external confirmation. It confirms only after Square Appointments
returns the required successful booking ID and metadata; otherwise the
external-provider path remains unconfirmed and follows pending/retry/
reconciliation handling. This describes the seeded Square exercise, not a
universal POS prerequisite for `owner_manual` or `manleai_calendar`. For the
seeded salon, `+16292536211` is the inbound salon phone; `+13125550102` is only
the owner handoff phone.

## Notes

Sample data is not the production-live path. It is visibly classified,
fixture-only, and never runs in the normal startup migration chain.
