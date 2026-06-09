# Test Plan

## Milestone 1

- Verify API startup migrations create the local PostgreSQL schema.
- Seed local owner and salon with `backend/seed/local.sql`.
- Verify login succeeds and returns access and refresh tokens.
- Verify `GET /api/auth/me` returns the owner and primary salon ID.
- Verify owner can create, read, and update salons.
- Verify owner can read and update salon settings and business hours.
- Verify frontend login stores tokens and opens the dashboard.
- Verify empty dashboard state links to salon onboarding.

## Milestone 2

- Verify `GET /api/integrations/square/connect-url` returns a Square OAuth URL when Square env vars are set.
- Verify Square callback stores encrypted tokens in `pos_connections`.
- Verify token values are not returned by status endpoints.
- Verify `GET /api/integrations/square/status` returns connection state and sync logs.
- Verify `GET /api/integrations/square/locations` calls Square through `SquareAdapter`.
- Verify `POST /api/integrations/square/select-location` stores `location_id`.
- Verify `POST /api/integrations/square/sync` writes normalized `services` and `staff` rows.
- Verify Square errors create `pos_errors` rows and failed sync logs.

## Milestone 3 Backend Foundation

- Verify Square OAuth state is signed, persisted, expires, and cannot be replayed.
- Verify Square API calls send the configured `Square-Version` header.
- Verify synced services keep POS service version metadata for future booking payloads.
- Verify `GET /api/salons/:id/services` and `GET /api/salons/:id/staff` return only owner-scoped synced records.
- Verify booking service tests only use `pos.POSProvider`, not `pos_square`.
- Verify POS success stores a confirmed appointment and POS booking ID.
- Verify POS failure stores `fallback_pending`, logs `pos_errors`, creates an owner notification, and does not create a confirmed appointment.
- Verify Square customer search/create payloads use Customers API fields and preserve customer IDs.
- Verify Square availability payloads include location, service variation, optional team member filter, and parse available slots.
- Verify Square create-booking payloads include location, start time, team member, service variation ID, service variation version, duration, and idempotency key.
- Verify confirmed appointments persist the POS booking version returned by Square.
- Verify Square reschedule payloads include booking version, location, start time, team member, service variation ID, service variation version, duration, and idempotency key.
- Verify Square cancel payloads include booking version and idempotency key.
- Verify reschedule/cancel service success updates internal appointment state only after the POS provider succeeds.
- Verify reschedule/cancel service failure stores `fallback_pending`, logs `pos_errors`, creates an owner notification, and leaves the existing appointment unchanged.
- Verify Square readiness blocks test booking until Square is connected, a location is selected, services/staff are synced, and at least one service/staff record is AI-bookable.
- Verify Square test booking creates a real booking through `booking.Service` with source `square_test_booking`.
- Verify cancel test booking cancels the latest Square test appointment through `booking.Service`.
- Verify AI booking enablement is blocked until the latest test booking is cancelled successfully.
- Verify the dashboard Integrations page handles loading, empty, error, success, disabled/gated, and mobile states for Square readiness.
- Verify the dashboard Appointments page distinguishes POS-confirmed appointments from `fallback_pending` requests that need owner review.
- Verify the dashboard Services and Staff pages let owners allow/block AI booking for active synced records without editing Square data.
- Verify inactive synced services/staff cannot be enabled for AI booking and remain unavailable in the dashboard.

## Milestone 4 Conversation Simulator

- Verify startup migrations create `call_sessions`, `call_transcript_messages`, and `handoff_requests`.
- Verify simulator sessions are owner-scoped by `salon_id`.
- Verify the conversation module does not import `modules/pos_square`.
- Verify simulator booking attempts call `booking.Service` with source `ai_conversation_simulator`.
- Verify simulator confirmed wording appears only after the booking service returns a confirmed attempt, POS booking ID, and appointment.
- Verify POS fallback from the booking service produces pending request wording and no confirmed appointment language.
- Verify disabled AI booking creates an owner handoff and does not call the booking service.
- Verify human requests, complaints, refunds, payment disputes, and group-booking language create owner handoffs.
- Verify the Calls dashboard handles loading, empty, error, success, disabled/gated, and mobile states.
- Verify simulator transcripts show customer, AI, and booking tool messages in sequence.

## Regression Guardrails

- Booking services must import `modules/pos`, not `modules/pos_square`.
- AI/conversation modules must not import Square packages.
- No API response may expose encrypted or raw POS tokens.
- Appointment confirmation, reschedule, and cancellation must remain impossible unless the active POS provider returns a successful booking response.
