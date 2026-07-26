#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: postgres-verify-restore.sh \
  --env-file PATH \
  --compose-file PATH \
  --project NAME \
  --db-user NAME \
  --target-db NAME \
  --migrations-dir PATH \
  --required-table NAME [--required-table NAME ...] \
  --required-constraint NAME [--required-constraint NAME ...]

Validates a restored isolated database. Every migration file must have an exact
app_schema_migrations name/checksum row, required schema objects must exist,
and bounded tenant-isolation smoke queries must return no violations.
USAGE
}

fail() {
  printf 'postgres-verify-restore: %s\n' "$*" >&2
  exit 1
}

require_identifier() {
  local label="$1"
  local value="$2"
  if [[ ! "$value" =~ ^[A-Za-z_][A-Za-z0-9_]{0,62}$ ]]; then
    fail "$label must be a PostgreSQL identifier containing only letters, numbers, and underscores."
  fi
}

env_file=""
compose_file=""
project=""
db_user=""
target_db=""
migrations_dir=""
required_tables=()
required_constraints=()

while (($# > 0)); do
  case "$1" in
    --env-file) env_file="${2:-}"; shift 2 ;;
    --compose-file) compose_file="${2:-}"; shift 2 ;;
    --project) project="${2:-}"; shift 2 ;;
    --db-user) db_user="${2:-}"; shift 2 ;;
    --target-db) target_db="${2:-}"; shift 2 ;;
    --migrations-dir) migrations_dir="${2:-}"; shift 2 ;;
    --required-table) required_tables+=("${2:-}"); shift 2 ;;
    --required-constraint) required_constraints+=("${2:-}"); shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) fail "unknown argument: $1" ;;
  esac
done

[[ -f "$env_file" ]] || fail "--env-file must name an existing regular file."
[[ -f "$compose_file" ]] || fail "--compose-file must name an existing regular file."
[[ -d "$migrations_dir" ]] || fail "--migrations-dir must name an existing directory."
require_identifier "--project" "$project"
require_identifier "--db-user" "$db_user"
require_identifier "--target-db" "$target_db"
((${#required_tables[@]} > 0)) || fail "at least one --required-table is required."
((${#required_constraints[@]} > 0)) || fail "at least one --required-constraint is required."

for table_name in "${required_tables[@]}"; do
  require_identifier "--required-table" "$table_name"
done
for constraint_name in "${required_constraints[@]}"; do
  require_identifier "--required-constraint" "$constraint_name"
done

command -v docker >/dev/null 2>&1 || fail "docker is required."
command -v sha256sum >/dev/null 2>&1 || fail "sha256sum is required."

compose() {
  docker compose \
    --env-file "$env_file" \
    -f "$compose_file" \
    -p "$project" "$@"
}

psql_target() {
  compose exec -T postgres psql \
    --username "$db_user" \
    --dbname "$target_db" \
    --no-psqlrc \
    --set ON_ERROR_STOP=1 "$@"
}

database_exists="$(compose exec -T postgres psql \
  --username "$db_user" \
  --dbname postgres \
  --no-psqlrc \
  --tuples-only \
  --no-align \
  --command "SELECT 1 FROM pg_database WHERE datname = '$target_db';")"
[[ "$database_exists" == "1" ]] || fail "the isolated target database does not exist."

migration_rows="$(psql_target --tuples-only --no-align --field-separator '|' --command \
  "SELECT version, name, checksum FROM app_schema_migrations ORDER BY version::integer;")"
[[ -n "$migration_rows" ]] || fail "app_schema_migrations is missing or empty."

shopt -s nullglob
migration_files=("$migrations_dir"/V*.sql)
shopt -u nullglob
((${#migration_files[@]} > 0)) || fail "no migration files were found."

database_migration_count=0
while IFS='|' read -r version migration_name recorded_checksum; do
  [[ "$version" =~ ^[0-9]+$ ]] || fail "restored migration version is not numeric."
  [[ "$recorded_checksum" =~ ^[0-9a-f]{64}$ ]] || fail "restored migration checksum has an invalid shape."

  shopt -s nullglob
  matching_files=("$migrations_dir"/"V${version}__"*.sql)
  shopt -u nullglob
  ((${#matching_files[@]} == 1)) || fail "migration V${version} does not map to exactly one release SQL file."

  migration_file="${matching_files[0]}"
  migration_base="$(basename "$migration_file")"
  expected_name="${migration_base#*__}"
  expected_name="${expected_name%.sql}"
  expected_name="${expected_name//_/ }"
  [[ "$migration_name" == "$expected_name" ]] || fail "migration V${version} name does not match the release file."

  expected_checksum="$(sha256sum "$migration_file" | awk '{print $1}')"
  [[ "$recorded_checksum" == "$expected_checksum" ]] || fail "migration V${version} checksum does not match the release file."
  database_migration_count=$((database_migration_count + 1))
done <<< "$migration_rows"

((database_migration_count == ${#migration_files[@]})) || fail "restored migration rows do not exactly match the release migration set."

for table_name in "${required_tables[@]}"; do
  table_exists="$(psql_target --tuples-only --no-align --command \
    "SELECT to_regclass('public.$table_name') IS NOT NULL;")"
  [[ "$table_exists" == "t" ]] || fail "required table is missing: $table_name"
done

for constraint_name in "${required_constraints[@]}"; do
  constraint_exists="$(psql_target --tuples-only --no-align --command \
    "SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE connamespace = 'public'::regnamespace AND conname = '$constraint_name');")"
  [[ "$constraint_exists" == "t" ]] || fail "required constraint is missing: $constraint_name"
done

psql_target --quiet <<'SQL'
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM appointments AS appointment
    JOIN booking_attempts AS attempt ON attempt.id = appointment.booking_attempt_id
    WHERE appointment.salon_id <> attempt.salon_id
  ) THEN
    RAISE EXCEPTION 'tenant smoke failed: appointment and booking attempt salon mismatch';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM appointment_services AS service
    JOIN appointments AS appointment ON appointment.id = service.appointment_id
    WHERE service.salon_id <> appointment.salon_id
  ) THEN
    RAISE EXCEPTION 'tenant smoke failed: appointment service and root salon mismatch';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM scheduling_requests AS request
    JOIN call_sessions AS session ON session.id = request.call_session_id
    WHERE request.salon_id <> session.salon_id
  ) THEN
    RAISE EXCEPTION 'tenant smoke failed: scheduling request and call session salon mismatch';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM salon_integration_configs AS config
    LEFT JOIN salons AS salon ON salon.id = config.salon_id
    WHERE salon.id IS NULL
  ) THEN
    RAISE EXCEPTION 'tenant smoke failed: integration config has no salon owner';
  END IF;
END
$$;
SQL

printf 'restore_verification=passed\n'
printf 'migration_count=%s\n' "$database_migration_count"
printf 'api_startup_handoff=ready_for_isolated_database_health_check_with_auto_migrate_false\n'
