// Package trace parses Bash xtrace coverage logs into per-file execution profiles.
package trace

import (
	"bufio"
	"errors"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/noissefnoc/bashcoat/internal/instrument"
)

// FuncSpec describes a statically detected function declaration within a target file.
type FuncSpec struct {
	Name      string
	StartLine int
	EndLine   int
	ExecLines map[int]bool
	FirstLine int
}

// FunctionCoverage holds execution statistics for a single function.
type FunctionCoverage struct {
	Name       string
	StartLine  int
	EndLine    int
	Calls      int
	LinesFound int
	LinesHit   int
	LineNums   []int // sorted 1-based executable lines directly owned by this function
}

// Profile holds the line and function execution counts and summary statistics for a single source file.
type Profile struct {
	SourceFile     string
	Hits           map[int]int
	LinesFound     int
	LinesHit       int
	Functions      []FunctionCoverage
	FunctionsFound int
	FunctionsHit   int
}

// Coverage returns the line coverage percentage (0.0 to 100.0).
func (p *Profile) Coverage() float64 {
	if p.LinesFound == 0 {
		return 0.0
	}
	return float64(p.LinesHit) / float64(p.LinesFound) * 100.0
}

// FunctionCoverageRate returns the function coverage percentage (0.0 to 100.0).
func (p *Profile) FunctionCoverageRate() float64 {
	if p.FunctionsFound == 0 {
		return 0.0
	}
	return float64(p.FunctionsHit) / float64(p.FunctionsFound) * 100.0
}

// TargetSpec describes a single instrumented target for trace attribution.
type TargetSpec struct {
	ID         int
	SourceFile string
	AbsPath    string
	RealPath   string
	WorkDir    string
	InsertLine int
	ExecLines  map[int]bool
	LineMap    map[int]int
	Functions  []FuncSpec
}

// Parse extracts execution hits from traceReader for a single targetFile.
func Parse(traceReader io.Reader, targetFile string, insertLine int, executableLines map[int]bool) (*Profile, error) {
	cleanTarget := filepath.Clean(targetFile)
	absTarget, err := filepath.Abs(cleanTarget)
	if err != nil {
		absTarget = cleanTarget
	}
	realTarget, err := filepath.EvalSymlinks(absTarget)
	if err != nil {
		realTarget = absTarget
	}

	profiles, err := ParseAll(traceReader, []TargetSpec{
		{
			ID:         0,
			SourceFile: cleanTarget,
			AbsPath:    absTarget,
			RealPath:   realTarget,
			InsertLine: insertLine,
			ExecLines:  executableLines,
		},
	})
	if err != nil {
		return nil, err
	}
	return profiles[0], nil
}

