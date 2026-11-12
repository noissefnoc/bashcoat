package instrument

import (
	"bytes"
	"fmt"
	"strings"
)

const TracePrefix = "+bashcoat:"

// Preamble returns the single-line Bash snippet configuring coverage tracing via BASH_XTRACEFD.
// Works seamlessly across Bash 4.x/5.x and falls back to DEBUG trap on Bash 3.2 (macOS default).
func Preamble(traceFilePath string) string {
	escapedPath := strings.ReplaceAll(traceFilePath, `'`, `'\''`)
	return fmt.Sprintf(
		`__bashcoat_file='%s'; if (( BASH_VERSINFO[0] >= 4 )); then if [[ -z "${__bashcoat_fd:-}" ]]; then exec {__bashcoat_fd}>>"${__bashcoat_file}"; export __bashcoat_fd; fi; BASH_XTRACEFD="${__bashcoat_fd}"; PS4='%s${BASH_SOURCE[0]:-}:${LINENO}: '; set -x; else set -o functrace; trap 'printf "%s%%s:%%s: \n" "${BASH_SOURCE[0]:-}" "${LINENO}" >> "${__bashcoat_file}"' DEBUG; fi`,
		escapedPath, TracePrefix, TracePrefix,
	)
}

// Script injects the coverage preamble into src and returns the modified bytes along with
// the 1-based line number where the preamble was inserted.
// It preserves shebang lines and contiguous top-level comments.
func Script(src []byte, traceFilePath string) ([]byte, int) {
	preamble := Preamble(traceFilePath)
	insertIdx, insertLine := findInsertionPoint(src)

	var buf bytes.Buffer
	buf.Grow(len(src) + len(preamble) + 2)
	buf.Write(src[:insertIdx])
	buf.WriteString(preamble)
	buf.WriteByte('\n')
	buf.Write(src[insertIdx:])

	return buf.Bytes(), insertLine
}

func findInsertionPoint(src []byte) (byteOffset, lineNumber int) {
	if len(src) == 0 {
		return 0, 1
	}

	lines := bytes.SplitAfter(src, []byte{'\n'})
	if len(lines) == 0 {
		return 0, 1
	}

	byteOffset = 0
	lineNumber = 1

	// 1. Skip shebang if present
	if bytes.HasPrefix(lines[0], []byte("#!")) {
		byteOffset += len(lines[0])
		lineNumber++
		lines = lines[1:]
	}

	// 2. Skip contiguous header comments and blank lines
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] == '#' {
			byteOffset += len(line)
			lineNumber++
		} else {
			break
		}
	}

	return byteOffset, lineNumber
}
