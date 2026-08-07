# Owner-First Production Release Gate

## Purpose And Ownership

Every `v*` release tag must pass the Owner-first release gate before application
images can be built or deployed. The gate proves the repository's code,
migration, PostgreSQL integration, tenant-isolation, secret-redaction, public
privacy, and callback-signature contracts against one exact revision.

The owned artifacts are:

- `.github/workflows/ci-cd.yml`: job dependencies and the isolated PostgreSQL
  service used by the tagged release;
- `deploy/owner-first-release-gate.sh`: fail-closed orchestration;
- `deploy/owner-first-release-gate.manifest`: versioned package and test-file
  ownership.

The manifest deliberately identifies packages and contract test files. The
gate does not grep test names or test output, so renaming a test function cannot
silently remove an entire contract area. Missing declared packages, test files,
web applications, package manifests, or lockfiles fail the gate.
The self-test also parses the local restart, migration checksum preflight,
pre-live sample-target preflight, and post-migration data-profile guard scripts;
checks the public domain smoke script; and rejects a CI/CD workflow that eagerly
promotes candidate `project.env`, loses the previous env/Compose rollback
boundary, or promotes env/current before edge upsert and public smoke. Missing,
reordered, or syntactically invalid deployment guards fail before tests or
image build.

## Required Checks

The backend job runs the complete `go test ./...` and `go vet ./...` suites,
followed by a bounded `-race` run over the high-risk authentication,
SaaS access-control, scheduling, booking, POS, worker, notification, operations-health,
conversation, training, and scheduling-load packages declared in the manifest.
The manifest owns the V86/V87/V88 migration safety tests, strict Square tenant-
identity and initial-activation PostgreSQL tests, real PostgreSQL external slot-
claim concurrency test, scope-bound Square capability tests, and load-
harness report/integration tests so those contracts cannot disappear silently.

The web matrix independently installs, typechecks, and builds `frontend`,
`landing`, and `pos-calendar`. Before typecheck/build, `frontend` and
`pos-calendar` also compile their explicitly owned scheduling-authority
evidence `.test.ts` files with the app-local TypeScript compiler in a unique
temporary directory and execute the emitted JavaScript with `node --test`;
`landing` has no scheduling-evidence contract. All three matrix results remain
required dependencies of image publication and deployment.

The PostgreSQL job uses a disposable PostgreSQL 17 service with a dedicated
database name and role. Trust authentication is confined to that isolated
GitHub Actions service. The release script rejects a missing database identity,
unsafe identifier shapes, the default `postgres` database, an existing
`app_schema_migrations` ledger, or any pre-existing public table. It constructs
`TEST_DATABASE_URL`, `MIGRATION_TEST_DATABASE_URL`, and
`POS_TAXONOMY_TEST_DATABASE_URL` in-process and never prints those URLs.

The fresh database contract:

1. applies the repository migration chain and re-applies it to prove checksum
   and migrate-twice safety;
2. verifies every SQL migration from V46 through the latest repository
   migration appears exactly once in `app_schema_migrations`;
3. runs the declared SaaS access-control, Business, Technical/Operations,
   runtime-role/RLS, tenant-quota/fairness, public-projection, scheduling authority, owner-manual, internal-calendar,
   authority-switch, booking/POS/Square, configuration-transfer, public
   catalog, owner/customer notification, Square webhook operations,
   scheduling-PII retention, operations-health, and V58 cross-table alias
   suites;
4. clones the verified migration-only baseline into one disposable database per
   migration-contract or integration package, then executes packages serially
   with bounded Go test timeouts. This prevents test fixtures from contaminating
   the template or any later package as well as preventing competing mutation.

Release-gate database mode exports
`OWNER_FIRST_RELEASE_GATE_DATABASE_REQUIRED=1` and the isolated load database
URL. V86, V87, external-slot-claim, and scheduling-load PostgreSQL coverage must
fail when required database identity/evidence is missing; it must not report a
successful gate by skipping those tests.

No release-gate mode calls a live POS, Twilio, OpenAI, paid API, or provider
console. Provider behavior is exercised through repository integration tests,
fixtures, local adapters, and fake transports.
The external Atomic Slot Commit load workload uses only a capability-aware fake
provider; any detected real provider runtime call fails its v2 report.

## Tenant And Security Contract

The explicit security step reruns the manifest-owned Go packages after
migrations and fails if any declared contract test file disappears. The web job
executes the manifest-owned browser/CSP evidence in the owning app. Combined
coverage includes:

- login non-enumeration, HttpOnly refresh-cookie transport, memory-only browser
  access tokens, and atomic refresh rotation with one exact concurrent
  successor replay, including disabled-account rejection without a replacement;
- live Redis atomic-bucket enforcement across two clients, HMAC-pseudonymous
  identities, typed `429`/`503` behavior, and fail-closed production defaults;
- nonce-bound production CSP contracts for all three web apps, including POS
  scheduler placement without inline style attributes;
- authenticated owner routes and cross-salon ownership rejection;
- server-owned ActorContext replacement of stale caller/JWT tenant and role
  state; tenant-membership isolation; exact Platform Ops salon/capability
  delegation; Platform Admin/Ops PII default deny with bounded grant,
  expiry/revocation, optimistic-version, action replay/conflict, immutable
  audit, opaque change-reference validation, role-bound delegation revocation,
  owner-membership, and concurrent last-admin guards;
