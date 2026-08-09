#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
operator_script="$script_dir/production-platform-admin-credential-recovery.sh"
workflow="$repo_root/.github/workflows/production-platform-admin-credential-recovery.yml"
encryptor="$script_dir/encrypt-platform-admin-credential-handoff.mjs"

fail() {
  printf 'platform-admin-recovery contract error: %s\n' "$1" >&2
  exit 1
}

bash -n "$operator_script"
[ -f "$workflow" ] || fail "protected workflow is missing"

for marker in \
  'ROTATE_PRODUCTION_PLATFORM_ADMIN_CREDENTIAL' \
  'PLATFORM_ADMIN_RECOVERY_PASSWORD' \
  'RECOVERY_ACTION_KEY: production-platform-admin-recovery-${{ github.run_id }}' \
  '::add-mask::$ADMIN_PASSWORD' \
  '::add-mask::$admin_email' \
  'encrypt-platform-admin-credential-handoff.mjs' \
  'actions/upload-artifact@v4' \
  'retention-days: 1' \
  'Remove remote recovery bundle' \
  'Remove local recovery plaintext and SSH key'; do
  grep -Fq "$marker" "$workflow" || fail "workflow is missing security marker: $marker"
done

for marker in 'aes-256-gcm' 'rsa-oaep-sha256' 'modulusLength < 3072' 'flag: "wx"'; do
  grep -Fq "$marker" "$encryptor" || fail "encryptor is missing security invariant: $marker"
done

for marker in \
  'assert_single_admin_state "$before_audit"' \
  'assert_single_admin_state "$after_audit"' \
  'rotate-single-admin-password' \
  '--user "$(id -u):$(id -g)"' \
  'chmod 600 "$result_file"'; do
  grep -Fq -- "$marker" "$operator_script" || fail "operator script is missing invariant: $marker"
done

if grep -Fq 'cat "$RUNNER_TEMP/platform-admin-recovery/result.json"' "$workflow"; then
  fail "workflow prints the plaintext recovery result"
fi

printf 'platform-admin-recovery contract: ok\n'
