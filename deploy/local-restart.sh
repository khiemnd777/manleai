#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
env_file="$repo_root/.env"
credentials_dir="$repo_root/.local"
credentials_file="$credentials_dir/sample-data.env"
compose_file="$repo_root/docker-compose.yml"
migrations_dir="$repo_root/backend/migrations"
environment_contract="$script_dir/local-environment-contract.sh"

fail() {
  printf 'local-restart error: %s\n' "$1" >&2
  exit 1
}

[ -f "$environment_contract" ] || fail "local environment contract is missing"
# shellcheck source=local-environment-contract.sh
source "$environment_contract"

env_file_value() {
  local key="$1"
  awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); print; exit }' "$env_file"
}

resolved_local_value() {
  local key="$1"
  local fallback="$2"
  local value="${!key:-}"
  if [ -z "$value" ]; then
    value="$(env_file_value "$key")"
  fi
  if [ -z "$value" ]; then
    value="$fallback"
  fi
  printf '%s\n' "$value"
}

require_local_postgres_url() {
  local label="$1"
  local value="$2"
  case "$value" in
    postgres://*@postgres:*/*|postgresql://*@postgres:*/*) ;;
    *) fail "$label must target the local Compose postgres service" ;;
  esac
}

validate_local_environment() {
  local app_env database_url migration_database_url redis_url frontend_url public_api_url
  app_env="$(resolved_local_value APP_ENV local)"
  case "$app_env" in local|production|auto) ;; *) fail "APP_ENV must be local, production, or auto" ;; esac

  database_url="$(resolved_local_value DATABASE_URL 'postgres://ai_receptionist:ai_receptionist@postgres:5432/ai_receptionist?sslmode=disable')"
  migration_database_url="$(resolved_local_value MIGRATION_DATABASE_URL '')"
  redis_url="$(resolved_local_value REDIS_URL 'redis://redis:6379/0')"
  frontend_url="$(resolved_local_value FRONTEND_URL 'http://localhost:3088')"
  public_api_url="$(resolved_local_value NEXT_PUBLIC_API_BASE_URL 'http://localhost:18089')"

  require_local_postgres_url DATABASE_URL "$database_url"
  if [ -n "$migration_database_url" ]; then
    require_local_postgres_url MIGRATION_DATABASE_URL "$migration_database_url"
  fi
  case "$redis_url" in redis://redis:*/*) ;; *) fail "REDIS_URL must target the local Compose redis service" ;; esac
  case "$frontend_url" in http://localhost:*|http://127.0.0.1:*) ;; *) fail "FRONTEND_URL must remain on localhost" ;; esac
  case "$public_api_url" in http://localhost:*|http://127.0.0.1:*) ;; *) fail "NEXT_PUBLIC_API_BASE_URL must remain on localhost" ;; esac

  printf '%s\n' "$app_env"
}

validate_rendered_landing_api_origin() {
  local landing_container_id landing_api_url compose_services validation_output
  landing_container_id="$("${compose[@]}" ps -a -q landing)"
  [ -n "$landing_container_id" ] || fail "rendered landing container is unavailable for environment validation"
  landing_api_url="$(docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$landing_container_id" | awk -F= '$1 == "LANDING_API_BASE_URL" { sub(/^[^=]*=/, ""); print; exit }')"
  compose_services="$("${compose[@]}" config --services)"
  if ! validation_output="$(validate_compose_service_http_origin LANDING_API_BASE_URL "$landing_api_url" "$compose_services" 2>&1)"; then
    fail "$validation_output"
  fi
}

verify_landing_api_health() {
  if ! "${compose[@]}" exec -T landing node -e '
const origin = process.env.LANDING_API_BASE_URL;
if (!origin) throw new Error("LANDING_API_BASE_URL is missing");
(async () => {
  const response = await fetch(new URL("/healthz", origin), {
    headers: { Accept: "application/json" },
    signal: AbortSignal.timeout(5000)
  });
  if (!response.ok) throw new Error(`landing API health returned ${response.status}`);
})().catch((error) => {
  console.error(error instanceof Error ? error.message : "landing API health failed");
  process.exit(1);
});
'; then
    fail "landing cannot reach its rendered Compose API origin"
  fi
}

command -v docker >/dev/null 2>&1 || fail "docker is required"
command -v openssl >/dev/null 2>&1 || fail "openssl is required"
docker info >/dev/null 2>&1 || fail "Docker is not running"

if [ ! -f "$env_file" ]; then
  jwt_secret="$(openssl rand -hex 48)"
  encryption_key="$(openssl rand -base64 32 | tr -d '\n')"
  temporary_env="$(mktemp "$repo_root/.env.tmp.XXXXXX")"
  trap 'rm -f "$temporary_env"' EXIT
  awk -v jwt="$jwt_secret" -v encryption="$encryption_key" '
    /^JWT_SECRET=/ { print "JWT_SECRET=" jwt; next }
    /^TOKEN_ENCRYPTION_KEY_BASE64=/ { print "TOKEN_ENCRYPTION_KEY_BASE64=" encryption; next }
    { print }
  ' "$repo_root/.env.example" > "$temporary_env"
  chmod 600 "$temporary_env"
  mv "$temporary_env" "$env_file"
  trap - EXIT
  printf 'Created private local infrastructure configuration at %s\n' "$env_file"
fi

