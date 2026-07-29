// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/visitor"
)

// TestValidateMicroflow_DivIntoInteger covers MDL041: integer division ('div')
// yields a Decimal, so assigning it to an Integer/Long variable fails mx check
// with CE0117 even though the MDL is syntactically valid. Rounding-function
// results assigned to Integer are accepted by Mendix and must not be flagged.
func TestValidateMicroflow_DivIntoInteger(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantMDL bool
	}{
		{"div into Integer var", "declare $I Integer = 0;\n  set $I = $a * 100 div $b;", true},
		{"div into declared Integer", "declare $I Integer = $a div $b;", true},
		{"div into Long", "declare $L Long = $a div $b;", true},
		{"div into Decimal is fine", "declare $D Decimal = $a div $b;", false},
		{"integer add is fine", "declare $I Integer = 0;\n  set $I = $a + $b;", false},
		{"integer mult is fine", "declare $I Integer = 0;\n  set $I = $a * $b;", false},
		{"round result into Integer is fine", "declare $I Integer = round(sqrt($a));", false},
		{"round of div into Integer is fine", "declare $I Integer = round($a div $b);", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "create microflow M.F ($a: Integer, $b: Integer)\nbegin\n  " + tc.body + "\nend;"
			prog, errs := visitor.Build(src)
			if len(errs) > 0 {
				t.Fatalf("parse errors: %v", errs)
			}
			var got bool
			for _, s := range prog.Statements {
				mf, ok := s.(*ast.CreateMicroflowStmt)
				if !ok {
					continue
				}
				for _, vi := range ValidateMicroflow(mf) {
					if vi.RuleID == "MDL041" {
						got = true
					}
				}
			}
			if got != tc.wantMDL {
				t.Errorf("MDL041 fired=%v, want %v (body: %q)", got, tc.wantMDL, tc.body)
			}
		})
	}
}

// TestValidateMicroflow_SlashDivision covers MDL045 (ledger finding #17): `/`
// used as an arithmetic division operator is CE0117 in Mendix — `/` navigates
// associations, division is `div`. The `$a / literal` and `(...) / $x` forms
// parse to a BinaryExpr with operator `/` and must be flagged; a legitimate
// member/association path (`$obj/Attr`) and correct `div` must not be.
func TestValidateMicroflow_SlashDivision(t *testing.T) {
	cases := []struct {
		name    string
		params  string
		body    string
		wantMDL bool
	}{
		{"slash divide by literal", "$Dec: Decimal", "set $R = $Dec / 2;", true},
		{"slash divide by variable", "$Dec: Decimal, $D2: Decimal", "set $R = $Dec / $D2;", true},
		{"slash divide parenthesized", "$Dec: Decimal, $D2: Decimal", "set $R = ($Dec + 1) / $D2;", true},
		{"slash divide by variable spaced", "$Dec: Decimal, $D2: Decimal", "set $R = $Dec/$D2;", true},
		{"slash inside function arg", "$Dec: Decimal", "set $R = round($Dec / 3);", true},
		{"slash in return", "$Dec: Decimal", "return $Dec / 4;", true},
		// Division-by-variable EMBEDDED in a larger expression (ledger #17 round 2):
		// `$a / $b` degrades to a member-path AttributePathExpr nested under a
		// BinaryExpr/FunctionCallExpr, so the structural `/`-BinaryExpr walk misses
		// it; the source scan catches it.
		{"embedded div then add", "$Dec: Decimal, $D2: Decimal", "set $R = $Dec / $D2 + 1;", true},
		{"embedded add then div", "$Dec: Decimal, $D2: Decimal", "set $R = 1 + $Dec / $D2;", true},
		{"embedded div in function", "$Dec: Decimal, $D2: Decimal", "set $R = round($Dec / $D2);", true},
		{"embedded div then mul", "$Dec: Decimal, $D2: Decimal", "set $R = $Dec / $D2 * 100;", true},
		{"embedded div in return", "$Dec: Decimal, $D2: Decimal", "return $Dec / $D2 + 1;", true},
		{"div is fine", "$Dec: Decimal, $D2: Decimal", "set $R = $Dec div $D2;", false},
		{"member path is fine", "$O: M.Order", "set $R = $O/M.Order_Cust/Name;", false},
		{"spaced member path is fine", "$O: M.Order", "set $R = $O / M.Order_Cust / Name;", false},
		// A `/$` sequence inside a string literal is NOT a division misuse.
		{"slash-dollar inside string literal is fine", "$x: String", "set $R = 'path/$var here';", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "create microflow M.F (" + tc.params + ")\nreturns String\nbegin\n  " + tc.body + "\nend;"
			prog, errs := visitor.Build(src)
			if len(errs) > 0 {
				t.Fatalf("parse errors: %v", errs)
			}
			mf := prog.Statements[0].(*ast.CreateMicroflowStmt)
			var got bool
			for _, vi := range ValidateMicroflow(mf) {
				if vi.RuleID == "MDL045" {
					got = true
				}
			}
			if got != tc.wantMDL {
				t.Errorf("MDL045 fired=%v, want %v (body: %q)", got, tc.wantMDL, tc.body)
			}
		})
	}
}

// TestValidateMicroflow_DivMessage checks the diagnostic names the target and
// the div cause, and suggests the fix.
func TestValidateMicroflow_DivMessage(t *testing.T) {
	src := "create microflow M.F ($a: Integer, $b: Integer)\nbegin\n  declare $Count Integer = $a div $b;\nend;"
	prog, errs := visitor.Build(src)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	mf := prog.Statements[0].(*ast.CreateMicroflowStmt)
	var msg, sugg string
	for _, vi := range ValidateMicroflow(mf) {
		if vi.RuleID == "MDL041" {
			msg, sugg = vi.Message, vi.Suggestion
		}
	}
	if msg == "" {
		t.Fatal("expected MDL041 violation")
	}
	if !strings.Contains(msg, "$Count") || !strings.Contains(msg, "CE0117") || !strings.Contains(msg, "div") {
		t.Errorf("message missing detail: %q", msg)
	}
	if !strings.Contains(sugg, "Decimal") {
		t.Errorf("suggestion should mention Decimal: %q", sugg)
	}
}
