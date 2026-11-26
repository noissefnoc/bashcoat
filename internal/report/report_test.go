package report

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/noissefnoc/bashcoat/internal/trace"
)

func TestWriteLCOV(t *testing.T) {
	profiles := []*trace.Profile{
		{
			SourceFile: "src/b.sh",
			Hits: map[int]int{
				1: 2,
				3: 0,
			},
			LinesFound: 2,
			LinesHit:   1,
		},
		{
			SourceFile: "src/a.sh",
			Hits: map[int]int{
				5: 1,
			},
			LinesFound: 1,
			LinesHit:   1,
		},
	}

	var buf bytes.Buffer
	if err := WriteLCOV(&buf, profiles); err != nil {
		t.Fatalf("WriteLCOV failed: %v", err)
	}

	out := buf.String()
	expected := `TN:
SF:src/a.sh
DA:5,1
LF:1
LH:1
end_of_record
TN:
SF:src/b.sh
DA:1,2
DA:3,0
LF:2
LH:1
end_of_record
`
	if out != expected {
		t.Errorf("WriteLCOV mismatch.\nGot:\n%s\nWant:\n%s", out, expected)
	}
}

func TestWriteSummary(t *testing.T) {
	profiles := []*trace.Profile{
		{
			SourceFile: "src/auth.sh",
			Hits:       map[int]int{1: 1, 2: 1},
			LinesFound: 2,
			LinesHit:   2,
		},
		{
			SourceFile: "src/deploy.sh",
			Hits:       map[int]int{1: 1, 2: 0},
			LinesFound: 2,
			LinesHit:   1,
		},
	}

	var buf bytes.Buffer
	if err := WriteSummary(&buf, profiles); err != nil {
		t.Fatalf("WriteSummary failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "src/auth.sh") || !strings.Contains(out, "src/deploy.sh") || !strings.Contains(out, "Total") {
		t.Errorf("unexpected summary table output:\n%s", out)
	}
}

func TestWriteSummary_ShortAndMultibyteAlignment(t *testing.T) {
	profiles := []*trace.Profile{
		{SourceFile: "a.sh", LinesFound: 1, LinesHit: 1},
		{SourceFile: "b.sh", LinesFound: 1, LinesHit: 0},
	}
	var buf bytes.Buffer
	if err := WriteSummary(&buf, profiles); err != nil {
		t.Fatalf("WriteSummary failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	// Header, sep, row1, row2, sep, Total -> all pipe columns should align at rune index 5
	for i, line := range lines {
		if strings.HasPrefix(line, "-") {
			if !strings.HasPrefix(line, "------+") {
				t.Errorf("line %d separator misaligned: %q", i, line)
			}
			continue
		}
		if idx := strings.Index(line, " | "); idx != 5 {
			t.Errorf("line %d pipe column expected at 5, got %d (%q)", i, idx, line)
		}
	}
}

func TestWriteSummary_ZeroLinesShowsNAAndWideCharsAlign(t *testing.T) {
	profiles := []*trace.Profile{
		{SourceFile: "空.sh", LinesFound: 0, LinesHit: 0},
		{SourceFile: "run.sh", LinesFound: 2, LinesHit: 1},
	}
	var buf bytes.Buffer
	if err := WriteSummary(&buf, profiles); err != nil {
		t.Fatalf("WriteSummary failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "     N/A") {
		t.Errorf("expected N/A for 0-line profile, got:\n%s", out)
	}
	if !strings.Contains(out, "   50.0%") {
		t.Errorf("expected 50.0%% total coverage, got:\n%s", out)
	}

	// "run.sh" has display width 6, "空.sh" has display width 5 (2+1+2), so separator starts with 7 dashes (6 + 1 padding).
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if !strings.HasPrefix(lines[1], "-------+") {
		t.Errorf("expected separator width 7, got %q", lines[1])
	}
	// Visual display width before " | " should equal 6 for every row
	for _, row := range []string{lines[0], lines[2], lines[3], lines[5]} {
		parts := strings.SplitN(row, " | ", 2)
		if len(parts) != 2 {
			t.Fatalf("unexpected row format: %q", row)
		}
		if w := displayWidth(parts[0]); w != 6 {
			t.Errorf("expected display width 6 for %q, got %d", parts[0], w)
		}
	}

	// All-zero files -> Total should also be N/A (both Coverage and Fn Cov columns)
	buf.Reset()
	err := WriteSummary(&buf, []*trace.Profile{
		{SourceFile: "a.sh", LinesFound: 0, LinesHit: 0},
		{SourceFile: "b.sh", LinesFound: 0, LinesHit: 0},
	})
	if err != nil {
		t.Fatalf("WriteSummary failed: %v", err)
	}
	if strings.Count(buf.String(), "     N/A") != 6 {
		t.Errorf("expected 6 N/A entries (3 rows x 2 columns), got:\n%s", buf.String())
	}
}

func TestWriteReports_WithFunctions(t *testing.T) {
	profiles := []*trace.Profile{
		{
			SourceFile: "src/app.sh",
			Hits:       map[int]int{4: 2, 5: 2, 10: 0},
			LinesFound: 3,
			LinesHit:   2,
			Functions: []trace.FunctionCoverage{
				{
					Name:       "run",
					StartLine:  3,
					EndLine:    6,
					Calls:      2,
					LinesFound: 2,
					LinesHit:   2,
					LineNums:   []int{4, 5},
				},
				{
					Name:       "unused",
					StartLine:  9,
					EndLine:    11,
					Calls:      0,
					LinesFound: 1,
					LinesHit:   0,
					LineNums:   []int{10},
				},
			},
			FunctionsFound: 2,
			FunctionsHit:   1,
		},
		{
			SourceFile: "src/lib.sh",
			Hits:       map[int]int{3: 1},
			LinesFound: 1,
			LinesHit:   1,
			Functions: []trace.FunctionCoverage{
				{
					Name:       "helper",
					StartLine:  2,
					EndLine:    4,
					Calls:      1,
					LinesFound: 1,
					LinesHit:   1,
					LineNums:   []int{3},
				},
			},
			FunctionsFound: 1,
			FunctionsHit:   1,
		},
	}

	var lcovBuf bytes.Buffer
	if err := WriteLCOV(&lcovBuf, profiles); err != nil {
		t.Fatalf("WriteLCOV failed: %v", err)
	}
	lcovOut := lcovBuf.String()
	for _, want := range []string{
		"FN:3,run\n", "FN:9,unused\n",
		"FNDA:2,run\n", "FNDA:0,unused\n",
		"FNF:2\n", "FNH:1\n",
		"FN:2,helper\n", "FNDA:1,helper\n",
		"FNF:1\n", "FNH:1\n",
	} {
		if !strings.Contains(lcovOut, want) {
			t.Errorf("LCOV missing %q:\n%s", want, lcovOut)
		}
	}

	var xmlBuf bytes.Buffer
	if err := WriteCoberturaWithOptions(&xmlBuf, profiles, CoberturaOptions{Timestamp: time.Unix(1, 0)}); err != nil {
		t.Fatalf("WriteCobertura failed: %v", err)
	}
	var parsed coberturaCoverage
	if err := xml.Unmarshal(xmlBuf.Bytes(), &parsed); err != nil {
		t.Fatalf("Cobertura XML failed to unmarshal: %v\n%s", err, xmlBuf.String())
	}
	methods := parsed.Packages.Packages[0].Classes.Classes[0].Methods.Methods
	if len(methods) != 2 {
		t.Fatalf("expected 2 methods in Cobertura XML for src/app.sh, got %d", len(methods))
	}
	if methods[0].Name != "run" || methods[0].LineRate != 1 || len(methods[0].Lines.Lines) != 2 {
		t.Errorf("unexpected method[0]: %+v", methods[0])
	}
	if methods[1].Name != "unused" || methods[1].LineRate != 0 || len(methods[1].Lines.Lines) != 1 {
		t.Errorf("unexpected method[1]: %+v", methods[1])
	}

	var sumBuf bytes.Buffer
	if err := WriteSummary(&sumBuf, profiles); err != nil {
		t.Fatalf("WriteSummary failed: %v", err)
	}
	sumOut := sumBuf.String()
	for _, want := range []string{"Funcs", "Fn Cov", "1/2", "50.0%", "1/1", "100.0%", "2/3", "66.7%"} {
		if !strings.Contains(sumOut, want) {
			t.Errorf("summary output missing %q:\n%s", want, sumOut)
		}
	}
}

func TestWriteCobertura(t *testing.T) {
	profiles := []*trace.Profile{
		{
			SourceFile: "src/lib/b.sh",
			Hits: map[int]int{
				10: 0,
				2:  3,
			},
			LinesFound: 2,
			LinesHit:   1,
		},
		{
			SourceFile: "src/lib/a.sh",
			Hits: map[int]int{
				5: 1,
				7: 2,
			},
			LinesFound: 2,
			LinesHit:   2,
		},
		{
			SourceFile: "root.sh",
			Hits: map[int]int{
				3: 1,
			},
			LinesFound: 1,
			LinesHit:   1,
		},
	}

	var buf bytes.Buffer
	opts := CoberturaOptions{
		SourceDir: ".",
		Version:   "0.2.0",
		Timestamp: time.Unix(1700000000, 0).UTC(),
	}
	if err := WriteCoberturaWithOptions(&buf, profiles, opts); err != nil {
		t.Fatalf("WriteCoberturaWithOptions failed: %v", err)
	}

	out := buf.String()
	if !strings.HasPrefix(out, xml.Header+coberturaDoctype) {
		t.Fatalf("expected XML header and Cobertura DOCTYPE, got:\n%s", out)
	}

	var parsed coberturaCoverage
	if err := xml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("generated Cobertura XML failed to unmarshal: %v\n%s", err, out)
	}

	if parsed.Version != "0.2.0" {
		t.Errorf("version = %q, want %q", parsed.Version, "0.2.0")
	}
	if parsed.Timestamp != 1700000000 {
		t.Errorf("timestamp = %d, want 1700000000", parsed.Timestamp)
	}
	if parsed.LinesValid != 5 || parsed.LinesCovered != 4 {
		t.Errorf("root lines = %d/%d, want 4/5", parsed.LinesCovered, parsed.LinesValid)
	}
	if parsed.LineRate != 0.8 {
		t.Errorf("root line-rate = %v, want 0.8", parsed.LineRate)
	}
	if len(parsed.Sources.Source) != 1 || parsed.Sources.Source[0] != "." {
		t.Errorf("sources = %v, want [.]", parsed.Sources.Source)
	}
	if len(parsed.Packages.Packages) != 2 {
		t.Fatalf("packages len = %d, want 2", len(parsed.Packages.Packages))
	}

	// Packages must be sorted deterministically: "." before "src/lib"
	pkg0 := parsed.Packages.Packages[0]
	if pkg0.Name != "." || pkg0.LineRate != 1 {
		t.Errorf("pkg[0] = %+v, want name='.' line-rate=1", pkg0)
	}
	if len(pkg0.Classes.Classes) != 1 || pkg0.Classes.Classes[0].Filename != "root.sh" || pkg0.Classes.Classes[0].Name != "root.sh" {
		t.Errorf("pkg[0].classes = %+v, want single root.sh class", pkg0.Classes.Classes)
	}

	pkg1 := parsed.Packages.Packages[1]
	if pkg1.Name != "src/lib" || pkg1.LineRate != 0.75 {
		t.Errorf("pkg[1] = %+v, want name='src/lib' line-rate=0.75", pkg1)
	}
	if len(pkg1.Classes.Classes) != 2 {
		t.Fatalf("pkg[1].classes len = %d, want 2", len(pkg1.Classes.Classes))
	}
	// Classes sorted by filename: a.sh before b.sh
	clsA := pkg1.Classes.Classes[0]
	clsB := pkg1.Classes.Classes[1]
	if clsA.Filename != "src/lib/a.sh" || clsA.Name != "a.sh" || clsA.LineRate != 1 {
		t.Errorf("clsA = %+v", clsA)
	}
	if clsB.Filename != "src/lib/b.sh" || clsB.Name != "b.sh" || clsB.LineRate != 0.5 {
		t.Errorf("clsB = %+v", clsB)
	}
	// Lines sorted ascending: line 2 (hits=3), then line 10 (hits=0)
	if len(clsB.Lines.Lines) != 2 || clsB.Lines.Lines[0].Number != 2 || clsB.Lines.Lines[0].Hits != 3 || clsB.Lines.Lines[1].Number != 10 || clsB.Lines.Lines[1].Hits != 0 {
		t.Errorf("clsB lines = %+v, want [{2 3 false} {10 0 false}]", clsB.Lines.Lines)
	}
}

func TestWriteCobertura_EmptyAndZeroLines(t *testing.T) {
	// Empty profiles list must still include required <packages> element
	var buf bytes.Buffer
	if err := WriteCoberturaWithOptions(&buf, nil, CoberturaOptions{Timestamp: time.Unix(1, 0)}); err != nil {
		t.Fatalf("WriteCoberturaWithOptions(nil) failed: %v", err)
	}
	emptyOut := buf.String()
	if !strings.Contains(emptyOut, "<packages></packages>") {
		t.Errorf("expected required <packages></packages> for empty report, got:\n%s", emptyOut)
	}
	if strings.Contains(emptyOut, "NaN") || strings.Contains(emptyOut, "Inf") {
		t.Errorf("unexpected NaN/Inf in empty report:\n%s", emptyOut)
	}

	// File with 0 executable lines must still emit required <methods> and <lines> elements
	buf.Reset()
	err := WriteCoberturaWithOptions(&buf, []*trace.Profile{
		{SourceFile: "comments_only.sh", Hits: map[int]int{}, LinesFound: 0, LinesHit: 0},
	}, CoberturaOptions{Timestamp: time.Unix(1, 0)})
	if err != nil {
		t.Fatalf("WriteCoberturaWithOptions(zero-lines) failed: %v", err)
	}
	zeroOut := buf.String()
	if !strings.Contains(zeroOut, "<methods></methods>") || !strings.Contains(zeroOut, "<lines></lines>") {
		t.Errorf("expected required <methods> and <lines> elements for zero-line file, got:\n%s", zeroOut)
	}
	if strings.Contains(zeroOut, "NaN") || strings.Contains(zeroOut, "Inf") {
		t.Errorf("unexpected NaN/Inf in zero-line report:\n%s", zeroOut)
	}
	var parsed coberturaCoverage
	if err := xml.Unmarshal([]byte(zeroOut), &parsed); err != nil {
		t.Fatalf("zero-line XML failed to unmarshal: %v", err)
	}
	if parsed.LineRate != 0 {
		t.Errorf("expected line-rate 0 for zero-line report, got %v", parsed.LineRate)
	}
}

func TestWriteCobertura_SpecialCharsAndDefaults(t *testing.T) {
	var buf bytes.Buffer
	profiles := []*trace.Profile{
		{
			SourceFile: `dir/a&b<"c">.sh`,
			Hits:       map[int]int{1: 1},
			LinesFound: 1,
			LinesHit:   1,
		},
	}
	if err := WriteCobertura(&buf, profiles); err != nil {
		t.Fatalf("WriteCobertura failed: %v", err)
	}

	var parsed coberturaCoverage
	if err := xml.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("XML with special filename chars failed to unmarshal: %v\n%s", err, buf.String())
	}
	if parsed.Version != "bashcoat" {
		t.Errorf("default version = %q, want bashcoat", parsed.Version)
	}
	if parsed.Timestamp <= 0 {
		t.Errorf("expected positive default timestamp, got %d", parsed.Timestamp)
	}
	gotFilename := parsed.Packages.Packages[0].Classes.Classes[0].Filename
	if gotFilename != `dir/a&b<"c">.sh` {
		t.Errorf("round-tripped filename = %q, want %q", gotFilename, `dir/a&b<"c">.sh`)
	}
}
