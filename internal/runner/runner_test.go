package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRun_RealBashExecution(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "helper.sh")

	originalScript := `#!/usr/bin/env bash
check_val() {
    if [[ "$1" == "secret" ]]; then
        echo "authorized"
        return 0
    fi
    echo "denied"
    return 1
}

check_val "$@"
`
	if err := os.WriteFile(targetPath, []byte(originalScript), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	cfg := Config{
		Targets: []string{"helper.sh"},
		Command: []string{"bash", targetPath, "secret"},
		WorkDir: tmpDir,
		Stdout:  stdout,
		Stderr:  stderr,
	}

	profiles, exitCode, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("expected exitCode 0, got %d (stderr: %s)", exitCode, stderr.String())
	}

	// Verify original script is completely restored!
	restored, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != originalScript {
		t.Errorf("target script was not restored!\nGot:\n%s\nWant:\n%s", string(restored), originalScript)
	}

	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}

	prof := profiles[0]
	if prof.LinesHit == 0 {
		t.Error("expected lines to be hit")
	}

	// Line 4 (echo "authorized") should be hit
	if prof.Hits[4] == 0 {
		t.Errorf("expected line 4 (authorized) to be hit, hits: %v", prof.Hits)
	}
	// Line 7 (echo "denied") should NOT be hit because we passed "secret"
	if prof.Hits[7] != 0 {
		t.Errorf("expected line 7 (denied) to be unhit, got %d hits", prof.Hits[7])
	}
}