- separate non-owner/non-superuser/non-`BYPASSRLS` runtime role verification;
  per-operation DB actor/scope reset; cross-tenant RLS; public zero base-table
  visibility through V71 safe projection; V72 exact Platform PII scope across
  customer, call, appointment, and notification tables;
- V78 expand-phase `app.system_salon_id` application/reset, provider-only
  salon-ID locators, per-item provider/worker binding tests, and a static guard
  proving V78 does not prematurely tighten base RLS before the compatible image
  is deployed;
- V79 contract-preparation worker-only claim/recovery/cleanup/retention
  functions, exact per-item worker binding, fail-closed call-child mismatch
  preflight, and composite salon/session foreign-key enforcement, with a static
  guard proving base RLS is still unchanged;
- V80 strict system-tenant RLS matching for every ordinary provider/worker
  base-table policy, runtime-role negative coverage for unbound and cross-tenant
  reads/writes, and a PostgreSQL policy-catalog audit that rejects remaining
  direct broad system-scope branches;
- V84 OpenAI tenant/config/credential/destination fences, purpose-separated
  credential uniqueness identity, official-destination-only transport,
  durable per-capability verification evidence, bounded worker discovery, and
  exact returned-tenant processing; release tests use fake transports and do
  not call OpenAI;
- tenant quota/usage accounting, provider-write rejection, and fair worker
  claim limits without cross-tenant starvation;
- immutable V74 Tenant/Platform principal scope, fail-closed mixed-identity
  migration, realm-specific account directories, and database-enforced
  assignment targets;
- V75 Owner-authorized Platform Services/AI Training/Calls access with no Admin
  bypass: current base role/assignment capability plus exact active Owner
  authorization, request-linked Calls PII, bounded expiry/revocation, per-action
  actual-actor audit, feature RLS, and select-only call-linked scheduling
  evidence without general Appointments access; compatibility Platform Business
  Services routes cannot bypass that gate, and support never substitutes the
  salon Owner identity;
- authority-aware Training evaluation with tenant-first and unknown-authority
  fail-closed behavior;
- integration token and secret whole-response redaction plus stable POS/Square
  and Twilio/OpenAI provider-diagnostic masking, including V63 historical data
  cleanup;
- zero POS/provider/reconciliation evidence for `owner_manual` and internal
  calendar work;
- public catalog responses without customer PII or provider identifiers and
  without direct public base-table access;
- masked owner/customer notification destinations, scheduling-PII retention,
  and callback-signature enforcement;
- authenticated Square webhook backlog/dead-letter/replay ownership;
- explicit salon context on authenticated Square control/write routes, with no
  primary-salon middleware fallback;
- Square and Twilio webhook signature contracts;
- scheduling authority, target-origin, and tenant fences.

This is a repository contract gate. A passing run is evidence for code
readiness at the tagged revision, not evidence about the current state of any
salon's dashboard-managed provider configuration.
For V80 it also is not evidence that the required V79-aware image observation
has occurred in the target environment. The gate proves the strict contract at
the tagged revision; production cutover still requires all old replicas to be
drained and provider/worker paths to be observed before migration.

## Release Dependency

`build-images` requires the backend, web, and Owner-first release-gate jobs.
`deploy` requires those jobs plus `build-images`, and both jobs remain restricted
to `v*` tags. The release gate is additive to the existing forward-migration,
pre-deploy backup, compatibility declaration, healthcheck, and rollback gates;
it does not weaken or replace them.

## Local Validation

The non-database contract can be checked without credentials:

```bash
bash deploy/owner-first-release-gate.sh self-test
bash deploy/owner-first-release-gate.sh backend
```

Database modes require an explicitly created, empty, disposable PostgreSQL
database and these identity variables: `PGHOST`, `PGPORT`, `PGUSER`, and
`PGDATABASE`. `PGPASSWORD` may be supplied through normal libpq handling and is
never logged by the script. The dedicated database name must contain the exact
`release_gate` marker; ordinary development and production database names are
rejected before a connection is attempted.

```bash
bash deploy/owner-first-release-gate.sh postgres
bash deploy/owner-first-release-gate.sh security
```

The `postgres` mode intentionally refuses a database after the migration ledger
or any public table exists. Run it once on a fresh database, then run `security`
against that migrated database. Never point either command at production or a
shared development database.

The `security` mode also requires a disposable Redis database through
`TEST_REDIS_URL` using `redis://` or `rediss://`. CI provisions Redis 7 and uses
database 15. The URL is required before tests begin so the distributed limiter
integration test cannot silently skip in the release security gate.

## Operational Limitations

A passing gate does not prove live provider credentials, callback reachability,
actual SMS or phone delivery, provider uptime, paid API access, production data
quality, production capacity, alert routing, or on-call response. It also does
not prove that the production backup destination is encrypted/private with
sufficient capacity, that retention and key rotation are configured, or that a
witnessed restore drill met approved RPO/RTO objectives. Those remain separate
operational approvals and evidence requirements in the production readiness,
backup/restore, notification, and operations-health runbooks.

The gate proves the repository browser-session, CSP, and Redis/API rate-limit
contracts and their production build shape. It does not prove the deployed edge
is serving the expected CSP, that production Redis has suitable capacity and
alerting, or that abuse thresholds match observed traffic. Those deployment and
operational checks remain explicit production evidence.
