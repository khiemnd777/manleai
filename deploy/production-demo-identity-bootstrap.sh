#!/usr/bin/env bash
set -euo pipefail

umask 077

: "${BOOTSTRAP_ID:?BOOTSTRAP_ID is required}"
: "${EXPECTED_RELEASE_TAG:?EXPECTED_RELEASE_TAG is required}"
: "${APPROVAL_REFERENCE:?APPROVAL_REFERENCE is required}"
: "${TENANT_RENAME_ACTION_KEY:?TENANT_RENAME_ACTION_KEY is required}"
: "${PLATFORM_ADMIN_ACTION_KEY:?PLATFORM_ADMIN_ACTION_KEY is required}"

if [[ ! "$BOOTSTRAP_ID" =~ ^[0-9]+-[0-9]+$ ]] || \
   [[ ! "$EXPECTED_RELEASE_TAG" =~ ^v[0-9]{4}\.[0-9]{2}\.[0-9]{2}\.[0-9]+$ ]] || \
   [[ ! "$APPROVAL_REFERENCE" =~ ^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$ ]] || \
   [[ ! "$TENANT_RENAME_ACTION_KEY" =~ ^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$ ]] || \
   [[ ! "$PLATFORM_ADMIN_ACTION_KEY" =~ ^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$ ]]; then
  echo "The production identity bootstrap metadata is invalid." >&2
  exit 1
fi

work_dir="/tmp/manleai-production-demo-identity-$BOOTSTRAP_ID"
project_env="/opt/manleai/project.env"
current_release="/opt/manleai/current"
compose_file="$current_release/docker-compose.prod.yml"
images_env="$current_release/images.env"
platform_access_binary="$work_dir/platform-access"
audit_script="$work_dir/production-account-audit.sh"
current_email_file="$work_dir/tenant-current-email"
new_email_file="$work_dir/tenant-new-email"
admin_email_file="$work_dir/platform-admin-email"
admin_name_file="$work_dir/platform-admin-name"
admin_password_file="$work_dir/platform-admin-password"
rename_output="$work_dir/tenant-rename-result.json"
admin_output="$work_dir/platform-admin-result.json"

for path in \
  "$project_env" "$compose_file" "$images_env" "$platform_access_binary" \
  "$audit_script" "$current_email_file" "$new_email_file" \
  "$admin_email_file" "$admin_name_file" "$admin_password_file"; do
  [[ -f "$path" && ! -L "$path" ]] || {
    echo "A required production identity bootstrap input is missing or unsafe." >&2
    exit 1
  }
done
chmod 700 "$platform_access_binary" "$audit_script"
chmod 600 "$current_email_file" "$new_email_file" "$admin_email_file" "$admin_name_file" "$admin_password_file"

tenant_current_email="$(<"$current_email_file")"
tenant_new_email="$(<"$new_email_file")"
platform_admin_email="$(<"$admin_email_file")"
platform_admin_name="$(<"$admin_name_file")"
if [ -z "$tenant_current_email" ] || [ -z "$tenant_new_email" ] || \
   [ -z "$platform_admin_email" ] || [ -z "$platform_admin_name" ]; then
  echo "A production identity bootstrap value is empty." >&2
  exit 1
fi

current_image_tag="$(awk -F= '$1=="IMAGE_TAG" {print $2}' "$images_env")"
if [ "$current_image_tag" != "$EXPECTED_RELEASE_TAG" ]; then
  echo "The running production release does not match the approved identity-bootstrap release." >&2
  exit 1
fi

before_audit="$(bash "$audit_script" "$project_env")"
audit_value() {
  local payload="$1"
  local key="$2"
  printf '%s\n' "$payload" | awk -F= -v target="$key" '$1==target {print $2}'
}
preflight_is_initial=false
preflight_is_complete=false
if [ "$(audit_value "$before_audit" total_accounts)" = "1" ] && \
   [ "$(audit_value "$before_audit" active_accounts)" = "1" ] && \
   [ "$(audit_value "$before_audit" tenant_accounts)" = "1" ] && \
   [ "$(audit_value "$before_audit" platform_accounts)" = "0" ] && \
   [ "$(audit_value "$before_audit" active_platform_admin_accounts)" = "0" ] && \
   [ "$(audit_value "$before_audit" legacy_super_admin_accounts)" = "0" ]; then
  preflight_is_initial=true
