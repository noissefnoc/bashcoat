package trace

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	traceData := `+bashcoat:src/utils.sh:1: echo hello
+bashcoat:src/utils.sh:5: preamble
+bashcoat:src/utils.sh:6: echo "line 5 in orig"
+bashcoat:src/utils.sh:6: echo "line 5 in orig"
+bashcoat:other.sh:10: echo other
+bashcoat:src/utils.sh:8: echo "line 7 in orig"
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
	// Raw line 6 was shifted by preamble at line 5 -> actual line 5
	if prof.Hits[5] != 2 {
		t.Errorf("expected line 5 to have 2 hits, got %d", prof.Hits[5])
	}
	// Raw line 8 was shifted by preamble at line 5 -> actual line 7
	if prof.Hits[7] != 1 {
		t.Errorf("expected line 7 to have 1 hit, got %d", prof.Hits[7])
	}
	if prof.Hits[9] != 0 {
		t.Errorf("expected line 9 to have 0 hits, got %d", prof.Hits[9])
	}

	expectedCov := 75.0 // 3 out of 4
	if prof.Coverage() != expectedCov {
		t.Errorf("expected Coverage() = %v, got %v", expectedCov, prof.Coverage())
	}
}