compose=(docker compose --env-file "$env_file" -f "$compose_file")
project_name="$("${compose[@]}" config | awk '$1 == "name:" { print $2; exit }')"
[[ "$project_name" =~ ^[a-z0-9][a-z0-9_-]{0,62}$ ]] || fail "Compose project name has an unsafe shape"
resolved_app_env="$(validate_local_environment)"

if [ ! -f "$credentials_file" ]; then
  mkdir -p "$credentials_dir"
  chmod 700 "$credentials_dir"
  temporary_credentials="$(mktemp "$credentials_dir/sample-data.env.tmp.XXXXXX")"
  trap 'rm -f "$temporary_credentials"' EXIT
  {
    printf "SAMPLE_PLATFORM_ADMIN_EMAIL='%s'\n" 'admin@sample.manleai.test'
    printf "SAMPLE_PLATFORM_ADMIN_NAME='%s'\n" 'Sample Platform Admin'
    printf "SAMPLE_PLATFORM_ADMIN_PASSWORD='%s'\n" "$(openssl rand -hex 24)"
    printf "SAMPLE_PLATFORM_OPS_EMAIL='%s'\n" 'ops@sample.manleai.test'
    printf "SAMPLE_PLATFORM_OPS_NAME='%s'\n" 'Sample Platform Ops'
    printf "SAMPLE_PLATFORM_OPS_PASSWORD='%s'\n" "$(openssl rand -hex 24)"
    printf "SAMPLE_TENANT_OWNER_PASSWORD='%s'\n" "$(openssl rand -hex 24)"
  } > "$temporary_credentials"
  chmod 600 "$temporary_credentials"
  mv "$temporary_credentials" "$credentials_file"
  trap - EXIT
  printf 'Created private sample credentials at %s\n' "$credentials_file"
fi

chmod 600 "$credentials_file"
set -a
# shellcheck source=/dev/null
source "$credentials_file"
set +a
for required_name in \
  SAMPLE_PLATFORM_ADMIN_EMAIL SAMPLE_PLATFORM_ADMIN_NAME SAMPLE_PLATFORM_ADMIN_PASSWORD \
  SAMPLE_PLATFORM_OPS_EMAIL SAMPLE_PLATFORM_OPS_NAME SAMPLE_PLATFORM_OPS_PASSWORD \
  SAMPLE_TENANT_OWNER_PASSWORD; do
  [ -n "${!required_name:-}" ] || fail "$required_name is missing from $credentials_file"
done

reset_local_postgres_volume() {
  local volume_name
  local volumes=()
  "${compose[@]}" down --remove-orphans
  while IFS= read -r volume_name; do
    [ -n "$volume_name" ] && volumes+=("$volume_name")
  done < <(docker volume ls --quiet \
    --filter "label=com.docker.compose.project=$project_name" \
    --filter 'label=com.docker.compose.volume=postgres_data')
  [ "${#volumes[@]}" -eq 1 ] || fail "expected exactly one Compose-owned PostgreSQL volume, found ${#volumes[@]}"
  docker volume rm "${volumes[0]}"
  printf 'Reset incompatible local sample PostgreSQL volume %s exactly once.\n' "${volumes[0]}"
}

run_migration_preflight() {
  bash "$script_dir/postgres-migration-preflight.sh" \
    --env-file "$env_file" \
    --compose-file "$compose_file" \
    --project "$project_name" \
    --migrations-dir "$migrations_dir"
}

cd "$repo_root"
"${compose[@]}" down --remove-orphans
"${compose[@]}" build
"${compose[@]}" create landing >/dev/null
validate_rendered_landing_api_origin
"${compose[@]}" up -d --wait postgres redis

set +e
preflight_output="$(run_migration_preflight 2>&1)"
preflight_status=$?
set -e
printf '%s\n' "$preflight_output"
if [ "$preflight_status" -eq 42 ]; then
  reset_local_postgres_volume
  "${compose[@]}" up -d --wait postgres redis
  run_migration_preflight
elif [ "$preflight_status" -ne 0 ]; then
  exit "$preflight_status"
fi

"${compose[@]}" up -d --wait api
"${compose[@]}" run --rm --no-deps \
  -e SAMPLE_PLATFORM_ADMIN_PASSWORD \
  -e SAMPLE_PLATFORM_OPS_PASSWORD \
  -e SAMPLE_TENANT_OWNER_PASSWORD \
  api /bin/sample-data apply \
  --profile sample_test \
  --confirm APPLY_SAMPLE_TEST_DATA \
  --admin-email "$SAMPLE_PLATFORM_ADMIN_EMAIL" \
  --admin-name "$SAMPLE_PLATFORM_ADMIN_NAME" \
  --ops-email "$SAMPLE_PLATFORM_OPS_EMAIL" \
  --ops-name "$SAMPLE_PLATFORM_OPS_NAME"

unset SAMPLE_PLATFORM_ADMIN_PASSWORD SAMPLE_PLATFORM_OPS_PASSWORD SAMPLE_TENANT_OWNER_PASSWORD
"${compose[@]}" up -d --wait
verify_landing_api_health
bash "$script_dir/postgres-data-profile-guard.sh" \
  --profile sample_test \
  --env-file "$env_file" \
  --compose-file "$compose_file" \
  --project "$project_name"

printf 'Local stack is healthy (deployment_env=local app_env=%s). Sample credentials remain in %s (mode 600).\n' "$resolved_app_env" "$credentials_file"
