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
	// src/a.sh should come before src/b.sh due to sorting
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
