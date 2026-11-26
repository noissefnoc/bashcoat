// Package executable detects executable line numbers and multi-line statement spans in Bash scripts.
package executable

import (
	"bytes"
	"sort"

	"mvdan.cc/sh/v3/syntax"
)

// FuncInfo holds static metadata for a Bash function declaration extracted from the AST.
type FuncInfo struct {
	Name      string
	StartLine int
	EndLine   int
	ExecLines map[int]bool // 1-based executable lines directly owned by this function (excluding nested functions)
	FirstLine int          // earliest 1-based executable line directly owned by this function (0 if none)
}

// StatementInfo holds the detected executable lines, multi-line statement mappings,
// and statically declared function metadata.
type StatementInfo struct {
	Lines     map[int]bool
	LineMap   map[int]int
	Functions []FuncInfo
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
	var funcs []*FuncInfo
	var funcStack []int

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
		if len(funcStack) > 0 {
			fn := funcs[funcStack[len(funcStack)-1]]
			fn.ExecLines[startLine] = true
			if fn.FirstLine == 0 || startLine < fn.FirstLine {
				fn.FirstLine = startLine
			}
		}
	}

	var walkFn func(node syntax.Node) bool
	walkFn = func(node syntax.Node) bool {
		if node == nil {
			return false
		}

		switch n := node.(type) {
		case *syntax.FuncDecl:
			if n.Name != nil && n.Name.Value != "" {
				fn := &FuncInfo{
					Name:      n.Name.Value,
					StartLine: int(n.Pos().Line()),
					EndLine:   int(n.End().Line()),
					ExecLines: make(map[int]bool),
				}
				idx := len(funcs)
				funcs = append(funcs, fn)
				funcStack = append(funcStack, idx)
				if n.Body != nil {
					syntax.Walk(n.Body, walkFn)
				}
				funcStack = funcStack[:len(funcStack)-1]
				return false
			}
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

	outFuncs := make([]FuncInfo, len(funcs))
	for i, fn := range funcs {
		outFuncs[i] = *fn
	}
	sort.Slice(outFuncs, func(i, j int) bool {
		if outFuncs[i].StartLine != outFuncs[j].StartLine {
			return outFuncs[i].StartLine < outFuncs[j].StartLine
		}
		if outFuncs[i].EndLine != outFuncs[j].EndLine {
			return outFuncs[i].EndLine < outFuncs[j].EndLine
		}
		return outFuncs[i].Name < outFuncs[j].Name
	})

	return &StatementInfo{
		Lines:     lines,
		LineMap:   lineMap,
		Functions: outFuncs,
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
