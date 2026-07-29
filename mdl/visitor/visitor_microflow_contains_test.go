// SPDX-License-Identifier: Apache-2.0

package visitor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
)

// TestContainsOverloadParsing covers ledger finding #53: `contains` is overloaded
// as both the LIST operation contains(list, object) and the STRING function
// contains(haystack, needle). When an argument is a literal or a computed
// expression the call is unambiguously the string function and must parse as a
// value expression (MfSetStmt), never a lossy List operation activity. When both
// arguments are plain variables the kind stays ambiguous at parse time and is
// kept as a ListOperationStmt for the flow builder to disambiguate downstream.
func TestContainsOverloadParsing(t *testing.T) {
	cases := []struct {
		name       string
		setExpr    string
		wantListOp bool // true → ListOperationStmt, false → MfSetStmt (string expression)
	}{
		{"literal second arg is string contains", "contains($Email, '@')", false},
		{"literal first arg is string contains", "contains('needle in haystack', $Needle)", false},
		{"computed second arg is string contains", "contains($Text, $A + $B)", false},
		{"both plain variables stay a list op (ambiguous)", "contains($Items, $One)", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "create microflow M.F ($Email: String, $Needle: String, $Text: String, $A: String, $B: String, $Items: list of M.Item, $One: M.Item)\n" +
				"returns Boolean\nbegin\n  declare $Match Boolean = false;\n  set $Match = " + tc.setExpr + ";\n  return $Match;\nend;"
			prog, errs := Build(src)
			if len(errs) > 0 {
				t.Fatalf("unexpected parse errors: %v", errs)
			}
			mf := prog.Statements[0].(*ast.CreateMicroflowStmt)

			var stmt ast.MicroflowStatement
			for _, s := range mf.Body {
				switch v := s.(type) {
				case *ast.ListOperationStmt:
					if v.OutputVariable == "Match" {
						stmt = s
					}
				case *ast.MfSetStmt:
					if v.Target == "Match" {
						stmt = s
					}
				}
			}
			if stmt == nil {
				t.Fatalf("no statement assigning $Match found")
			}

			_, isListOp := stmt.(*ast.ListOperationStmt)
			if isListOp != tc.wantListOp {
				t.Errorf("statement type = %T, wantListOp=%v (expr: %q)", stmt, tc.wantListOp, tc.setExpr)
			}
		})
	}
}
