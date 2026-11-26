// Package runner orchestrates target script instrumentation, execution, restoration, and trace parsing.
package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bmatcuk/doublestar/v4"
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

type resolvedTarget struct {
	displayPath string
	absPath     string
	realPath    string
	perm        os.FileMode
}

type targetState struct {
	id              int
	displayPath     string
	absPath         string
	realPath        string
	perm            os.FileMode
	backupPath      string
	lockPath        string
	lockFile        *os.File
	originalContent []byte
	insertLine      int
	stmtInfo        *executable.StatementInfo
	instrumented    bool
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
	if absWD, err := filepath.Abs(workDir); err == nil {
		workDir = absWD
	}

	resolvedTargets, err := resolveTargets(workDir, cfg.Targets)
	if err != nil {
		return nil, 1, fmt.Errorf("resolving targets: %w", err)
	}
	if len(resolvedTargets) == 0 {
		return nil, 1, fmt.Errorf("no matching files found for target pattern(s): %v", cfg.Targets)
	}

	// Create temporary trace file (normalized to absolute path so cd in scripts never breaks it)
	traceFile, err := os.CreateTemp("", "bashcoat-*.trace")
	if err != nil {
		return nil, 1, fmt.Errorf("creating temp trace file: %w", err)
	}
	tracePath := traceFile.Name()
	_ = traceFile.Close()
	if absTrace, absErr := filepath.Abs(tracePath); absErr == nil {
		tracePath = absTrace
	}
	defer func() {
		_ = os.Remove(tracePath)
	}()

	states := make([]*targetState, 0, len(resolvedTargets))
	var restoreMu sync.Mutex
	restored := false

	restoreAll := func() error {
		restoreMu.Lock()
		defer restoreMu.Unlock()
		if restored {
			return nil
		}
		restored = true

		var errs []string
		// Restore all target files and remove backups before releasing any advisory locks.
		for _, s := range states {
			if s.instrumented {
				if err := writeFileAtomic(s.realPath, s.originalContent, s.perm); err != nil {
					errs = append(errs, fmt.Sprintf("restoring %s (backup kept at %s): %v", s.realPath, s.backupPath, err))
				} else if s.backupPath != "" {
					if rmErr := os.Remove(s.backupPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
						errs = append(errs, fmt.Sprintf("removing backup %s: %v", s.backupPath, rmErr))
					}
				}
			} else if s.backupPath != "" {
				_ = os.Remove(s.backupPath)
			}
		}

		for _, s := range states {
			if s.lockFile != nil {
				releaseTargetLock(s.lockPath, s.lockFile)
			}
		}

		if len(errs) > 0 {
			return errors.New(strings.Join(errs, "; "))
		}
		return nil
	}

	defer func() {
		if rErr := restoreAll(); rErr != nil && cfg.Stderr != nil {
			_, _ = fmt.Fprintf(cfg.Stderr, "bashcoat: error during target restoration: %v\n", rErr)
		}
	}()

	for i, rt := range resolvedTargets {
		if err := ctx.Err(); err != nil {
			return nil, 130, fmt.Errorf("execution interrupted: %w", err)
		}
		st, err := prepareAndInstrumentTarget(i, rt, tracePath)
		if st != nil {
			states = append(states, st)
		}
		if err != nil {
			return nil, 1, err
		}
	}

	// Execute command in its own process group so cancellation cleans up grandchildren as well.
	// Scrub inherited bashcoat FD variables so nested/outer environments cannot hijack trace output.
	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...)
	cmd.Dir = workDir
	cmd.Env = cleanChildEnv(os.Environ())
	cmd.Stdin = cfg.Stdin
	cmd.Stdout = cfg.Stdout
	cmd.Stderr = cfg.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	cmdDone := make(chan struct{})
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		pgid := -cmd.Process.Pid
		err := syscall.Kill(pgid, syscall.SIGTERM)
		go func() {
			timer := time.NewTimer(1500 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-cmdDone:
			case <-timer.C:
				_ = syscall.Kill(pgid, syscall.SIGKILL)
			}
		}()
		return err
	}
	cmd.WaitDelay = 2 * time.Second

	cmdErr := cmd.Run()
	close(cmdDone)
	exitCode := 0
	var execStartErr error
	if cmdErr != nil {
		var exitErr *exec.ExitError
		if errors.As(cmdErr, &exitErr) {
			exitCode = exitErr.ExitCode()
			if exitCode < 0 {
				if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
					exitCode = 128 + int(ws.Signal())
				} else {
					exitCode = 1
				}
			}
		} else {
			exitCode = 1
			execStartErr = cmdErr
		}
	}

	// Restore original script files immediately after command completion
	if rErr := restoreAll(); rErr != nil {
		return nil, 1, fmt.Errorf("restoring target scripts: %w", rErr)
	}

	if ctx.Err() != nil {
		if exitCode == 0 {
			exitCode = 130
		}
		return nil, exitCode, fmt.Errorf("execution interrupted: %w", ctx.Err())
	}

	if execStartErr != nil {
		return nil, exitCode, fmt.Errorf("running command: %w", execStartErr)
	}

	// Parse trace file for all targets in a single pass
	tf, err := os.Open(tracePath)
	if err != nil {
		return nil, exitCode, fmt.Errorf("reading trace file: %w", err)
	}
	defer func() {
		_ = tf.Close()
	}()

	specs := make([]trace.TargetSpec, 0, len(states))
	for _, s := range states {
		fnSpecs := make([]trace.FuncSpec, len(s.stmtInfo.Functions))
		for i, fn := range s.stmtInfo.Functions {
			fnSpecs[i] = trace.FuncSpec{
				Name:      fn.Name,
				StartLine: fn.StartLine,
				EndLine:   fn.EndLine,
				ExecLines: fn.ExecLines,
				FirstLine: fn.FirstLine,
			}
		}
		specs = append(specs, trace.TargetSpec{
			ID:         s.id,
			SourceFile: s.displayPath,
			AbsPath:    s.absPath,
			RealPath:   s.realPath,
			WorkDir:    workDir,
			InsertLine: s.insertLine,
			ExecLines:  s.stmtInfo.Lines,
			LineMap:    s.stmtInfo.LineMap,
			Functions:  fnSpecs,
		})
	}

	profiles, err := trace.ParseAll(tf, specs)
	if err != nil {
		return nil, exitCode, fmt.Errorf("parsing trace: %w", err)
	}

	if ctx.Err() != nil {
		return nil, 130, fmt.Errorf("execution interrupted: %w", ctx.Err())
	}

	return profiles, exitCode, nil
}

