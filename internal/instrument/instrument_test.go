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
	inst, insertLine := Script([]byte(src), traceFile, 0)

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
	if !strings.Contains(lines[4], InstrumentedMarker) {
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
	inst, insertLine := Script(nil, "/tmp/trace", 0)
	if insertLine != 1 {
		t.Errorf("expected insertLine 1 for empty src, got %d", insertLine)
	}
	if !bytes.Contains(inst, []byte(InstrumentedMarker)) {
		t.Error("expected preamble in output")
	}
}

func TestScript_ShebangOnlyWithAndWithoutNewline(t *testing.T) {
	inst1, line1 := Script([]byte("#!/usr/bin/env bash\n"), "/tmp/trace", 1)
	if line1 != 2 {
		t.Errorf("expected insertLine 2 for shebang with trailing newline, got %d", line1)
	}
	lines1 := strings.Split(string(inst1), "\n")
	if lines1[0] != "#!/usr/bin/env bash" || !strings.Contains(lines1[1], InstrumentedMarker) {
		t.Errorf("unexpected lines for trailing-newline shebang: %q", lines1)
	}

	inst2, line2 := Script([]byte("#!/usr/bin/env bash"), "/tmp/trace", 1)
	if line2 != 2 {
		t.Errorf("expected insertLine 2 for shebang without trailing newline, got %d", line2)
	}
	lines2 := strings.Split(string(inst2), "\n")
	if lines2[0] != "#!/usr/bin/env bash" || !strings.Contains(lines2[1], InstrumentedMarker) {
		t.Errorf("shebang without trailing newline was corrupted: %q", lines2)
	}
}

func TestScript_UTF8BOM(t *testing.T) {
	src := append([]byte{0xEF, 0xBB, 0xBF}, []byte("#!/usr/bin/env bash\necho hi\n")...)
	inst, insertLine := Script(src, "/tmp/trace", 0)
	if insertLine != 2 {
		t.Errorf("expected insertLine 2 with BOM, got %d", insertLine)
	}
	if !bytes.HasPrefix(inst, []byte{0xEF, 0xBB, 0xBF, '#', '!'}) {
		t.Errorf("BOM + shebang was not preserved: %q", string(inst))
	}
}

func TestScript_UTF8BOMWithoutShebang(t *testing.T) {
	src := append([]byte{0xEF, 0xBB, 0xBF}, []byte("echo hi\n")...)
	inst, insertLine := Script(src, "/tmp/trace", 0)
	if insertLine != 1 {
		t.Errorf("expected insertLine 1 for BOM without shebang, got %d", insertLine)
	}
	lines := strings.Split(string(inst), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}
	if !bytes.HasPrefix([]byte(lines[0]), utf8BOM) || !strings.Contains(lines[0], InstrumentedMarker) {
		t.Errorf("line 1 should start with BOM followed directly by preamble, got %q", lines[0])
	}
	if lines[1] != "echo hi" {
		t.Errorf("line 2 should be original code, got %q", lines[1])
	}
}

func TestHasMarker(t *testing.T) {
	inst, _ := Script([]byte("echo ok\n"), "/tmp/t", 3)
	if !HasMarker(inst) {
		t.Error("expected HasMarker to return true for instrumented script")
	}
	if HasMarker([]byte("echo ok\n")) {
		t.Error("expected HasMarker to return false for plain script")
	}
}
