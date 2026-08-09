#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
operator_script="$script_dir/production-tenant-owner-credential-recovery.sh"
workflow="$repo_root/.github/workflows/production-tenant-owner-credential-recovery.yml"

fail() {
  printf 'tenant-owner-recovery contract error: %s\n' "$1" >&2
  exit 1
}

bash -n "$operator_script"
[ -f "$workflow" ] || fail "protected workflow is missing"

for marker in \
  'ROTATE_PRODUCTION_TENANT_OWNER_CREDENTIAL' \
  'TENANT_OWNER_RECOVERY_EMAIL' \
  'TENANT_OWNER_RECOVERY_PASSWORD' \
  'RECOVERY_ACTION_KEY: production-tenant-owner-recovery-${{ github.run_id }}' \
  '::add-mask::$TENANT_OWNER_EMAIL' \
  '::add-mask::$TENANT_OWNER_PASSWORD' \
  'chmod 600 "$private_dir/tenant-owner-email" "$private_dir/tenant-owner-password"' \
  'go test ./cmd/platform-access ./modules/access' \
  'Remove remote recovery bundle' \
  'Remove local recovery plaintext and SSH key'; do
  grep -Fq "$marker" "$workflow" || fail "workflow is missing security marker: $marker"
done

for marker in \
  'assert_exact_two_account_state "$before_audit"' \
  'assert_exact_two_account_state "$after_audit"' \
  'rotate-tenant-owner-password' \
  '--user "$(id -u):$(id -g)"' \
  'chmod 600 "$result_file"' \
  'The Tenant owner recovery result exposed credential material.'; do
  grep -Fq -- "$marker" "$operator_script" || fail "operator script is missing invariant: $marker"
done

if grep -Eq 'cat .*tenant-owner|cat .*recovery-result' "$workflow"; then
  fail "workflow prints a private Tenant recovery input or result"
fi

printf 'tenant-owner-recovery contract: ok\n'
