# Deployment

This release deploys the Go API, background worker, PostgreSQL, Redis, the
owner admin dashboard, the public salon landing app, and the POS calendar app.
Public HTTP and HTTPS are owned by the shared VPS edge gateway, not by the
ManleAI Docker Compose project.

Scheduling load and concurrency verification is a separate non-production
release operation. It must not run in the deployment stack or against its
database. Provision an explicitly attested isolated database and follow
`docs/operations/scheduling-load-harness.md`; a passing harness report is not a
production capacity claim without an approved witnessed representative run.

## Production Domains

- Admin dashboard: `https://ai.knasoftware.com`
- API through the admin origin: `https://ai.knasoftware.com/api/*`
- Marketing website: `https://manle.knasoftware.com`
- Public salon pages: `https://salon.knasoftware.com/s/[slug]`
- POS calendar app: `https://pos.knasoftware.com`
- Square redirect URL: `https://ai.knasoftware.com/api/integrations/square/callback`
- Voice public base URL: `https://ai.knasoftware.com`

All DNS `A` records must point to the VPS before Caddy can issue certificates.
`manle.knasoftware.com` and `salon.knasoftware.com` currently share the
`landing` process, but host routing keeps marketing content off the salon host
and salon catalog pages off the marketing host. `ai.knasoftware.com` remains
the only API/admin origin.

### Future `manle.ai` cutover (runbook only)

The repository does not activate `manle.ai`. After the domain is purchased,
prepare `ops.manle.ai`, `salon.manle.ai`, and `pos.manle.ai` as a reviewed
release: create DNS records, add the exact hosts to the edge manifest/Caddy
render and CORS allowlist, update canonical/hreflang and public base URLs, then
verify certificate issuance and `/healthz` before traffic cutover. Refresh
cookies remain host-only and are intentionally not portable across the old and
new admin hosts, so operators and tenants must sign in again.

Before enabling provider callbacks, update the salon-scoped stored Platform
Technical configuration and provider consoles for the new exact public URLs.
Square OAuth redirect and Twilio voice/messaging callback URLs require explicit
review; no migration may infer provider settings from intake text or repository
environment files. Run smoke checks for marketing canonical/hreflang, public
salon pages, API CORS/CSP, Platform login, owner invitation acceptance, Square
OAuth state/callback, and Twilio signature-bound routes. Roll back by restoring
the prior edge route/base URLs and provider callbacks; do not roll back the
database migration or silently switch scheduling authority.

## Edge Gateway

The VPS has one shared systemd Caddy service. It owns ports `80` and `443`; its
managed root config imports `/etc/caddy/projects/*.caddy`. ManleAI does not run
its own Caddy container and must never edit `/etc/caddy/Caddyfile` directly.

`project-edgectl` is the only supported project route manager. It stores each
project route under `/etc/caddy/projects/<project-id>.caddy`, records ownership
in `/var/lib/project-edge/registry/<project-id>.json`, validates the combined
Caddy config, rejects domain or upstream conflicts, backs up the project route,
and reloads Caddy with rollback on failure.

ManleAI exposes:

```txt
127.0.0.1:18089 -> api:8080
127.0.0.1:13088 -> frontend:3000
127.0.0.1:13090 -> landing:3000
127.0.0.1:13091 -> pos-calendar:3000
```

For `/api/*`, the rendered Caddy route overwrites
`X-ManleAI-Client-IP` with `{remote_host}` before proxying. The API uses only
that edge-owned header (or the direct socket IP outside Caddy), HMACs the value,
and never accepts a caller-selected rate-limit identity.

The CI/CD deploy job renders `deploy/manleai.caddy.template` and
`deploy/manleai.edge-manifest.template`, then runs:

```bash
sudo -n project-edgectl validate manleai <route-file> <manifest-file>
sudo -n project-edgectl upsert manleai <route-file> <manifest-file>
```

`upsert` runs only after the ManleAI API loopback healthcheck succeeds. It can
change only the ManleAI route and manifest; existing project route files remain
owned by their registered projects.

## GitHub Actions

The workflow in `.github/workflows/ci-cd.yml` runs only when a release tag
matching `v*` is pushed. Commit pushes to `main` do not trigger this workflow.
The normal release command is:

```bash
make release TAG=v2026.06.25.1
```

That command requires a clean `main` worktree, creates an annotated git tag, and
pushes the tag to `origin`. The tag push runs the complete backend test/vet
contract, a bounded high-risk race suite, all three web-app typecheck/build
jobs, and the isolated PostgreSQL Owner-first release gate documented in
`docs/operations/release-gate.md`. The PostgreSQL gate starts from an empty
dedicated database, proves migrate-twice/checksum behavior, verifies every V46
through latest migration ledger entry, runs the declared integration packages
serially in isolated per-package database clones, and executes the explicit
tenant/security contract. Image publication and deployment require all of those
jobs to pass and remain tag-only.

The workflow then builds and publishes the five application images to GitHub
Container Registry (GHCR) with that immutable release tag. The VPS receives
the Compose/Caddy deployment bundle, `project.env`, image tag metadata, and—
only for the protected `sample_test` profile—a temporary mode-`600` sample
credential file. It logs into GHCR with the job's ephemeral `GITHUB_TOKEN`,
pulls the tagged images, logs out, and starts the stack without compiling
application source code. Each image carries the `org.opencontainers.image.source`
label for this repository, so the GHCR package inherits repository access
permissions.

A passing release gate is code-readiness evidence only. It does not prove live
salon-scoped provider configuration, credentials, callback reachability,
provider delivery, production backup capacity/retention, a witnessed restore,
alert routing, or on-call readiness. Those remain separate dashboard-backed
runtime checks and operational approvals.

