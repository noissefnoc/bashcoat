#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/src/lib/utils.sh"

[[ "$(greet "bashcoat")" == "hello, bashcoat" ]]
is_non_empty "ok"
if is_non_empty ""; then
  echo "expected empty string to fail" >&2
  exit 1
fi
echo "PASS: utils tests"
