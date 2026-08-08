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
  echo "The production tenant catalog audit requires an exact UUID tenant ID." >&2
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

WITH target AS (
  SELECT
    count(*)::bigint AS salon_exists,
    COALESCE(max(salon.data_classification::text), 'none') AS data_classification,
    COALESCE(max(NULLIF(btrim(salon.active_pos_provider), '')), 'none') AS active_pos_provider,
    COALESCE(max(settings.scheduling_authority), 'none') AS scheduling_authority
  FROM salons AS salon
  LEFT JOIN salon_settings AS settings ON settings.salon_id = salon.id
  WHERE salon.id = :'target_salon_id'::uuid
),
catalog_counts AS (
  SELECT
    count(*)::bigint AS services_total,
    count(*) FILTER (WHERE service.active AND service.archived_at IS NULL) AS services_active,
    count(*) FILTER (WHERE service.archived_at IS NOT NULL) AS services_archived,
    count(*) FILTER (WHERE service.ai_bookable AND service.active AND service.archived_at IS NULL) AS services_ai_bookable,
    count(*) FILTER (
      WHERE btrim(service.pos_provider) = NULLIF(target.active_pos_provider, 'none')
    ) AS services_matching_active_provider
  FROM services AS service
  CROSS JOIN target
  WHERE service.salon_id = :'target_salon_id'::uuid
),
category_counts AS (
  SELECT
    count(*)::bigint AS service_categories_total,
    count(*) FILTER (WHERE category.status = 'active') AS service_categories_active
  FROM service_categories AS category
  WHERE category.salon_id = :'target_salon_id'::uuid
),
service_alias_counts AS (
  SELECT
    count(*)::bigint AS service_aliases_total,
    count(*) FILTER (WHERE alias.status = 'active') AS service_aliases_active
  FROM service_aliases AS alias
  WHERE alias.salon_id = :'target_salon_id'::uuid
),
category_alias_counts AS (
  SELECT
    count(*)::bigint AS service_category_aliases_total,
    count(*) FILTER (WHERE alias.status = 'active') AS service_category_aliases_active
  FROM service_category_aliases AS alias
  WHERE alias.salon_id = :'target_salon_id'::uuid
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
authorization_counts AS (
  SELECT
    count(*)::bigint AS active_platform_admin_accounts,
    count(*) FILTER (
      WHERE public.app_platform_admin_capability(admin.id, 'services.read')
    ) AS platform_admin_services_read_capable_accounts,
    count(*) FILTER (
      WHERE public.app_active_support_authorization(
        admin.id, :'target_salon_id'::uuid, 'services.read'
      )
    ) AS support_services_read_authorized_admin_accounts,
    count(*) FILTER (
      WHERE public.app_actor_feature_access(
        admin.id, :'target_salon_id'::uuid, 'services.read'
      )
    ) AS actor_feature_services_read_authorized_admin_accounts
  FROM active_platform_admins AS admin
)
SELECT output.line
FROM target
CROSS JOIN catalog_counts AS catalog
CROSS JOIN category_counts AS categories
CROSS JOIN service_alias_counts AS service_aliases
CROSS JOIN category_alias_counts AS category_aliases
CROSS JOIN authorization_counts AS auth_counts
CROSS JOIN LATERAL (
  VALUES
    (1, 'audit_scope=production_tenant_catalog'),
    (2, 'target_salon_id=' || :'target_salon_id'),
    (3, 'salon_exists=' || target.salon_exists),
    (4, 'data_classification=' || target.data_classification),
    (5, 'active_pos_provider=' || target.active_pos_provider),
    (6, 'scheduling_authority=' || target.scheduling_authority),
    (7, 'services_total=' || catalog.services_total),
    (8, 'services_active=' || catalog.services_active),
    (9, 'services_archived=' || catalog.services_archived),
    (10, 'services_ai_bookable=' || catalog.services_ai_bookable),
    (11, 'services_matching_active_provider=' || catalog.services_matching_active_provider),
    (12, 'service_categories_total=' || categories.service_categories_total),
    (13, 'service_categories_active=' || categories.service_categories_active),
    (14, 'service_aliases_total=' || service_aliases.service_aliases_total),
    (15, 'service_aliases_active=' || service_aliases.service_aliases_active),
    (16, 'service_category_aliases_total=' || category_aliases.service_category_aliases_total),
    (17, 'service_category_aliases_active=' || category_aliases.service_category_aliases_active),
    (18, 'active_platform_admin_accounts=' || auth_counts.active_platform_admin_accounts),
    (19, 'platform_admin_services_read_capable_accounts=' || auth_counts.platform_admin_services_read_capable_accounts),
    (20, 'support_services_read_authorized_admin_accounts=' || auth_counts.support_services_read_authorized_admin_accounts),
    (21, 'actor_feature_services_read_authorized_admin_accounts=' || auth_counts.actor_feature_services_read_authorized_admin_accounts)
) AS output(ordinal, line)
ORDER BY output.ordinal;

COMMIT;
EOSQL

expected_keys='audit_scope
target_salon_id
salon_exists
data_classification
active_pos_provider
scheduling_authority
services_total
services_active
services_archived
services_ai_bookable
services_matching_active_provider
service_categories_total
service_categories_active
service_aliases_total
service_aliases_active
service_category_aliases_total
service_category_aliases_active
active_platform_admin_accounts
platform_admin_services_read_capable_accounts
support_services_read_authorized_admin_accounts
actor_feature_services_read_authorized_admin_accounts'
actual_keys="$(cut -d= -f1 "$audit_output")"
if [ "$actual_keys" != "$expected_keys" ]; then
  echo "The production tenant catalog audit returned an unexpected output contract." >&2
  exit 1
fi

while IFS='=' read -r key value; do
  case "$key" in
    audit_scope)
      if [ "$value" != "production_tenant_catalog" ]; then
        echo "The production tenant catalog audit scope is invalid." >&2
        exit 1
      fi
      ;;
    target_salon_id)
      if [ "$value" != "$tenant_id" ]; then
        echo "The production tenant catalog audit returned a different tenant ID." >&2
        exit 1
      fi
      ;;
    data_classification)
      case "$value" in
        live|sample_test|none) ;;
        *) echo "The production tenant catalog audit returned an invalid data classification." >&2; exit 1 ;;
      esac
      ;;
    active_pos_provider)
      case "$value" in
        ''|*[!A-Za-z0-9._-]*)
          echo "The production tenant catalog audit returned an invalid active provider token." >&2
          exit 1
          ;;
      esac
      ;;
    scheduling_authority)
      case "$value" in
        owner_manual|manleai_calendar|external_provider|none) ;;
        *) echo "The production tenant catalog audit returned an invalid scheduling authority." >&2; exit 1 ;;
      esac
      ;;
    *)
      case "$value" in
        ''|*[!0-9]*)
          echo "The production tenant catalog audit returned a non-numeric count." >&2
          exit 1
          ;;
      esac
      ;;
  esac
done < "$audit_output"

cat "$audit_output"