Every deploy SSH/SCP connection has a 15-second connection timeout and sends
keepalives during long-running work. Before replacing containers, the release
requires an exact tag-specific declaration that the previous image is
compatible with the candidate forward schema, takes and validates a private
pre-deploy PostgreSQL backup, and tags the currently running application images
locally. If the API healthcheck or edge-gateway validation/reload fails, it may
restore those images only under that compatibility declaration and keeps the
previous `/opt/manleai/current` release active. Image rollback does not restore
the database; PostgreSQL remains at its forward schema. Only after both checks
succeed does the workflow move `/opt/manleai/current` to the new release:

```bash
docker login ghcr.io
for image in manleai-api manleai-worker manleai-frontend manleai-landing manleai-pos-calendar; do
  docker pull "ghcr.io/khiemnd777/$image:<release-tag>"
done
docker logout ghcr.io
docker compose \
  --env-file /opt/manleai/project.env \
  --env-file /opt/manleai/releases/<release>/images.env \
  -f docker-compose.prod.yml -p manleai up -d --no-build --remove-orphans
sudo -n project-edgectl validate manleai <route-file> <manifest-file>
sudo -n project-edgectl upsert manleai <route-file> <manifest-file>
```

The workflow uses an SSH private key for the deploy identity. The GHCR token is
written to a mode-`600` temporary file on the VPS only for `docker login`, then
removed on normal success and failure paths; diagnostics also attempt removal.
It is never added to `project.env`, committed to the repository, passed as a
remote command argument, or left in Docker's credential store after
`docker logout`. The workflow uses the repository-scoped `GITHUB_TOKEN`; no
additional long-lived GHCR credential is required.

The VPS pulls release images one at a time to bound memory, CPU, network, and
disk-extraction pressure. It does not prune shared Docker build cache. Docker
Compose starts containers only after every required image is present locally.
During the first GHCR migration, an old running container can refer to image
metadata that Docker no longer retains. The workflow reports that case and
proceeds without automatic image rollback; after the first successful GHCR
release, every running application image is tag-addressable and rollback is
available again.

If the remote deploy command fails or its SSH session disconnects, the workflow
opens a short diagnostic SSH session and reports memory, deploy-path disk
usage, Docker disk usage, Compose status, and bounded application logs. It does
not print `project.env` or any deployment secret. If that follow-up connection
also fails, the original deploy failure remains the authoritative result.

Required GitHub Secrets:

```txt
SERVER_IP
REMOTE_USER
SSH_PRIVATE_KEY
PROJECT_ENV_B64
```

The current pre-live `sample_test` profile additionally requires:

```txt
SAMPLE_DATA_ENV_B64
```

Use the VPS host and dedicated deploy identity for `SERVER_IP` and
`REMOTE_USER`. `SSH_PRIVATE_KEY` must be the matching private key. Do not
commit the private key or production environment file.

Required variables in the protected `production` GitHub environment:

```txt
MIGRATION_COMPATIBILITY_RELEASE_TAG
PREVIOUS_IMAGE_DB_COMPATIBLE
MIGRATION_COMPATIBILITY_APPROVER
POSTGRES_BACKUP_STORAGE_APPROVAL
DEPLOY_DATA_PROFILE
```

`DEPLOY_DATA_PROFILE` must be exactly `sample_test` for the current pre-live
deployment or `live` after production-live cutover. When an incompatible
pre-live migration ledger or non-sample database must be replaced, set the
additional protected variable `SAMPLE_TEST_RESET_RELEASE_TAG` to the exact
candidate tag. The workflow backs up the current database, removes only the
Compose-owned `manleai_postgres_data` volume, records the reset, migrates from
empty, and provisions the fixture. Clear that reset variable after the
successful release. A mismatched or missing reset tag fails before deletion.
The `live` profile ignores sample credentials and always refuses automatic
database reset.

The compatibility tag, decision, and approver are release-specific. The release
tag must match exactly, compatibility must be the literal `true`, and the
approver must be a bounded reviewer identity. A prior compatibility declaration
does not authorize a later tag. Storage approval must be the exact value
`encrypted-private:/opt/manleai/backups` and remains valid only while the path,
access controls, encryption, retention, and key ownership remain approved. The
deploy fails before database mutation when the declaration or storage approval
is missing, stale, malformed, or false. The storage value is an operator
attestation; it does not encrypt the path.

## Database Backup, Restore Drill, And Rollback

The release bundle includes `backend/migrations` for checksum validation plus
the bounded database scripts in `deploy/`:

- `postgres-backup.sh` creates a custom-format `pg_dump`, checks its catalog
  with `pg_restore --list`, and writes an SHA-256 sidecar;
- `postgres-restore-drill.sh` restores only into one new explicit
  `manleai_restore_drill_...` database and never drops or overwrites a target;
- `postgres-verify-restore.sh` checks exact `app_schema_migrations` file/name/
  checksum parity, required tables/constraints, and bounded tenant smoke
  queries.

Before a non-initial deployment starts the candidate API/migrator, the workflow
backs up the explicit `POSTGRES_DB` on the existing Compose PostgreSQL service
to `/opt/manleai/backups/predeploy-<release-id>.dump`. The artifact, checksum,
compatibility declaration, and backup record use private permissions and must
live on an approved encrypted volume. If an existing container is stopped, or
the project volume exists without a Compose container, the source is ambiguous
and deployment stops. Only a truly empty initial deployment with no database
container and no project volume may record backup as not applicable.

