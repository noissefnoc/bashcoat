package instrument

import (
	"bytes"
	"strings"
	"testing"
)

func TestScript_PreservesShebangAndHeaderComments(t *testing.T) {
	src := `#!/usr/bin/env bash
# First comment
# Second comment

echo "hello"
`
	traceFile := "/tmp/test.trace"
	inst, insertLine := Script([]byte(src), traceFile)

	lines := strings.Split(string(inst), "\n")
	if len(lines) < 6 {
		t.Fatalf("expected at least 6 lines, got %d", len(lines))
	}

	if lines[0] != "#!/usr/bin/env bash" {
		t.Errorf("line 1 should be shebang, got %q", lines[0])
	}
	if lines[1] != "# First comment" {
		t.Errorf("line 2 should be comment, got %q", lines[1])
	}
	if lines[2] != "# Second comment" {
		t.Errorf("line 3 should be comment, got %q", lines[2])
	}
	if lines[3] != "" {
		t.Errorf("line 4 should be empty line, got %q", lines[3])
	}
	if !strings.HasPrefix(lines[4], "__bashcoat_file=") {
		t.Errorf("line 5 should be preamble, got %q", lines[4])
	}
	if lines[5] != `echo "hello"` {
		t.Errorf("line 6 should be original code, got %q", lines[5])
	}

	if insertLine != 5 {
		t.Errorf("expected insertLine 5, got %d", insertLine)
	}
}

func TestScript_Empty(t *testing.T) {
	inst, insertLine := Script(nil, "/tmp/trace")
	if insertLine != 1 {
		t.Errorf("expected insertLine 1 for empty src, got %d", insertLine)
	}
	if !bytes.Contains(inst, []byte("__bashcoat_file=")) {
		t.Error("expected preamble in output")
	}
}
