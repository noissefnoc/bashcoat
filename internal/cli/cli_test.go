package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_CLI(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "sample.sh")
	lcovPath := filepath.Join(tmpDir, "coverage.lcov")

	script := `#!/usr/bin/env bash
greet() {
    echo "hi $1"
}
greet "user"
`
	if err := os.WriteFile(targetPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	args := []string{
		"-t", targetPath,
		"-o", lcovPath,
		"--",
		"bash", targetPath,
	}

	code := Run(context.Background(), args, strings.NewReader(""), stdout, stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}

	// Verify LCOV file was generated
	lcovBytes, err := os.ReadFile(lcovPath)
	if err != nil {
		t.Fatalf("failed to read generated LCOV file: %v", err)
	}
	if !strings.Contains(string(lcovBytes), "SF:"+targetPath) || !strings.Contains(string(lcovBytes), "end_of_record") {
		t.Errorf("unexpected LCOV content:\n%s", string(lcovBytes))
	}

	// Verify summary table in stdout
	if !strings.Contains(stdout.String(), "Coverage") || !strings.Contains(stdout.String(), "100.0%") {
		t.Errorf("expected summary table in stdout, got:\n%s", stdout.String())
	}
}

func TestRun_MissingArgs(t *testing.T) {
	stderr := &bytes.Buffer{}
	code := Run(context.Background(), []string{}, strings.NewReader(""), &bytes.Buffer{}, stderr)
	if code != 2 {
		t.Errorf("expected code 2 for missing args, got %d", code)
	}
}