Database restore is never part of automatic deploy failure handling. Use the
manual `.github/workflows/postgres-restore-drill.yml` only with its protected
non-production environment and dedicated `DRILL_SERVER_IP`,
`DRILL_REMOTE_USER`, and `DRILL_SSH_PRIVATE_KEY` secrets. It takes explicit
source/target names and RPO/RTO targets, leaves the private dump and isolated
target on the non-production host, and uploads only a sanitized drill report.
Never configure those secrets to the production VPS.

Complete procedures and evidence requirements are binding in:

- [PostgreSQL Backup And Isolated Restore Drills](operations/postgres-backup-restore.md)
- [Migration Release And Rollback](operations/migration-rollback.md)

Migrations are forward-only expand/contract changes. The previous image must be
proven compatible with the expanded schema before automated image rollback is
available. A database recovery first restores and validates a selected artifact
in an isolated database, then requires a separate DBA-approved production
recovery/cutover plan that accounts for writes after the snapshot and provider-
side reconciliation. No repository script automatically deletes or replaces a
production database.

## Project Env

Create the production env file from the template:

```bash
cp deploy/project.env.example project.env
```

Generate replacement values for the required placeholders:

```bash
openssl rand -hex 24
openssl rand -base64 48
openssl rand -base64 32
```

Use separate URL-safe random values for `POSTGRES_PASSWORD` and
`DATABASE_RUNTIME_PASSWORD`, the 48-byte base64 value for `JWT_SECRET`, and the
32-byte base64 value for `TOKEN_ENCRYPTION_KEY_BASE64`. `DATABASE_URL` must use
the non-owner `DATABASE_RUNTIME_ROLE`; `MIGRATION_DATABASE_URL` must use the
migration/table owner. Never make the runtime role a member of the migration
role and never grant it `SUPERUSER` or `BYPASSRLS`.

On a new PostgreSQL volume, `deploy/postgres-init-runtime-role.sh` creates the
runtime login with `NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS`.
For an existing volume, create or rotate that role once through an approved DBA
change before deploying this release. The exact statement is equivalent to:

```sql
CREATE ROLE manleai_runtime LOGIN PASSWORD '<separate-secret>'
  NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
```

Do not put the password in shell history. The candidate API uses the migration
connection to apply all pending release migrations through V80 and grant
table/sequence/function privileges to the
already-existing runtime role, then closes that connection. API requests and
the worker use only the runtime connection. The worker does not receive
`MIGRATION_DATABASE_URL`; production startup fails if the connected role name,
ownership, `SUPERUSER`, `BYPASSRLS`, or RLS policy checks are unsafe.

### V78-V80 system-tenant context rollout

`V78__system_tenant_context_expand.sql` is intentionally the expand release of
a two-release database change. It adds `app.system_salon_id` support and narrow
provider salon-ID locator functions without changing the existing provider or
worker base RLS branches. This keeps migration-first deployment and image
rollback compatible. Deploy the V78-aware API/worker image and verify provider
callbacks plus worker item processing before scheduling the later contract
migration. Do not add that contract migration to the same release: startup
migrations run before the new image can prove every runtime path supplies the
new context.

`V79__system_tenant_contract_preparation.sql` is the next additive release. It
moves remaining global worker claim/recovery/cleanup/retention discovery behind
bounded worker-only database functions, binds each selected item to its salon
before ordinary repository work, and installs validated composite
salon/session constraints for call children after a fail-closed preflight. It
still does not tighten provider/worker base RLS, so it must not be described as
the contract release. Deploy and observe V79 worker/provider paths first; only a
later separately reviewed migration may require matching
`app.system_salon_id` in base RLS.

`V80__strict_system_tenant_rls_contract.sql` is that separate contract
migration. Do not allow the candidate API to apply V80 while any pre-V79 API or
worker replica can still use the database. Before migration, record the exact
V79-aware image revision, prove all old replicas are drained, and record
successful observation of Twilio inbound/stream/callback, Square OAuth/webhook,
POS sync, booking lease recovery, Square repair, owner/customer notification,
call expiry, and scheduling-retention paths. V80 requires exact
`app.system_salon_id` matching for provider/worker base-table policies,
including the direct Calls/Support policies introduced by V75-V76, while the
bounded V78-V79 locator/discovery functions remain the only unbound paths.

After V80 applies, run runtime-role negative checks for unbound and cross-tenant
base-table reads/writes, exercise one provider callback and one claimed item per
worker class, and inspect operations-health for failures. Image rollback is
limited to a V79-aware image that carries system tenant context and uses V79
discovery functions. A pre-V79 image is database-incompatible after V80; never
restore the production database merely to remove the contract migration.

Before deploying the strict voice resolver in this image, inspect the Platform
technical integration-config API or persisted configuration records and confirm
that every salon expected to receive live calls has enabled, complete Twilio and
OpenAI rows. A process environment value is not a substitute and must not be
used as readiness evidence. A missing record now fails that salon's provider
runtime closed.

Production forces rate limiting on (the template records
`RATE_LIMIT_ENABLED=true`), uses `REDIS_URL` as a required request-protection
dependency, and keeps
`RATE_LIMIT_CLIENT_IP_HEADER=X-ManleAI-Client-IP` aligned with the Caddy route.
API startup fails if the configured Redis endpoint cannot be reached, runtime
Redis failures return generic `503 RATE_LIMIT_UNAVAILABLE`, and `/healthz`
fails until Redis recovers. Local Compose leaves the limiter off by default;
set `RATE_LIMIT_ENABLED=true` to exercise it against the local Redis service.

