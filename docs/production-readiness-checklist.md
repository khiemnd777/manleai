# Production Readiness Checklist

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
- [x] Add salon-scoped AI receptionist tone presets that flow from Settings through phone/LLM reply generation without changing booking guardrails.
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

## Production Service Understanding

- [x] Interpret service utterances against the active salon catalog instead of hardcoded salon-specific keyword patches.
- [x] Add owner-reviewable service categories and category aliases for menu-level service understanding.
- [x] Keep category and category-alias matches as clarification candidates, not directly bookable services.
- [x] Add idempotent system taxonomy refresh for common nail salon groups without overriding owner-reviewed service assignments.
- [x] Treat generic and fuzzy service-family matches as clarification candidates, not selected services.
- [x] Persist salon-scoped service aliases from owner corrections with stable `(salon_id, normalized_alias)` upsert semantics.
- [x] Prevent one normalized phrase from being both an active service alias and an active category alias.
- [x] Feed active service aliases into simulator and phone conversation runtime.
- [x] Feed active service categories and category aliases into simulator and phone conversation runtime.
- [x] Re-check availability when a caller corrects the service after a time or slot was already collected.
- [x] Record service-understanding status, reason, confidence, candidates, selected service, alias source, alias ID, category ID, and category name in transcript metadata.

## Party Booking Owner Review

- [x] Detect group and party booking requests as owner handoffs, not confirmed bookings.
- [x] Persist structured `party_booking_requests` with stable salon/session/event keys to avoid duplicate requests on retry.
- [x] Add owner status transitions for pending, contacted, resolved, and dismissed party requests.
- [x] Redact party request representative PII and guest-service notes during call lifecycle redaction.

## Canonical POS Ownership And Provider Switch Gates

- [x] Add durable `pos_entity_links` and sync status fields for canonical services, staff, and customers.
- [x] Add local create/update/archive flows for services, staff, and customers while keeping local-only or unmapped records out of booking.
- [x] Add capability-gated `pos_sync_jobs` with worker processing, sync logs, POS errors, and duplicate open-job prevention.
- [x] Add `salons.active_pos_provider` as the active booking/readiness source of truth.
- [x] Add provider switch readiness, switch run persistence, service/staff/customer match review, dry-run readiness, and gated import wizard shell.
- [x] Keep provider switch import, dry-run, and activation disabled while Square Appointments is the only native POS adapter.
- [ ] Enable executable alternate-provider import, dry-run, and activation only after a real alternate adapter exists.

## Public Catalog And Landing App

- [x] Add owner-managed public catalog settings and publish readiness.
- [x] Add unauthenticated public-safe catalog APIs for first published salon and slug lookup.
- [x] Add separate `landing/` Next.js app for public salon pages.
- [x] Keep public pages call-to-book only; no web booking attempt is presented as confirmed.
- [x] Avoid exposing staff contact details, POS IDs, sync errors, owner identifiers, or provider tokens.

## Configuration Transfer

- [x] Add sanitized owner-scoped configuration export with stable schema version.
- [x] Exclude services, staff, customers, appointments, call sessions, transcripts, POS OAuth tokens, API keys, client secrets, encrypted secrets, and operational records.
- [x] Include service category taxonomy and service category aliases in schema v4 while continuing to exclude service records and provider state.
- [x] Use stable category slugs and normalized alias keys so repeated imports update existing taxonomy instead of duplicating it.
- [x] Add import preview and apply flows for existing salons and onboarding.
- [x] Use request IDs for repeated import applies so retries do not create duplicate import runs.
- [x] Gate live `ai_enabled`, confirmed booking mode, and public catalog publishing until Square Appointments readiness passes on the target salon.

## Call Lifecycle Retention And Realtime Streams

- [x] Add call session lifecycle filters for active, archived, and redacted sessions.
- [x] Add idempotent archive and irreversible redaction actions with active-session conflict gates.
- [x] Add worker-driven 90-day retention redaction for expired active sessions.
- [x] Clear customer PII, transcript bodies, handoff summaries, webhook payloads, and temporary voice audio while preserving booking/handoff/provider-call audit links.
- [x] Add Twilio Media Streams and OpenAI Realtime adapter path with completed transcripts routed back through the same conversation engine and booking service.
