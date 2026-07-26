# Migration Release And Rollback

Database migrations are forward-only. The API startup migrator verifies
`app_schema_migrations` checksums and applies pending SQL inside one migration
transaction. A release must not depend on reversing a committed migration in
place or running ad hoc down SQL against production.

An application image rollback and a database rollback are different actions:

- **Image rollback** restarts the previously running API, worker, and web
  images. It does not change PostgreSQL data or schema.
- **Database recovery** selects an approved backup, validates it by restoring
  into a new isolated database, and then follows a separate reviewed recovery or
  cutover plan. The deploy workflow never performs it automatically.

## Expand/Contract Rule

Every schema change must be split so the previous production image remains
compatible throughout the rollback window:

1. **Expand:** add nullable columns, new tables/indexes/constraints, or additive
   data shapes without removing or reinterpreting fields used by the previous
   image.
2. **Migrate/backfill:** deploy code that can read the old and new shape, perform
   bounded idempotent backfill, and verify it before changing read ownership.
3. **Contract:** remove old reads/writes or schema only in a later release after
   the rollback window closes and the previous image is no longer a supported
   rollback target.

A release with destructive DDL, incompatible type/meaning changes, required
new writes that the previous image cannot tolerate, or an incomplete backfill
must declare the previous image incompatible. The current automated deploy then
refuses to proceed. Such a release needs a separately approved maintenance and
database-recovery plan; changing the declaration to bypass the gate is not a
rollback strategy.

## Per-Release Compatibility Declaration

Before pushing a release tag, a migration reviewer must compare:

- the currently running image and database schema;
- every new or changed migration in the candidate release;
- repository/service queries used by the previous image; and
- any backfill, constraint-validation, or data-ownership transition.

The protected `production` GitHub environment must then set all four
environment variables:

```txt
MIGRATION_COMPATIBILITY_RELEASE_TAG=<exact release tag>
PREVIOUS_IMAGE_DB_COMPATIBLE=true
MIGRATION_COMPATIBILITY_APPROVER=<bounded reviewer identity>
POSTGRES_BACKUP_STORAGE_APPROVAL=encrypted-private:/opt/manleai/backups
```

The tag must match the release exactly. The deploy job rejects a missing,
stale, malformed, or false declaration. The remote release directory retains a
mode-`600` declaration record containing only the release tag, compatibility
decision, approver, and exact storage approval. The storage value attests an
operator-verified encrypted private path but does not encrypt it. These values
are release evidence, not database or provider configuration.

After a release completes, treat the declaration as consumed. A later tag must
receive a new review and exact tag value; compatibility approval never carries
forward implicitly.

## Release Procedure

### 1. Preflight

- Review the candidate migration set and current
  `app_schema_migrations` history/checksums.
- Run migration tests and any migration-specific read-only preflight, such as
  the V39 ambiguous-attempt query in `docs/deployment.md`.
- Confirm the expand/contract phase and the exact prior image being retained.
- Set the tag-specific compatibility declaration above.
- Confirm `/opt/manleai/backups` is an approved encrypted private storage
  boundary with sufficient free space and retention.
- Confirm the PostgreSQL container is healthy. A stopped container or an
  existing volume without its Compose container is ambiguous and blocks the
  release.

### 2. Backup

Before the new API image starts and before its startup migrator can commit new
schema, `.github/workflows/ci-cd.yml` runs
`deploy/postgres-backup.sh` against the explicit `POSTGRES_DB` and
`POSTGRES_USER` infrastructure values. It creates
`predeploy-<release-id>.dump`, validates the archive catalog, writes the
SHA-256 sidecar, and stores the artifact path, checksum path, checksum, and UTC
completion timestamp in a bounded backup record in the release directory.

An initial deployment may skip the backup only when neither a PostgreSQL
Compose container nor the project database volume exists. A volume without a
container is not considered empty and fails closed.

### 3. Deploy And Observe

- Pull the immutable tagged images.
- Capture the currently running image IDs.
- Start the candidate stack; the API owns migrations and the worker waits for
  API health with `AUTO_MIGRATE=false`.
- Require API health and edge-route validation before moving the `current`
  release symlink.
- Preserve the backup, checksum, compatibility declaration, prior image tags,
  and bounded logs for the change record.

### 4. Abort Conditions

Abort before mutation when:

- the compatibility declaration is absent, stale, or false;
- the existing database source is ambiguous;
- the PostgreSQL service is unhealthy;
- encrypted private backup storage is unavailable or full;
- `pg_dump`, `pg_restore --list`, or SHA-256 generation fails; or
- a migration-specific preflight returns blockers.

After the new API starts, health, migration, checksum, constraint, or edge
validation failure keeps the release failed. Never delete rows, alter migration
history, change checksums, mark an unknown provider outcome safe, or run a
manual down migration to make a release pass.

## Failure And Rollback Decision

If the candidate fails after migrations commit:

1. Stop new traffic and preserve logs/evidence.
2. Determine whether the failure is application-only or whether data/schema is
   unsafe.
3. If the tag-specific declaration proves the previous image remains compatible
   with the expanded schema, the deploy workflow may restart those prior images.
   PostgreSQL remains at the forward schema.
4. If database state is suspect, do not rely on image rollback. Select the exact
   pre-deploy artifact and validate it with the isolated restore procedure in
   [PostgreSQL Backup And Isolated Restore Drills](postgres-backup-restore.md).
5. Compare the validated isolated database, provider-side outcomes, and writes
   accepted after the backup. Define the recovery point, expected data loss,
   reconciliation, downtime, and tenant communication in a new approved plan.
6. Only a DBA/change approver may authorize a production recovery/cutover.
   The repository scripts intentionally cannot overwrite or delete the existing
   production database.

Restoring the pre-deploy backup can lose writes made after its snapshot and can
diverge from provider-side bookings, webhooks, notifications, and calls. Those
effects must be reconciled explicitly. RPO is the accepted distance to the
recovery point; RTO is the accepted time to restore, validate, and return safe
service. Neither value is inferred from a successful image restart.

## Required Evidence

The release/change record must retain, without secrets or PII:

- release tag and previous image identities;
- compatibility approver and declaration;
- backup artifact ID and SHA-256;
- backup timestamp and storage/retention class;
- migration versions/checksum validation;
- preflight, health, and edge results;
- image rollback decision and confirmation that it did not restore the DB;
- isolated restore drill/report reference where recovery was considered; and
- actual RPO/RTO/duration plus final recovery approver for any recovery event.

Provider credentials and runtime provider settings remain salon-scoped records
managed through the dashboard-backed integration configuration. Do not inspect
or copy repository environment values to diagnose their active state, and do
not place provider secrets or customer data in release/rollback evidence.