The dashboard and POS apps use credentialed requests with an HttpOnly refresh
cookie and memory-only access tokens. Production CORS therefore allows
credentials only for the configured exact origins. The three web apps install
a per-request nonce CSP and force dynamic rendering so Next.js scripts and
styles receive the matching nonce. Production policy has no wildcard,
`unsafe-inline`, or `unsafe-eval`; validate the emitted
`Content-Security-Policy` header after deployment rather than inferring it from
the template alone.

After editing `project.env`, store it as a single-line base64 GitHub Secret:

```bash
gh secret set PROJECT_ENV_B64 --body "$(base64 -i project.env | tr -d '\n')"
```

Or paste the output of this command into the GitHub Secrets UI:

```bash
base64 -i project.env | tr -d '\n'
```

`project.env` is ignored by git.

For the pre-live profile, copy `deploy/sample-data.env.example` to the ignored
`deploy/sample-data.env`, replace every placeholder, and store it separately:

```bash
gh secret set SAMPLE_DATA_ENV_B64 --env production \
  --body "$(base64 -i deploy/sample-data.env | tr -d '\n')"
gh variable set DEPLOY_DATA_PROFILE --env production --body sample_test
```

Only for the first release that is authorized to replace an incompatible
pre-live database, also set the exact candidate tag, then remove the variable
after that release succeeds:

```bash
gh variable set SAMPLE_TEST_RESET_RELEASE_TAG --env production --body '<release-tag>'
gh variable delete SAMPLE_TEST_RESET_RELEASE_TAG --env production
```

The sample credential file is never added to `project.env`. CI validates it,
uploads it only for a `sample_test` deployment, supplies passwords to the
one-off fixture container through process environment, and removes the remote
file on both success and diagnostic failure paths.

## VPS Requirements

- Docker Engine with Docker Compose v2.
- Ports `80` and `443` open to the internet.
- DNS for `ai.knasoftware.com`, `manle.knasoftware.com`, `salon.knasoftware.com`, and
  `pos.knasoftware.com` pointing to the VPS before certificate issuance.
- Systemd Caddy active with managed root `/etc/caddy/Caddyfile` and
  `project-edgectl` installed.
- A dedicated deploy identity that can access Docker, owns `/opt/manleai`, and
  has passwordless sudo only for `/usr/local/bin/project-edgectl`.
- SSH public-key access for that deploy identity; the matching private key is
  stored only in the `SSH_PRIVATE_KEY` GitHub secret.

Existing project Caddy routes are not modified by this deployment. Do not
install another Caddy container, restart Caddy manually, or bind application
containers to ports `80` or `443`.

## First Platform Administrator Bootstrap

V64/V74 intentionally do not add an unauthenticated Platform-admin HTTP
bootstrap. After the migrations are applied, an operator creates one dedicated
Platform identity only while no active Platform Admin exists. Never reuse a
Tenant owner, manager, or staff login. Put the initial password in a regular
owner-readable-only file (`chmod 600`) and do not pass it on the command line:

```bash
cd backend
go run ./cmd/platform-access bootstrap-admin \
  --email operator@example.com \
  --full-name "Platform Administrator" \
  --password-file /run/secrets/manleai-platform-admin-password \
  --action-key initial-platform-admin-2026-07-26 \
  --reason approved-change-reference
```

The command prefers the migration-owner `MIGRATION_DATABASE_URL` (and uses
`DATABASE_URL` only in non-RLS legacy/local setups), takes an advisory
lock, creates an immutable `principal_scope=platform` identity and assignment plus
immutable action/event evidence atomically, and prints only the bounded
assignment result. Exact action replay returns the same result. Changed action
reuse conflicts, and the command closes permanently as soon as any active
Platform Admin exists. All later Platform role, tenant membership, salon
assignment, capability, and PII-grant changes use the authenticated
`/api/platform/access/*` APIs. Do not use direct SQL or the legacy
`super_admin` role as an authorization substitute.

If the email already belongs to a Tenant identity, the command returns a
not-found outcome and never converts it. A person needing access to both realms
must have two distinct login emails/identities.

`--reason` is an opaque operator change reference, not a note. It must match
`[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}` and must not contain customer data.

## Opt-In Sample Test Data

V73 adds only the `live|sample_test` classification contract to `users` and
`salons`; the normal API startup migration chain never inserts a user, salon,
or fixture. Sample data is a separate, explicit profile owned by
`backend/sampledata`. It is allowed on local and on the current pre-live
production deployment, but it refuses any database that already contains a
`live` user or salon.

V74 separately classifies the two sample operations identities as `platform`
and the Lotus owner as `tenant`. Sample/live classification never grants a
principal realm.

The fixture creates exactly three `sample_test` accounts: one Platform Admin,
one Platform Ops, and the fixed Lotus tenant owner
`owner@lotusnails.example`. The Admin and Ops email/name values are supplied at
execution time. The Ops account receives all seven non-PII capabilities for
the one sample salon and receives no PII grant. The salon is `Lotus Nails
Studio`, with seven canonical services, four technicians, full staff/service
eligibility, local Monday-Saturday hours, `owner_manual` pending-review
scheduling, and all provider, AI, public-catalog, recording, and SMS execution
disabled.

Passwords are required through process environment variables, must contain at
least 12 characters, and are never command arguments, migration SQL, or
repository defaults. Local setup is fully orchestrated by the root command:

```bash
make restart
```

That command creates missing private local config and credentials, builds the
stack, runs migration checksum preflight, starts the API, applies the fixture,
and waits for health. It stores logins in `.local/sample-data.env`. Repeating
the command preserves compatible PostgreSQL data and is checksum-validated
through `sample_data_migrations`.

