#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: postgres-restore-drill.sh \
  --env-file PATH \
  --compose-file PATH \
  --project NAME \
  --source-db NAME \
  --target-db manleai_restore_drill_NAME \
  --db-user NAME \
  --artifact PATH \
  --checksum-file PATH \
  --artifact-id ID \
  --migrations-dir PATH \
  --report-file ABSOLUTE_PATH \
  --approver ID \
  --approval-reference ID \
  --release-ref ID \
  --rpo-target-seconds NUMBER \
  --rto-target-seconds NUMBER \
  --drill-start-epoch NUMBER \
  --storage-class encrypted-private

Restores a checked custom-format artifact into one new, explicitly named,
isolated drill database and validates it. The script refuses an existing target
and never drops, truncates, renames, or overwrites any database.
USAGE
}

fail() {
  printf 'postgres-restore-drill: %s\n' "$*" >&2
  exit 1
}

require_identifier() {
  local label="$1"
  local value="$2"
  if [[ ! "$value" =~ ^[A-Za-z_][A-Za-z0-9_]{0,62}$ ]]; then
    fail "$label must be a PostgreSQL identifier containing only letters, numbers, and underscores."
  fi
}

require_report_value() {
  local label="$1"
  local value="$2"
  if [[ ! "$value" =~ ^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,159}$ ]]; then
    fail "$label contains unsupported report characters or is too long."
  fi
}

env_file=""
compose_file=""
project=""
source_db=""
target_db=""
db_user=""
artifact=""
checksum_file=""
artifact_id=""
migrations_dir=""
report_file=""
approver=""
approval_reference=""
release_ref=""
rpo_target_seconds=""
rto_target_seconds=""
drill_start_epoch=""
storage_class=""

while (($# > 0)); do
  case "$1" in
    --env-file) env_file="${2:-}"; shift 2 ;;
    --compose-file) compose_file="${2:-}"; shift 2 ;;
    --project) project="${2:-}"; shift 2 ;;
    --source-db) source_db="${2:-}"; shift 2 ;;
    --target-db) target_db="${2:-}"; shift 2 ;;
    --db-user) db_user="${2:-}"; shift 2 ;;
    --artifact) artifact="${2:-}"; shift 2 ;;
    --checksum-file) checksum_file="${2:-}"; shift 2 ;;
    --artifact-id) artifact_id="${2:-}"; shift 2 ;;
    --migrations-dir) migrations_dir="${2:-}"; shift 2 ;;
    --report-file) report_file="${2:-}"; shift 2 ;;
    --approver) approver="${2:-}"; shift 2 ;;
    --approval-reference) approval_reference="${2:-}"; shift 2 ;;
    --release-ref) release_ref="${2:-}"; shift 2 ;;
    --rpo-target-seconds) rpo_target_seconds="${2:-}"; shift 2 ;;
    --rto-target-seconds) rto_target_seconds="${2:-}"; shift 2 ;;
    --drill-start-epoch) drill_start_epoch="${2:-}"; shift 2 ;;
    --storage-class) storage_class="${2:-}"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) fail "unknown argument: $1" ;;
  esac
done

