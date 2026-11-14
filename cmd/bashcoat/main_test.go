package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
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

func TestHelperProcess(_ *testing.T) {
	if os.Getenv("BASHCOAT_TEST_EXEC_MAIN") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"bashcoat"}, os.Args[i+1:]...)
			break
		}
	}
	main()
}

func TestMain_SignalRestoresTargetAndRespectsQuiet(t *testing.T) {
	bash5 := findBash5(t)

	for _, tc := range []struct {
		name       string
		quiet      bool
		wantNotice bool
	}{
		{name: "default prints signal notice", quiet: false, wantNotice: true},
		{name: "quiet suppresses signal notice", quiet: true, wantNotice: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			scriptPath := filepath.Join(tmpDir, "target.sh")
			startedMarker := filepath.Join(tmpDir, "started")
			orig := "#!/usr/bin/env bash\ntouch \"" + startedMarker + "\"\nsleep 30\n"
			if err := os.WriteFile(scriptPath, []byte(orig), 0o755); err != nil {
				t.Fatal(err)
			}

			subArgs := []string{"-test.run=^TestHelperProcess$", "--"}
			if tc.quiet {
				subArgs = append(subArgs, "-q")
			}
			subArgs = append(subArgs, "-t", "target.sh", "--", bash5, scriptPath)

			cmd := exec.Command(os.Args[0], subArgs...)
			cmd.Dir = tmpDir
			cmd.Env = append(os.Environ(), "BASHCOAT_TEST_EXEC_MAIN=1")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr

			if err := cmd.Start(); err != nil {
				t.Fatalf("failed to start subprocess: %v", err)
			}

			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(startedMarker); err == nil {
					break
				}
				if time.Now().After(deadline) {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					t.Fatal("timed out waiting for target script to start")
				}
				time.Sleep(15 * time.Millisecond)
			}

			if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatalf("failed to send SIGINT: %v", err)
			}

			waitErr := cmd.Wait()
			if waitErr == nil {
				t.Fatalf("expected non-zero exit after SIGINT, stderr:\n%s", stderr.String())
			}

			got, err := os.ReadFile(scriptPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != orig {
				t.Errorf("target script not restored after SIGINT:\nGot:\n%s\nWant:\n%s", string(got), orig)
			}

			bakPath := filepath.Join(tmpDir, ".target.sh.bashcoat.bak")
			if _, statErr := os.Stat(bakPath); !os.IsNotExist(statErr) {
				t.Errorf("expected backup file %s to be removed, stat err: %v", bakPath, statErr)
			}

			hasNotice := strings.Contains(stderr.String(), "received signal")
			if hasNotice != tc.wantNotice {
				t.Errorf("wantNotice=%v, got stderr:\n%s", tc.wantNotice, stderr.String())
			}
		})
	}
}