func prepareAndInstrumentTarget(id int, rt resolvedTarget, tracePath string) (*targetState, error) {
	lockPath := lockFilePath(rt.realPath)
	lockFile, err := acquireTargetLock(lockPath, rt.displayPath)
	if err != nil {
		return nil, err
	}

	st := &targetState{
		id:          id,
		displayPath: rt.displayPath,
		absPath:     rt.absPath,
		realPath:    rt.realPath,
		perm:        rt.perm,
		lockPath:    lockPath,
		lockFile:    lockFile,
	}

	backupPath := filepath.Join(filepath.Dir(rt.realPath), "."+filepath.Base(rt.realPath)+".bashcoat.bak")
	if _, err := os.Lstat(backupPath); err == nil {
		return st, fmt.Errorf("leftover backup %s detected from a previous interrupted run; inspect and restore or remove it before running bashcoat", backupPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return st, fmt.Errorf("checking backup path %s: %w", backupPath, err)
	}

	src, err := os.ReadFile(rt.realPath)
	if err != nil {
		return st, fmt.Errorf("reading target %s: %w", rt.displayPath, err)
	}
	if instrument.HasMarker(src) {
		return st, fmt.Errorf("target %s already contains bashcoat instrumentation marker; restore original source before running bashcoat", rt.displayPath)
	}

	stmtInfo, err := executable.DetectStatements(src)
	if err != nil {
		return st, fmt.Errorf("parsing target %s: %w", rt.displayPath, err)
	}

	// Persist untouched original content into an exclusive backup file before modifying the target.
	bf, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, rt.perm)
	if err != nil {
		return st, fmt.Errorf("creating backup %s: %w", backupPath, err)
	}
	if _, err := bf.Write(src); err != nil {
		_ = bf.Close()
		_ = os.Remove(backupPath)
		return st, fmt.Errorf("writing backup %s: %w", backupPath, err)
	}
	if err := bf.Sync(); err != nil {
		_ = bf.Close()
		_ = os.Remove(backupPath)
		return st, fmt.Errorf("syncing backup %s: %w", backupPath, err)
	}
	if err := bf.Close(); err != nil {
		_ = os.Remove(backupPath)
		return st, fmt.Errorf("closing backup %s: %w", backupPath, err)
	}
	st.backupPath = backupPath
	st.originalContent = src
	st.insertLine = 0
	st.stmtInfo = stmtInfo

	instrumented, insertLine := instrument.Script(src, tracePath, id)
	st.insertLine = insertLine

	if err := writeFileAtomic(rt.realPath, instrumented, rt.perm); err != nil {
		return st, fmt.Errorf("instrumenting target %s: %w", rt.displayPath, err)
	}
	st.instrumented = true

	return st, nil
}

