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
EOSQL
