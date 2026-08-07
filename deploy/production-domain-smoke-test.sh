#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
smoke_script="$repo_root/deploy/production-domain-smoke.sh"

fail() {
  echo "production-domain-smoke-test: $*" >&2
  exit 1
}

curl() {
  local headers_file=""
  local method="GET"
  local url=""

  while [ "$#" -gt 0 ]; do
    case "$1" in
      -D)
        headers_file="$2"
        shift 2
        ;;
      -X)
        method="$2"
        shift 2
        ;;
      -H|-o|--output|--connect-timeout|--max-time|--max-redirs)
        shift 2
        ;;
      -fsS|-fsSL|-sS|--silent|--show-error)
        shift
        ;;
      https://*)
        url="$1"
        shift
        ;;
      *)
        shift
        ;;
    esac
  done

  if [ "${FAKE_FAIL_PROBE:-}" = "marketing-login" ] && [ "$url" = "https://ai.example.test/login" ]; then
    return 1
  fi

  case "$method $url" in
    "GET https://platform.example.test/healthz")
      return 0
      ;;
    "GET https://platform.example.test/login")
      printf 'HTTP/2 200\r\ncontent-security-policy: nonce-test\r\n\r\n' > "$headers_file"
      ;;
    "GET https://salon.example.test/")
      printf 'HTTP/2 404\r\ncontent-security-policy: nonce-test\r\n\r\n' > "$headers_file"
      ;;
    "GET https://pos.example.test/"|"GET https://ai.example.test/")
      printf 'HTTP/2 200\r\ncontent-security-policy: nonce-test\r\n\r\n' > "$headers_file"
      ;;
    "GET https://ai.example.test/login")
      printf 'HTTP/2 308\r\nlocation: https://platform.example.test/login\r\n\r\n' > "$headers_file"
      ;;
    "GET https://ai.example.test/api/public/salons/domain-smoke-nonexistent")
      printf 'HTTP/2 404\r\ncontent-type: application/json; charset=utf-8\r\n\r\n' > "$headers_file"
      ;;
    "OPTIONS https://platform.example.test/api/public/tenant-registration-requests")
      printf 'HTTP/2 204\r\naccess-control-allow-origin: https://ai.example.test\r\n\r\n' > "$headers_file"
      ;;
    *)
      return 1
      ;;
  esac
}
export -f curl

output="$({
  PRODUCTION_DOMAIN_SMOKE_ATTEMPTS=1 \
    bash "$smoke_script" \
      platform.example.test \
      ai.example.test \
      salon.example.test \
      pos.example.test
} 2>&1)" || fail "salon root 404 should preserve the public salon contract: $output"
grep -Fq 'Production domain smoke passed' <<< "$output" || fail "success output is missing"

set +e
failure_output="$({
  FAKE_FAIL_PROBE=marketing-login \
    PRODUCTION_DOMAIN_SMOKE_ATTEMPTS=1 \
    bash "$smoke_script" \
      platform.example.test \
      ai.example.test \
      salon.example.test \
      pos.example.test
} 2>&1)"
failure_status=$?
set -e
[ "$failure_status" -ne 0 ] || fail "forced marketing redirect failure unexpectedly passed"
grep -Fq 'marketing admin redirect' <<< "$failure_output" || fail "failure output did not identify the marketing admin redirect probe"

echo "production-domain-smoke-test: passed"
