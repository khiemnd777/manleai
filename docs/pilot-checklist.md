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

- [ ] Add signed/persisted OAuth state nonce.
- [ ] Confirm Square Appointments API payload details against a real sandbox account.
- [ ] Add integration tests with mocked Square HTTP responses.
- [ ] Add service/staff list endpoints for dashboard tables.

## Milestone 3 Next Steps

- Add booking_attempts and appointment tables.
- Implement customer search/create in SquareAdapter.
- Implement availability checks in SquareAdapter.
- Implement create/reschedule/cancel appointment in SquareAdapter.
- Add booking service that depends only on `pos.POSProvider`.
- Add booking test UI and AI booking enablement gate.