For the current pre-live production stack, CI/CD performs the same guarded
fixture invocation after API migrations and before edge activation. A
compatible sample deployment replays without duplicates. An incompatible
ledger, legacy live rows, or partial sample fixture can be reset only after the
exact tag is approved through `SAMPLE_TEST_RESET_RELEASE_TAG` and after the
pre-deploy backup succeeds. No additional VPS command is required.

Before production-live cutover, replace/reset the pre-live database through the
approved database recovery procedure, set `DEPLOY_DATA_PROFILE=live`, clear
`SAMPLE_TEST_RESET_RELEASE_TAG`, remove `SAMPLE_DATA_ENV_B64`, and deploy only
the normal migration chain. The live post-migration guard requires zero
`sample_test` users/salons and no `sample_data_migrations` table before edge
activation. At that point `/api/auth/bootstrap/status` remains available
because normal migrations created no account. Create the first owner through
normal bootstrap, then use the documented first-Platform-Admin bootstrap
against the approved existing account. CI/CD never invokes `/bin/sample-data`
under `live`.

## Dashboard-Managed Provider Configuration

Square Appointments, Twilio, and OpenAI credentials are configured in the
Platform tenant detail's Technical tab and stored encrypted per salon in
`salon_integration_configs`. Repository env files and env templates contain
infrastructure settings only; do not add provider credentials, provider model
settings, webhook paths, transports, or provider URLs to them.

For active-runtime diagnosis, use `/platform/tenants/:tenant_id/technical`,
`GET /api/platform/tenants/:tenant_id/technical/integration-configs`, the relevant readiness/debug endpoint
such as `GET /api/salons/:id/voice/status`, persisted provider records, and the
runtime resolver code. Never use `project.env`, `.env`, Compose defaults,
GitHub secrets, or process environment values as evidence of the active
salon-scoped provider configuration.

Square, Twilio, and OpenAI resolvers are database-only and fail closed when the
exact salon record is missing. Repository failures, malformed/non-string
persisted settings, and stored-secret decryption failures propagate and stop
provider runtime. A stored row owns enabled state, settings, and credentials:
missing credentials never inherit environment secrets, and a disabled provider
cannot be re-enabled by process configuration.

OpenAI runtime uses only the fixed `openai_public` destination profile. Raw
tenant Base URL editing is not supported. V84 adds the purpose-separated
credential HMAC identity, credential revision, destination profile, async
verification tables, immutable verification events, exact-tenant RLS, and the
bounded worker claim function. Existing OpenAI rows remain unavailable to the
strict runtime until an operator saves them through Platform Technical, which
canonicalizes the destination and establishes the credential identity/revision.
If two tenants currently share one plaintext key, the second save returns
`OPENAI_CREDENTIAL_TENANT_CONFLICT`; rotate one tenant key rather than moving or
copying identity data.

Roll out and roll back V84 using
`docs/operations/openai-tenant-runtime.md`. CI and release self-tests use fake
OpenAI transports only; live verification is an explicit Platform operation
after deployment and may incur provider usage.

The authenticated integration response follows the same ownership boundary.
Missing Square/Twilio/OpenAI rows return unconfigured, database-owned empty
state and never surface environment-derived URLs, paths, models, enabled state,
or secret-source metadata. When a stored row exists but
its secret is empty or unreadable, the response reports source `none` and
configured `false`; it never returns the secret value.
Whole-response contract tests also prohibit write-only credential, SID,
destination, and `clear_*` request fields from appearing in serialized reads.

### Twilio Voice tenant-bound expand and migration

V83 is an additive expand migration. Before applying it, inspect stored Twilio
rows without reading or printing encrypted secrets:

```sql
SELECT salon_id, id AS route_id,
       settings->>'voice_inbound_number' AS voice_inbound_number,
       settings->>'voice_routing_enabled' AS voice_routing_enabled
FROM salon_integration_configs
WHERE provider = 'twilio'
ORDER BY salon_id;

SELECT settings->>'voice_inbound_number' AS voice_inbound_number,
       count(*) AS active_routes
FROM salon_integration_configs
WHERE provider = 'twilio'
  AND enabled = true
  AND settings->>'voice_routing_enabled' = 'true'
GROUP BY settings->>'voice_inbound_number'
HAVING count(*) > 1;
```

The second query must return zero rows. Invalid non-empty numbers must be fixed
to canonical E.164 before V83; never copy `salons.phone` automatically and
never move a number between tenants implicitly. V83 adds the active-number
unique index, the provider-only route locator, and bounded live-verification
indexes. Provider-wide route and legacy `CallSid` discovery is available only
before tenant binding; once bound, callback lookup is restricted to that exact
tenant and cannot rebind through a locator. V83 does not remove shared routes.

For each tenant during operational migration:

1. In Platform tenant Technical > Twilio, store the exact inbound E.164 number,
   Account SID, Auth Token, host-only public HTTPS base, and enable tenant-bound
   routing.
2. Copy that tenant's computed incoming URL to the matching Twilio number.
3. Place a real call and confirm the resulting `call_sessions.salon_id` equals
   the intended tenant.
4. Confirm `GET /api/platform/tenants/:tenant_id/technical/voice-routing-status`
   reports `routing_configured=true`, `live_verified=true`, and a matching
   `last_verified_inbound_at` for the current fingerprint.
5. Record operator evidence before moving to the next number. `Routing
   configured` alone is not completion evidence.

If a tenant call fails during expand, restore that number's prior shared URL;
do not change route IDs, transfer numbers, disable another tenant, or copy
credentials. The additive schema can remain in place. A contract release may
unmount shared routes and remove phone fallback only after every production
tenant has current live evidence and the incident rollback window is closed.
Until then, release notes must describe tenant-bound routing as opt-in expanded
behavior, not a fleet-wide guarantee.