// ParseAll parses the trace stream in a single pass and returns profiles matching targets in order.
func ParseAll(traceReader io.Reader, targets []TargetSpec) ([]*Profile, error) {
	type pidSourceKey struct {
		pid    string
		source string
	}

	const ambiguousIdx = -2

	byID := make(map[int]int, len(targets))
	absToIdx := make(map[string]int, len(targets))
	realToIdx := make(map[string]int, len(targets))
	hitsPerTarget := make([]map[int]int, len(targets))

	for i, t := range targets {
		byID[t.ID] = i
		if t.AbsPath != "" {
			absToIdx[filepath.Clean(t.AbsPath)] = i
		}
		if t.RealPath != "" {
			realToIdx[filepath.Clean(t.RealPath)] = i
		}
		hitsPerTarget[i] = make(map[int]int)
	}

	// Tracks dynamic BASH_SOURCE[0] -> target slice index from +bashcoat_map lines,
	// scoped by BASHPID and root shell PID ($$) to avoid collisions on identical relative paths.
	pidSourceToIdx := make(map[pidSourceKey]int)
	rootSourceToIdx := make(map[pidSourceKey]int)
	legacySourceToIdx := make(map[string]int)

	recordDynamicMapping := func(bashPID, rootPID, bashSource string, idx int) {
		if bashPID != "" {
			pidSourceToIdx[pidSourceKey{pid: bashPID, source: bashSource}] = idx
			if rootPID != "" {
				rKey := pidSourceKey{pid: rootPID, source: bashSource}
				if existing, ok := rootSourceToIdx[rKey]; !ok {
					rootSourceToIdx[rKey] = idx
				} else if existing != idx {
					rootSourceToIdx[rKey] = ambiguousIdx
				}
			}
			return
		}
		if existing, ok := legacySourceToIdx[bashSource]; !ok {
			legacySourceToIdx[bashSource] = idx
		} else if existing != idx {
			legacySourceToIdx[bashSource] = ambiguousIdx
		}
	}

	resolveTargetIdx := func(bashPID, rootPID, sourcePath string) (int, bool) {
		if sourcePath == "" {
			return -1, false
		}
		clean := filepath.Clean(sourcePath)

		// 1. Exact absolute path match (or symlink-resolved match) takes highest precedence.
		if filepath.IsAbs(clean) {
			if idx, ok := absToIdx[clean]; ok {
				return idx, true
			}
			if idx, ok := realToIdx[clean]; ok {
				return idx, true
			}
			if resolved, err := filepath.EvalSymlinks(clean); err == nil {
				if idx, ok := realToIdx[filepath.Clean(resolved)]; ok {
					return idx, true
				}
			}
			return -1, false
		}

		// 2. Process-scoped dynamic mapping emitted at runtime via +bashcoat_map.
		if bashPID != "" {
			if idx, ok := pidSourceToIdx[pidSourceKey{pid: bashPID, source: sourcePath}]; ok {
				return idx, true
			}
			if rootPID != "" {
				if idx, ok := rootSourceToIdx[pidSourceKey{pid: rootPID, source: sourcePath}]; ok && idx >= 0 {
					return idx, true
				}
			}
		}

		// 3. Legacy un-scoped dynamic mapping (for trace streams without @p<pid>:<root> metadata).
		if bashPID == "" {
			if idx, ok := legacySourceToIdx[sourcePath]; ok && idx >= 0 {
				return idx, true
			}
		}

		// 4. Relative path resolved against each target's WorkDir.
		for i, t := range targets {
			if t.WorkDir == "" {
				continue
			}
			joined := filepath.Clean(filepath.Join(t.WorkDir, clean))
			if joined == filepath.Clean(t.AbsPath) || joined == filepath.Clean(t.RealPath) {
				return i, true
			}
			if resolved, err := filepath.EvalSymlinks(joined); err == nil {
				if filepath.Clean(resolved) == filepath.Clean(t.RealPath) {
					return i, true
				}
			}
		}

		return -1, false
	}

	reader := bufio.NewReader(traceReader)
	const traceEndToken = "|bc_end|"

	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}

		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed != "" {
			if strings.HasPrefix(trimmed, instrument.TraceMapPrefix) {
				handleMapLine(trimmed[len(instrument.TraceMapPrefix):], byID, targets, recordDynamicMapping)
			} else {
				plusCount := 0
				for plusCount < len(trimmed) && trimmed[plusCount] == '+' {
					plusCount++
				}
				if plusCount > 0 && strings.HasPrefix(trimmed[plusCount:], "bashcoat|") {
					payload := trimmed[plusCount+len("bashcoat|"):]
					if firstPipe := strings.IndexByte(payload, '|'); firstPipe != -1 {
						lineStr := payload[:firstPipe]
						rest := payload[firstPipe+1:]
						if endIdx := strings.Index(rest, traceEndToken); endIdx != -1 {
							if rawLineNum, convErr := strconv.Atoi(strings.TrimSpace(lineStr)); convErr == nil && rawLineNum > 0 {
								bashPID, rootPID, sourcePath := parseTraceSourceField(rest[:endIdx])
								if idx, ok := resolveTargetIdx(bashPID, rootPID, sourcePath); ok {
									t := targets[idx]
									if rawLineNum != t.InsertLine {
										actualLine := rawLineNum
										if actualLine > t.InsertLine {
											actualLine--
										}
										if len(t.LineMap) > 0 {
											if mapped, exists := t.LineMap[actualLine]; exists {
												actualLine = mapped
											}
										}
										if len(t.ExecLines) == 0 || t.ExecLines[actualLine] {
											hitsPerTarget[idx][actualLine]++
										}
									}
								}
							}
						}
					}
				}
			}
		}

		if errors.Is(err, io.EOF) {
			break
		}
	}

	profiles := make([]*Profile, len(targets))
	for i, t := range targets {
		hits := hitsPerTarget[i]
		for l := range t.ExecLines {
			if _, ok := hits[l]; !ok {
				hits[l] = 0
			}
		}

		linesFound := len(t.ExecLines)
		if linesFound == 0 {
			linesFound = len(hits)
		}

		linesHit := 0
		for _, count := range hits {
			if count > 0 {
				linesHit++
			}
		}

		sourceFile := t.SourceFile
		if sourceFile == "" {
			sourceFile = filepath.Clean(t.AbsPath)
		}

		fnCovs, fnHit := buildFunctionCoverage(t.Functions, hits)

		profiles[i] = &Profile{
			SourceFile:     sourceFile,
			Hits:           hits,
			LinesFound:     linesFound,
			LinesHit:       linesHit,
			Functions:      fnCovs,
			FunctionsFound: len(fnCovs),
			FunctionsHit:   fnHit,
		}
	}

	return profiles, nil
}

