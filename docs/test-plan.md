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

## Regression Guardrails

- Booking services must import `modules/pos`, not `modules/pos_square`.
- AI/conversation modules must not import Square packages.
- No API response may expose encrypted or raw POS tokens.
- Appointment confirmation must remain impossible until Square booking success is implemented in Milestone 3.
