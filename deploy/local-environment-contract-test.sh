#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=local-environment-contract.sh
source "$script_dir/local-environment-contract.sh"

fail() {
  printf 'local-environment-contract-test error: %s\n' "$1" >&2
  exit 1
}

expect_valid() {
  local origin="$1"
  local services="$2"
  local expected_host="$3"
  local actual_host
  actual_host="$(validate_compose_service_http_origin TEST_ORIGIN "$origin" "$services")" || fail "expected valid origin: $origin"
  [ "$actual_host" = "$expected_host" ] || fail "unexpected host for valid origin: $origin"
}

expect_invalid() {
  local origin="$1"
  local services="$2"
  if validate_compose_service_http_origin TEST_ORIGIN "$origin" "$services" >/dev/null 2>&1; then
    fail "expected invalid origin: $origin"
  fi
}

services=$'catalog-backend\nweb-runtime\ncache'
expect_valid 'http://catalog-backend:49152' "$services" 'catalog-backend'
expect_valid 'http://catalog-backend:49152/' "$services" 'catalog-backend'
expect_valid 'http://web-runtime' "$services" 'web-runtime'

expect_invalid '' "$services"
expect_invalid 'https://catalog-backend:49152' "$services"
expect_invalid 'http://outside-runtime:49152' "$services"
expect_invalid 'http://user@catalog-backend:49152' "$services"
expect_invalid 'http://catalog-backend:49152/api' "$services"
expect_invalid 'http://catalog-backend:49152?mode=test' "$services"
expect_invalid 'http://catalog-backend:49152#fragment' "$services"
expect_invalid 'http://catalog-backend:not-a-port' "$services"
expect_invalid 'http://catalog-backend:70000' "$services"

printf 'Local Compose environment contract tests passed.\n'