// buildFunctionCoverage maps line execution counts onto statically detected function
// declarations. Duplicate declarations sharing the same name within a file (e.g. inside
// conditional branches) are aggregated by name for LCOV/Cobertura compatibility.
func buildFunctionCoverage(specs []FuncSpec, hits map[int]int) ([]FunctionCoverage, int) {
	if len(specs) == 0 {
		return nil, 0
	}

	type funcAgg struct {
		name      string
		startLine int
		endLine   int
		execLines map[int]bool
		calls     int
	}

	byName := make(map[string]*funcAgg, len(specs))
	order := make([]*funcAgg, 0, len(specs))

	for _, fn := range specs {
		defCalls := 0
		if fn.FirstLine > 0 && hits[fn.FirstLine] > 0 {
			defCalls = hits[fn.FirstLine]
		} else {
			minNonZero := 0
			for l := range fn.ExecLines {
				if c := hits[l]; c > 0 && (minNonZero == 0 || c < minNonZero) {
					minNonZero = c
				}
			}
			defCalls = minNonZero
		}

		agg, exists := byName[fn.Name]
		if !exists {
			agg = &funcAgg{
				name:      fn.Name,
				startLine: fn.StartLine,
				endLine:   fn.EndLine,
				execLines: make(map[int]bool, len(fn.ExecLines)),
			}
			byName[fn.Name] = agg
			order = append(order, agg)
		} else {
			if fn.StartLine > 0 && (agg.startLine == 0 || fn.StartLine < agg.startLine) {
				agg.startLine = fn.StartLine
			}
			if fn.EndLine > agg.endLine {
				agg.endLine = fn.EndLine
			}
		}

		for l := range fn.ExecLines {
			agg.execLines[l] = true
		}
		agg.calls += defCalls
	}

	covs := make([]FunctionCoverage, 0, len(order))
	fnHit := 0

	for _, agg := range order {
		lineNums := make([]int, 0, len(agg.execLines))
		for l := range agg.execLines {
			lineNums = append(lineNums, l)
		}
		sort.Ints(lineNums)

		linesHit := 0
		for _, l := range lineNums {
			if hits[l] > 0 {
				linesHit++
			}
		}
		if linesHit > 0 {
			fnHit++
		}

		covs = append(covs, FunctionCoverage{
			Name:       agg.name,
			StartLine:  agg.startLine,
			EndLine:    agg.endLine,
			Calls:      agg.calls,
			LinesFound: len(lineNums),
			LinesHit:   linesHit,
			LineNums:   lineNums,
		})
	}

	sort.Slice(covs, func(i, j int) bool {
		if covs[i].StartLine != covs[j].StartLine {
			return covs[i].StartLine < covs[j].StartLine
		}
		return covs[i].Name < covs[j].Name
	})

	return covs, fnHit
}

