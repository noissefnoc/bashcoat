#!/usr/bin/env bash

format_greeting() {
  local name="$1"
  if [[ -z "$name" ]]; then
    echo "hello, guest"
    return 0
  fi
  cat <<EOF
hello, $name
EOF
}

never_called_helper() {
  local x=1
  (( x++ ))
  echo "$x"
}
