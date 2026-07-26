#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: postgres-backup.sh \
  --env-file PATH \
  --compose-file PATH \
  --project NAME \
  --source-db NAME \
  --db-user NAME \
  --output-dir ABSOLUTE_PATH \
  --artifact-id ID \
  --storage-class encrypted-private

Creates a PostgreSQL custom-format dump, validates its archive catalog, and
writes a SHA-256 sidecar. The output directory and files are private. This
script never deletes a database and never uploads the artifact.
USAGE
}

fail() {
  printf 'postgres-backup: %s\n' "$*" >&2
  exit 1
}

require_identifier() {
  local label="$1"
  local value="$2"
  if [[ ! "$value" =~ ^[A-Za-z_][A-Za-z0-9_]{0,62}$ ]]; then
    fail "$label must be an explicit PostgreSQL identifier containing only letters, numbers, and underscores."
  fi
}

env_file=""
compose_file=""
project=""
source_db=""
db_user=""
output_dir=""
artifact_id=""
storage_class=""

while (($# > 0)); do
  case "$1" in
    --env-file) env_file="${2:-}"; shift 2 ;;
    --compose-file) compose_file="${2:-}"; shift 2 ;;
    --project) project="${2:-}"; shift 2 ;;
    --source-db) source_db="${2:-}"; shift 2 ;;
    --db-user) db_user="${2:-}"; shift 2 ;;
    --output-dir) output_dir="${2:-}"; shift 2 ;;
    --artifact-id) artifact_id="${2:-}"; shift 2 ;;
    --storage-class) storage_class="${2:-}"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) fail "unknown argument: $1" ;;
  esac
done

[[ -f "$env_file" ]] || fail "--env-file must name an existing regular file."
[[ -f "$compose_file" ]] || fail "--compose-file must name an existing regular file."
require_identifier "--project" "$project"
require_identifier "--source-db" "$source_db"
require_identifier "--db-user" "$db_user"
[[ "$source_db" != "postgres" && "$source_db" != "template0" && "$source_db" != "template1" ]] || fail "refusing to back up a reserved PostgreSQL database."
[[ "$output_dir" == /* && "$output_dir" != "/" && "$output_dir" != "/tmp" ]] || fail "--output-dir must be an explicit persistent absolute directory, not / or /tmp."
[[ "$artifact_id" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,119}$ ]] || fail "--artifact-id contains unsupported characters or is too long."
[[ "$storage_class" == "encrypted-private" ]] || fail "--storage-class must explicitly attest encrypted-private storage."

command -v docker >/dev/null 2>&1 || fail "docker is required."
command -v sha256sum >/dev/null 2>&1 || fail "sha256sum is required."
docker compose version >/dev/null

umask 077
install -d -m 700 "$output_dir"
[[ ! -L "$output_dir" ]] || fail "refusing a symlinked output directory."

artifact_name="${artifact_id}.dump"
checksum_name="${artifact_name}.sha256"
artifact_path="$output_dir/$artifact_name"
checksum_path="$output_dir/$checksum_name"
[[ ! -e "$artifact_path" && ! -e "$checksum_path" ]] || fail "artifact or checksum already exists; use a new artifact ID."

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

temporary_artifact="$(mktemp "$output_dir/.${artifact_name}.partial.XXXXXX")"
temporary_checksum="$(mktemp "$output_dir/.${checksum_name}.partial.XXXXXX")"
cleanup() {
  rm -f "$temporary_artifact" "$temporary_checksum"
}
trap cleanup EXIT

compose exec -T postgres pg_dump \
  --username "$db_user" \
  --dbname "$source_db" \
  --format=custom \
  --compress=6 \
  --no-owner \
  --no-acl > "$temporary_artifact"

[[ -s "$temporary_artifact" ]] || fail "pg_dump produced an empty artifact."
compose exec -T postgres pg_restore --list < "$temporary_artifact" >/dev/null

artifact_checksum="$(sha256sum "$temporary_artifact" | awk '{print $1}')"
[[ "$artifact_checksum" =~ ^[0-9a-f]{64}$ ]] || fail "could not calculate a valid SHA-256 checksum."
printf '%s  %s\n' "$artifact_checksum" "$artifact_name" > "$temporary_checksum"

chmod 600 "$temporary_artifact" "$temporary_checksum"
mv "$temporary_artifact" "$artifact_path"
mv "$temporary_checksum" "$checksum_path"
trap - EXIT

printf 'backup_artifact=%s\n' "$artifact_path"
printf 'backup_checksum=%s\n' "$checksum_path"
printf 'backup_sha256=%s\n' "$artifact_checksum"
printf 'backup_timestamp_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
