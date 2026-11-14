package runner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func findBash5(t *testing.T) string {
	t.Helper()
	candidates := []string{"bash", "/opt/homebrew/bin/bash", "/usr/local/bin/bash"}
	for _, cand := range candidates {
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
	t.Fatal("Bash 5.0+ executable not found in PATH or standard Homebrew locations")
	return ""
}

func TestRun_RealBashExecutionAndPermPreservation(t *testing.T) {
	bash5 := findBash5(t)
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
	if err := os.WriteFile(targetPath, []byte(originalScript), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	cfg := Config{
		Targets: []string{"helper.sh"},
		Command: []string{bash5, targetPath, "secret"},
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

	restored, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != originalScript {
		t.Errorf("target script was not restored!\nGot:\n%s\nWant:\n%s", string(restored), originalScript)
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("expected original perm 0644 to be preserved, got %o", info.Mode().Perm())
	}

	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	prof := profiles[0]
	if prof.SourceFile != "helper.sh" {
		t.Errorf("expected relative display path helper.sh, got %q", prof.SourceFile)
	}
	if prof.Hits[4] == 0 {
		t.Errorf("expected line 4 (authorized) to be hit, hits: %v", prof.Hits)
	}
	if prof.Hits[7] != 0 {
		t.Errorf("expected line 7 (denied) to be unhit, got %d hits", prof.Hits[7])
	}
}

func TestRun_PythonCloseFDs(t *testing.T) {
	bash5 := findBash5(t)
	py3, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}

	tmpDir := t.TempDir()
	outerPath := filepath.Join(tmpDir, "outer.sh")
	innerPath := filepath.Join(tmpDir, "inner.sh")

	outer := "#!" + bash5 + "\n" + py3 + ` -c "import subprocess; subprocess.run(['` + bash5 + `', '` + innerPath + `'], check=True)"` + "\n"
	inner := "#!" + bash5 + "\necho inside_inner\n"

	if err := os.WriteFile(outerPath, []byte(outer), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(innerPath, []byte(inner), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	profiles, exitCode, err := Run(context.Background(), Config{
		Targets: []string{"outer.sh", "inner.sh"},
		Command: []string{bash5, outerPath},
		WorkDir: tmpDir,
		Stdout:  stdout,
		Stderr:  stderr,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exitCode 0, got %d (stderr: %s)", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("expected empty stderr (no BASH_XTRACEFD leak), got:\n%s", stderr.String())
	}
	if len(profiles) != 2 || profiles[0].LinesHit != 1 || profiles[1].LinesHit != 1 {
		t.Errorf("expected both outer and inner to have 1/1 hit, got %+v", profiles)
	}
}

func TestRun_GlobstarAndDirectoryAndDuplicateAndColonAndArith(t *testing.T) {
	bash5 := findBash5(t)
	tmpDir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(tmpDir, "sub", "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "sub", "c"), 0o755); err != nil {
		t.Fatal(err)
	}

	zero := filepath.Join(tmpDir, "sub", "zero.sh")
	deep := filepath.Join(tmpDir, "sub", "a", "b", "lib.sh")
	otherLib := filepath.Join(tmpDir, "sub", "c", "lib.sh")
	colon := filepath.Join(tmpDir, "sub", "foo:bar.sh")

	sZero := "#!/usr/bin/env bash\nx=0\n(( x++ ))\n[[ $x -eq 1 ]]\nmsg=\"hello\nworld\"\n"
	sDeep := "#!/usr/bin/env bash\necho deep\n"
	sOther := "#!/usr/bin/env bash\necho other\n"
	sColon := "#!/usr/bin/env bash\necho colon\n"

	for p, content := range map[string]string{
		zero:     sZero,
		deep:     sDeep,
		otherLib: sOther,
		colon:    sColon,
	} {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	// Run zero.sh, deep lib.sh, and foo:bar.sh, leaving otherLib untouched
	runnerCmd := strings.Join([]string{
		bash5 + ` "` + zero + `"`,
		bash5 + ` "` + deep + `"`,
		bash5 + ` "` + colon + `"`,
	}, " && ")

	profiles, exitCode, err := Run(context.Background(), Config{
		Targets: []string{"sub/**/*.sh", "sub/*"},
		Command: []string{bash5, "-c", runnerCmd},
		WorkDir: tmpDir,
		Stdout:  stdout,
		Stderr:  stderr,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", exitCode, stderr.String())
	}

	byName := make(map[string]int)
	for _, p := range profiles {
		if p.LinesHit > p.LinesFound {
			t.Errorf("%s has LinesHit (%d) > LinesFound (%d)", p.SourceFile, p.LinesHit, p.LinesFound)
		}
		byName[p.SourceFile] = p.LinesHit
	}

	if byName[filepath.Join("sub", "zero.sh")] != 4 {
		t.Errorf("expected zero.sh 4 hits, got %d (profiles: %+v)", byName[filepath.Join("sub", "zero.sh")], profiles)
	}
	if byName[filepath.Join("sub", "a", "b", "lib.sh")] != 1 {
		t.Errorf("expected sub/a/b/lib.sh 1 hit, got %d", byName[filepath.Join("sub", "a", "b", "lib.sh")])
	}
	if byName[filepath.Join("sub", "c", "lib.sh")] != 0 {
		t.Errorf("expected sub/c/lib.sh 0 hits (must not collide with a/b/lib.sh), got %d", byName[filepath.Join("sub", "c", "lib.sh")])
	}
	if byName[filepath.Join("sub", "foo:bar.sh")] != 1 {
		t.Errorf("expected sub/foo:bar.sh 1 hit, got %d", byName[filepath.Join("sub", "foo:bar.sh")])
	}
}

func TestRun_SyntaxErrorAndBackupAndMarkerGuards(t *testing.T) {
	tmpDir := t.TempDir()
	good := filepath.Join(tmpDir, "good.sh")
	bad := filepath.Join(tmpDir, "bad.sh")

	goodOrig := "#!/usr/bin/env bash\necho ok\n"
	if err := os.WriteFile(good, []byte(goodOrig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("#!/usr/bin/env bash\nif then fi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := Run(context.Background(), Config{
		Targets: []string{"good.sh", "bad.sh"},
		Command: []string{"true"},
		WorkDir: tmpDir,
	})
	if err == nil {
		t.Fatal("expected syntax error on bad.sh")
	}

	// Ensure good.sh was restored and its backup cleaned up
	gotGood, _ := os.ReadFile(good)
	if string(gotGood) != goodOrig {
		t.Errorf("good.sh was not restored after bad.sh failure")
	}
	if _, statErr := os.Stat(filepath.Join(tmpDir, ".good.sh.bashcoat.bak")); !os.IsNotExist(statErr) {
		t.Errorf("expected .good.sh.bashcoat.bak to be cleaned up")
	}

	// Test leftover backup detection
	bakPath := filepath.Join(tmpDir, ".good.sh.bashcoat.bak")
	if err := os.WriteFile(bakPath, []byte(goodOrig), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = Run(context.Background(), Config{
		Targets: []string{"good.sh"},
		Command: []string{"true"},
		WorkDir: tmpDir,
	})
	if err == nil || !strings.Contains(err.Error(), "leftover backup") {
		t.Errorf("expected leftover backup error, got: %v", err)
	}
	_ = os.Remove(bakPath)

	// Test instrumentation marker detection
	if err := os.WriteFile(good, []byte("__bashcoat_file='/tmp/x'\necho ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = Run(context.Background(), Config{
		Targets: []string{"good.sh"},
		Command: []string{"true"},
		WorkDir: tmpDir,
	})
	if err == nil || !strings.Contains(err.Error(), "already contains bashcoat instrumentation") {
		t.Errorf("expected instrumentation marker error, got: %v", err)
	}
}

func TestRun_BashBelow5Rejected(t *testing.T) {
	out, err := exec.Command("/bin/bash", "-c", `printf '%s' "${BASH_VERSINFO[0]:-0}"`).Output()
	if err != nil || strings.TrimSpace(string(out)) >= "5" {
		t.Skip("/bin/bash is Bash 5+ or unavailable")
	}

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "old.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	stderr := &bytes.Buffer{}
	_, exitCode, err := Run(context.Background(), Config{
		Targets: []string{"old.sh"},
		Command: []string{"/bin/bash", scriptPath},
		WorkDir: tmpDir,
		Stdout:  &bytes.Buffer{},
		Stderr:  stderr,
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if exitCode == 0 {
		t.Errorf("expected non-zero exitCode when run under Bash < 5")
	}
	if !strings.Contains(stderr.String(), "Bash 5.0+ is required") {
		t.Errorf("expected Bash 5.0+ required message, got: %s", stderr.String())
	}
}

func TestRun_CancellationRestoresTarget(t *testing.T) {
	bash5 := findBash5(t)
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "sleep.sh")
	orig := "#!/usr/bin/env bash\nsleep 30\n"
	if err := os.WriteFile(scriptPath, []byte(orig), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_, _, err := Run(ctx, Config{
		Targets: []string{"sleep.sh"},
		Command: []string{bash5, scriptPath},
		WorkDir: tmpDir,
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	})
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("expected interrupted error, got: %v", err)
	}

	restored, _ := os.ReadFile(scriptPath)
	if string(restored) != orig {
		t.Errorf("target script not restored after cancellation")
	}
}

func TestRun_MultilineConstructsFullCoverageInRealBash(t *testing.T) {
	bash5 := findBash5(t)
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "multiline.sh")

	src := `#!/usr/bin/env bash
myfunc() { :; }
f() {
  local \
    a=1 \
    b=2
  echo "$a$b"
}
f
A=1 \
  B=2
cat <<HEREDOC >/dev/null
hello $(echo hdoc)
HEREDOC
myfunc \
  "$(echo cont)" \
  "arg"
cat <(
  echo procsub
) >/dev/null
case "$(
  echo x
)" in
  x) echo matched ;;
esac
arr=(
  "$(echo elem)"
)
`
	if err := os.WriteFile(scriptPath, []byte(src), 0o755); err != nil {
		t.Fatal(err)
	}

	profiles, exitCode, err := Run(context.Background(), Config{
		Targets: []string{"multiline.sh"},
		Command: []string{bash5, scriptPath},
		WorkDir: tmpDir,
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d", exitCode)
	}
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	if profiles[0].LinesFound == 0 || profiles[0].LinesHit != profiles[0].LinesFound {
		t.Errorf("expected 100%% coverage on executed multiline constructs, got %d/%d (hits: %v)",
			profiles[0].LinesHit, profiles[0].LinesFound, profiles[0].Hits)
	}
}

func TestRun_LockContentionRejectsConcurrentRun(t *testing.T) {
	bash5 := findBash5(t)
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "slow.sh")
	marker := filepath.Join(tmpDir, "started")
	orig := "#!/usr/bin/env bash\ntouch \"" + marker + "\"\nsleep 0.4\n"
	if err := os.WriteFile(scriptPath, []byte(orig), 0o755); err != nil {
		t.Fatal(err)
	}

	type runRes struct {
		exitCode int
		err      error
	}
	done := make(chan runRes, 1)
	go func() {
		_, code, runErr := Run(context.Background(), Config{
			Targets: []string{"slow.sh"},
			Command: []string{bash5, scriptPath},
			WorkDir: tmpDir,
			Stdout:  &bytes.Buffer{},
			Stderr:  &bytes.Buffer{},
		})
		done <- runRes{exitCode: code, err: runErr}
	}()

	// Wait until the first run has instrumented and started executing slow.sh
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for first run to start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Second concurrent run on the same target must fail with lock contention error
	// and must not corrupt or prematurely restore the first run's files.
	_, _, err2 := Run(context.Background(), Config{
		Targets: []string{"slow.sh"},
		Command: []string{"true"},
		WorkDir: tmpDir,
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	})
	if err2 == nil || !strings.Contains(err2.Error(), "currently locked") {
		t.Errorf("expected lock contention error, got: %v", err2)
	}

	res1 := <-done
	if res1.err != nil || res1.exitCode != 0 {
		t.Fatalf("first run should complete cleanly, got exit=%d err=%v", res1.exitCode, res1.err)
	}

	restored, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != orig {
		t.Errorf("target script not restored after first run finished")
	}
}

func TestRun_BatsIntegration(t *testing.T) {
	batsPath, err := exec.LookPath("bats")
	if err != nil {
		t.Skip("bats not installed")
	}
	bash5 := findBash5(t)

	tmpDir := t.TempDir()
	libPath := filepath.Join(tmpDir, "math.sh")
	batsTestPath := filepath.Join(tmpDir, "math.bats")

	libSrc := `add() {
    echo $(( $1 + $2 ))
}

unused_fn() {
    echo "never called"
}
`
	batsSrc := `#!/usr/bin/env bats

setup() {
    source "` + libPath + `"
}

@test "add works" {
    run add 2 3
    [ "$status" -eq 0 ]
    [ "$output" = "5" ]
}
`
	if err := os.WriteFile(libPath, []byte(libSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(batsTestPath, []byte(batsSrc), 0o755); err != nil {
		t.Fatal(err)
	}

	// Ensure bats uses Bash 5+ in PATH on macOS
	t.Setenv("PATH", filepath.Dir(bash5)+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	profiles, exitCode, err := Run(context.Background(), Config{
		Targets: []string{"math.sh"},
		Command: []string{batsPath, batsTestPath},
		WorkDir: tmpDir,
		Stdout:  stdout,
		Stderr:  stderr,
	})
	if err != nil {
		t.Fatalf("Bats Run failed: %v (stderr: %s)", err, stderr.String())
	}
	if exitCode != 0 {
		t.Fatalf("Bats exited with %d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout.String(), stderr.String())
	}
	if len(profiles) != 1 || profiles[0].LinesFound != 2 || profiles[0].LinesHit != 1 {
		t.Errorf("expected 1/2 coverage on math.sh, got %+v", profiles)
	}
}

func TestRun_ScrubsInheritedFDAndDisambiguatesSameRelativeSource(t *testing.T) {
	bash5 := findBash5(t)
	tmpDir := t.TempDir()

	// Simulate leaked __bashcoat_fd=2 / __bashcoat_file in parent environment
	t.Setenv("__bashcoat_fd", "2")
	t.Setenv("__bashcoat_file", filepath.Join(tmpDir, "wrong.trace"))

	// Also use a relative TMPDIR and a WorkDir containing glob metacharacters
	relTmp := filepath.Join(tmpDir, "reltmp")
	if err := os.MkdirAll(relTmp, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(tmpDir)
	t.Setenv("TMPDIR", "reltmp")

	workDir := filepath.Join(tmpDir, "work[1]")
	dir1 := filepath.Join(workDir, "dir1")
	dir2 := filepath.Join(workDir, "dir2")
	if err := os.MkdirAll(dir1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir2, 0o755); err != nil {
		t.Fatal(err)
	}

	s1 := "#!/usr/bin/env bash\necho from_dir1\n"
	s2 := "#!/usr/bin/env bash\necho from_dir2\nunused_in_dir2() {\n  echo nope\n}\n"
	if err := os.WriteFile(filepath.Join(dir1, "lib.sh"), []byte(s1), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "lib.sh"), []byte(s2), 0o755); err != nil {
		t.Fatal(err)
	}

	// Execute both via identical relative "./lib.sh" in separate subshells/processes
	runnerCmd := "(cd dir1 && " + bash5 + " ./lib.sh) && (cd dir2 && " + bash5 + " ./lib.sh)"

	stderr := &bytes.Buffer{}
	profiles, exitCode, err := Run(context.Background(), Config{
		Targets: []string{"**/*.sh"},
		Command: []string{bash5, "-c", runnerCmd},
		WorkDir: workDir,
		Stdout:  &bytes.Buffer{},
		Stderr:  stderr,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("expected empty stderr (no leaked xtrace to FD 2), got:\n%s", stderr.String())
	}
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}

	byFile := make(map[string]int)
	for _, p := range profiles {
		byFile[p.SourceFile] = p.LinesHit
	}
	if byFile[filepath.Join("dir1", "lib.sh")] != 1 {
		t.Errorf("expected dir1/lib.sh to have 1 hit, got %d", byFile[filepath.Join("dir1", "lib.sh")])
	}
	if byFile[filepath.Join("dir2", "lib.sh")] != 1 {
		t.Errorf("expected dir2/lib.sh to have 1 hit, got %d", byFile[filepath.Join("dir2", "lib.sh")])
	}
}

func TestRun_UnmatchedTargetPatternReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "ok.sh"), []byte("echo ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := Run(context.Background(), Config{
		Targets: []string{"ok.sh", "missing.sh"},
		Command: []string{"true"},
		WorkDir: tmpDir,
	})
	if err == nil || !strings.Contains(err.Error(), "missing.sh") {
		t.Errorf("expected error naming unmatched target missing.sh, got: %v", err)
	}
}

func TestRun_GracefulSIGTERMOnCancellation(t *testing.T) {
	bash5 := findBash5(t)
	tmpDir := t.TempDir()
	marker := filepath.Join(tmpDir, "cleaned_up")
	scriptPath := filepath.Join(tmpDir, "trap_sleep.sh")

	orig := "#!/usr/bin/env bash\ntrap 'touch \"" + marker + "\"; exit 0' TERM\nsleep 30 &\nwait\n"
	if err := os.WriteFile(scriptPath, []byte(orig), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, _, err := Run(ctx, Config{
		Targets: []string{"trap_sleep.sh"},
		Command: []string{bash5, scriptPath},
		WorkDir: tmpDir,
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	})
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("expected interrupted error, got: %v", err)
	}

	if _, statErr := os.Stat(marker); statErr != nil {
		t.Errorf("expected SIGTERM trap cleanup marker to exist, got: %v", statErr)
	}
	restored, _ := os.ReadFile(scriptPath)
	if string(restored) != orig {
		t.Errorf("target script not restored after cancellation")
	}
}
