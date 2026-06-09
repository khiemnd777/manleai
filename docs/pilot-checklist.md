# Pilot Checklist

## Milestone 1 and 2 Checklist

- [x] Monorepo structure
- [x] Docker Compose for PostgreSQL, Redis, API, frontend
- [x] Go/Fiber API
- [x] Startup foundation migration
- [x] Ent schema mirror
- [x] Auth module
- [x] Salon module
- [x] POSProvider interface
- [x] Square adapter package
- [x] Square OAuth URL and callback
- [x] Encrypted POS token storage
- [x] POS connection, sync log, and error tables
- [x] Admin login
- [x] Dashboard shell
- [x] Salon profile creation UI
- [x] Square integration page

## Before Milestone 3 Review

- [x] Add signed/persisted OAuth state nonce.
- [ ] Confirm Square Appointments API payload details against a real sandbox account.
- [x] Add integration tests with mocked Square HTTP responses.
- [x] Add service/staff list endpoints for dashboard tables.

## Milestone 3 Next Steps

- [x] Add booking_attempts and appointment tables.
- [x] Add booking service that depends only on `pos.POSProvider`.
- [x] Implement customer search/create in SquareAdapter.
- [x] Implement availability checks in SquareAdapter.
- [x] Implement create appointment in SquareAdapter.
- [x] Implement reschedule/cancel appointment in SquareAdapter.
- [x] Add booking test UI and AI booking enablement gate.
- [x] Add owner appointments dashboard with confirmed POS bookings and fallback pending requests.
- [x] Add service/staff AI booking controls for synced Square records.

## Milestone 4 Conversation Simulator

- [x] Add call session, transcript, and handoff persistence.
- [x] Add deterministic conversation engine module that does not import Square packages.
- [x] Route simulator booking attempts through the provider-neutral booking service.
- [x] Keep AI-disabled and human-request paths as owner handoffs, not confirmed appointments.
- [x] Add dashboard Calls page with simulator transcript, detected details, outcomes, and recent sessions.
- [x] Add backend tests for confirmed, fallback pending, AI-disabled, and handoff paths.

## Milestone 5 Live Voice Webhooks

- [x] Add `phone` call session channel and provider call metadata.
- [x] Add voice webhook event audit persistence.
- [x] Add provider-neutral voice runtime interfaces for telephony, STT, LLM, and TTS adapters.
- [x] Add Twilio incoming and speech-turn webhooks with `X-Twilio-Signature` verification.
- [x] Route Twilio phone sessions through the same conversation engine and booking service.
- [x] Keep phone booking confirmations impossible unless POS booking succeeds.
- [x] Add Calls dashboard live phone readiness, channel badges, and phone/simulator metrics.
- [ ] Connect external OpenAI STT/LLM/TTS adapters after the deterministic phone webhook path is reviewed.
