package trace

import (
	"os"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	traceData := `+bashcoat_map|0|` + wd + `|src/utils.sh
+bashcoat|1|src/utils.sh|bc_end| echo hello
+bashcoat|5|src/utils.sh|bc_end| preamble
+bashcoat|6|src/utils.sh|bc_end| echo "line 5 in orig"
++bashcoat|6|src/utils.sh|bc_end| echo "nested cmd subst"
+bashcoat|10|other.sh|bc_end| echo other
+bashcoat|8|src/utils.sh|bc_end| echo "line 7 in orig"
  +bashcoat|99|src/utils.sh|bc_end| injected line in multiline body
`
	executableLines := map[int]bool{
		1: true,
		5: true,
		7: true,
		9: true, // not hit
	}

	prof, err := Parse(strings.NewReader(traceData), "src/utils.sh", 5, executableLines)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if prof.LinesFound != 4 {
		t.Errorf("expected LinesFound 4, got %d", prof.LinesFound)
	}
	if prof.LinesHit != 3 {
		t.Errorf("expected LinesHit 3 (lines 1, 5, 7), got %d", prof.LinesHit)
	}

	if prof.Hits[1] != 1 {
		t.Errorf("expected line 1 to have 1 hit, got %d", prof.Hits[1])
	}
	if prof.Hits[5] != 2 {
		t.Errorf("expected line 5 to have 2 hits, got %d", prof.Hits[5])
	}
	if prof.Hits[7] != 1 {
		t.Errorf("expected line 7 to have 1 hit, got %d", prof.Hits[7])
	}
	if prof.Hits[9] != 0 {
		t.Errorf("expected line 9 to have 0 hits, got %d", prof.Hits[9])
	}
	if _, exists := prof.Hits[98]; exists {
		t.Errorf("expected injected line not to be counted")
	}

	expectedCov := 75.0
	if prof.Coverage() != expectedCov {
		t.Errorf("expected Coverage() = %v, got %v", expectedCov, prof.Coverage())
	}
}

func TestParseAll_DisambiguatesBasenameAndColonAndFiltersNonExec(t *testing.T) {
	traceData := `+bashcoat_map|0|/repo|/repo/a/lib.sh
+bashcoat_map|2|/repo|/repo/dir/foo:bar.sh
+bashcoat|2|/repo/a/lib.sh|bc_end| echo a
+bashcoat|99|/repo/a/lib.sh|bc_end| untracked_line
+bashcoat|2|/repo/dir/foo:bar.sh|bc_end| echo colon
`
	specs := []TargetSpec{
		{
			ID:         0,
			SourceFile: "a/lib.sh",
			AbsPath:    "/repo/a/lib.sh",
			RealPath:   "/repo/a/lib.sh",
			WorkDir:    "/repo",
			InsertLine: 1,
			ExecLines:  map[int]bool{1: true},
		},
		{
			ID:         1,
			SourceFile: "b/lib.sh",
			AbsPath:    "/repo/b/lib.sh",
			RealPath:   "/repo/b/lib.sh",
			WorkDir:    "/repo",
			InsertLine: 1,
			ExecLines:  map[int]bool{1: true},
		},
		{
			ID:         2,
			SourceFile: "dir/foo:bar.sh",
			AbsPath:    "/repo/dir/foo:bar.sh",
			RealPath:   "/repo/dir/foo:bar.sh",
			WorkDir:    "/repo",
			InsertLine: 1,
			ExecLines:  map[int]bool{1: true},
		},
	}

	profiles, err := ParseAll(strings.NewReader(traceData), specs)
	if err != nil {
		t.Fatalf("ParseAll failed: %v", err)
	}
	if len(profiles) != 3 {
		t.Fatalf("expected 3 profiles, got %d", len(profiles))
	}

	if profiles[0].LinesHit != 1 || profiles[0].LinesFound != 1 {
		t.Errorf("a/lib.sh expected 1/1, got %d/%d", profiles[0].LinesHit, profiles[0].LinesFound)
	}
	if profiles[1].LinesHit != 0 {
		t.Errorf("b/lib.sh must not be polluted by a/lib.sh, got %d hits", profiles[1].LinesHit)
	}
	if profiles[2].LinesHit != 1 {
		t.Errorf("dir/foo:bar.sh expected 1 hit, got %d", profiles[2].LinesHit)
	}
}

