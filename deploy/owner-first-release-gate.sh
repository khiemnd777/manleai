#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
manifest="$script_dir/owner-first-release-gate.manifest"

fail() {
  printf 'release-gate error: %s\n' "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command is unavailable: $1"
}

load_manifest() {
  [ -f "$manifest" ] || fail "release-gate manifest is missing"
  # shellcheck source=owner-first-release-gate.manifest
  source "$manifest"
  [ "${RELEASE_GATE_MANIFEST_VERSION:-}" = "1" ] || fail "unsupported release-gate manifest version"
  [[ "${OWNER_FIRST_MIGRATION_MIN_VERSION:-}" =~ ^[0-9]+$ ]] || fail "invalid Owner-first migration minimum"

  local array_name value_count
  for array_name in \
    BACKEND_RACE_PACKAGES \
    POSTGRES_MIGRATION_PACKAGES \
    POSTGRES_INTEGRATION_PACKAGES \
    SECURITY_CONTRACT_PACKAGES \
    SECURITY_CONTRACT_TEST_FILES \
    WEB_APPLICATIONS; do
    declare -p "$array_name" >/dev/null 2>&1 || fail "manifest array is missing: $array_name"
    eval "value_count=\${#$array_name[@]}"
    [ "$value_count" -gt 0 ] || fail "manifest array is empty: $array_name"
  done
}

validate_manifest_paths() {
  load_manifest
  local path package app
  for path in "${SECURITY_CONTRACT_TEST_FILES[@]}"; do
    [ -f "$repo_root/$path" ] || fail "security contract test file is missing: $path"
  done
  for package in \
    "${BACKEND_RACE_PACKAGES[@]}" \
    "${POSTGRES_MIGRATION_PACKAGES[@]}" \
    "${POSTGRES_INTEGRATION_PACKAGES[@]}" \
    "${SECURITY_CONTRACT_PACKAGES[@]}"; do
    [ -d "$repo_root/backend/${package#./}" ] || fail "backend package is missing: $package"
  done
  for app in "${WEB_APPLICATIONS[@]}"; do
    [ -f "$repo_root/$app/package-lock.json" ] || fail "web lockfile is missing: $app/package-lock.json"
    [ -f "$repo_root/$app/package.json" ] || fail "web package is missing: $app/package.json"
  done
}

require_database_identity() {
  : "${PGHOST:?PGHOST is required}"
  : "${PGPORT:?PGPORT is required}"
  : "${PGUSER:?PGUSER is required}"
  : "${PGDATABASE:?PGDATABASE is required}"
  [[ "$PGHOST" =~ ^[A-Za-z0-9._-]+$ ]] || fail "PGHOST has an unsafe shape"
  [[ "$PGPORT" =~ ^[0-9]{1,5}$ ]] || fail "PGPORT has an unsafe shape"
  [[ "$PGUSER" =~ ^[A-Za-z_][A-Za-z0-9_]{0,62}$ ]] || fail "PGUSER has an unsafe shape"
  [[ "$PGDATABASE" =~ ^[A-Za-z_][A-Za-z0-9_]{0,62}$ ]] || fail "PGDATABASE has an unsafe shape"
  [ "$PGDATABASE" != "postgres" ] || fail "the release gate requires a dedicated non-default database"
  [[ "$PGDATABASE" == *release_gate* ]] || fail "the dedicated database name must contain release_gate"
}

configure_database_urls() {
  require_database_identity
  local database_url="postgres://${PGUSER}@${PGHOST}:${PGPORT}/${PGDATABASE}?sslmode=disable"
  export TEST_DATABASE_URL="$database_url"
  export MIGRATION_TEST_DATABASE_URL="$database_url"
  export POS_TAXONOMY_TEST_DATABASE_URL="$database_url"
  : "${TEST_DATABASE_URL:?TEST_DATABASE_URL is required}"
  : "${MIGRATION_TEST_DATABASE_URL:?MIGRATION_TEST_DATABASE_URL is required}"
}

