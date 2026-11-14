// Package executable detects executable line numbers and multi-line statement spans in Bash scripts.
package executable

import (
	"bytes"

	"mvdan.cc/sh/v3/syntax"
)

// StatementInfo holds the detected executable lines and a mapping from any line
// inside a multi-line leaf statement to its representative executable line.
type StatementInfo struct {
	Lines   map[int]bool
	LineMap map[int]int
}

// Detect parses a Bash script and returns a set of 1-based executable line numbers.
// Comments, blank lines, function declarations, and non-executable syntax boundaries are excluded.
func Detect(src []byte) (map[int]bool, error) {
	info, err := DetectStatements(src)
	if err != nil {
		return nil, err
	}
	return info.Lines, nil
}

// DetectStatements parses a Bash script and returns executable line metadata
// including multi-line leaf statement mappings.
func DetectStatements(src []byte) (*StatementInfo, error) {
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	file, err := parser.Parse(bytes.NewReader(src), "")
	if err != nil {
		return nil, err
	}

	continuedFromPrev := detectLineContinuations(src)

	lines := make(map[int]bool)
	lineMap := make(map[int]int)

	registerSpan := func(startLine, endLine int) {
		if startLine <= 0 {
			return
		}
		lines[startLine] = true
		if endLine < startLine {
			endLine = startLine
		}
		for l := startLine; l <= endLine; l++ {
			lineMap[l] = startLine
		}
	}

	var walkFn func(node syntax.Node) bool
	walkFn = func(node syntax.Node) bool {
		if node == nil {
			return false
		}

		switch n := node.(type) {
		case *syntax.CallExpr:
			if len(n.Args) > 0 || len(n.Assigns) > 0 {
				registerSpan(int(n.Pos().Line()), int(n.End().Line()))
			}
			// If arguments span onto backslash-continued lines, Bash pins LINENO for
			// command substitutions on those lines to the original command start line.
			for _, assign := range n.Assigns {
				syntax.Walk(assign, walkFn)
			}
			for _, arg := range n.Args {
				if continuedFromPrev[int(arg.Pos().Line())] && containsCmdSubst(arg) {
					continue
				}
				syntax.Walk(arg, walkFn)
			}
			return false
		case *syntax.ForClause:
			end := int(n.Pos().Line())
			if n.DoPos.IsValid() {
				end = int(n.DoPos.Line())
			}
			registerSpan(int(n.Pos().Line()), end)
		case *syntax.CaseClause:
			registerSpan(int(n.Pos().Line()), int(n.Pos().Line()))
			for _, item := range n.Items {
				syntax.Walk(item, walkFn)
			}
			return false
		case *syntax.DeclClause:
			registerSpan(int(n.Pos().Line()), int(n.End().Line()))
			for _, arg := range n.Args {
				if continuedFromPrev[int(arg.Pos().Line())] && containsCmdSubst(arg) {
					continue
				}
				syntax.Walk(arg, walkFn)
			}
			return false
		case *syntax.Assign:
			if n.Array != nil {
				// Array compound assignments execute element substitutions with LINENO
				// pinned to the closing ')' line, which already maps to the assignment start.
				return false
			}
		case *syntax.Redirect:
			if n.Hdoc != nil {
				if n.Word != nil {
					syntax.Walk(n.Word, walkFn)
				}
				return false
			}
		case *syntax.ProcSubst:
			return false
		case *syntax.ArithmCmd:
			registerSpan(int(n.Pos().Line()), int(n.End().Line()))
		case *syntax.TestClause:
			registerSpan(int(n.Pos().Line()), int(n.End().Line()))
		case *syntax.LetClause:
			registerSpan(int(n.Pos().Line()), int(n.End().Line()))
		}

		return true
	}

	syntax.Walk(file, walkFn)

	return &StatementInfo{
		Lines:   lines,
		LineMap: lineMap,
	}, nil
}

// detectLineContinuations returns a map where continued[line] is true if physical
// line (line - 1) ends with an unescaped backslash line continuation.
func detectLineContinuations(src []byte) map[int]bool {
	continued := make(map[int]bool)
	rawLines := bytes.Split(src, []byte{'\n'})
	for i, line := range rawLines {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		bs := 0
		for j := len(line) - 1; j >= 0 && line[j] == '\\'; j-- {
			bs++
		}
		if bs%2 == 1 {
			// Line numbers are 1-based; line index i is line (i+1), so next is (i+2).
			continued[i+2] = true
		}
	}
	return continued
}

func containsCmdSubst(node syntax.Node) bool {
	found := false
	syntax.Walk(node, func(n syntax.Node) bool {
		if _, ok := n.(*syntax.CmdSubst); ok {
			found = true
			return false
		}
		return !found
	})
	return found
}
