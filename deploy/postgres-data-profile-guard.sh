#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'data-profile-guard error: %s\n' "$1" >&2
  exit 1
}

usage() {
  printf 'usage: %s --profile {live|sample_test} --env-file PATH --compose-file PATH --project NAME\n' "${0##*/}" >&2
  exit 2
}

profile=""
env_file=""
compose_file=""
project_name=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --profile) profile="${2:-}"; shift 2 ;;
    --env-file) env_file="${2:-}"; shift 2 ;;
    --compose-file) compose_file="${2:-}"; shift 2 ;;
    --project) project_name="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

case "$profile" in live|sample_test) ;; *) fail "profile must be live or sample_test" ;; esac
[ -f "$env_file" ] || fail "--env-file must name an existing file"
[ -f "$compose_file" ] || fail "--compose-file must name an existing file"
[[ "$project_name" =~ ^[a-z0-9][a-z0-9_-]{0,62}$ ]] || fail "--project has an unsafe shape"

compose=(docker compose --env-file "$env_file" -f "$compose_file" -p "$project_name")
[ -n "$("${compose[@]}" ps -q postgres)" ] || fail "the PostgreSQL service is not running"

profile_query="
  SELECT
    (SELECT count(*) FROM users WHERE data_classification='sample_test'),
    (SELECT count(*) FROM salons WHERE data_classification='sample_test'),
    to_regclass('public.sample_data_migrations') IS NOT NULL
"
profile_state="$("${compose[@]}" exec -T postgres sh -c \
  'exec psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -AtF "|" -c "$1"' \
  _ "$profile_query")"
IFS='|' read -r sample_users sample_salons sample_ledger extra <<<"$profile_state"
[ -z "${extra:-}" ] || fail "profile query returned an unexpected shape"

case "$profile" in
  live)
    [ "$sample_users" = "0" ] || fail "live profile contains sample_test users"
    [ "$sample_salons" = "0" ] || fail "live profile contains sample_test salons"
    [ "$sample_ledger" = "f" ] || fail "live profile contains the sample fixture ledger"
    ;;
  sample_test)
    [ "$sample_users" = "3" ] || fail "sample_test profile must contain exactly three sample users"
    [ "$sample_salons" = "1" ] || fail "sample_test profile must contain exactly one sample salon"
    [ "$sample_ledger" = "t" ] || fail "sample_test profile is missing the sample fixture ledger"
    ;;
esac

printf 'data_profile=%s\nsample_user_count=%s\nsample_salon_count=%s\nsample_ledger_present=%s\n' \
  "$profile" "$sample_users" "$sample_salons" "$sample_ledger"
