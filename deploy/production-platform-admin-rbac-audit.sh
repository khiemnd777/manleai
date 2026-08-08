#!/bin/sh
set -eu

umask 077

if [ "$#" -gt 2 ]; then
  echo "Usage: $0 [project-env-path] <tenant-id>" >&2
  exit 1
fi

project_env=${1:-/opt/manleai/project.env}
tenant_id=${2:-}

if [ ! -f "$project_env" ] || [ -L "$project_env" ]; then
  echo "The production project env must be a regular, non-symlink file." >&2
  exit 1
fi
if ! printf '%s\n' "$tenant_id" | grep -Eq '^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$'; then
  echo "The production Platform Admin RBAC audit requires an exact UUID tenant ID." >&2
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
  sh -c 'exec psql -X -qAt -v ON_ERROR_STOP=1 -v target_salon_id="$1" --username "$POSTGRES_USER" --dbname "$POSTGRES_DB"' sh "$tenant_id" \
  > "$audit_output" <<'EOSQL'
BEGIN TRANSACTION READ ONLY;

WITH capability_matrix(ordinal, output_key, capability) AS (
  VALUES
    (1,  'platform_tenants_read', 'platform.tenants.read'),
    (2,  'platform_access_manage', 'platform.access.manage'),
    (3,  'platform_registration_requests_read', 'platform.registration_requests.read'),
    (4,  'platform_registration_requests_manage', 'platform.registration_requests.manage'),
    (5,  'platform_tenants_provision', 'platform.tenants.provision'),
    (6,  'business_read', 'business.read'),
    (7,  'business_write', 'business.write'),
    (8,  'technical_read', 'technical.read'),
    (9,  'technical_write', 'technical.write'),
    (10, 'operations_read', 'operations.read'),
    (11, 'operations_write', 'operations.write'),
    (12, 'audit_read', 'audit.read'),
    (13, 'services_read', 'services.read'),
    (14, 'services_write', 'services.write'),
    (15, 'training_read', 'training.read'),
    (16, 'training_write', 'training.write'),
    (17, 'calls_read', 'calls.read'),
    (18, 'calls_manage', 'calls.manage'),
    (19, 'calls_simulate', 'calls.simulate'),
    (20, 'calls_redact', 'calls.redact')
),
pii_matrix(ordinal, output_key, capability, pii_scope) AS (
  VALUES
    (1, 'customers_via_business_read', 'business.read', 'customers'),
    (2, 'calls_via_calls_read', 'calls.read', 'calls'),
    (3, 'appointments_via_business_read', 'business.read', 'appointments'),
    (4, 'notifications_via_operations_read', 'operations.read', 'notifications')
),
target AS (
  SELECT count(*)::bigint AS salon_exists
  FROM salons
  WHERE id = :'target_salon_id'::uuid
),
active_platform_admins AS (
  SELECT DISTINCT account.id
  FROM users AS account
  JOIN platform_role_assignments AS assignment
    ON assignment.user_id = account.id
   AND assignment.status = 'active'
  JOIN roles AS role
    ON role.id = assignment.role_id
   AND role.name = 'platform_admin'
  WHERE account.status = 'active'
    AND account.principal_scope = 'platform'
),
admin_counts AS (
  SELECT count(*)::bigint AS active_platform_admin_accounts
  FROM active_platform_admins
),
role_summary AS (
  SELECT
    count(DISTINCT permission.name)::bigint AS platform_admin_role_distinct_capabilities,
    count(DISTINCT permission.name) FILTER (
      WHERE matrix.capability IS NULL
    )::bigint AS platform_admin_role_unexpected_capabilities
  FROM roles AS role
  JOIN role_permissions AS role_permission ON role_permission.role_id = role.id
  JOIN permissions AS permission ON permission.id = role_permission.permission_id
  LEFT JOIN capability_matrix AS matrix ON matrix.capability = permission.name
  WHERE role.name = 'platform_admin'
),
capability_results AS (
  SELECT
    matrix.ordinal,
    matrix.output_key,
    count(admin.id) FILTER (
      WHERE public.app_platform_admin_capability(admin.id, matrix.capability)
    )::bigint AS capable_accounts
  FROM capability_matrix AS matrix
  LEFT JOIN active_platform_admins AS admin ON true
  GROUP BY matrix.ordinal, matrix.output_key
),
pii_results AS (
  SELECT
    matrix.ordinal,
    matrix.output_key,
    count(admin.id) FILTER (
      WHERE public.app_actor_feature_access(
        admin.id,
        :'target_salon_id'::uuid,
        matrix.capability,
        matrix.pii_scope
      )
    )::bigint AS capable_accounts
  FROM pii_matrix AS matrix
  LEFT JOIN active_platform_admins AS admin ON true
  GROUP BY matrix.ordinal, matrix.output_key
),
output AS (
  SELECT 1 AS ordinal, 'audit_scope=production_platform_admin_rbac_matrix' AS line
  UNION ALL
  SELECT 2, 'target_salon_id=' || :'target_salon_id'
  UNION ALL
  SELECT 3, 'salon_exists=' || target.salon_exists FROM target
  UNION ALL
  SELECT 4, 'active_platform_admin_accounts=' || admin_counts.active_platform_admin_accounts FROM admin_counts
  UNION ALL
  SELECT 5, 'expected_capabilities=20'
  UNION ALL
  SELECT 6, 'platform_admin_role_distinct_capabilities=' || role_summary.platform_admin_role_distinct_capabilities FROM role_summary
  UNION ALL
  SELECT 7, 'platform_admin_role_unexpected_capabilities=' || role_summary.platform_admin_role_unexpected_capabilities FROM role_summary
  UNION ALL
  SELECT 100 + result.ordinal, 'capability_' || result.output_key || '_accounts=' || result.capable_accounts
  FROM capability_results AS result
  UNION ALL
  SELECT 200 + result.ordinal, 'pii_' || result.output_key || '_accounts=' || result.capable_accounts
  FROM pii_results AS result
)
SELECT line
FROM output
ORDER BY ordinal;

