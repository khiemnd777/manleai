#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
env_file="$repo_root/.env"
compose_file="$repo_root/docker-compose.yml"

fail() {
  printf 'local-integration-test error: %s\n' "$1" >&2
  exit 1
}

for command_name in docker go psql createdb dropdb; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[ -f "$env_file" ] || fail "local .env is missing; run make restart first"
docker info >/dev/null 2>&1 || fail "Docker is not running"

compose=(docker compose --env-file "$env_file" -f "$compose_file")
project_name="$("${compose[@]}" config | awk '$1 == "name:" { print $2; exit }')"
[[ "$project_name" =~ ^[a-z0-9][a-z0-9_-]{0,62}$ ]] || fail "Compose project name has an unsafe shape"

"${compose[@]}" up -d --wait postgres redis

postgres_volumes=()
while IFS= read -r volume_name; do
  [ -n "$volume_name" ] && postgres_volumes+=("$volume_name")
done < <(docker volume ls --quiet \
  --filter "label=com.docker.compose.project=$project_name" \
  --filter 'label=com.docker.compose.volume=postgres_data')
[ "${#postgres_volumes[@]}" -eq 1 ] || fail "expected exactly one Compose-owned local PostgreSQL volume"

postgres_user="$("${compose[@]}" exec -T postgres sh -c 'printf %s "$POSTGRES_USER"')"
postgres_password="$("${compose[@]}" exec -T postgres sh -c 'printf %s "$POSTGRES_PASSWORD"')"
application_database="$("${compose[@]}" exec -T postgres sh -c 'printf %s "$POSTGRES_DB"')"
[[ "$postgres_user" =~ ^[A-Za-z_][A-Za-z0-9_]{0,62}$ ]] || fail "local PostgreSQL user has an unsafe shape"
[[ "$application_database" =~ ^[A-Za-z_][A-Za-z0-9_]{0,62}$ ]] || fail "local application database has an unsafe shape"

postgres_port_binding="$("${compose[@]}" port postgres 5432)"
redis_port_binding="$("${compose[@]}" port redis 6379)"
postgres_port="${postgres_port_binding##*:}"
redis_port="${redis_port_binding##*:}"
[[ "$postgres_port" =~ ^[0-9]{1,5}$ ]] || fail "could not resolve the local PostgreSQL port"
[[ "$redis_port" =~ ^[0-9]{1,5}$ ]] || fail "could not resolve the local Redis port"

export PGHOST=127.0.0.1
export PGPORT="$postgres_port"
export PGUSER="$postgres_user"
export PGPASSWORD="$postgres_password"
export TEST_REDIS_URL="redis://127.0.0.1:$redis_port/1"

application_profile() {
  psql -X -v ON_ERROR_STOP=1 \
    --dbname="$application_database" \
    --tuples-only \
    --no-align \
    --field-separator='|' \
    --command "
      SELECT
        (SELECT count(*) FROM users WHERE data_classification='live'),
        (SELECT count(*) FROM salons WHERE data_classification='live'),
        (SELECT count(*) FROM users WHERE data_classification='sample_test'),
        (SELECT count(*) FROM salons WHERE data_classification='sample_test');
    " | tr -d '[:space:]'
}

before_profile="$(application_profile)"
test_database="manleai_local_release_gate_$$_$(date +%s)"
[[ "$test_database" =~ ^[A-Za-z_][A-Za-z0-9_]{0,62}$ ]] || fail "generated test database name is unsafe"
[ "$test_database" != "$application_database" ] || fail "test database must differ from the application database"

test_database_active=false
cleanup_test_database() {
  if [ "$test_database_active" != true ]; then
    return
  fi
  test_database_active=false
  dropdb --if-exists "$test_database"
}
trap cleanup_test_database EXIT
trap 'cleanup_test_database; exit 130' INT
trap 'cleanup_test_database; exit 143' TERM

createdb "$test_database"
test_database_active=true
export PGDATABASE="$test_database"

printf 'local-integration-test: running in isolated database %s\n' "$test_database"
set +e
bash "$script_dir/owner-first-release-gate.sh" postgres
test_status=$?
set -e

cleanup_test_database
after_profile="$(application_profile)"
[ "$after_profile" = "$before_profile" ] || fail "application database counts changed during isolated integration tests"

if [ "$test_status" -ne 0 ]; then
  exit "$test_status"
fi
printf 'local-integration-test: passed; application database profile remained %s\n' "$after_profile"
