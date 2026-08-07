#!/usr/bin/env bash
set -euo pipefail

smoke_mode="domain_cutover"
marketing_domain=""
if [ "${1:-}" = "--legacy" ]; then
  smoke_mode="legacy"
  shift
  if [ "$#" -ne 3 ]; then
    echo "Usage: $0 --legacy <admin-domain> <salon-domain> <pos-domain>" >&2
    exit 2
  fi
  admin_domain="$1"
  salon_domain="$2"
  pos_domain="$3"
elif [ "$#" -eq 4 ]; then
  admin_domain="$1"
  marketing_domain="$2"
  salon_domain="$3"
  pos_domain="$4"
else
  echo "Usage: $0 <admin-domain> <marketing-domain> <salon-domain> <pos-domain>" >&2
  exit 2
fi
attempt_limit="${PRODUCTION_DOMAIN_SMOKE_ATTEMPTS:-30}"
retry_seconds="${PRODUCTION_DOMAIN_SMOKE_RETRY_SECONDS:-2}"
current_probe="not started"

if ! [[ "$attempt_limit" =~ ^[1-9][0-9]*$ ]] || [ "$attempt_limit" -gt 60 ]; then
  echo "Production domain smoke attempts must be an integer from 1 through 60." >&2
  exit 2
fi
if ! [[ "$retry_seconds" =~ ^[1-9][0-9]*$ ]] || [ "$retry_seconds" -gt 10 ]; then
  echo "Production domain smoke retry seconds must be an integer from 1 through 10." >&2
  exit 2
fi

for command_name in curl awk grep mktemp; do
  command -v "$command_name" >/dev/null 2>&1 || {
    echo "Production domain smoke requires ${command_name}." >&2
    exit 1
  }
done

headers_file="$(mktemp)"
cors_headers_file="$(mktemp)"
cleanup() {
  rm -f "$headers_file" "$cors_headers_file"
}
trap cleanup EXIT

last_http_status() {
  awk '/^HTTP\// { status=$2 } END { gsub("\r", "", status); print status }' "$1"
}

last_header_value() {
  local header_name="$1"
  awk -v target="$header_name" '
    BEGIN { target=tolower(target) }
    index($0, ":") {
      name=substr($0, 1, index($0, ":") - 1)
      gsub("\r", "", name)
      if (tolower(name) == target) {
        value=substr($0, index($0, ":") + 1)
        sub(/^[[:space:]]+/, "", value)
        gsub("\r", "", value)
      }
    }
    END { print value }
  ' "$2"
}

probe_domains() {
  local status location content_type

  current_probe="platform API health"
  curl -fsS --connect-timeout 8 --max-time 20 -o /dev/null "https://${admin_domain}/healthz" || return 1

  current_probe="platform login CSP"
  : > "$headers_file"
  curl -fsS --connect-timeout 8 --max-time 20 -D "$headers_file" -o /dev/null "https://${admin_domain}/login" || return 1
  grep -Eiq '^content-security-policy:' "$headers_file" || return 1

  current_probe="salon landing root"
  : > "$headers_file"
  curl -sS --connect-timeout 8 --max-time 20 -D "$headers_file" -o /dev/null "https://${salon_domain}/" || return 1
  status="$(last_http_status "$headers_file")"
  [ "$status" = "200" ] || [ "$status" = "404" ] || return 1
  grep -Eiq '^content-security-policy:' "$headers_file" || return 1

  current_probe="POS root CSP"
  : > "$headers_file"
  curl -fsSL --connect-timeout 8 --max-time 20 --max-redirs 5 -D "$headers_file" -o /dev/null "https://${pos_domain}/" || return 1
  grep -Eiq '^content-security-policy:' "$headers_file" || return 1

  if [ "$smoke_mode" = "legacy" ]; then
    return 0
  fi

  current_probe="marketing root CSP"
  : > "$headers_file"
  curl -fsS --connect-timeout 8 --max-time 20 -D "$headers_file" -o /dev/null "https://${marketing_domain}/" || return 1
  grep -Eiq '^content-security-policy:' "$headers_file" || return 1

  current_probe="marketing admin redirect"
  : > "$headers_file"
  curl -sS --connect-timeout 8 --max-time 20 -D "$headers_file" -o /dev/null "https://${marketing_domain}/login" || return 1
  status="$(last_http_status "$headers_file")"
  location="$(last_header_value location "$headers_file")"
  [ "$status" = "308" ] || return 1
  [ "$location" = "https://${admin_domain}/login" ] || return 1

  current_probe="marketing API compatibility"
  : > "$headers_file"
  curl -sS --connect-timeout 8 --max-time 20 -D "$headers_file" -o /dev/null "https://${marketing_domain}/api/public/salons/domain-smoke-nonexistent" || return 1
  status="$(last_http_status "$headers_file")"
  content_type="$(last_header_value content-type "$headers_file")"
  [ "$status" = "404" ] || return 1
  [[ "$content_type" == application/json* ]] || return 1

  current_probe="marketing-origin CORS"
  : > "$cors_headers_file"
  curl -fsS --connect-timeout 8 --max-time 20 \
    -X OPTIONS \
    -H "Origin: https://${marketing_domain}" \
    -H 'Access-Control-Request-Method: POST' \
    -D "$cors_headers_file" \
    -o /dev/null \
    "https://${admin_domain}/api/public/tenant-registration-requests" || return 1
  [ "$(last_header_value access-control-allow-origin "$cors_headers_file")" = "https://${marketing_domain}" ] || return 1

  return 0
}

for ((attempt = 1; attempt <= attempt_limit; attempt++)); do
  if probe_domains; then
    echo "Production domain smoke passed for the ${smoke_mode} host contract."
    exit 0
  fi
  echo "Production domain smoke attempt ${attempt}/${attempt_limit} failed at ${current_probe}." >&2
  if [ "$attempt" -lt "$attempt_limit" ]; then
    sleep "$retry_seconds"
  fi
done

echo "Production domain smoke failed after ${attempt_limit} attempts." >&2
exit 1
