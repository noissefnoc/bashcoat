#!/usr/bin/env bash

greet() {
  local name="${1:-world}"
  echo "hello, ${name}"
}

is_non_empty() {
  local value="${1:-}"
  if [[ -n "$value" ]]; then
    return 0
  fi
  return 1
}