func parsePIDTag(tag string) (bashPID, rootPID string, ok bool) {
	if !strings.HasPrefix(tag, "@p") {
		return "", "", false
	}
	body := tag[2:]
	colon := strings.IndexByte(body, ':')
	if colon == -1 {
		return body, "", body != ""
	}
	return body[:colon], body[colon+1:], body[:colon] != ""
}

func parseTraceSourceField(raw string) (bashPID, rootPID, sourcePath string) {
	if strings.HasPrefix(raw, "@p") {
		if pipe := strings.IndexByte(raw, '|'); pipe != -1 {
			if bp, rp, ok := parsePIDTag(raw[:pipe]); ok {
				return bp, rp, raw[pipe+1:]
			}
		}
	}
	return "", "", raw
}

func handleMapLine(
	payload string,
	byID map[int]int,
	targets []TargetSpec,
	record func(bashPID, rootPID, bashSource string, idx int),
) {
	// Format (v1.1): <id>|@p<bashpid>:<rootpid>|<pwd>|<bash_source>
	// Format (legacy): <id>|<pwd>|<bash_source>
	firstPipe := strings.IndexByte(payload, '|')
	if firstPipe == -1 {
		return
	}
	id, err := strconv.Atoi(strings.TrimSpace(payload[:firstPipe]))
	if err != nil {
		return
	}
	idx, ok := byID[id]
	if !ok {
		return
	}

	rest := payload[firstPipe+1:]
	var bashPID, rootPID string
	if strings.HasPrefix(rest, "@p") {
		tagPipe := strings.IndexByte(rest, '|')
		if tagPipe == -1 {
			return
		}
		bp, rp, validTag := parsePIDTag(rest[:tagPipe])
		if !validTag {
			return
		}
		bashPID, rootPID = bp, rp
		rest = rest[tagPipe+1:]
	}

	t := targets[idx]
	cleanAbs := filepath.Clean(t.AbsPath)
	cleanReal := filepath.Clean(t.RealPath)

	// Try each '|' split in rest so paths or $PWD containing '|' still resolve accurately.
	for i := 0; i < len(rest); i++ {
		if rest[i] != '|' {
			continue
		}
		pwd := rest[:i]
		bashSource := rest[i+1:]
		if bashSource == "" {
			continue
		}
		if matchesTargetPath(pwd, bashSource, cleanAbs, cleanReal) {
			record(bashPID, rootPID, bashSource, idx)
			return
		}
	}
}

func matchesTargetPath(pwd, bashSource, cleanAbs, cleanReal string) bool {
	if filepath.IsAbs(bashSource) {
		cleanSrc := filepath.Clean(bashSource)
		if cleanSrc == cleanAbs || cleanSrc == cleanReal {
			return true
		}
		if resolved, err := filepath.EvalSymlinks(cleanSrc); err == nil {
			return filepath.Clean(resolved) == cleanReal
		}
		return false
	}

	if pwd == "" {
		return false
	}

	joined := filepath.Clean(filepath.Join(pwd, bashSource))
	if joined == cleanAbs || joined == cleanReal {
		return true
	}
	if resolved, err := filepath.EvalSymlinks(joined); err == nil {
		return filepath.Clean(resolved) == cleanReal
	}
	return false
}
