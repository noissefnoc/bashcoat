#!/usr/bin/env bash

classify_number() {
  local val="$1"
  local \
    abs="$val" \
    sign="pos"

  if (( val < 0 )); then
    (( abs = -val ))
    sign="neg"
  elif (( val == 0 )); then
    echo "zero"
    return 0
  fi

  case "$sign" in
    pos)
      if [[ "$abs" -gt 10 ]]; then
        echo "large-positive"
      else
        echo "small-positive"
      fi
      ;;
    neg)
      echo "negative"
      ;;
  esac
}

unused_function() {
  echo "never executed"
}
