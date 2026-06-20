# Local Development

## Prerequisites

- Go 1.23+
- Node 20+
- Docker
- PostgreSQL client tools for `make seed-local`

## Start Services

```bash
cp .env.example .env
docker compose up -d --build
```

## Seed Local Owner

```bash
cd backend
DATABASE_URL=postgres://ai_receptionist:ai_receptionist@localhost:55432/ai_receptionist?sslmode=disable make seed-local
```

## Run API

```bash
cp backend/.env.example backend/.env
cd backend
make run-api
```

## Run Frontend

```bash
cd frontend
npm install
npm run dev
```

Visit `http://localhost:3088/login`.

## External Voice AI

The local app can run without external AI providers. To exercise Milestone 6 voice provider readiness and recording-mode turns, set these values in `.env` or `backend/.env`:

```bash
VOICE_AI_PROVIDER=openai
VOICE_OPENAI_API_KEY=...
VOICE_OPENAI_TRANSCRIPTION_MODEL=gpt-4o-mini-transcribe
VOICE_OPENAI_REPLY_MODEL=gpt-4.1-mini
VOICE_OPENAI_SPEECH_MODEL=gpt-4o-mini-tts
VOICE_OPENAI_SPEECH_VOICE=alloy
```

When external STT is configured, Twilio can use `/api/voice/twilio/recording`; otherwise the existing `/api/voice/twilio/turn` speech gather path remains available.

## Simulate Twilio Phone Booking Webhooks

Use the local Twilio simulator to exercise the real phone webhook path without hand-writing signed requests. The API must be running, `VOICE_TWILIO_AUTH_TOKEN` must match the backend config, and `-to` must be the salon phone number stored for routing inbound calls.

```bash
cd backend
go run ./cmd/twilio-sim \
  -auth-token "$VOICE_TWILIO_AUTH_TOKEN" \
  -to "+13125550101" \
  -turn "I need a classic manicure tomorrow." \
  -turn "The first one works. My name is Linh Tran and my phone is 312-555-0199."
```

The simulator posts to `/api/voice/twilio/incoming`, then posts each customer utterance to `/api/voice/twilio/turn` with a valid `X-Twilio-Signature`. This exercises the same flow as a live call: AI greeting, availability lookup inside valid booking hours, slot negotiation, and POS-first confirmation. A booking is confirmed only if Square Appointments returns a successful booking ID; otherwise the call path must fall back to a pending owner request. For the seeded salon, `+13125550101` is the inbound salon phone; `+13125550102` is only the owner handoff phone.

## Notes

Mock data is not the production path. The local seed creates a demo owner and salon only so the dashboard can be exercised without manual SQL.
