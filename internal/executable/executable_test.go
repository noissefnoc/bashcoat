package executable

import (
	"testing"
)

func TestDetect(t *testing.T) {
	script := `#!/usr/bin/env bash
# Header comment

VAR="hello"

greet() {
    local name="$1"
    if [[ "$name" == "admin" ]]; then
        echo "welcome admin"
    else
        echo "hello user"
    fi
}

greet "world"
`
	lines, err := Detect([]byte(script))
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	// Line 1: #! shebang -> not executable
	// Line 2: comment -> not executable
	// Line 3: blank -> not executable
	// Line 4: VAR="hello" -> executable
	// Line 6: greet() { -> not executable
	// Line 7: local name="$1" -> executable
	// Line 8: if [[ "$name" == "admin" ]] -> executable
	// Line 9: echo "welcome admin" -> executable
	// Line 11: echo "hello user" -> executable
	// Line 15: greet "world" -> executable

	expected := []int{4, 7, 8, 9, 11, 15}
	for _, l := range expected {
		if !lines[l] {
			t.Errorf("expected line %d to be marked executable", l)
		}
	}

	nonExecutable := []int{1, 2, 3, 5, 6, 10, 12, 13, 14}
	for _, l := range nonExecutable {
		if lines[l] {
			t.Errorf("line %d should not be marked executable", l)
		}
	}
}