[[ -f "$env_file" ]] || fail "--env-file must name an existing regular file."
[[ -f "$compose_file" ]] || fail "--compose-file must name an existing regular file."
[[ -f "$artifact" && ! -L "$artifact" ]] || fail "--artifact must name a regular non-symlink file."
[[ -f "$checksum_file" && ! -L "$checksum_file" ]] || fail "--checksum-file must name a regular non-symlink file."
[[ -d "$migrations_dir" ]] || fail "--migrations-dir must name an existing directory."
[[ "$report_file" == /* && ! -e "$report_file" && ! -L "$report_file" ]] || fail "--report-file must be a new explicit absolute path."
require_identifier "--project" "$project"
require_identifier "--source-db" "$source_db"
require_identifier "--target-db" "$target_db"
require_identifier "--db-user" "$db_user"
[[ "$source_db" != "$target_db" ]] || fail "source and target databases must be different."
[[ "$target_db" == manleai_restore_drill_* ]] || fail "target database must use the manleai_restore_drill_ isolation prefix."
[[ "$target_db" != "postgres" && "$target_db" != "template0" && "$target_db" != "template1" ]] || fail "refusing a reserved PostgreSQL target."
[[ "$artifact_id" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,119}$ ]] || fail "--artifact-id contains unsupported characters or is too long."
require_report_value "--approver" "$approver"
require_report_value "--approval-reference" "$approval_reference"
require_report_value "--release-ref" "$release_ref"
[[ "$rpo_target_seconds" =~ ^[1-9][0-9]*$ ]] || fail "--rpo-target-seconds must be a positive integer."
[[ "$rto_target_seconds" =~ ^[1-9][0-9]*$ ]] || fail "--rto-target-seconds must be a positive integer."
[[ "$drill_start_epoch" =~ ^[1-9][0-9]*$ ]] || fail "--drill-start-epoch must be a positive Unix timestamp."
[[ "$storage_class" == "encrypted-private" ]] || fail "--storage-class must explicitly attest encrypted-private storage."

command -v docker >/dev/null 2>&1 || fail "docker is required."
command -v sha256sum >/dev/null 2>&1 || fail "sha256sum is required."
command -v stat >/dev/null 2>&1 || fail "stat is required."

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
verify_script="$script_dir/postgres-verify-restore.sh"
[[ -x "$verify_script" ]] || fail "postgres-verify-restore.sh is missing or not executable."

artifact_dir="$(cd "$(dirname "$artifact")" && pwd)"
artifact_path="$artifact_dir/$(basename "$artifact")"
checksum_path="$(cd "$(dirname "$checksum_file")" && pwd)/$(basename "$checksum_file")"
artifact_name="$(basename "$artifact_path")"

read -r recorded_checksum recorded_name extra < "$checksum_path" || fail "could not read checksum file."
[[ -z "${extra:-}" ]] || fail "checksum file must contain exactly one artifact entry."
[[ "$recorded_checksum" =~ ^[0-9a-f]{64}$ ]] || fail "checksum file does not contain a SHA-256 value."
[[ "$recorded_name" == "$artifact_name" ]] || fail "checksum file names a different artifact."
actual_checksum="$(sha256sum "$artifact_path" | awk '{print $1}')"
[[ "$actual_checksum" == "$recorded_checksum" ]] || fail "artifact checksum mismatch."

compose() {
  docker compose \
    --env-file "$env_file" \
    -f "$compose_file" \
    -p "$project" "$@"
}

source_exists="$(compose exec -T postgres psql \
  --username "$db_user" \
  --dbname postgres \
  --no-psqlrc \
  --tuples-only \
  --no-align \
  --command "SELECT 1 FROM pg_database WHERE datname = '$source_db';")"
[[ "$source_exists" == "1" ]] || fail "the explicit source database does not exist."

target_exists="$(compose exec -T postgres psql \
  --username "$db_user" \
  --dbname postgres \
  --no-psqlrc \
  --tuples-only \
  --no-align \
  --command "SELECT 1 FROM pg_database WHERE datname = '$target_db';")"
[[ -z "$target_exists" ]] || fail "the isolated target already exists; refusing to overwrite or delete it."

umask 077
report_dir="$(dirname "$report_file")"
install -d -m 700 "$report_dir"
[[ ! -L "$report_dir" ]] || fail "refusing a symlinked report directory."

stage="archive_catalog"
status="failed"
restore_start_epoch="$(date +%s)"
restore_finish_epoch="$restore_start_epoch"
drill_finish_epoch="$restore_start_epoch"
artifact_epoch="$(stat -c %Y "$artifact_path")"
observed_rpo_seconds=$((restore_start_epoch - artifact_epoch))
((observed_rpo_seconds >= 0)) || observed_rpo_seconds=0

write_report() {
  local report_status="$1"
  local api_startup_handoff="not_ready"
  local temporary_report
  if [[ "$report_status" == "passed" ]]; then
    api_startup_handoff="ready_for_isolated_database_health_check_with_auto_migrate_false"
  elif [[ "$report_status" == "failed_objective" ]]; then
    api_startup_handoff="blocked_rpo_rto_objective"
  fi
  restore_finish_epoch="$(date +%s)"
  drill_finish_epoch="$restore_finish_epoch"
  temporary_report="$(mktemp "$report_dir/.restore-drill-report.partial.XXXXXX")"
  {
    printf 'status=%s\n' "$report_status"
    printf 'failure_stage=%s\n' "$stage"
    printf 'drill_timestamp_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf 'artifact_id=%s\n' "$artifact_id"
    printf 'artifact_sha256=%s\n' "$actual_checksum"
    printf 'release_ref=%s\n' "$release_ref"
    printf 'approval_reference=%s\n' "$approval_reference"
    printf 'approver=%s\n' "$approver"
    printf 'rpo_target_seconds=%s\n' "$rpo_target_seconds"
    printf 'observed_artifact_age_seconds=%s\n' "$observed_rpo_seconds"
    printf 'rto_target_seconds=%s\n' "$rto_target_seconds"
    printf 'restore_validation_duration_seconds=%s\n' "$((restore_finish_epoch - restore_start_epoch))"
    printf 'drill_duration_seconds=%s\n' "$((drill_finish_epoch - drill_start_epoch))"
    printf 'api_startup_handoff=%s\n' "$api_startup_handoff"
  } > "$temporary_report"
  chmod 600 "$temporary_report"
  mv "$temporary_report" "$report_file"
}

on_exit() {
  local exit_code=$?
  trap - EXIT
  if [[ "$status" != "passed" && ! -e "$report_file" ]]; then
    write_report "failed"
  fi
  exit "$exit_code"
}
trap on_exit EXIT

compose exec -T postgres pg_restore --list < "$artifact_path" >/dev/null

stage="create_isolated_target"
compose exec -T postgres psql \
  --username "$db_user" \
  --dbname postgres \
  --no-psqlrc \
  --set ON_ERROR_STOP=1 \
  --command "CREATE DATABASE \"$target_db\" TEMPLATE template0;" >/dev/null

stage="restore_archive"
compose exec -T postgres pg_restore \
  --username "$db_user" \
  --dbname "$target_db" \
  --exit-on-error \
  --no-owner \
  --no-acl < "$artifact_path"

stage="verify_schema_and_tenants"
verify_args=(
  --env-file "$env_file"
  --compose-file "$compose_file"
  --project "$project"
  --db-user "$db_user"
  --target-db "$target_db"
  --migrations-dir "$migrations_dir"
)

for required_table in \
  salons \
  salon_settings \
  salon_integration_configs \
  booking_attempts \
  booking_attempt_segments \
  appointments \
  appointment_services \
  availability_quotes \
  call_sessions \
  scheduling_requests \
  manleai_calendar_configs \
  manleai_calendar_execution_events; do
  verify_args+=(--required-table "$required_table")
done

for required_constraint in \
  booking_attempt_segments_attempt_tenant_fk \
  appointment_services_appointment_tenant_fk \
  appointment_services_manleai_staff_no_overlap \
  appointment_services_released_by_attempt_tenant_fk \
  manleai_calendar_execution_events_appointment_version_key; do
  verify_args+=(--required-constraint "$required_constraint")
done

"$verify_script" "${verify_args[@]}"

restore_finish_epoch="$(date +%s)"
drill_finish_epoch="$restore_finish_epoch"
restore_duration_seconds=$((restore_finish_epoch - restore_start_epoch))
drill_duration_seconds=$((drill_finish_epoch - drill_start_epoch))

stage="objectives"
objective_status="passed"
if ((observed_rpo_seconds > rpo_target_seconds || restore_duration_seconds > rto_target_seconds)); then
  objective_status="failed_objective"
  stage="rpo_rto_objectives"
else
  stage="complete"
fi

status="passed"
write_report "$objective_status"
trap - EXIT

printf 'restore_target=%s\n' "$target_db"
printf 'restore_report=%s\n' "$report_file"
printf 'restore_status=%s\n' "$objective_status"
printf 'observed_artifact_age_seconds=%s\n' "$observed_rpo_seconds"
printf 'restore_validation_duration_seconds=%s\n' "$restore_duration_seconds"
printf 'drill_duration_seconds=%s\n' "$drill_duration_seconds"

[[ "$objective_status" == "passed" ]] || fail "restore validated, but the declared RPO or RTO objective was exceeded."
