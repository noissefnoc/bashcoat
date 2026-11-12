package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/noissefnoc/bashcoat/internal/report"
	"github.com/noissefnoc/bashcoat/internal/runner"
)

var Version = "dev"

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(val string) error {
	for _, part := range strings.Split(val, ",") {
		p := strings.TrimSpace(part)
		if p != "" {
			*s = append(*s, p)
		}
	}
	return nil
}

// Run executes the bashcoat CLI application.
func Run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	var targets stringSliceFlag
	var outputFile string
	var showSummary bool
	var quiet bool
	var version bool

	fs := flag.NewFlagSet("bashcoat", flag.ContinueOnError)
	fs.SetOutput(errOut)

	fs.Var(&targets, "target", "Target script path or glob pattern to measure (repeatable, e.g. -target src/**/*.sh)")
	fs.Var(&targets, "t", "Alias for -target")
	fs.StringVar(&outputFile, "output", "lcov.info", "LCOV report output file path (set to empty to disable)")
	fs.StringVar(&outputFile, "o", "lcov.info", "Alias for -output")
	fs.BoolVar(&showSummary, "summary", true, "Print coverage summary table to stdout")
	fs.BoolVar(&quiet, "quiet", false, "Suppress summary table and diagnostic logs")
	fs.BoolVar(&quiet, "q", false, "Alias for -quiet")
	fs.BoolVar(&version, "version", false, "Print version information")
	fs.BoolVar(&version, "v", false, "Alias for -version")

	fs.Usage = func() {
		fmt.Fprintf(errOut, "bashcoat - Fast, zero-dependency C0 code coverage for Bash scripts\n\n")
		fmt.Fprintf(errOut, "Usage:\n")
		fmt.Fprintf(errOut, "  bashcoat [options] -t <script.sh> -- <test_command> [args...]\n\n")
		fmt.Fprintf(errOut, "Examples:\n")
		fmt.Fprintf(errOut, "  bashcoat -t src/lib.sh -- bats test/lib.bats\n")
		fmt.Fprintf(errOut, "  bashcoat -t 'src/*.sh' -o coverage.lcov -- ./run_tests.sh\n\n")
		fmt.Fprintf(errOut, "Options:\n")
		fs.PrintDefaults()
	}

	// Separate flags from the target command following '--'
	var flagArgs, cmdArgs []string
	dashDashIdx := -1
	for i, arg := range args {
		if arg == "--" {
			dashDashIdx = i
			break
		}
	}
	if dashDashIdx != -1 {
		flagArgs = args[:dashDashIdx]
		cmdArgs = args[dashDashIdx+1:]
	} else {
		flagArgs = args
	}

	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}

	if version {
		fmt.Fprintf(out, "bashcoat version %s\n", Version)
		return 0
	}

	if len(targets) == 0 {
		fmt.Fprintln(errOut, "error: at least one -target script is required")
		fs.Usage()
		return 2
	}

	if len(cmdArgs) == 0 {
		fmt.Fprintln(errOut, "error: command to run after '--' is required (e.g. bashcoat -t script.sh -- bats test.bats)")
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
		fmt.Fprintf(errOut, "error: %v\n", err)
		return 1
	}

	// Write LCOV output file if configured
	if outputFile != "" {
		f, err := os.Create(outputFile)
		if err != nil {
			fmt.Fprintf(errOut, "error creating LCOV file %s: %v\n", outputFile, err)
			return 1
		}
		defer f.Close()

		if err := report.WriteLCOV(f, profiles); err != nil {
			fmt.Fprintf(errOut, "error writing LCOV report: %v\n", err)
			return 1
		}
	}

	// Print summary table if requested
	if showSummary && !quiet {
		_ = report.WriteSummary(out, profiles)
	}

	return exitCode
}
