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

## Required Checks

The backend job runs the complete `go test ./...` and `go vet ./...` suites,
followed by a bounded `-race` run over the high-risk authentication,
scheduling, booking, POS, worker, notification, operations-health,
conversation, and training packages declared in the manifest.

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
3. runs the declared scheduling authority, owner-manual, internal-calendar,
   authority-switch, booking/POS/Square, configuration-transfer, public
   catalog, owner/customer notification, Square webhook operations,
   scheduling-PII retention, operations-health, and V58 cross-table alias
   suites;
4. clones the verified migration-only baseline into one disposable database per
   package, then executes packages serially with bounded Go test timeouts. This
   prevents cross-package fixture contamination as well as competing mutation.

No release-gate mode calls a live POS, Twilio, OpenAI, paid API, or provider
console. Provider behavior is exercised through repository integration tests,
fixtures, local adapters, and fake transports.

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
- authority-aware Training evaluation with tenant-first and unknown-authority
  fail-closed behavior;
- integration token and secret DTO redaction;
- zero POS/provider/reconciliation evidence for `owner_manual` and internal
  calendar work;
- public catalog responses without customer PII or provider identifiers;
- masked owner/customer notification destinations, scheduling-PII retention,
  and callback-signature enforcement;
- authenticated Square webhook backlog/dead-letter/replay ownership;
- Square and Twilio webhook signature contracts;
- scheduling authority, target-origin, and tenant fences.

This is a repository contract gate. A passing run is evidence for code
readiness at the tagged revision, not evidence about the current state of any
salon's dashboard-managed provider configuration.

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
