// Package cli implements the command-line entrypoint and flag handling for bashcoat.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/noissefnoc/bashcoat/internal/report"
	"github.com/noissefnoc/bashcoat/internal/runner"
	"github.com/noissefnoc/bashcoat/internal/trace"
)

// Version is the CLI version string, overridable via -ldflags at build time.
var Version = "dev"

const (
	formatLCOV      = "lcov"
	formatCobertura = "cobertura"
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(val string) error {
	if val == "" {
		return fmt.Errorf("target pattern must not be empty")
	}
	*s = append(*s, val)
	return nil
}

// Run executes the bashcoat CLI application.
func Run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	var targets stringSliceFlag
	var outputFile string
	var format string
	var showSummary bool
	var quiet bool
	var version bool

	fs := flag.NewFlagSet("bashcoat", flag.ContinueOnError)
	fs.SetOutput(errOut)

	fs.Var(&targets, "target", "Target script path or glob pattern to measure (repeatable, e.g. -target 'src/**/*.sh')")
	fs.Var(&targets, "t", "Alias for -target")
	fs.StringVar(&outputFile, "output", "lcov.info", `Report output file path (default: "lcov.info" for lcov, "coverage.xml" for cobertura; set to empty to disable)`)
	fs.StringVar(&outputFile, "o", "lcov.info", "Alias for -output")
	fs.StringVar(&format, "format", formatLCOV, "Report output format: lcov or cobertura")
	fs.StringVar(&format, "f", formatLCOV, "Alias for -format")
	fs.BoolVar(&showSummary, "summary", true, "Print coverage summary table to stdout")
	fs.BoolVar(&quiet, "quiet", false, "Suppress summary table and diagnostic logs")
	fs.BoolVar(&quiet, "q", false, "Alias for -quiet")
	fs.BoolVar(&version, "version", false, "Print version information")
	fs.BoolVar(&version, "v", false, "Alias for -version")

	fs.Usage = func() {
		_, _ = fmt.Fprintf(errOut, "bashcoat - Fast, zero-dependency C0 code coverage for Bash 5+ scripts\n\n")
		_, _ = fmt.Fprintf(errOut, "Usage:\n")
		_, _ = fmt.Fprintf(errOut, "  bashcoat [options] -t <script.sh> -- <test_command> [args...]\n\n")
		_, _ = fmt.Fprintf(errOut, "Examples:\n")
		_, _ = fmt.Fprintf(errOut, "  bashcoat -t src/lib.sh -- bats test/lib.bats\n")
		_, _ = fmt.Fprintf(errOut, "  bashcoat -t 'src/**/*.sh' -o coverage.lcov -- ./run_tests.sh\n")
		_, _ = fmt.Fprintf(errOut, "  bashcoat -t 'src/**/*.sh' -f cobertura -o coverage.xml -- bats test/\n\n")
		_, _ = fmt.Fprintf(errOut, "Options:\n")
		fs.PrintDefaults()
	}

	flagArgs, cmdArgs := splitFlagAndCmdArgs(args)

	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}

	if version {
		_, _ = fmt.Fprintf(out, "bashcoat version %s\n", resolveVersion())
		return 0
	}

	format = strings.ToLower(strings.TrimSpace(format))
	if format != formatLCOV && format != formatCobertura {
		_, _ = fmt.Fprintf(errOut, "error: unsupported format %q (supported: lcov, cobertura)\n", format)
		fs.Usage()
		return 2
	}

	outputExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "o" || f.Name == "output" {
			outputExplicit = true
		}
	})
	if !outputExplicit {
		outputFile = defaultOutputFile(format)
	}

	if len(targets) == 0 {
		_, _ = fmt.Fprintln(errOut, "error: at least one -target script is required")
		fs.Usage()
		return 2
	}

	if len(cmdArgs) == 0 {
		_, _ = fmt.Fprintln(errOut, "error: command to run after '--' is required (e.g. bashcoat -t script.sh -- bats test.bats)")
		fs.Usage()
		return 2
	}

	profiles, exitCode, err := runner.Run(ctx, runner.Config{
		Targets: targets,
		Command: cmdArgs,
		Stdin:   in,
		Stdout:  out,
		Stderr:  errOut,
	})
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "error: %v\n", err)
		if exitCode == 0 {
			return 1
		}
		return exitCode
	}

	// Write coverage report file if configured
	if outputFile != "" {
		if err := writeReportFile(outputFile, format, profiles); err != nil {
			_, _ = fmt.Fprintf(errOut, "error writing %s report to %s: %v\n", format, outputFile, err)
			return 1
		}
	}

	// Print summary table if requested
	if showSummary && !quiet {
		if err := report.WriteSummary(out, profiles); err != nil {
			_, _ = fmt.Fprintf(errOut, "error writing summary table: %v\n", err)
			return 1
		}
	}

	return exitCode
}

func defaultOutputFile(format string) string {
	if format == formatCobertura {
		return "coverage.xml"
	}
	return "lcov.info"
}

func resolveVersion() string {
	if Version != "" && Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// HasQuietFlag reports whether -q or -quiet is enabled in the bashcoat flag arguments before '--'.
func HasQuietFlag(args []string) bool {
	flagArgs, _ := splitFlagAndCmdArgs(args)
	quiet := false
	for _, arg := range flagArgs {
		switch arg {
		case "-q", "--q", "-quiet", "--quiet", "-q=true", "--q=true", "-quiet=true", "--quiet=true":
			quiet = true
		case "-q=false", "--q=false", "-quiet=false", "--quiet=false":
			quiet = false
		}
	}
	return quiet
}

func writeReportFile(outputFile, format string, profiles []*trace.Profile) error {
	dir := filepath.Dir(outputFile)
	if dir == "" {
		dir = "."
	}
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	tmp, err := os.CreateTemp(dir, ".bashcoat-report-*")
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

	switch format {
	case formatLCOV:
		err = report.WriteLCOV(tmp, profiles)
	case formatCobertura:
		err = report.WriteCoberturaWithOptions(tmp, profiles, report.CoberturaOptions{
			Version: resolveVersion(),
		})
	default:
		err = fmt.Errorf("unsupported format %q", format)
	}
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, outputFile); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// splitFlagAndCmdArgs separates bashcoat flags from the target command following '--',
// respecting string flags that consume the subsequent argument as a value.
func splitFlagAndCmdArgs(args []string) ([]string, []string) {
	valueFlags := map[string]bool{
		"-t":       true,
		"--t":      true,
		"-target":  true,
		"--target": true,
		"-o":       true,
		"--o":      true,
		"-output":  true,
		"--output": true,
		"-f":       true,
		"--f":      true,
		"-format":  true,
		"--format": true,
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return args[:i], args[i+1:]
		}
		if valueFlags[arg] {
			i++ // skip flag value
		}
	}
	return args, nil
}
