#!/usr/bin/env bash
set -euo pipefail

umask 077

: "${RECOVERY_ID:?RECOVERY_ID is required}"
: "${EXPECTED_RELEASE_TAG:?EXPECTED_RELEASE_TAG is required}"
: "${APPROVAL_REFERENCE:?APPROVAL_REFERENCE is required}"
: "${RECOVERY_ACTION_KEY:?RECOVERY_ACTION_KEY is required}"

if [[ ! "$RECOVERY_ID" =~ ^[0-9]+-[0-9]+$ ]] || \
   [[ ! "$EXPECTED_RELEASE_TAG" =~ ^v[0-9]{4}\.[0-9]{2}\.[0-9]{2}\.[0-9]+$ ]] || \
   [[ ! "$APPROVAL_REFERENCE" =~ ^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$ ]] || \
   [[ ! "$RECOVERY_ACTION_KEY" =~ ^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$ ]]; then
  echo "The production Platform Admin recovery metadata is invalid." >&2
  exit 1
fi

work_dir="/tmp/manleai-platform-admin-recovery-$RECOVERY_ID"
project_env="/opt/manleai/project.env"
current_release="/opt/manleai/current"
compose_file="$current_release/docker-compose.prod.yml"
images_env="$current_release/images.env"
platform_access_binary="$work_dir/platform-access"
audit_script="$work_dir/production-account-audit.sh"
password_file="$work_dir/platform-admin-password"
output_dir="$work_dir/output"
result_file="$output_dir/platform-admin-recovery-result.json"

for path in \
  "$project_env" "$compose_file" "$images_env" "$platform_access_binary" \
  "$audit_script" "$password_file"; do
  [[ -f "$path" && ! -L "$path" ]] || {
    echo "A required production Platform Admin recovery input is missing or unsafe." >&2
    exit 1
  }
done
[[ ! -e "$result_file" ]] || {
  echo "The production Platform Admin recovery result path already exists." >&2
  exit 1
}
install -d -m 700 "$output_dir"
chmod 700 "$platform_access_binary" "$audit_script"
chmod 600 "$password_file"

current_image_tag="$(awk -F= '$1=="IMAGE_TAG" {print $2}' "$images_env")"
if [ "$current_image_tag" != "$EXPECTED_RELEASE_TAG" ]; then
  echo "The running production release does not match the approved Platform Admin recovery release." >&2
  exit 1
fi

audit_value() {
  local payload="$1"
  local key="$2"
  printf '%s\n' "$payload" | awk -F= -v target="$key" '$1==target {print $2}'
}

assert_single_admin_state() {
  local audit="$1"
  if [ "$(audit_value "$audit" total_accounts)" != "2" ] || \
     [ "$(audit_value "$audit" active_accounts)" != "2" ] || \
     [ "$(audit_value "$audit" tenant_accounts)" != "1" ] || \
     [ "$(audit_value "$audit" platform_accounts)" != "1" ] || \
     [ "$(audit_value "$audit" active_platform_admin_accounts)" != "1" ] || \
     [ "$(audit_value "$audit" legacy_super_admin_accounts)" != "0" ]; then
    echo "The production account state does not match the approved single-Admin recovery invariant." >&2
    exit 1
  fi
}

before_audit="$(bash "$audit_script" "$project_env")"
assert_single_admin_state "$before_audit"

compose=(
  docker compose
  --env-file "$project_env"
  --env-file "$images_env"
  -f "$compose_file"
  -p manleai
)

"${compose[@]}" run --rm --no-deps --user "$(id -u):$(id -g)" \
  --volume "$platform_access_binary:/bin/platform-access:ro" \
  --volume "$password_file:/run/secrets/manleai-platform-admin-password:ro" \
  --volume "$output_dir:/run/recovery" \
  api /bin/platform-access rotate-single-admin-password \
  --password-file /run/secrets/manleai-platform-admin-password \
  --action-key "$RECOVERY_ACTION_KEY" \
  --reason "$APPROVAL_REFERENCE" \
  --output-file /run/recovery/platform-admin-recovery-result.json

[[ -f "$result_file" && ! -L "$result_file" ]] || {
  echo "The Platform Admin recovery did not create a private result." >&2
  exit 1
}
chmod 600 "$result_file"

after_audit="$(bash "$audit_script" "$project_env")"
assert_single_admin_state "$after_audit"

printf '%s\n' "$after_audit"
printf 'platform_admin_credential_recovery=ready\n'
printf 'running_release=%s\n' "$current_image_tag"
