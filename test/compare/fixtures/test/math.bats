#!/usr/bin/env bats

setup() {
  source "${BATS_TEST_DIRNAME}/../src/math.sh"
  source "${BATS_TEST_DIRNAME}/../src/helper.sh"
}

@test "classify positive and zero" {
  run classify_number 42
  [ "$status" -eq 0 ]
  [ "$output" = "large-positive" ]

  run classify_number 0
  [ "$status" -eq 0 ]
  [ "$output" = "zero" ]
}

@test "format_greeting with name" {
  run format_greeting "bashcoat"
  [ "$status" -eq 0 ]
  [ "$output" = "hello, bashcoat" ]
}