fi
if [ "$(audit_value "$before_audit" total_accounts)" = "2" ] && \
   [ "$(audit_value "$before_audit" active_accounts)" = "2" ] && \
   [ "$(audit_value "$before_audit" tenant_accounts)" = "1" ] && \
   [ "$(audit_value "$before_audit" platform_accounts)" = "1" ] && \
   [ "$(audit_value "$before_audit" active_platform_admin_accounts)" = "1" ] && \
   [ "$(audit_value "$before_audit" legacy_super_admin_accounts)" = "0" ]; then
  preflight_is_complete=true
fi
if [ "$preflight_is_initial" != true ] && [ "$preflight_is_complete" != true ]; then
  echo "The production identity preflight matches neither the approved initial state nor the exact replay state." >&2
  exit 1
fi

compose=(
  docker compose
  --env-file "$project_env"
  --env-file "$images_env"
  -f "$compose_file"
  -p manleai
)

"${compose[@]}" run --rm --no-deps --user 0 \
  --volume "$platform_access_binary:/bin/platform-access:ro" \
  api /bin/platform-access rename-tenant-email \
  --current-email "$tenant_current_email" \
  --new-email "$tenant_new_email" \
  --action-key "$TENANT_RENAME_ACTION_KEY" \
  --reason "$APPROVAL_REFERENCE" \
  > "$rename_output"

grep -q '"principal_scope":"tenant"' "$rename_output" || {
  echo "The Tenant login rename did not return the expected Tenant scope." >&2
  exit 1
}
grep -q '"status":"active"' "$rename_output" || {
  echo "The Tenant login rename did not preserve active status." >&2
  exit 1
}

"${compose[@]}" run --rm --no-deps --user 0 \
  --volume "$platform_access_binary:/bin/platform-access:ro" \
  --volume "$admin_password_file:/run/secrets/manleai-platform-admin-password:ro" \
  api /bin/platform-access bootstrap-admin \
  --email "$platform_admin_email" \
  --full-name "$platform_admin_name" \
  --password-file /run/secrets/manleai-platform-admin-password \
  --action-key "$PLATFORM_ADMIN_ACTION_KEY" \
  --reason "$APPROVAL_REFERENCE" \
  > "$admin_output"

grep -q '"role":"platform_admin"' "$admin_output" || {
  echo "The Platform Administrator bootstrap returned an unexpected role." >&2
  exit 1
}
grep -q '"principal_scope":"platform"' "$admin_output" || {
  echo "The Platform Administrator bootstrap returned an unexpected principal scope." >&2
  exit 1
}
grep -q '"status":"active"' "$admin_output" || {
  echo "The Platform Administrator bootstrap returned an inactive assignment." >&2
  exit 1
}

after_audit="$(bash "$audit_script" "$project_env")"
if [ "$(audit_value "$after_audit" total_accounts)" != "2" ] || \
   [ "$(audit_value "$after_audit" active_accounts)" != "2" ] || \
   [ "$(audit_value "$after_audit" tenant_accounts)" != "1" ] || \
   [ "$(audit_value "$after_audit" platform_accounts)" != "1" ] || \
   [ "$(audit_value "$after_audit" active_platform_admin_accounts)" != "1" ] || \
   [ "$(audit_value "$after_audit" legacy_super_admin_accounts)" != "0" ]; then
  echo "The production identity postflight does not match the approved Tenant/Admin state." >&2
  exit 1
fi

printf '%s\n' "$after_audit"
printf 'tenant_login_rename=ready\n'
printf 'platform_admin_bootstrap=ready\n'
printf 'running_release=%s\n' "$current_image_tag"
