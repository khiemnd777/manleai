#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "Usage: $0 <project-env-file>" >&2
  exit 2
fi

env_file="$1"
if [ ! -f "$env_file" ]; then
  echo "Production domain contract env file is missing." >&2
  exit 1
fi

set -a
. "$env_file"
set +a

required=(
  ADMIN_DOMAIN
  MARKETING_DOMAIN
  LANDING_DOMAIN
  POS_DOMAIN
  CORS_ALLOWED_ORIGINS
  FRONTEND_URL
  NEXT_PUBLIC_API_BASE_URL
  NEXT_PUBLIC_LANDING_BASE_URL
  NEXT_PUBLIC_MARKETING_BASE_URL
  MARKETING_BASE_URL
  SALON_PUBLIC_BASE_URL
)

for key in "${required[@]}"; do
  if [ -z "${!key:-}" ]; then
    echo "Production domain contract is missing ${key}." >&2
    exit 1
  fi
done

domains=("$ADMIN_DOMAIN" "$MARKETING_DOMAIN" "$LANDING_DOMAIN" "$POS_DOMAIN")
for domain in "${domains[@]}"; do
  if [ "${#domain}" -gt 253 ] || [[ ! "$domain" =~ ^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ ]]; then
    echo "Production domain contract contains an invalid hostname." >&2
    exit 1
  fi
done

for ((left = 0; left < ${#domains[@]}; left++)); do
  for ((right = left + 1; right < ${#domains[@]}; right++)); do
    if [ "${domains[$left]}" = "${domains[$right]}" ]; then
      echo "Production domain contract requires four distinct application hosts." >&2
      exit 1
    fi
  done
done

require_origin() {
  local key="$1"
  local actual="$2"
  local domain="$3"
  if [ "$actual" != "https://$domain" ]; then
    echo "Production domain contract has an inconsistent ${key}." >&2
    exit 1
  fi
}

require_origin FRONTEND_URL "$FRONTEND_URL" "$ADMIN_DOMAIN"
require_origin NEXT_PUBLIC_API_BASE_URL "$NEXT_PUBLIC_API_BASE_URL" "$ADMIN_DOMAIN"
require_origin NEXT_PUBLIC_MARKETING_BASE_URL "$NEXT_PUBLIC_MARKETING_BASE_URL" "$MARKETING_DOMAIN"
require_origin MARKETING_BASE_URL "$MARKETING_BASE_URL" "$MARKETING_DOMAIN"
require_origin NEXT_PUBLIC_LANDING_BASE_URL "$NEXT_PUBLIC_LANDING_BASE_URL" "$LANDING_DOMAIN"
require_origin SALON_PUBLIC_BASE_URL "$SALON_PUBLIC_BASE_URL" "$LANDING_DOMAIN"

if [[ "$CORS_ALLOWED_ORIGINS" =~ [[:space:]] ]]; then
  echo "Production domain contract requires a whitespace-free CORS origin list." >&2
  exit 1
fi

IFS=',' read -r -a cors_origins <<< "$CORS_ALLOWED_ORIGINS"
expected_origins=(
  "https://$ADMIN_DOMAIN"
  "https://$MARKETING_DOMAIN"
  "https://$LANDING_DOMAIN"
  "https://$POS_DOMAIN"
)

if [ "${#cors_origins[@]}" -ne "${#expected_origins[@]}" ]; then
  echo "Production domain contract requires exactly four CORS origins." >&2
  exit 1
fi

for expected in "${expected_origins[@]}"; do
  matches=0
  for actual in "${cors_origins[@]}"; do
    if [ "$actual" = "$expected" ]; then
      matches=$((matches + 1))
    fi
  done
  if [ "$matches" -ne 1 ]; then
    echo "Production domain contract CORS origins do not match the application hosts." >&2
    exit 1
  fi
done

echo "Production domain contract validated."