The owner-notification Twilio Messaging resolver is an explicit exception to
that legacy behavior: it resolves only the salon's stored encrypted record and
fails closed on missing, disabled, invalid, repository, or decryption state. It
never reads provider configuration from environment variables. Its dashboard
contract includes explicit owner-SMS enablement, the exact E.164 owner
destination and fresh consent attestation, Account SID/Auth Token, one
Messaging Service SID or sender number, and public HTTPS status/inbound callback
URLs. The response exposes only configured flags, a masked destination, and the
computed callback URLs.

Public buffered-TTS playback uses a separate narrow exception:
`ResolveStoredTwilioAuthToken` reads only the salon's encrypted Twilio auth
token from `salon_integration_configs`. It never uses legacy environment
fallback and does not require Account SID, Messaging Service, sender,
owner-SMS enablement, or owner-SMS consent. Voice-only salons can therefore
sign `/api/voice/audio/:id` capabilities without enabling messaging. If the
stored token is missing or cannot be decrypted, the backend emits no unsigned
audio URL and the phone response falls back to safe TwiML speech. Token rotation
immediately invalidates any previously generated audio capabilities.

Square booking-webhook verification is also salon-scoped dashboard data. Store
the exact public HTTPS notification URL and write-only signature key in the
Platform tenant Technical tab; do not add either value to repository env templates.
`webhook_configured` means verifier credentials are present, not that the
Square subscription or deliveries are healthy. Configuration transfer
preserves the destination deployment URL and requires secret re-entry. The API
process receives and durably enqueues events; the worker processes fenced
claims and runs scheduled calendar repair. Migrations V39-V42 add booking
integrity/reconciliation/quotes, fail-closed consultation defaults, Square
webhook/repair persistence, and conversation state revision. They remain under
the existing startup migrator and must not be run manually during source review.
V60 adds bounded dead-letter/requeue state, action-key-idempotent owner actions,
safe diagnostic class/code fields, and calendar-repair health evidence. The
connected Square card reads that state through the authenticated
`/api/salons/:id/square-webhook-events` operations API. It never receives raw
payloads, signatures, claim tokens, provider identifiers/responses, customer
data, or raw errors. A requeue is allowed only when the backend returns
`can_requeue=true`; after an uncertain browser response the dashboard repeats
the same action key and uses `X-Idempotent-Replay` only to label exact recovery.
For a separately hosted dashboard/API deployment, CORS must expose that response
header. Header absence must not be interpreted as replay or as failure.
V63 removes historical provider-controlled POS/Square diagnostic text and
payloads and narrows legacy Twilio/OpenAI Realtime failure audit payloads to the
same fixed-message/allowlisted contract enforced by the current runtime.
V64 is an expand-only SaaS access-control migration. It retains
`salons.owner_user_id`, legacy roles, and every previous owner query while
adding owner-membership backfill/synchronization, Platform role and per-salon
delegation tables, maximum-24-hour Platform PII grants, and immutable access
actions/events. The previous image remains schema-compatible, but the V64
backfill and access integration tests must pass. V65-V72 complete the shared
Business contract, Platform technical plane, runtime membership boundary, RLS,
tenant quotas/fairness, audited AI-runtime control, public safe projection, and
exact Platform PII-scope enforcement. All V63-V72 entries must appear exactly
once in `app_schema_migrations` before the candidate is healthy. V73 adds
fixture-free data classification; V74 adds immutable Tenant/Platform principal
isolation and must also appear exactly once.
Migration V43 adds the monotonic location-scoped provider snapshot generation
used to reject stale and out-of-order full imports. On first deployment it
preserves Square credentials and terminal connection states, but changes every
previously runnable Square connection to `connected`, clears `last_sync_at`,
and requires one successful full sync for the selected location before catalog,
availability, retry, or booking paths become ready again. Do not bypass that
resync by editing connection status or timestamps. Every later full snapshot
also clears `last_sync_at` when it begins; an error completion keeps it empty,
and only an `active` successful completion restores the timestamp and synced
readiness.

The configured OpenAI reply model may serve guarded style-only rewriting,
strict structured interpretation for freeform semantic-lane turns, and phrasing
of one backend-selected consultation question. Turn input is limited to a
PII-reduced caller utterance, boolean customer-field presence,
selected/catalog service and staff references, ready consultation profiles and
revisions, party-group references, pending act, booking action, dialog phase,
and draft revision. The backend rejects low-confidence goals, invalid
entity/operation combinations, malformed counts, and non-catalog IDs. The model
cannot mutate the draft, rank/recommend services, call booking, or produce
confirmed appointment state. When the provider is disabled or unavailable,
ambiguous mutations preserve the current draft and enter safe
clarification/handoff behavior.

After saving salon-scoped OpenAI settings, queue the full durable verification
from Platform Technical. The legacy owner semantic-only diagnostic remains:

```txt
POST /api/salons/:id/voice/semantic-check
```

This owner-authenticated probe uses the same configured reply model and strict
structured-turn schema as live interpretation, but contains no caller
transcript and creates no conversation/POS state. `verified=true` proves the
provider accepted the current contract. A nonretryable live 4xx contract
rejection opens a local circuit scoped to salon plus provider/model/key/schema
fingerprint so repeated caller turns do not resend the same invalid request;
changing that configuration resets the scope, and a successful semantic check
closes the matching circuit. Operational output is limited to request ID,
status class, safe provider error type/code/parameter, schema fingerprint, and
circuit state; response bodies and provider messages are never logged or
returned.

## Consultation Data Pack After Deployment

