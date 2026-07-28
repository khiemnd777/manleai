#!/usr/bin/env bash
set -euo pipefail

readonly incompatible_sample_target_exit=43

fail() {
  printf 'sample-target-preflight error: %s\n' "$1" >&2
  exit 1
}

usage() {
  printf 'usage: %s --env-file PATH --compose-file PATH --project NAME\n' "${0##*/}" >&2
  exit 2
}

env_file=""
compose_file=""
project_name=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file) env_file="${2:-}"; shift 2 ;;
    --compose-file) compose_file="${2:-}"; shift 2 ;;
    --project) project_name="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

[ -f "$env_file" ] || fail "--env-file must name an existing file"
[ -f "$compose_file" ] || fail "--compose-file must name an existing file"
[[ "$project_name" =~ ^[a-z0-9][a-z0-9_-]{0,62}$ ]] || fail "--project has an unsafe shape"

compose=(docker compose --env-file "$env_file" -f "$compose_file" -p "$project_name")
[ -n "$("${compose[@]}" ps -q postgres)" ] || fail "the PostgreSQL service is not running"

postgres_sql() {
  local statement="$1"
  "${compose[@]}" exec -T postgres sh -c \
    'exec psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -AtF "|" -c "$1"' \
    _ "$statement"
}

table_state="$(postgres_sql "
  SELECT
    to_regclass('public.users') IS NOT NULL,
    to_regclass('public.salons') IS NOT NULL
")"
IFS='|' read -r users_present salons_present extra <<<"$table_state"
[ -z "${extra:-}" ] || fail "table-state query returned an unexpected shape"

if [ "$users_present" = "f" ] && [ "$salons_present" = "f" ]; then
  printf 'sample_target_preflight=empty\nuser_count=0\nsalon_count=0\n'
  exit 0
fi
if [ "$users_present" != "t" ] || [ "$salons_present" != "t" ]; then
  printf 'sample_target_preflight=incompatible\nreason=partial_identity_schema\n'
  exit "$incompatible_sample_target_exit"
fi

classification_columns="$(postgres_sql "
  SELECT count(*)
  FROM information_schema.columns
  WHERE table_schema='public'
    AND table_name IN ('users','salons')
    AND column_name='data_classification'
")"

case "$classification_columns" in
  0)
    counts="$(postgres_sql 'SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM salons)')"
    IFS='|' read -r user_count salon_count extra <<<"$counts"
    [ -z "${extra:-}" ] || fail "legacy count query returned an unexpected shape"
    if [ "$user_count" = "0" ] && [ "$salon_count" = "0" ]; then
      printf 'sample_target_preflight=empty_legacy_schema\nuser_count=0\nsalon_count=0\n'
      exit 0
    fi
    printf 'sample_target_preflight=incompatible\nreason=unclassified_existing_rows\nuser_count=%s\nsalon_count=%s\n' \
      "$user_count" "$salon_count"
    exit "$incompatible_sample_target_exit"
    ;;
  2)
    counts="$(postgres_sql "
      SELECT
        (SELECT count(*) FROM users WHERE data_classification='live'),
        (SELECT count(*) FROM salons WHERE data_classification='live'),
        (SELECT count(*) FROM users WHERE data_classification='sample_test'),
        (SELECT count(*) FROM salons WHERE data_classification='sample_test')
    ")"
    IFS='|' read -r live_users live_salons sample_users sample_salons extra <<<"$counts"
    [ -z "${extra:-}" ] || fail "classified count query returned an unexpected shape"
    if [ "$live_users" != "0" ] || [ "$live_salons" != "0" ]; then
      printf 'sample_target_preflight=incompatible\nreason=live_rows_present\nlive_user_count=%s\nlive_salon_count=%s\n' \
        "$live_users" "$live_salons"
      exit "$incompatible_sample_target_exit"
    fi
    if { [ "$sample_users" = "0" ] && [ "$sample_salons" = "0" ]; } || \
       { [ "$sample_users" = "3" ] && [ "$sample_salons" = "1" ]; }; then
      printf 'sample_target_preflight=compatible\nsample_user_count=%s\nsample_salon_count=%s\n' \
        "$sample_users" "$sample_salons"
      exit 0
    fi
    printf 'sample_target_preflight=incompatible\nreason=partial_sample_fixture\nsample_user_count=%s\nsample_salon_count=%s\n' \
      "$sample_users" "$sample_salons"
    exit "$incompatible_sample_target_exit"
    ;;
  *)
    printf 'sample_target_preflight=incompatible\nreason=partial_classification_schema\n'
    exit "$incompatible_sample_target_exit"
    ;;
esac
