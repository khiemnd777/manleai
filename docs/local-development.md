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

## Notes

Mock data is not the production path. The local seed creates a demo owner and salon only so the dashboard can be exercised without manual SQL.
