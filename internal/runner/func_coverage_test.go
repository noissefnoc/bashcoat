package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRun_FunctionCoverage_NestedSubshellAndConditionalDedup(t *testing.T) {
	bash5 := findBash5(t)
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "funcs.sh")

	src := `#!/usr/bin/env bash
set -euo pipefail

outer() {
  inner_uncalled() {
    echo "inner_uncalled"
  }
  echo "outer"
}

in_subshell() {
  echo "sub"
}

if [[ "${1:-a}" == "a" ]]; then
  notify() {
    echo "branch_a"
  }
else
  notify() {
    echo "branch_b"
  }
fi

captured_rc=""
on_err() {
  captured_rc="$?"
}

unused() {
  echo "unused"
}

outer
( in_subshell )
notify
(exit 42) || on_err
[[ "$captured_rc" == "42" ]]
`
	if err := os.WriteFile(scriptPath, []byte(src), 0o755); err != nil {
		t.Fatal(err)
	}

	stderr := &bytes.Buffer{}
	profiles, exitCode, err := Run(context.Background(), Config{
		Targets: []string{"funcs.sh"},
		Command: []string{bash5, scriptPath, "a"},
		WorkDir: tmpDir,
		Stdout:  &bytes.Buffer{},
		Stderr:  stderr,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", exitCode, stderr.String())
	}

	p := profiles[0]
	// Unique functions: outer (hit), inner_uncalled (miss), in_subshell (hit), notify (aggregated, hit), on_err (hit), unused (miss) -> 4/6
	if p.FunctionsFound != 6 || p.FunctionsHit != 4 {
		t.Fatalf("expected 4/6 functions covered, got %d/%d: %+v", p.FunctionsHit, p.FunctionsFound, p.Functions)
	}

	byName := make(map[string]int)
	for _, fn := range p.Functions {
		byName[fn.Name] = fn.LinesHit
		if fn.Name == "notify" && (fn.LinesFound != 2 || fn.LinesHit != 1 || fn.Calls != 1) {
			t.Errorf("expected aggregated notify 1/2 lines hit and 1 call, got %+v", fn)
		}
	}

	for _, hitFn := range []string{"outer", "in_subshell", "notify", "on_err"} {
		if byName[hitFn] == 0 {
			t.Errorf("expected %s to be covered, got %+v", hitFn, p.Functions)
		}
	}
	for _, missFn := range []string{"inner_uncalled", "unused"} {
		if byName[missFn] != 0 {
			t.Errorf("expected %s to be uncovered, got %+v", missFn, p.Functions)
		}
	}
}
