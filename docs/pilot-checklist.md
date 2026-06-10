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
- [x] Add Customers dashboard with internal customer activity and gated Square phone lookup.

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

## Milestone 6 External AI Voice Providers

- [x] Add provider adapters for external STT, LLM, and TTS behind `modules/voice` interfaces.
- [x] Keep Twilio-specific webhook validation and TwiML response logic outside the conversation engine.
- [x] Add AI reply guardrails for one-question-at-a-time turns, low confidence, human requests, complaints, refunds, payment disputes, and complex group bookings.
- [x] Keep confirmed wording impossible unless `booking.Service` returns a POS-confirmed booking attempt, POS booking ID, and appointment.
- [x] Add fallback and owner-handoff behavior when STT, LLM, TTS, or POS calls fail.
- [x] Add Calls dashboard readiness for external STT, LLM, and TTS provider configuration.
- [x] Keep knowledge base CRUD, owner corrections, and training workflows out of M6; those remain Milestone 7 scope.

## Milestone 7A Knowledge And Owner Corrections

- [x] Add salon-scoped knowledge item persistence for FAQ, policy, services, hours, handoff, and operations notes.
- [x] Add owner correction persistence with pending, applied, and dismissed statuses.
- [x] Add owner-scoped knowledge and correction API routes.
- [x] Feed active knowledge into conversation reply generation as advisory context only.
- [x] Keep appointment confirmation impossible unless the booking service returns POS-confirmed booking state.
- [x] Replace the AI Training placeholder with an operational dashboard for knowledge CRUD and corrections.

## Milestone 7B Transcript Review Corrections

- [x] Add correction capture actions to call transcript messages.
- [x] Persist transcript-linked corrections with call session and transcript message IDs.
- [x] Require transcript-linked corrections to include a call session source.
- [x] Add review-before-apply flow so owners can edit corrections into reusable knowledge.
- [x] Show correction source context in AI Training.

## Milestone 7C Training Evaluation Preview

- [x] Add read-only training evaluation endpoint for active knowledge previews.
- [x] Keep training evaluation from creating call sessions, transcripts, bookings, or POS calls.
- [x] Return matched knowledge, preview reply, outcome, booking action, and POS confirmation boundary.
- [x] Add AI Training dashboard panel for testing sample customer questions.
- [x] Keep unsafe confirmation wording guarded by Square Appointments confirmation requirements.