func TestParseAll_LongTraceLineOver1MB(t *testing.T) {
	hugeArg := strings.Repeat("x", 2*1024*1024)
	traceData := "+bashcoat|2|/repo/big.sh|bc_end| echo " + hugeArg + "\n"

	profiles, err := ParseAll(strings.NewReader(traceData), []TargetSpec{
		{
			ID:         0,
			SourceFile: "big.sh",
			AbsPath:    "/repo/big.sh",
			RealPath:   "/repo/big.sh",
			WorkDir:    "/repo",
			InsertLine: 1,
			ExecLines:  map[int]bool{1: true},
		},
	})
	if err != nil {
		t.Fatalf("expected huge trace line to parse without ErrTooLong, got: %v", err)
	}
	if profiles[0].LinesHit != 1 {
		t.Errorf("expected 1 hit, got %d", profiles[0].LinesHit)
	}
}

func TestParseAll_PIDScopedRelativePathsAndRejectsInvalidMap(t *testing.T) {
	traceData := `+bashcoat_map|0|@p101:101|/repo/dir1|./lib.sh
+bashcoat_map|1|@p102:102|/repo/dir2|./lib.sh
+bashcoat_map|0|@p999:999|/repo/wrong|./lib.sh
+bashcoat|2|@p101:101|./lib.sh|bc_end| echo dir1
++bashcoat|2|@p103:101|./lib.sh|bc_end| echo subshell_of_dir1
+bashcoat|2|@p102:102|./lib.sh|bc_end| echo dir2
+bashcoat|2|@p999:999|./lib.sh|bc_end| echo unverified_non_target
`
	specs := []TargetSpec{
		{
			ID:         0,
			SourceFile: "dir1/lib.sh",
			AbsPath:    "/repo/dir1/lib.sh",
			RealPath:   "/repo/dir1/lib.sh",
			WorkDir:    "/repo",
			InsertLine: 1,
			ExecLines:  map[int]bool{1: true},
		},
		{
			ID:         1,
			SourceFile: "dir2/lib.sh",
			AbsPath:    "/repo/dir2/lib.sh",
			RealPath:   "/repo/dir2/lib.sh",
			WorkDir:    "/repo",
			InsertLine: 1,
			ExecLines:  map[int]bool{1: true},
		},
	}

	profiles, err := ParseAll(strings.NewReader(traceData), specs)
	if err != nil {
		t.Fatalf("ParseAll failed: %v", err)
	}

	// dir1/lib.sh should receive 2 hits (main pid 101 + child subshell 103) and NOT pid 999
	if profiles[0].Hits[1] != 2 {
		t.Errorf("expected dir1/lib.sh line 1 to have 2 hits, got %d", profiles[0].Hits[1])
	}
	if profiles[1].Hits[1] != 1 {
		t.Errorf("expected dir2/lib.sh line 1 to have 1 hit, got %d", profiles[1].Hits[1])
	}
}

func TestParseAll_PipeInPWDAndSourcePath(t *testing.T) {
	traceData := `+bashcoat_map|0|@p201:201|/repo/a|b|./lib.sh
+bashcoat_map|1|@p202:202|/repo/dir|./x|y.sh
+bashcoat|2|@p201:201|./lib.sh|bc_end| echo one
+bashcoat|2|@p202:202|./x|y.sh|bc_end| echo two
`
	specs := []TargetSpec{
		{
			ID:         0,
			SourceFile: "a|b/lib.sh",
			AbsPath:    "/repo/a|b/lib.sh",
			RealPath:   "/repo/a|b/lib.sh",
			WorkDir:    "/repo",
			InsertLine: 1,
			ExecLines:  map[int]bool{1: true},
		},
		{
			ID:         1,
			SourceFile: "dir/x|y.sh",
			AbsPath:    "/repo/dir/x|y.sh",
			RealPath:   "/repo/dir/x|y.sh",
			WorkDir:    "/repo",
			InsertLine: 1,
			ExecLines:  map[int]bool{1: true},
		},
	}

	profiles, err := ParseAll(strings.NewReader(traceData), specs)
	if err != nil {
		t.Fatalf("ParseAll failed: %v", err)
	}
	if profiles[0].Hits[1] != 1 {
		t.Errorf("expected a|b/lib.sh to have 1 hit, got %d", profiles[0].Hits[1])
	}
	if profiles[1].Hits[1] != 1 {
		t.Errorf("expected dir/x|y.sh to have 1 hit, got %d", profiles[1].Hits[1])
	}
}
