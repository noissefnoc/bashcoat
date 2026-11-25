package cli

import (
	"bytes"
	"context"
	"encoding/xml"
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

func TestRun_CoberturaFormatDefaultOutput(t *testing.T) {
	bash5 := findBash5(t)
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	targetPath := filepath.Join(tmpDir, "sample.sh")
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
		"-f", "cobertura",
		"--",
		bash5, targetPath,
	}

	code := Run(context.Background(), args, strings.NewReader(""), stdout, stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}

	xmlBytes, err := os.ReadFile(filepath.Join(tmpDir, "coverage.xml"))
	if err != nil {
		t.Fatalf("expected default coverage.xml to be created: %v", err)
	}

	xmlStr := string(xmlBytes)
	if !strings.Contains(xmlStr, "coverage-04.dtd") || !strings.Contains(xmlStr, `filename="sample.sh"`) {
		t.Errorf("unexpected Cobertura XML content:\n%s", xmlStr)
	}

	var parsed struct {
		XMLName      xml.Name `xml:"coverage"`
		LineRate     float64  `xml:"line-rate,attr"`
		LinesValid   int      `xml:"lines-valid,attr"`
		LinesCovered int      `xml:"lines-covered,attr"`
	}
	if err := xml.Unmarshal(xmlBytes, &parsed); err != nil {
		t.Fatalf("generated coverage.xml is not valid XML: %v\n%s", err, xmlStr)
	}
	if parsed.LineRate != 1 || parsed.LinesValid == 0 || parsed.LinesCovered != parsed.LinesValid {
		t.Errorf("unexpected parsed Cobertura stats: %+v", parsed)
	}

	// Default lcov.info should NOT be created when -f cobertura is used
	if _, err := os.Stat(filepath.Join(tmpDir, "lcov.info")); !os.IsNotExist(err) {
		t.Errorf("expected lcov.info not to exist when -f cobertura is set, stat err: %v", err)
	}
}

func TestRun_CoberturaFormatExplicitOutputAndDisable(t *testing.T) {
	bash5 := findBash5(t)
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	targetPath := filepath.Join(tmpDir, "sample.sh")
	if err := os.WriteFile(targetPath, []byte("#!/usr/bin/env bash\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	customOut := filepath.Join(tmpDir, "reports", "cobertura.xml")
	stderr := &bytes.Buffer{}
	code := Run(context.Background(), []string{
		"-t", targetPath,
		"-format", "COBERTURA",
		"-o", customOut,
		"--",
		bash5, targetPath,
	}, strings.NewReader(""), &bytes.Buffer{}, stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	if _, err := os.Stat(customOut); err != nil {
		t.Fatalf("expected custom output file %s: %v", customOut, err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "coverage.xml")); !os.IsNotExist(err) {
		t.Errorf("expected default coverage.xml not to be created when -o is explicit")
	}

	// Explicit -o "" disables file output even for cobertura format
	stderr.Reset()
	code = Run(context.Background(), []string{
		"-t", targetPath,
		"-f", "cobertura",
		"-o", "",
		"--",
		bash5, targetPath,
	}, strings.NewReader(""), &bytes.Buffer{}, stderr)
	if code != 0 {
		t.Fatalf("expected exit 0 when -o '' disables output, got %d (stderr: %s)", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "coverage.xml")); !os.IsNotExist(err) {
		t.Errorf("expected no coverage.xml when -o '' is set")
	}
}

func TestRun_InvalidFormatRejected(t *testing.T) {
	stderr := &bytes.Buffer{}
	code := Run(context.Background(), []string{"-f", "json", "-t", "nonexistent.sh", "--", "true"}, strings.NewReader(""), &bytes.Buffer{}, stderr)
	if code != 2 {
		t.Errorf("expected exit code 2 for unsupported format, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unsupported format") {
		t.Errorf("expected unsupported format error message, got:\n%s", stderr.String())
	}
}

func TestSplitFlagAndCmdArgs_FormatFlag(t *testing.T) {
	flagArgs, cmdArgs := splitFlagAndCmdArgs([]string{"-f", "--", "-t", "a.sh", "--", "bats", "test.bats"})
	if strings.Join(flagArgs, " ") != "-f -- -t a.sh" {
		t.Errorf("unexpected flagArgs: %v", flagArgs)
	}
	if strings.Join(cmdArgs, " ") != "bats test.bats" {
		t.Errorf("unexpected cmdArgs: %v", cmdArgs)
	}
}