Schema migration V38 creates the consultation profile table, but schema
migrations do not contain tenant-specific salon data. To reproduce the investor
demo catalog configuration without manual re-entry:

1. Deploy the release and connect the target salon to Square Appointments.
2. Sync Square until the seven target services exist with matching names and
   durations and are active-provider, POS-linked, synced, and AI-bookable.
3. In `/platform/tenants/<tenant_id>/transfer`, choose the canonical
   `docs/lotus-investor-demo-consultation-pack-v8.json`. The original
   `docs/lotus-investor-demo-consultation-pack-v7.json` is also accepted as a
   scoped legacy content pack and is reviewed as canonical v8.
4. Confirm the file automatically selects only categories, service aliases,
   and consultation profiles. Preview must report 5 service categories and 20
   category aliases;
   Preview reports 25 taxonomy creates, 7 service alias creates, and 7 service
   consultation profile creates with zero conflicts, then Apply once.
5. Verify profile coverage in Services and verify the independently managed
   consultation-enabled setting before the production call test. The scoped
   pack does not change that runtime setting.

The v8 pack and its safely adapted v7 source do not overwrite salon profile,
integration configuration, AI runtime settings, secrets, services, or POS
mappings. Missing, ambiguous, or ineligible profile targets block Apply.
Reapplying the same payload is idempotent and does not create duplicate
profiles or increment unchanged profile revisions.

## Production Rules

### V86/V87 Square buyer-write rollout and rollback

V86/V87 deployment does not itself authorize Square auto-confirmation. Use the
following reviewed sequence only after explicit production-deploy and provider-
write authorization:

1. Pass the Owner-first release gate at the exact revision.
2. Complete backup and forward-migration preflight.
3. Deploy V86/V87 schema and compatible API/worker/UI images with Square AI
   runtime still disabled.
4. Smoke `owner_manual` request-only and `manleai_calendar` atomic behavior.
5. Reconnect exactly one pilot tenant with buyer-level
   `APPOINTMENTS_WRITE` and without `APPOINTMENTS_ALL_WRITE`; never silently
   downscope an existing seller-write connection.
6. Re-evaluate the exact tenant's persisted capability evidence from Platform
   tenant Technical.
7. Enable buyer-write, concrete-staff single create only.
8. Keep Square reschedule, external party, and resource-capacity execution
   request-only/fail-closed.
9. Monitor `SLOT_COMMIT_CONFLICT`, pre-dispatch claim age,
   `dispatched_unknown`, reconciliation age/backlog, DB pool wait, and claim
   latency through the approved observation window.
10. Expand in reviewed tenant batches only after the pilot evidence remains
    healthy.

Rollback begins by disabling the salon AI runtime. Never mass-release active
claims: `dispatched_unknown` remains owned by reconciliation until exact
authoritative non-creation evidence permits release. Roll back the application
image only when the previous image is forward-compatible with V86/V87; do not
drop or reverse either migration. Scheduling-authority changes use the explicit
reviewed switch, and historical operations continue through their originating
authority. A local or unwitnessed harness pass is implementation evidence, not
production capacity proof.

### V88 strict Square tenant-binding rollout

V88 is the expand/runtime half of a two-release contract. Before migration,
run the normalized duplicate preflight for complete
`(provider, merchant_id, location_id)` identities and stop if any identity is
owned by more than one salon. Deploy V88 together with the API, worker, and UI
that require explicit Square tenant context, database-only config resolution,
blank-provider fail-closed behavior, reconnect invalidation, and the reviewed
initial activation action. Verify two distinct tenants through OAuth URL
resolution, location selection, sync, status, and a zero-network blank-tenant
failure. Do not change the `salons.active_pos_provider` database default in this
release.

Release B may remove that default only after every older API/worker replica is
drained, callback/worker traffic is observed on the Release A contract, and new
tenant provisioning has an approved explicit provider-selection workflow. V88
is forward-only; rollback uses the prior compatible application image and does
not drop the identity index or audit/version evidence.

- Do not invoke `/bin/sample-data` on a production-live database. Normal
  migrations never apply its embedded fixture; CI/CD enforces the exact `live`
  profile and fails if sample rows or the sample fixture ledger remain.
- Do not log Square access or refresh tokens.
- Keep `AUTO_MIGRATE=true` for the API unless another release process applies
  the same SQL migrations. The worker runs with `AUTO_MIGRATE=false` and starts
  only after the API healthcheck succeeds.
- V39 intentionally aborts when one request fingerprint has multiple historical
  attempts whose provider dispatch cannot be proven not started. Reconcile
  those POS outcomes before retrying the migration; do not force the unique
  fingerprint index by deleting or auto-closing unknown attempts.
- Configure the Square redirect URL in the Platform tenant Technical tab to the deployed API callback.
- Configure the exact Square booking-webhook HTTPS notification URL and signature key in the Platform tenant Technical tab, then verify subscription/delivery health separately in Square; the local configured badge is not a delivery-health check.
- Monitor the connected Square card's webhook backlog, failed/dead-letter
  counts, recent success window, and calendar-repair state. Treat
  `processing`, repeated failures, dead letters, a degraded repair backstop, or
  stale worker evidence as incident inputs. Requeue only records with backend
  `can_requeue=true`; an `ignored` event is read-only, and webhook processing is
  never direct appointment-confirmation evidence.
