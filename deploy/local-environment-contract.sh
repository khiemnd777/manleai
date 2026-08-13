#!/usr/bin/env bash

validate_compose_service_http_origin() {
  local label="$1"
  local origin="$2"
  local compose_services="$3"
  local authority host port service matched=false

  if [ -z "$origin" ]; then
    printf '%s is missing from the rendered Compose landing environment\n' "$label" >&2
    return 1
  fi
  if [ "$origin" != "${origin%/}" ]; then
    origin="${origin%/}"
  fi
  case "$origin" in
    http://*) authority="${origin#http://}" ;;
    *)
      printf '%s must be an exact HTTP origin for a Compose service\n' "$label" >&2
      return 1
      ;;
  esac
  case "$authority" in
    ''|*'/'*|*'?'*|*'#'*|*'@'*)
      printf '%s must not contain credentials, path, query, or fragment\n' "$label" >&2
      return 1
      ;;
  esac

  host="${authority%%:*}"
  if [ "$host" = "$authority" ]; then
    port=""
  else
    port="${authority#*:}"
    case "$port" in
      ''|*[!0-9]*)
        printf '%s contains an invalid port\n' "$label" >&2
        return 1
        ;;
    esac
    if [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
      printf '%s contains a port outside the valid range\n' "$label" >&2
      return 1
    fi
  fi
  case "$host" in
    ''|*[!a-z0-9_.-]*)
      printf '%s contains an invalid Compose service hostname\n' "$label" >&2
      return 1
      ;;
  esac

  while IFS= read -r service; do
    if [ "$service" = "$host" ]; then
      matched=true
      break
    fi
  done <<< "$compose_services"
  if [ "$matched" != true ]; then
    printf '%s hostname must resolve to a service in the rendered local Compose project\n' "$label" >&2
    return 1
  fi

  printf '%s\n' "$host"
}