build_clone_database_name() {
  local base_database="$1"
  local suite="$2"
  local index="$3"
  local process_id="$4"
  local clone_database="${base_database:0:18}_release_gate_rg_${suite}_${index}_${process_id}"
  [[ "$clone_database" =~ ^[A-Za-z_][A-Za-z0-9_]{0,62}$ ]] || fail "generated test database name is unsafe"
  [[ "$clone_database" == *release_gate* ]] || fail "generated test database lost the release_gate marker"
  printf '%s\n' "$clone_database"
}

require_redis_test_url() {
  : "${TEST_REDIS_URL:?TEST_REDIS_URL is required}"
  case "$TEST_REDIS_URL" in
    redis://*|rediss://*) ;;
    *) fail "TEST_REDIS_URL must use redis:// or rediss://" ;;
  esac
}

run_go_packages_serially() {
  local package
  for package in "$@"; do
    printf 'release-gate: testing package %s\n' "$package"
    go test -p 1 -count=1 -timeout=20m "$package"
  done
}

active_clone_database=""

cleanup_active_clone() {
  local clone_database
  if [ -z "$active_clone_database" ]; then
    return
  fi
  clone_database="$active_clone_database"
  active_clone_database=""
  if ! dropdb \
    --host="$PGHOST" \
    --port="$PGPORT" \
    --username="$PGUSER" \
    "$clone_database"; then
    printf 'release-gate error: failed to remove isolated package database\n' >&2
    return 1
  fi
}

run_go_packages_isolated() {
  local suite="$1"
  shift
  local base_database="$PGDATABASE"
  local package clone_database package_status index=0
  for package in "$@"; do
    index=$((index + 1))
    clone_database="$(build_clone_database_name "$base_database" "$suite" "$index" "$$")"
    printf 'release-gate: creating isolated database for package %s\n' "$package"
    createdb \
      --host="$PGHOST" \
      --port="$PGPORT" \
      --username="$PGUSER" \
      --template="$base_database" \
      "$clone_database"
    active_clone_database="$clone_database"
    export PGDATABASE="$clone_database"
    configure_database_urls
    printf 'release-gate: testing package %s\n' "$package"
    set +e
    go test -p 1 -count=1 -timeout=20m "$package"
    package_status=$?
    set -e
    export PGDATABASE="$base_database"
    configure_database_urls
    cleanup_active_clone
    if [ "$package_status" -ne 0 ]; then
      return "$package_status"
    fi
  done
}

run_backend_gate() {
  validate_manifest_paths
  require_command go
  cd "$repo_root/backend"
  printf 'release-gate: running complete backend test suite\n'
  go test ./...
  printf 'release-gate: running complete backend vet suite\n'
  go vet ./...
  printf 'release-gate: running bounded high-risk race suite\n'
  go test -race -p 1 -count=1 -timeout=25m "${BACKEND_RACE_PACKAGES[@]}"
}

assert_fresh_database() {
  local migration_table user_table_count
  migration_table="$(psql -X -v ON_ERROR_STOP=1 -Atqc "SELECT to_regclass('public.app_schema_migrations') IS NULL")"
  [ "$migration_table" = "t" ] || fail "release-gate database is not fresh"
  user_table_count="$(psql -X -v ON_ERROR_STOP=1 -Atqc "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'")"
  [ "$user_table_count" = "0" ] || fail "release-gate database contains public tables before migration"
}