- Run the worker for POS sync, booking lease expiry, bounded availability-quote cleanup, Square webhook processing, scheduled calendar repair, owner/customer-notification delivery, call retention, and V61 scheduling-PII retention. The generic scheduler persists start/heartbeat/finish evidence in V57, uses a distributed job lease across worker replicas, and stores only safe class/code diagnostics. Each job still has an independent synchronous recurring loop, so a slow webhook/repair batch cannot starve lease recovery. Quote cleanup runs every five minutes, drains at most eight batches of 250 quotes per run, keeps a 24-hour post-expiry grace for unconsumed quotes and a 30-day audit window for orphaned consumed quotes, and preserves every quote still linked to a booking attempt. `scheduling_pii_retention` runs every five minutes with a default maximum of 100 retention work items, uses bounded `SKIP LOCKED` claims and one-row transactions, and makes no provider calls. Its baseline 90-day expiry applies only after terminal business and delivery state; pending/contacted requests, live leases, queued/retrying/unknown outcomes, open reconciliation, and active customer consent/STOP routing keys remain intact. See `operations/operations-health.md`; deployed code is not configured proactive monitoring until the worker is running and an external monitor/on-call process evaluates the owner status endpoint.
- Treat the V61 90-day duration as a technical baseline, not legal approval. Before production rollout, obtain the applicable privacy/legal decision for scheduling, notification, audio, backup, litigation-hold, and deletion/DSAR obligations; document any approved policy-version change and keep the worker batch/cadence capacity-monitored.
- Configure the Platform tenant Technical Twilio public base URL to the deployed API origin used in Twilio webhook settings.
- Keep the Platform-managed Twilio auth token present for signed buffered-TTS playback.
  Audio URLs are database-expiry-bounded HMAC capabilities; do not rewrite,
  log, or persist their query strings. Rotating the token revokes outstanding
  playback URLs and new phone turns generate capabilities with the new token.
- For owner-operational SMS, configure explicit owner consent/destination,
  Account SID/Auth Token, Messaging Service SID or sender, and the displayed
  status/inbound callback URLs in the Platform tenant Technical tab. Preserve the
  exact public URL/form parameters for `X-Twilio-Signature` verification,
  configure Messaging Service Advanced Opt-Out, and monitor the
  `notification_delivery` job/backlog/dead-letter evidence. `queued`,
  `provider_accepted`, and `sent` are not delivery proof. Follow
  `operations/owner-notification-delivery.md` for retry and unknown-outcome
  handling.
- For customer appointment SMS, configure the same salon-scoped Twilio
  transport in `/platform/tenants/:tenant_id/technical`, then separately enable the
  default-off customer policy and quiet hours in `/dashboard/settings`.
  Configure Twilio Messaging Advanced Opt-Out and the displayed salon-scoped
  inbound URL. The inbound/status callbacks require exact signatures plus the
  stored Account SID and Messaging Service SID (or exact sender `To`) binding;
  shared auth tokens do not authorize cross-salon routing. Do not infer consent
  from caller ID or webhook `Body`.
- Run and monitor the separate `customer_notification_delivery` worker job and
  `customer_notifications` queue. Treat queued, accepted, and sent as pending
  states, not delivery proof. Unknown post-dispatch outcomes and stale
  request/appointment copy are fail-closed; requeue only when the owner API
  returns `can_requeue=true`.
- Configure realtime phone mode from the Platform tenant Technical tab: Twilio `voice_transport=realtime_stream`; OpenAI realtime model/voice and the location-neutral background-noise handling policy (`automatic` is the default); and `speech_output_mode=streaming_tts` for low-latency backend-approved output. `buffered_realtime` is a legacy rollback mode.
- Keep Twilio and OpenAI secrets out of logs and docs; dashboard responses expose only configured/source metadata.
- Enable OpenAI voice AI in the Platform tenant Technical tab only when external AI voice turns should be enabled.
- Keep OpenAI model and voice settings configurable through the dashboard so model changes do not require code changes.
- Restrict CORS to the deployed admin/API, marketing, public salon, and POS
  calendar origins.

### V39 booking-attempt preflight

Run this read-only query against the pre-V39 database during an approved change
window, after taking the normal deployment backup. It intentionally uses only
columns that exist before V39; `superseded_at` is created by V39 and must not be
referenced by the preflight.

```sql
SELECT
    salon_id,
    operation_type,
    request_fingerprint,
    count(*) FILTER (WHERE provider_outcome <> 'not_started') AS dispatched_or_uncertain_count,
    array_agg(id ORDER BY created_at, id) AS attempt_ids,
    array_agg(status ORDER BY created_at, id) AS statuses,
    array_agg(provider_outcome ORDER BY created_at, id) AS provider_outcomes,
    array_agg(COALESCE(pos_booking_id, '') ORDER BY created_at, id) AS provider_booking_ids
FROM booking_attempts
WHERE request_fingerprint IS NOT NULL
  AND (
      status = 'pos_pending'
      OR reconciliation_status = 'required'
  )
GROUP BY salon_id, operation_type, request_fingerprint
HAVING count(*) FILTER (WHERE provider_outcome <> 'not_started') > 1
ORDER BY salon_id, operation_type, request_fingerprint;
```

Zero rows means the V39 duplicate-safety guard has no known ambiguous group to
block. Any returned row blocks the release. V39 cannot create its reconciliation
tables until the migration transaction commits, so there is no in-product V39
resolution workflow available for this preflight state. For each returned
attempt, verify the authoritative POS booking outcome, provider booking ID and
version, requested time, customer, and idempotency evidence under the normal
provider-access and privacy controls. Escalate required corrections through a
peer-reviewed DBA/product reconciliation change that keeps the booking attempt,
appointment, provider identity/version, notification, and audit records
consistent in one transaction. Do not delete, merge, auto-close, or mark an
unknown/dispatched attempt as `not_started` merely to make the query or index
pass. Rerun the read-only query only after authoritative reconciliation proves
that each fingerprint group has at most one dispatched or uncertain attempt.