func writeFileAtomic(targetPath string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(targetPath)
	tmp, err := os.CreateTemp(dir, ".bashcoat-tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func acquireTargetLock(lockPath, displayPath string) (*os.File, error) {
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, fmt.Errorf("opening lock for %s: %w", displayPath, err)
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("target %s is currently locked by another bashcoat process: %w", displayPath, err)
		}
		// Verify the locked FD still corresponds to the file at lockPath (guards against unlink races).
		pathInfo, pathErr := os.Stat(lockPath)
		fdInfo, fdErr := f.Stat()
		if pathErr == nil && fdErr == nil && os.SameFile(pathInfo, fdInfo) {
			return f, nil
		}
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
}

func releaseTargetLock(lockPath string, lockFile *os.File) {
	if lockPath != "" {
		pathInfo, pathErr := os.Stat(lockPath)
		fdInfo, fdErr := lockFile.Stat()
		if pathErr == nil && fdErr == nil && os.SameFile(pathInfo, fdInfo) {
			_ = os.Remove(lockPath)
		}
	}
	_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	_ = lockFile.Close()
}

func lockFilePath(realPath string) string {
	sum := sha256.Sum256([]byte(realPath))
	return filepath.Join(os.TempDir(), fmt.Sprintf("bashcoat-%d-%s.lock", os.Getuid(), hex.EncodeToString(sum[:8])))
}

func cleanChildEnv(environ []string) []string {
	cleaned := make([]string, 0, len(environ))
	for _, entry := range environ {
		if strings.HasPrefix(entry, "__bashcoat_fd=") ||
			strings.HasPrefix(entry, "__bashcoat_file=") ||
			strings.HasPrefix(entry, "__bashcoat_active_file=") {
			continue
		}
		cleaned = append(cleaned, entry)
	}
	return cleaned
}

func resolveTargets(workDir string, rawPatterns []string) ([]resolvedTarget, error) {
	seenReal := make(map[string]bool)
	var targets []resolvedTarget

	for _, pat := range rawPatterns {
		absPat := pat
		if !filepath.IsAbs(pat) {
			absPat = filepath.Join(workDir, pat)
		}
		absPat = filepath.Clean(absPat)

		matches, err := globPattern(workDir, pat, absPat)
		if err != nil {
			return nil, err
		}

		matchedRegular := 0
		for _, m := range matches {
			cleanAbs := filepath.Clean(m)
			info, statErr := os.Stat(cleanAbs)
			if statErr != nil {
				return nil, fmt.Errorf("stat %s: %w", cleanAbs, statErr)
			}
			if info.IsDir() || !info.Mode().IsRegular() {
				continue
			}
			matchedRegular++

			realPath, err := filepath.EvalSymlinks(cleanAbs)
			if err != nil {
				realPath = cleanAbs
			}
			realPath = filepath.Clean(realPath)

			if seenReal[realPath] {
				continue
			}
			seenReal[realPath] = true

			displayPath := cleanAbs
			if rel, relErr := filepath.Rel(workDir, cleanAbs); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				displayPath = rel
			}

			targets = append(targets, resolvedTarget{
				displayPath: displayPath,
				absPath:     cleanAbs,
				realPath:    realPath,
				perm:        info.Mode().Perm(),
			})
		}

		if matchedRegular == 0 {
			return nil, fmt.Errorf("no matching files found for target pattern %q", pat)
		}
	}

	return targets, nil
}

func globPattern(workDir, pat, absPat string) ([]string, error) {
	cleanPat := filepath.Clean(pat)
	base, subPat := doublestar.SplitPattern(filepath.ToSlash(cleanPat))

	var matches []string
	if subPat != "" {
		searchBase := filepath.FromSlash(base)
		if !filepath.IsAbs(cleanPat) {
			searchBase = filepath.Join(workDir, searchBase)
		}
		relMatches, err := doublestar.Glob(os.DirFS(searchBase), subPat)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			// Fall back to direct stat if pattern is a literal path with glob characters (e.g. "foo[.sh")
			if info, statErr := os.Stat(absPat); statErr == nil && !info.IsDir() {
				return []string{absPat}, nil
			}
			return nil, fmt.Errorf("invalid pattern %q: %w", pat, err)
		}
		for _, rm := range relMatches {
			matches = append(matches, filepath.Join(searchBase, filepath.FromSlash(rm)))
		}
	}

	if len(matches) == 0 {
		if info, statErr := os.Stat(absPat); statErr == nil && !info.IsDir() {
			matches = []string{absPat}
		}
	}
	return matches, nil
}
