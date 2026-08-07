#!/bin/sh
set -eu

runtime_role=${DATABASE_RUNTIME_ROLE:?DATABASE_RUNTIME_ROLE is required}
runtime_password=${DATABASE_RUNTIME_PASSWORD:?DATABASE_RUNTIME_PASSWORD is required}

case "$runtime_role" in
  ''|[0-9]*|*[!a-z0-9_]*)
    echo "DATABASE_RUNTIME_ROLE must match ^[a-z_][a-z0-9_]{0,62}$" >&2
    exit 1
    ;;
esac

if [ "${#runtime_role}" -gt 63 ]; then
  echo "DATABASE_RUNTIME_ROLE exceeds PostgreSQL identifier length" >&2
  exit 1
fi

unsafe_existing_role="$({
  psql -X -v ON_ERROR_STOP=1 \
    --username "$POSTGRES_USER" \
    --dbname "$POSTGRES_DB" \
    --tuples-only \
    --no-align \
    --set runtime_role="$runtime_role" <<-'EOSQL'
SELECT EXISTS (
  SELECT 1
  FROM pg_roles role
  WHERE role.rolname=:'runtime_role'
    AND (
      EXISTS (
        SELECT 1
        FROM pg_auth_members membership
        WHERE membership.member=role.oid
      )
      OR EXISTS (
        SELECT 1
        FROM pg_class relation
        WHERE relation.relowner=role.oid
      )
    )
);
EOSQL
} | tr -d '[:space:]')"

if [ "$unsafe_existing_role" != 'f' ]; then
  echo "DATABASE_RUNTIME_ROLE has membership or relation ownership; refusing reconciliation" >&2
  exit 1
fi

psql -v ON_ERROR_STOP=1 \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  --set runtime_role="$runtime_role" \
  --set runtime_password="$runtime_password" <<-'EOSQL'
SELECT format(
  'CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS',
  :'runtime_role',
  :'runtime_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname=:'runtime_role')
\gexec

SELECT format(
  'ALTER ROLE %I WITH LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS',
  :'runtime_role',
  :'runtime_password'
)
\gexec
EOSQL

runtime_state="$({
  psql -X -v ON_ERROR_STOP=1 \
    --username "$POSTGRES_USER" \
    --dbname "$POSTGRES_DB" \
    --tuples-only \
    --no-align \
    --field-separator '|' \
    --set runtime_role="$runtime_role" <<-'EOSQL'
SELECT
  role.rolcanlogin,
  role.rolsuper,
  role.rolcreatedb,
  role.rolcreaterole,
  role.rolinherit,
  role.rolbypassrls,
  NOT EXISTS (
    SELECT 1
    FROM pg_auth_members membership
    WHERE membership.member=role.oid
  ),
  NOT EXISTS (
    SELECT 1
    FROM pg_class relation
    WHERE relation.relowner=role.oid
  )
FROM pg_roles role
WHERE role.rolname=:'runtime_role';
EOSQL
} | tr -d '[:space:]')"

if [ "$runtime_state" != 't|f|f|f|f|f|t|t' ]; then
  echo "DATABASE_RUNTIME_ROLE failed the restricted-role verification" >&2
  exit 1
fi

echo "runtime_role_bootstrap=ready"
