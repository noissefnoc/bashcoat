package executable

import (
	"testing"
)

func TestDetect(t *testing.T) {
	script := `#!/usr/bin/env bash
# Header comment

VAR="hello"

greet() {
    local name="$1"
    if [[ "$name" == "admin" ]]; then
        echo "welcome admin"
    else
        echo "hello user"
    fi
}

greet "world"
`
	lines, err := Detect([]byte(script))
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	expected := []int{4, 7, 8, 9, 11, 15}
	for _, l := range expected {
		if !lines[l] {
			t.Errorf("expected line %d to be marked executable", l)
		}
	}

	nonExecutable := []int{1, 2, 3, 5, 6, 10, 12, 13, 14}
	for _, l := range nonExecutable {
		if lines[l] {
			t.Errorf("line %d should not be marked executable", l)
		}
	}
}

func TestDetectStatements_ExtendedSyntaxAndMultiline(t *testing.T) {
	script := `#!/usr/bin/env bash
(( count++ ))
[[ -n "$HOME" ]]
let total=1+2
> /tmp/ignored_redir
msg="line1
line2"
true && {
  echo ok
}
for i in a b; do
  echo "$i"
done
select opt in x y; do
  break
done
case "$1" in
  a) echo A ;;
esac
`
	info, err := DetectStatements([]byte(script))
	if err != nil {
		t.Fatalf("DetectStatements failed: %v", err)
	}

	// (( count++ )) -> line 2
	// [[ -n "$HOME" ]] -> line 3
	// let total=1+2 -> line 4
	// > /tmp/ignored_redir -> excluded (line 5)
	// msg="line1\nline2" -> rep line 6, mapped from 6 and 7
	// true -> line 8; echo ok -> line 9; "}" -> excluded (line 10)
	// for -> line 11; echo "$i" -> line 12
	// select -> line 14; break -> line 15
	// case -> line 17; echo A -> line 18
	for _, l := range []int{2, 3, 4, 6, 8, 9, 11, 12, 14, 15, 17, 18} {
		if !info.Lines[l] {
			t.Errorf("expected line %d to be executable", l)
		}
	}

	for _, l := range []int{1, 5, 7, 10, 13, 16, 19} {
		if info.Lines[l] {
			t.Errorf("line %d should not be executable", l)
		}
	}

	if got := info.LineMap[7]; got != 6 {
		t.Errorf("expected multiline assign line 7 to map to line 6, got %d", got)
	}
}

func TestDetectStatements_SyntaxError(t *testing.T) {
	_, err := DetectStatements([]byte("if then fi\n"))
	if err == nil {
		t.Fatal("expected syntax error, got nil")
	}
}

func TestDetectStatements_BashLinenoEdgeCases(t *testing.T) {
	script := `#!/usr/bin/env bash
local \
  a=1 \
  b=2
A=1 \
  B=2
cat <<HEREDOC >/dev/null
hello $(echo hdoc)
HEREDOC
myfunc \
  "$(echo cont)" \
  "arg"
cat <(
  echo procsub
)
case "$(
  echo caseword
)" in
  x) echo matched ;;
esac
arr=(
  "$(echo arrval)"
)
`
	info, err := DetectStatements([]byte(script))
	if err != nil {
		t.Fatalf("DetectStatements failed: %v", err)
	}

	// Expected executable start lines:
	// 2: local \, 5: A=1 \, 7: cat <<HEREDOC, 10: myfunc \, 13: cat <(, 16: case, 19: echo matched, 21: arr=(
	expectedExec := []int{2, 5, 7, 10, 13, 16, 19, 21}
	for _, l := range expectedExec {
		if !info.Lines[l] {
			t.Errorf("expected line %d to be executable", l)
		}
	}

	// Phantom lines that Bash never emits independently must NOT be marked executable:
	// 3,4 (local assign args), 6 (B=2), 8 (heredoc $(echo)), 11 (backslash-continued $(echo)),
	// 14 (procsub body), 17 (case word $(echo)), 22 (array element $(echo))
	nonExec := []int{3, 4, 6, 8, 9, 11, 12, 14, 15, 17, 18, 20, 22, 23}
	for _, l := range nonExec {
		if info.Lines[l] {
			t.Errorf("line %d should not be marked executable", l)
		}
	}

	if got := info.LineMap[23]; got != 21 {
		t.Errorf("expected array closing line 23 to map to line 21, got %d", got)
	}
}
