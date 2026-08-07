#!/bin/sh
set -eu

umask 077

if [ "$#" -gt 1 ]; then
  echo "Usage: $0 [project-env-path]" >&2
  exit 1
fi

project_env=${1:-/opt/manleai/project.env}
if [ ! -f "$project_env" ] || [ -L "$project_env" ]; then
  echo "The production project env must be a regular, non-symlink file." >&2
  exit 1
fi

set -a
. "$project_env"
set +a

: "${POSTGRES_USER:?POSTGRES_USER is required}"
: "${POSTGRES_DB:?POSTGRES_DB is required}"

if [ "${COMPOSE_PROJECT_NAME:-}" != "manleai" ]; then
  echo "The production Compose project must be exactly manleai." >&2
  exit 1
fi

case "$POSTGRES_USER" in
  ''|[0-9]*|*[!A-Za-z0-9_]*)
    echo "POSTGRES_USER is not a valid PostgreSQL identifier." >&2
    exit 1
    ;;
esac
case "$POSTGRES_DB" in
  ''|[0-9]*|*[!A-Za-z0-9_]*)
    echo "POSTGRES_DB is not a valid PostgreSQL identifier." >&2
    exit 1
    ;;
esac
if [ "${#POSTGRES_USER}" -gt 63 ] || [ "${#POSTGRES_DB}" -gt 63 ]; then
  echo "A PostgreSQL identifier exceeds the 63-byte limit." >&2
  exit 1
fi

command -v docker >/dev/null 2>&1
docker info >/dev/null

postgres_container_ids="$(docker ps \
  --filter 'label=com.docker.compose.project=manleai' \
  --filter 'label=com.docker.compose.service=postgres' \
  --format '{{.ID}}')"

old_ifs=$IFS
IFS='
'
set -- $postgres_container_ids
IFS=$old_ifs
if [ "$#" -ne 1 ]; then
  echo "Expected exactly one running ManleAI PostgreSQL container." >&2
  exit 1
fi
postgres_container=$1

postgres_health="$(docker inspect \
  --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' \
  "$postgres_container")"
if [ "$postgres_health" != "healthy" ]; then
  echo "The production PostgreSQL container is not healthy." >&2
  exit 1
fi

audit_output="$(mktemp)"
cleanup() {
  rm -f "$audit_output"
}
trap cleanup EXIT HUP INT TERM

readonly_options='-c default_transaction_read_only=on -c statement_timeout=15000 -c lock_timeout=3000'
docker exec -i \
  -e POSTGRES_USER \
  -e POSTGRES_DB \
  -e "PGOPTIONS=$readonly_options" \
  "$postgres_container" \
  sh -c 'exec psql -X -qAt -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB"' \
  > "$audit_output" <<'EOSQL'
WITH account_counts AS (
  SELECT
    count(*) AS total_accounts,
    count(*) FILTER (WHERE status = 'active') AS active_accounts,
    count(*) FILTER (WHERE status = 'disabled') AS disabled_accounts,
    count(*) FILTER (WHERE status = 'invited') AS invited_accounts,
    count(*) FILTER (WHERE principal_scope = 'tenant') AS tenant_accounts,
    count(*) FILTER (WHERE principal_scope = 'platform') AS platform_accounts,
    count(*) FILTER (
      WHERE status = 'active' AND principal_scope = 'tenant'
    ) AS active_tenant_accounts,
    count(*) FILTER (
      WHERE status = 'active' AND principal_scope = 'platform'
    ) AS active_platform_accounts
  FROM users
),
platform_admin_counts AS (
  SELECT count(DISTINCT account.id) AS active_platform_admin_accounts
  FROM users AS account
  JOIN platform_role_assignments AS assignment
    ON assignment.user_id = account.id
  JOIN roles AS role
    ON role.id = assignment.role_id
  WHERE account.status = 'active'
    AND account.principal_scope = 'platform'
    AND assignment.status = 'active'
    AND role.name = 'platform_admin'
),
legacy_admin_counts AS (
  SELECT
    count(DISTINCT account.id) AS legacy_super_admin_accounts,
    count(DISTINCT account.id) FILTER (
      WHERE account.status = 'active'
    ) AS active_legacy_super_admin_accounts
  FROM users AS account
  JOIN user_roles AS assignment
    ON assignment.user_id = account.id
  JOIN roles AS role
    ON role.id = assignment.role_id
  WHERE role.name = 'super_admin'
)
SELECT output.line
FROM account_counts AS accounts
CROSS JOIN platform_admin_counts AS platform_admins
CROSS JOIN legacy_admin_counts AS legacy_admins
CROSS JOIN LATERAL (
  VALUES
    (1, 'audit_scope=production_account_admin_counts'),
    (2, 'total_accounts=' || accounts.total_accounts),
    (3, 'active_accounts=' || accounts.active_accounts),
    (4, 'disabled_accounts=' || accounts.disabled_accounts),
    (5, 'invited_accounts=' || accounts.invited_accounts),
    (6, 'tenant_accounts=' || accounts.tenant_accounts),
    (7, 'platform_accounts=' || accounts.platform_accounts),
    (8, 'active_tenant_accounts=' || accounts.active_tenant_accounts),
    (9, 'active_platform_accounts=' || accounts.active_platform_accounts),
    (10, 'active_platform_admin_accounts=' || platform_admins.active_platform_admin_accounts),
    (11, 'legacy_super_admin_accounts=' || legacy_admins.legacy_super_admin_accounts),
    (12, 'active_legacy_super_admin_accounts=' || legacy_admins.active_legacy_super_admin_accounts)
) AS output(ordinal, line)
ORDER BY output.ordinal;
EOSQL

expected_keys='audit_scope
total_accounts
active_accounts
disabled_accounts
invited_accounts
tenant_accounts
platform_accounts
active_tenant_accounts
active_platform_accounts
active_platform_admin_accounts
legacy_super_admin_accounts
active_legacy_super_admin_accounts'
actual_keys="$(cut -d= -f1 "$audit_output")"
if [ "$actual_keys" != "$expected_keys" ]; then
  echo "The production account audit returned an unexpected output contract." >&2
  exit 1
fi

while IFS='=' read -r key value; do
  case "$key" in
    audit_scope)
      if [ "$value" != "production_account_admin_counts" ]; then
        echo "The production account audit scope is invalid." >&2
        exit 1
      fi
      ;;
    *)
      case "$value" in
        ''|*[!0-9]*)
          echo "The production account audit returned a non-numeric count." >&2
          exit 1
          ;;
      esac
      ;;
  esac
done < "$audit_output"

cat "$audit_output"
