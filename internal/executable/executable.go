package executable

import (
	"bytes"

	"mvdan.cc/sh/v3/syntax"
)

// Detect parses a Bash script and returns a set of 1-based executable line numbers.
// Comments, blank lines, function declarations, and non-executable syntax boundaries are excluded.
func Detect(src []byte) (map[int]bool, error) {
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	file, err := parser.Parse(bytes.NewReader(src), "")
	if err != nil {
		return nil, err
	}

	lines := make(map[int]bool)

	syntax.Walk(file, func(node syntax.Node) bool {
		if node == nil {
			return false
		}

		switch n := node.(type) {
		case *syntax.CallExpr:
			if len(n.Args) > 0 {
				lines[int(n.Pos().Line())] = true
			}
		case *syntax.IfClause:
			for cur := n; cur != nil; cur = cur.Else {
				for _, stmt := range cur.Cond {
					lines[int(stmt.Pos().Line())] = true
				}
			}
		case *syntax.WhileClause:
			for _, stmt := range n.Cond {
				lines[int(stmt.Pos().Line())] = true
			}
		case *syntax.ForClause:
			lines[int(n.Pos().Line())] = true
		case *syntax.CaseClause:
			lines[int(n.Pos().Line())] = true
		case *syntax.BinaryCmd:
			if n.X != nil {
				lines[int(n.X.Pos().Line())] = true
			}
			if n.Y != nil {
				lines[int(n.Y.Pos().Line())] = true
			}
		case *syntax.DeclClause:
			lines[int(n.Pos().Line())] = true
		case *syntax.Assign:
			lines[int(n.Pos().Line())] = true
		}

		return true
	})

	return lines, nil
}
