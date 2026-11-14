package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func findBash5(t *testing.T) string {
	t.Helper()
	for _, cand := range []string{"bash", "/opt/homebrew/bin/bash", "/usr/local/bin/bash"} {
		p, err := exec.LookPath(cand)
		if err != nil {
			continue
		}
		out, err := exec.Command(p, "-c", `printf '%s' "${BASH_VERSINFO[0]:-0}"`).Output()
		if err == nil {
			if major, convErr := strconv.Atoi(strings.TrimSpace(string(out))); convErr == nil && major >= 5 {
				return p
			}
		}
	}
	t.Fatal("Bash 5.0+ executable not found")
	return ""
}

func TestRun_CLI(t *testing.T) {
	bash5 := findBash5(t)
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	targetPath := filepath.Join(tmpDir, "sample.sh")
	lcovPath := filepath.Join(tmpDir, "nested", "dir", "coverage.lcov")

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
		bash5, targetPath,
	}

	code := Run(context.Background(), args, strings.NewReader(""), stdout, stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}

	lcovBytes, err := os.ReadFile(lcovPath)
	if err != nil {
		t.Fatalf("failed to read generated LCOV file: %v", err)
	}
	if !strings.Contains(string(lcovBytes), "SF:sample.sh") || !strings.Contains(string(lcovBytes), "end_of_record") {
		t.Errorf("unexpected LCOV content:\n%s", string(lcovBytes))
	}

	if !strings.Contains(stdout.String(), "Coverage") || !strings.Contains(stdout.String(), "100.0%") {
		t.Errorf("expected summary table in stdout, got:\n%s", stdout.String())
	}
}

func TestRun_CommaFilenameAndDashDashFlagValue(t *testing.T) {
	bash5 := findBash5(t)
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	commaTarget := filepath.Join(tmpDir, "a,b.sh")
	if err := os.WriteFile(commaTarget, []byte("#!/usr/bin/env bash\necho comma\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	// Pass "-o --" as an output filename literally named "--", followed by real "--" separator
	args := []string{
		"-t", "a,b.sh",
		"-o", "--",
		"--",
		bash5, commaTarget,
	}

	code := Run(context.Background(), args, strings.NewReader(""), stdout, stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "--")); err != nil {
		t.Errorf("expected literal LCOV file named '--' to be created: %v", err)
	}
}

func TestRun_MissingArgs(t *testing.T) {
	stderr := &bytes.Buffer{}
	code := Run(context.Background(), []string{}, strings.NewReader(""), &bytes.Buffer{}, stderr)
	if code != 2 {
		t.Errorf("expected code 2 for missing args, got %d", code)
	}
}

func TestRun_EmptyTargetRejectedAndQuietFlagDetection(t *testing.T) {
	stderr := &bytes.Buffer{}
	code := Run(context.Background(), []string{"-t", "", "--", "true"}, strings.NewReader(""), &bytes.Buffer{}, stderr)
	if code != 2 {
		t.Errorf("expected code 2 for empty -t, got %d", code)
	}

	if !HasQuietFlag([]string{"-q", "-t", "a.sh", "--", "bats"}) {
		t.Error("expected HasQuietFlag to detect -q")
	}
	if HasQuietFlag([]string{"-t", "a.sh", "--", "bats", "-q"}) {
		t.Error("expected HasQuietFlag to ignore -q after --")
	}
	if HasQuietFlag([]string{"-q", "-q=false", "-t", "a.sh", "--", "bats"}) {
		t.Error("expected HasQuietFlag to respect -q=false override")
	}
}