verify_owner_first_migrations() {
  local latest_version version applied
  local -a migration_versions=()
  while IFS= read -r version; do
    migration_versions[${#migration_versions[@]}]="$version"
  done < <(find "$repo_root/backend/migrations" -maxdepth 1 -type f -name 'V[0-9]*__*.sql' -print \
    | sed -E 's#.*/V([0-9]+)__.*#\1#' \
    | sort -n)
  [ "${#migration_versions[@]}" -gt 0 ] || fail "could not resolve migration versions"
  local latest_index
  latest_index=$((${#migration_versions[@]} - 1))
  latest_version="${migration_versions[$latest_index]}"
  [[ "$latest_version" =~ ^[0-9]+$ ]] || fail "could not resolve latest migration version"
  [ "$latest_version" -ge "$OWNER_FIRST_MIGRATION_MIN_VERSION" ] || fail "latest migration predates Owner-first"
  for version in "${migration_versions[@]}"; do
    if [ "$version" -lt "$OWNER_FIRST_MIGRATION_MIN_VERSION" ]; then
      continue
    fi
    applied="$(psql -X -v ON_ERROR_STOP=1 -Atqc "SELECT count(*) FROM app_schema_migrations WHERE version = '$version'")"
    [ "$applied" = "1" ] || fail "required Owner-first migration V$version was not applied exactly once"
  done
}

run_postgres_gate() {
  validate_manifest_paths
  require_command go
  require_command psql
  require_command createdb
  require_command dropdb
  configure_database_urls
  assert_fresh_database
  cd "$repo_root/backend"
  printf 'release-gate: applying the fresh migration chain twice and checking checksums\n'
  go test -p 1 -count=1 -timeout=20m ./internal/database -run '^TestMigrateAppliesForwardMigrationOnceWithoutChangingAppliedChecksums$'
  verify_owner_first_migrations
  printf 'release-gate: running migration contract packages in isolated databases\n'
  run_go_packages_isolated migration "${POSTGRES_MIGRATION_PACKAGES[@]}"
  printf 'release-gate: running PostgreSQL integration packages serially in isolated databases\n'
  run_go_packages_isolated integration "${POSTGRES_INTEGRATION_PACKAGES[@]}"
}

run_security_contract() {
  validate_manifest_paths
  require_command go
  require_command psql
  require_command createdb
  require_command dropdb
  require_redis_test_url
  configure_database_urls
  verify_owner_first_migrations
  cd "$repo_root/backend"
  printf 'release-gate: running tenant, secret, evidence, privacy, and callback-signature contract\n'
  run_go_packages_isolated security "${SECURITY_CONTRACT_PACKAGES[@]}"
}

run_self_test() {
  local clone_database
  validate_manifest_paths
  bash -n "$script_dir/owner-first-release-gate.sh"
  bash -n "$script_dir/local-restart.sh"
  bash -n "$script_dir/postgres-migration-preflight.sh"
  bash -n "$script_dir/postgres-sample-target-preflight.sh"
  bash -n "$script_dir/postgres-data-profile-guard.sh"
  bash -n "$manifest"
  clone_database="$(build_clone_database_name "manleai_phase10_release_gate_database_with_a_long_name" "integration" "19" "12345")"
  [[ "$clone_database" == *release_gate* ]] || fail "clone database self-test lost the release_gate marker"
  if (
    unset PGHOST PGPORT PGUSER PGDATABASE
    require_database_identity >/dev/null 2>&1
  ); then
    fail "database identity guard accepted missing configuration"
  fi
  if (
    PGHOST=localhost
    PGPORT=5432
    PGUSER=release_gate
    PGDATABASE=development
    require_database_identity >/dev/null 2>&1
  ); then
    fail "database identity guard accepted a database without the release_gate marker"
  fi
  if (
    unset TEST_REDIS_URL
    require_redis_test_url >/dev/null 2>&1
  ); then
    fail "Redis test URL guard accepted missing configuration"
  fi
  printf 'release-gate: manifest and fail-closed environment guards are valid\n'
}

usage() {
  printf 'usage: %s {self-test|backend|postgres|security}\n' "${0##*/}" >&2
  exit 2
}

trap cleanup_active_clone EXIT
trap 'cleanup_active_clone; exit 130' INT
trap 'cleanup_active_clone; exit 143' TERM

case "${1:-}" in
  self-test) run_self_test ;;
  backend) run_backend_gate ;;
  postgres) run_postgres_gate ;;
  security) run_security_contract ;;
  *) usage ;;
esac
