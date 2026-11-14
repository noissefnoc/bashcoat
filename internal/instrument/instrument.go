// Package instrument injects Bash 5+ coverage tracing preambles into target scripts.
package instrument

import (
	"bytes"
	"fmt"
	"strings"
)

// Trace format markers and instrumentation detection constants.
const (
	TracePrefix        = "+bashcoat|"
	TraceMapPrefix     = "+bashcoat_map|"
	TraceEndMarker     = "|bc_end| "
	InstrumentedMarker = "__bashcoat_file="
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Preamble returns the single-line Bash 5.0+ snippet configuring coverage tracing via BASH_XTRACEFD.
// If invoked under Bash < 5.0 (or non-Bash shells), it prints an error to stderr and exits immediately.
func Preamble(traceFilePath string, targetID int) string {
	escapedPath := strings.ReplaceAll(traceFilePath, `'`, `'\''`)
	return fmt.Sprintf(
		`if [ -z "${BASH_VERSION:-}" ] || [ "${BASH_VERSINFO[0]:-0}" -lt 5 ] 2>/dev/null; then printf 'bashcoat: Bash 5.0+ is required (found %%s)\n' "${BASH_VERSION:-unknown}" >&2; exit 1; fi; __bashcoat_file='%s'; if [[ -z "${__bashcoat_fd:-}" || "${__bashcoat_active_file:-}" != "${__bashcoat_file}" ]] || ! ( : >&"${__bashcoat_fd}" ) 2>/dev/null; then exec {__bashcoat_fd}>>"${__bashcoat_file}"; __bashcoat_active_file="${__bashcoat_file}"; export __bashcoat_fd __bashcoat_active_file; fi; printf '%s%d|@p%%s:%%s|%%s|%%s\n' "${BASHPID:-$$}" "$$" "$PWD" "${BASH_SOURCE[0]:-}" >&"${__bashcoat_fd}"; BASH_XTRACEFD="${__bashcoat_fd}"; PS4='%s${LINENO}|@p${BASHPID:-$$}:$$|${BASH_SOURCE[0]:-}%s'; set -x`,
		escapedPath, TraceMapPrefix, targetID, TracePrefix, TraceEndMarker,
	)
}

// HasMarker reports whether src appears to already contain bashcoat instrumentation.
func HasMarker(src []byte) bool {
	return bytes.Contains(src, []byte(InstrumentedMarker))
}

// Script injects the coverage preamble into src and returns the modified bytes along with
// the 1-based line number where the preamble was inserted.
// It preserves UTF-8 BOM, shebang lines, and contiguous top-level comments.
func Script(src []byte, traceFilePath string, targetID int) ([]byte, int) {
	preamble := Preamble(traceFilePath, targetID)
	insertIdx, insertLine, bomLen := findInsertionPoint(src)

	var buf bytes.Buffer
	buf.Grow(len(src) + len(preamble) + 3)
	buf.Write(src[:insertIdx])
	if insertIdx > bomLen && src[insertIdx-1] != '\n' {
		buf.WriteByte('\n')
	}
	buf.WriteString(preamble)
	buf.WriteByte('\n')
	buf.Write(src[insertIdx:])

	return buf.Bytes(), insertLine
}

func findInsertionPoint(src []byte) (byteOffset, lineNumber, bomLen int) {
	if len(src) == 0 {
		return 0, 1, 0
	}

	offset := 0
	if bytes.HasPrefix(src, utf8BOM) {
		offset = len(utf8BOM)
		src = src[offset:]
		if len(src) == 0 {
			return offset, 1, offset
		}
	}

	lines := bytes.SplitAfter(src, []byte{'\n'})
	byteOffset = offset
	lineNumber = 1

	// 1. Skip shebang if present
	if len(lines) > 0 && bytes.HasPrefix(lines[0], []byte("#!")) {
		byteOffset += len(lines[0])
		lineNumber++
		lines = lines[1:]
	}

	// 2. Skip contiguous header comments and blank lines
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] == '#' {
			byteOffset += len(line)
			lineNumber++
		} else {
			break
		}
	}

	return byteOffset, lineNumber, offset
}
