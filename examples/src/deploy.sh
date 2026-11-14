#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/utils.sh
source "${SCRIPT_DIR}/lib/utils.sh"

deploy() {
  local env="${1:-}"
  if ! is_non_empty "$env"; then
    echo "error: environment is required" >&2
    return 1
  fi

  case "$env" in
    staging|production)
      echo "deploying to ${env}"
      ;;
    *)
      echo "error: unknown environment: ${env}" >&2
      return 1
      ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  deploy "${1:-}"
fi
