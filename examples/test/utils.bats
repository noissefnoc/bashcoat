#!/usr/bin/env bats

setup() {
  source ./examples/src/lib/utils.sh
}

@test "greet prints greeting" {
  run greet "bashcoat"
  [ "$status" -eq 0 ]
  [ "$output" = "hello, bashcoat" ]
}

@test "is_non_empty checks non-empty strings" {
  run is_non_empty "ok"
  [ "$status" -eq 0 ]
}
