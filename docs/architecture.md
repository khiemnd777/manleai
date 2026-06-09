# Architecture

## Current Milestone

This codebase implements Milestone 1 and Milestone 2 of the AI Receptionist system.

The backend is organized as:

```txt
cmd/api              Fiber HTTP server
cmd/worker           worker entrypoint for later async jobs
internal/config      environment config
internal/database    PostgreSQL connection bootstrap and startup migrations
internal/encryption  AES-GCM token encryption
internal/middleware  JWT middleware
modules/auth         login, refresh, roles
modules/salon        salon profile, settings, business hours
modules/pos          provider-neutral POS contracts and persistence
modules/pos_square   Square adapter and Square integration routes
```

The frontend is organized as:

```txt
app/                 Next.js routes
components/ui        reusable UI primitives
components/layout    dashboard shell
features/auth        login flow
features/dashboard   dashboard home
features/integrations Square integration page
features/onboarding salon profile creation
lib/api              typed API client
types                API response types
```

## Core Boundary

Correct dependency direction:

```txt
HTTP handler -> service -> repository/provider interface -> concrete adapter
```

The booking service planned for Milestone 3 must depend on `modules/pos.POSProvider`. It must not import `modules/pos_square`.

The AI conversation engine planned for Milestone 4 must call booking tools only. It must not know Square OAuth, Square API payloads, Square location IDs, or token storage.

## Data Ownership

SQL files under `backend/migrations` are the current database source of truth. The API startup migrator applies them once and records checksums in `app_schema_migrations`. Ent schema files mirror the table structure so generated clients can be introduced without changing the domain boundaries.

Tenant isolation is enforced by owner-scoped queries in repositories and by salon ownership checks before POS actions.
