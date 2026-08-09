#!/usr/bin/env bash
set -euo pipefail

umask 077

: "${RECOVERY_ID:?RECOVERY_ID is required}"
: "${EXPECTED_RELEASE_TAG:?EXPECTED_RELEASE_TAG is required}"
: "${APPROVAL_REFERENCE:?APPROVAL_REFERENCE is required}"
: "${RECOVERY_ACTION_KEY:?RECOVERY_ACTION_KEY is required}"
: "${TENANT_ID:?TENANT_ID is required}"

if [[ ! "$RECOVERY_ID" =~ ^[0-9]+-[0-9]+$ ]] || \
   [[ ! "$EXPECTED_RELEASE_TAG" =~ ^v[0-9]{4}\.[0-9]{2}\.[0-9]{2}\.[0-9]+$ ]] || \
   [[ ! "$APPROVAL_REFERENCE" =~ ^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$ ]] || \
   [[ ! "$RECOVERY_ACTION_KEY" =~ ^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$ ]] || \
   [[ ! "$TENANT_ID" =~ ^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$ ]]; then
  echo "The production Tenant owner recovery metadata is invalid." >&2
  exit 1
fi

work_dir="/tmp/manleai-tenant-owner-recovery-$RECOVERY_ID"
project_env="/opt/manleai/project.env"
current_release="/opt/manleai/current"
compose_file="$current_release/docker-compose.prod.yml"
images_env="$current_release/images.env"
platform_access_binary="$work_dir/platform-access"
audit_script="$work_dir/production-account-audit.sh"
email_file="$work_dir/tenant-owner-email"
password_file="$work_dir/tenant-owner-password"
output_dir="$work_dir/output"
result_file="$output_dir/tenant-owner-recovery-result.json"

for path in \
  "$project_env" "$compose_file" "$images_env" "$platform_access_binary" \
  "$audit_script" "$email_file" "$password_file"; do
  [[ -f "$path" && ! -L "$path" ]] || {
    echo "A required production Tenant owner recovery input is missing or unsafe." >&2
    exit 1
  }
done
[[ ! -e "$result_file" ]] || {
  echo "The production Tenant owner recovery result path already exists." >&2
  exit 1
}
install -d -m 700 "$output_dir"
chmod 700 "$platform_access_binary" "$audit_script"
chmod 600 "$email_file" "$password_file"

current_image_tag="$(awk -F= '$1=="IMAGE_TAG" {print $2}' "$images_env")"
if [ "$current_image_tag" != "$EXPECTED_RELEASE_TAG" ]; then
  echo "The running production release does not match the approved Tenant owner recovery release." >&2
  exit 1
fi

audit_value() {
  local payload="$1"
  local key="$2"
  printf '%s\n' "$payload" | awk -F= -v target="$key" '$1==target {print $2}'
}

assert_exact_two_account_state() {
  local audit="$1"
  if [ "$(audit_value "$audit" total_accounts)" != "2" ] || \
     [ "$(audit_value "$audit" active_accounts)" != "2" ] || \
     [ "$(audit_value "$audit" disabled_accounts)" != "0" ] || \
     [ "$(audit_value "$audit" invited_accounts)" != "0" ] || \
     [ "$(audit_value "$audit" tenant_accounts)" != "1" ] || \
     [ "$(audit_value "$audit" platform_accounts)" != "1" ] || \
     [ "$(audit_value "$audit" active_tenant_accounts)" != "1" ] || \
     [ "$(audit_value "$audit" active_platform_accounts)" != "1" ] || \
     [ "$(audit_value "$audit" active_platform_admin_accounts)" != "1" ] || \
     [ "$(audit_value "$audit" legacy_super_admin_accounts)" != "0" ] || \
     [ "$(audit_value "$audit" active_legacy_super_admin_accounts)" != "0" ]; then
    echo "The production account state does not match the approved two-account recovery invariant." >&2
    exit 1
  fi
}

before_audit="$(bash "$audit_script" "$project_env")"
assert_exact_two_account_state "$before_audit"

compose=(
  docker compose
  --env-file "$project_env"
  --env-file "$images_env"
  -f "$compose_file"
  -p manleai
)

"${compose[@]}" run --rm --no-deps --user "$(id -u):$(id -g)" \
  --volume "$platform_access_binary:/bin/platform-access:ro" \
  --volume "$email_file:/run/secrets/manleai-tenant-owner-email:ro" \
  --volume "$password_file:/run/secrets/manleai-tenant-owner-password:ro" \
  --volume "$output_dir:/run/recovery" \
  api /bin/platform-access rotate-tenant-owner-password \
  --salon-id "$TENANT_ID" \
  --email-file /run/secrets/manleai-tenant-owner-email \
  --password-file /run/secrets/manleai-tenant-owner-password \
  --action-key "$RECOVERY_ACTION_KEY" \
  --reason "$APPROVAL_REFERENCE" \
  --output-file /run/recovery/tenant-owner-recovery-result.json

[[ -f "$result_file" && ! -L "$result_file" ]] || {
  echo "The Tenant owner recovery did not create a private result." >&2
  exit 1
}
chmod 600 "$result_file"

grep -Fq '"principal_scope":"tenant"' "$result_file" || {
  echo "The Tenant owner recovery result has the wrong principal scope." >&2
  exit 1
}
grep -Fq '"status":"active"' "$result_file" || {
  echo "The Tenant owner recovery result has the wrong account status." >&2
  exit 1
}
grep -Fq "\"salon_id\":\"$TENANT_ID\"" "$result_file" || {
  echo "The Tenant owner recovery result has the wrong salon." >&2
  exit 1
}
grep -Eq '"owned_salon_count":[1-9][0-9]*' "$result_file" || {
  echo "The Tenant owner recovery result has an unexpected ownership count." >&2
  exit 1
}
if grep -Eq '"(email|password|password_hash|password_fingerprint)"' "$result_file"; then
  echo "The Tenant owner recovery result exposed credential material." >&2
  exit 1
fi

after_audit="$(bash "$audit_script" "$project_env")"
assert_exact_two_account_state "$after_audit"

printf '%s\n' "$after_audit"
printf 'tenant_owner_credential_recovery=ready\n'
printf 'running_release=%s\n' "$current_image_tag"
