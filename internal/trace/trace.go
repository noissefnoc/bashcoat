package trace

import (
	"bufio"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/noissefnoc/bashcoat/internal/instrument"
)

// Profile holds the line execution counts and summary statistics for a single source file.
type Profile struct {
	SourceFile string
	Hits       map[int]int
	LinesFound int
	LinesHit   int
}

// Coverage returns the line coverage percentage (0.0 to 100.0).
func (p *Profile) Coverage() float64 {
	if p.LinesFound == 0 {
		return 0.0
	}
	return float64(p.LinesHit) / float64(p.LinesFound) * 100.0
}

// Parse extracts execution hits from traceReader for the specified targetFile.
// insertLine is the 1-based line number where the instrumentation preamble was inserted.
// executableLines defines all valid source line numbers that can be executed.
func Parse(traceReader io.Reader, targetFile string, insertLine int, executableLines map[int]bool) (*Profile, error) {
	hits := make(map[int]int)
	cleanTarget := filepath.Clean(targetFile)
	targetBase := filepath.Base(cleanTarget)

	scanner := bufio.NewScanner(traceReader)
	// Allow scanning long trace lines (up to 1MB)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		idx := strings.Index(line, instrument.TracePrefix)
		if idx == -1 {
			continue
		}

		payload := line[idx+len(instrument.TracePrefix):]
		// Payload format: <filepath>:<lineno>: ...
		firstColon := strings.IndexByte(payload, ':')
		if firstColon == -1 {
			continue
		}
		sourcePath := payload[:firstColon]

		// Check if sourcePath corresponds to targetFile
		if !matchesTarget(sourcePath, cleanTarget, targetBase) {
			continue
		}

		secondColon := strings.IndexByte(payload[firstColon+1:], ':')
		if secondColon == -1 {
			continue
		}
		lineStr := payload[firstColon+1 : firstColon+1+secondColon]
		rawLineNum, err := strconv.Atoi(strings.TrimSpace(lineStr))
		if err != nil || rawLineNum <= 0 {
			continue
		}

		// Adjust line number for the 1-line preamble inserted before execution
		actualLine := rawLineNum
		if actualLine == insertLine {
			// Executing the preamble line itself; ignore
			continue
		}
		if actualLine > insertLine {
			actualLine--
		}

		hits[actualLine]++
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Ensure all known executable lines are represented in Hits (default 0 hits if missed)
	for l := range executableLines {
		if _, ok := hits[l]; !ok {
			hits[l] = 0
		}
	}

	linesFound := len(executableLines)
	if linesFound == 0 {
		linesFound = len(hits)
	}

	linesHit := 0
	for _, count := range hits {
		if count > 0 {
			linesHit++
		}
	}

	return &Profile{
		SourceFile: cleanTarget,
		Hits:       hits,
		LinesFound: linesFound,
		LinesHit:   linesHit,
	}, nil
}

func matchesTarget(source, target, targetBase string) bool {
	if source == target {
		return true
	}
	cleanSource := filepath.Clean(source)
	if cleanSource == target {
		return true
	}
	if filepath.Base(cleanSource) == targetBase {
		return true
	}
	return strings.HasSuffix(cleanSource, target) || strings.HasSuffix(target, cleanSource)
}