COMMIT;
EOSQL

expected_keys='audit_scope
target_salon_id
salon_exists
active_platform_admin_accounts
expected_capabilities
platform_admin_role_distinct_capabilities
platform_admin_role_unexpected_capabilities
capability_platform_tenants_read_accounts
capability_platform_access_manage_accounts
capability_platform_registration_requests_read_accounts
capability_platform_registration_requests_manage_accounts
capability_platform_tenants_provision_accounts
capability_business_read_accounts
capability_business_write_accounts
capability_technical_read_accounts
capability_technical_write_accounts
capability_operations_read_accounts
capability_operations_write_accounts
capability_audit_read_accounts
capability_services_read_accounts
capability_services_write_accounts
capability_training_read_accounts
capability_training_write_accounts
capability_calls_read_accounts
capability_calls_manage_accounts
capability_calls_simulate_accounts
capability_calls_redact_accounts
pii_customers_via_business_read_accounts
pii_calls_via_calls_read_accounts
pii_appointments_via_business_read_accounts
pii_notifications_via_operations_read_accounts'
actual_keys="$(cut -d= -f1 "$audit_output")"
if [ "$actual_keys" != "$expected_keys" ]; then
  echo "The production Platform Admin RBAC audit returned an unexpected output contract." >&2
  exit 1
fi

while IFS='=' read -r key value; do
  case "$key" in
    audit_scope)
      if [ "$value" != "production_platform_admin_rbac_matrix" ]; then
        echo "The production Platform Admin RBAC audit scope is invalid." >&2
        exit 1
      fi
      ;;
    target_salon_id)
      if [ "$value" != "$tenant_id" ]; then
        echo "The production Platform Admin RBAC audit returned a different tenant ID." >&2
        exit 1
      fi
      ;;
    *)
      case "$value" in
        ''|*[!0-9]*)
          echo "The production Platform Admin RBAC audit returned a non-numeric count." >&2
          exit 1
          ;;
      esac
      ;;
  esac
done < "$audit_output"

cat "$audit_output"
