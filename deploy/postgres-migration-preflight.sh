#!/usr/bin/env bash
set -euo pipefail

readonly checksum_mismatch_exit=42

fail() {
  printf 'migration-preflight error: %s\n' "$1" >&2
  exit 1
}

usage() {
  cat >&2 <<'EOF'
usage: postgres-migration-preflight.sh \
  --env-file PATH \
  --compose-file PATH \
  --project NAME \
  --migrations-dir PATH

Exit status 42 means the persisted migration ledger is incompatible with the
candidate migration files. Other non-zero statuses are operational failures.
EOF
  exit 2
}

env_file=""
compose_file=""
project_name=""
migrations_dir=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file) env_file="${2:-}"; shift 2 ;;
    --compose-file) compose_file="${2:-}"; shift 2 ;;
    --project) project_name="${2:-}"; shift 2 ;;
    --migrations-dir) migrations_dir="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

[ -f "$env_file" ] || fail "--env-file must name an existing file"
[ -f "$compose_file" ] || fail "--compose-file must name an existing file"
[ -d "$migrations_dir" ] || fail "--migrations-dir must name an existing directory"
[[ "$project_name" =~ ^[a-z0-9][a-z0-9_-]{0,62}$ ]] || fail "--project has an unsafe shape"
command -v docker >/dev/null 2>&1 || fail "docker is required"

compose=(docker compose --env-file "$env_file" -f "$compose_file" -p "$project_name")
[ -n "$("${compose[@]}" ps -q postgres)" ] || fail "the PostgreSQL service is not running"

postgres_sql() {
  local statement="$1"
  "${compose[@]}" exec -T postgres sh -c \
    'exec psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -AtF "|" -c "$1"' \
    _ "$statement"
}

calculate_checksum() {
  local file_path="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file_path" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file_path" | awk '{print $1}'
    return
  fi
  fail "sha256sum or shasum is required"
}

migration_files=()
while IFS= read -r migration_file; do
  migration_files+=("$migration_file")
done < <(find "$migrations_dir" -maxdepth 1 -type f -name 'V[0-9]*__*.sql' -print)
[ "${#migration_files[@]}" -gt 0 ] || fail "no candidate migration files were found"

seen_versions=()
for migration_file in "${migration_files[@]}"; do
  migration_base="$(basename "$migration_file")"
  migration_version="${migration_base%%__*}"
  migration_version="${migration_version#V}"
  [[ "$migration_version" =~ ^[0-9]+$ ]] || fail "candidate migration version is invalid: $migration_base"
  if [ "${#seen_versions[@]}" -gt 0 ]; then
    for seen_version in "${seen_versions[@]}"; do
      [ "$migration_version" != "$seen_version" ] || fail "candidate migration version is duplicated: V$migration_version"
    done
  fi
  seen_versions+=("$migration_version")
done

ledger_state="$(postgres_sql "SELECT CASE WHEN to_regclass('public.app_schema_migrations') IS NULL THEN 'absent' ELSE 'present' END")"
case "$ledger_state" in
  absent)
    printf 'migration_preflight=pending\napplied_migration_count=0\n'
    exit 0
    ;;
  present) ;;
  *) fail "could not determine migration ledger state" ;;
esac

ledger_rows=()
while IFS= read -r ledger_row; do
  [ -n "$ledger_row" ] && ledger_rows+=("$ledger_row")
done < <(postgres_sql 'SELECT version,name,checksum FROM app_schema_migrations ORDER BY applied_at,version')

mismatch_count=0
if [ "${#ledger_rows[@]}" -gt 0 ]; then
  for ledger_row in "${ledger_rows[@]}"; do
    IFS='|' read -r recorded_version recorded_name recorded_checksum extra <<<"$ledger_row"
    if [ -n "${extra:-}" ] || ! [[ "$recorded_version" =~ ^[0-9]+$ ]] || ! [[ "$recorded_checksum" =~ ^[0-9a-f]{64}$ ]]; then
      printf 'migration-preflight mismatch: malformed ledger row for version %s\n' "${recorded_version:-unknown}" >&2
      mismatch_count=$((mismatch_count + 1))
      continue
    fi

    matching_files=("$migrations_dir/V${recorded_version}__"*.sql)
    if [ ! -e "${matching_files[0]}" ] || [ "${#matching_files[@]}" -ne 1 ]; then
      printf 'migration-preflight mismatch: V%s does not map to exactly one candidate file\n' "$recorded_version" >&2
      mismatch_count=$((mismatch_count + 1))
      continue
    fi

    migration_file="${matching_files[0]}"
    migration_base="$(basename "$migration_file")"
    expected_name="${migration_base#*__}"
    expected_name="${expected_name%.sql}"
    expected_name="${expected_name//_/ }"
    expected_checksum="$(calculate_checksum "$migration_file")"

    if [ "$recorded_name" != "$expected_name" ]; then
      printf 'migration-preflight mismatch: V%s name differs from candidate file\n' "$recorded_version" >&2
      mismatch_count=$((mismatch_count + 1))
    fi
    if [ "$recorded_checksum" != "$expected_checksum" ]; then
      printf 'migration-preflight mismatch: V%s checksum differs from candidate file\n' "$recorded_version" >&2
      mismatch_count=$((mismatch_count + 1))
    fi
  done
fi

if [ "$mismatch_count" -ne 0 ]; then
  printf 'migration_preflight=incompatible\napplied_migration_count=%s\nmismatch_count=%s\n' \
    "${#ledger_rows[@]}" "$mismatch_count"
  exit "$checksum_mismatch_exit"
fi

printf 'migration_preflight=compatible\napplied_migration_count=%s\n' "${#ledger_rows[@]}"
