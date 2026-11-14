#!/usr/bin/env bats

@test "deploy succeeds for staging" {
  run ./examples/src/deploy.sh staging
  [ "$status" -eq 0 ]
  [ "$output" = "deploying to staging" ]
}

@test "deploy fails when environment is missing" {
  run ./examples/src/deploy.sh
  [ "$status" -eq 1 ]
}
