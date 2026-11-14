package report

import (
	"bytes"
	"strings"
	"testing"

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

	// All-zero files -> Total should also be N/A
	buf.Reset()
	err := WriteSummary(&buf, []*trace.Profile{
		{SourceFile: "a.sh", LinesFound: 0, LinesHit: 0},
		{SourceFile: "b.sh", LinesFound: 0, LinesHit: 0},
	})
	if err != nil {
		t.Fatalf("WriteSummary failed: %v", err)
	}
	if strings.Count(buf.String(), "     N/A") != 3 {
		t.Errorf("expected 3 N/A entries (2 rows + Total), got:\n%s", buf.String())
	}
}
