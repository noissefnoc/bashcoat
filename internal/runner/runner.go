package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/noissefnoc/bashcoat/internal/executable"
	"github.com/noissefnoc/bashcoat/internal/instrument"
	"github.com/noissefnoc/bashcoat/internal/trace"
)

// Config configures a coverage collection run.
type Config struct {
	Targets []string
	Command []string
	WorkDir string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

type targetState struct {
	path            string
	originalContent []byte
	insertLine      int
	executableLines map[int]bool
}

// Run instruments all target scripts, executes the user's test command, restores the targets,
// and returns the collected coverage profiles and the command's exit code.
func Run(ctx context.Context, cfg Config) ([]*trace.Profile, int, error) {
	if len(cfg.Targets) == 0 {
		return nil, 1, fmt.Errorf("no target scripts specified")
	}
	if len(cfg.Command) == 0 {
		return nil, 1, fmt.Errorf("no command specified to run")
	}

	workDir := cfg.WorkDir
	if workDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, 1, err
		}
		workDir = wd
	}

	resolvedTargets, err := resolveTargets(workDir, cfg.Targets)
	if err != nil {
		return nil, 1, fmt.Errorf("resolving targets: %w", err)
	}
	if len(resolvedTargets) == 0 {
		return nil, 1, fmt.Errorf("no matching files found for target pattern(s): %v", cfg.Targets)
	}

	// Create temporary trace file
	traceFile, err := os.CreateTemp("", "bashcoat-*.trace")
	if err != nil {
		return nil, 1, fmt.Errorf("creating temp trace file: %w", err)
	}
	tracePath := traceFile.Name()
	_ = traceFile.Close()
	defer os.Remove(tracePath)

	// Instrument all targets and register defer restore
	states := make([]*targetState, 0, len(resolvedTargets))
	var restoreMu sync.Mutex
	restored := false

	restoreAll := func() {
		restoreMu.Lock()
		defer restoreMu.Unlock()
		if restored {
			return
		}
		for _, s := range states {
			_ = os.WriteFile(s.path, s.originalContent, 0o755)
		}
		restored = true
	}
	defer restoreAll()

	for _, tPath := range resolvedTargets {
		src, err := os.ReadFile(tPath)
		if err != nil {
			return nil, 1, fmt.Errorf("reading target %s: %w", tPath, err)
		}

		execLines, _ := executable.Detect(src)
		instrumented, insertLine := instrument.Script(src, tracePath)

		if err := os.WriteFile(tPath, instrumented, 0o755); err != nil {
			return nil, 1, fmt.Errorf("instrumenting target %s: %w", tPath, err)
		}

		states = append(states, &targetState{
			path:            tPath,
			originalContent: src,
			insertLine:      insertLine,
			executableLines: execLines,
		})
	}

	// Execute command
	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...)
	cmd.Dir = workDir
	cmd.Stdin = cfg.Stdin
	cmd.Stdout = cfg.Stdout
	cmd.Stderr = cfg.Stderr

	cmdErr := cmd.Run()
	exitCode := 0
	if cmdErr != nil {
		if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	// Restore original script files immediately after command completion
	restoreAll()

	// Parse trace file for each target
	tf, err := os.Open(tracePath)
	if err != nil {
		return nil, exitCode, fmt.Errorf("reading trace file: %w", err)
	}
	defer tf.Close()

	profiles := make([]*trace.Profile, 0, len(states))
	for _, s := range states {
		if _, err := tf.Seek(0, io.SeekStart); err != nil {
			return nil, exitCode, err
		}
		prof, err := trace.Parse(tf, s.path, s.insertLine, s.executableLines)
		if err != nil {
			return nil, exitCode, fmt.Errorf("parsing trace for %s: %w", s.path, err)
		}
		profiles = append(profiles, prof)
	}

	return profiles, exitCode, nil
}

func resolveTargets(workDir string, rawPatterns []string) ([]string, error) {
	seen := make(map[string]bool)
	var targets []string

	for _, pat := range rawPatterns {
		absPat := pat
		if !filepath.IsAbs(pat) {
			absPat = filepath.Join(workDir, pat)
		}

		matches, err := filepath.Glob(absPat)
		if err != nil {
			return nil, err
		}

		if len(matches) == 0 {
			// If not a glob pattern, check if the direct file exists
			if info, err := os.Stat(absPat); err == nil && !info.IsDir() {
				matches = []string{absPat}
			}
		}

		for _, m := range matches {
			clean := filepath.Clean(m)
			if !seen[clean] {
				seen[clean] = true
				targets = append(targets, clean)
			}
		}
	}

	return targets, nil
}
