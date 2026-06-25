#!/usr/bin/env bash
set -euo pipefail

CONFIG_FILE="${CONFIG_FILE:-sync_github_secret.env}"

if [ -f "$CONFIG_FILE" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$CONFIG_FILE"
  set +a
fi

REPO="${GITHUB_REPO:-khiemnd777/manleai}"
ENV_FILE="${ENV_FILE:-project.env}"
B64_FILE="${B64_FILE:-/private/tmp/manleai-PROJECT_ENV_B64.txt}"

usage() {
  cat <<USAGE
Usage:
  ./sync_github_secret.sh

Optional environment variables:
  CONFIG_FILE   Local secret config file. Default: $CONFIG_FILE
  GITHUB_REPO   GitHub owner/repo. Default: $REPO
  ENV_FILE      Local env file to encode. Default: $ENV_FILE
  B64_FILE      Temporary base64 output file. Default: $B64_FILE
  SERVER_IP     Syncs the SERVER_IP secret.
  REMOTE_USER   Syncs the REMOTE_USER secret.
  SSH_PASSWORD  Syncs the SSH_PASSWORD secret.

The script sources sync_github_secret.env when present and never prints secret values.
USAGE
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

sync_plain_secret() {
  local name="$1"
  local value="$2"

  if [ -z "$value" ]; then
    echo "Missing required secret value: $name" >&2
    echo "Set it in $CONFIG_FILE or export it before running this script." >&2
    exit 1
  fi

  printf '%s' "$value" | gh secret set "$name" --repo "$REPO" >/dev/null
  echo "Synced $name"
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi

require_command gh
require_command base64

if [ ! -f "$ENV_FILE" ]; then
  echo "Env file not found: $ENV_FILE" >&2
  echo "Create it from deploy/project.env.example first." >&2
  exit 1
fi

if grep -q 'replace-with-' "$ENV_FILE"; then
  echo "Env file still contains placeholder values: $ENV_FILE" >&2
  exit 1
fi

mkdir -p "$(dirname "$B64_FILE")"
base64 -i "$ENV_FILE" | tr -d '\n' > "$B64_FILE"
chmod 600 "$B64_FILE"

gh secret set PROJECT_ENV_B64 --repo "$REPO" < "$B64_FILE" >/dev/null
echo "Synced PROJECT_ENV_B64 from $ENV_FILE"

sync_plain_secret SERVER_IP "${SERVER_IP:-}"
sync_plain_secret REMOTE_USER "${REMOTE_USER:-}"
sync_plain_secret SSH_PASSWORD "${SSH_PASSWORD:-}"

echo "GitHub secret sync complete for $REPO"
