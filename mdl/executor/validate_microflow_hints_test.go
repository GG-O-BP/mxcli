// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/visitor"
)

// TestValidateMicroflow_DateTimeLiterals covers MDL046 (ledger finding #21):
// dateTime()/dateTimeUTC() accept only literal numeric constants — a variable or
// computed argument fails the build with CE0117.
func TestValidateMicroflow_DateTimeLiterals(t *testing.T) {
	cases := []struct {
		name    string
		params  string
		body    string
		wantMDL bool
	}{
		{"variable arg", "$Month: Integer, $Day: Integer", "set $C = dateTime(2026, $Month, $Day);", true},
		{"computed arg", "$Y: Integer", "set $C = dateTime($Y + 1, 1, 1);", true},
		{"utc variable arg", "$Month: Integer", "set $C = dateTimeUTC(2026, $Month, 1);", true},
		{"all literals", "", "set $C = dateTime(2026, 1, 1);", false},
		{"literals with time", "", "set $C = dateTime(2026, 12, 31, 23, 59, 59);", false},
		{"no datetime call", "$X: Integer", "set $C = $X + 1;", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "create microflow M.F (" + tc.params + ")\nreturns DateTime\nbegin\n  " + tc.body + "\n  return $C;\nend;"
			prog, errs := visitor.Build(src)
			if len(errs) > 0 {
				t.Fatalf("parse errors: %v", errs)
			}
			mf := prog.Statements[0].(*ast.CreateMicroflowStmt)
			var got bool
			for _, vi := range ValidateMicroflow(mf) {
				if vi.RuleID == "MDL046" {
					got = true
				}
			}
			if got != tc.wantMDL {
				t.Errorf("MDL046 fired=%v, want %v (body: %q)", got, tc.wantMDL, tc.body)
			}
		})
	}
}

// TestValidateMicroflow_XPathAssociationEmpty covers MDL047 (ledger finding #25):
// `[Module.Association = empty]` is CE0161 — XPath has no `= empty` for
// associations; the nullability test is `not(Assoc/Target)`. A bare attribute
// (`Name = empty`) and an attribute-over-association (`Assoc/Attr = empty`) are
// valid and must not be flagged.
func TestValidateMicroflow_XPathAssociationEmpty(t *testing.T) {
	cases := []struct {
		name    string
		where   string
		wantMDL bool
	}{
		{"association = empty", "[Ledger.Transaction_Category = empty]", true},
		{"negation form is fine", "[not(Ledger.Transaction_Category/Ledger.Category)]", false},
		{"bare attribute = empty is fine", "[Description = empty]", false},
		{"attribute over association is fine", "[Ledger.Transaction_Category/Ledger.Name = empty]", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "create microflow M.F ()\nreturns list of Ledger.Transaction\nbegin\n  retrieve $t from Ledger.Transaction where " + tc.where + ";\n  return $t;\nend;"
			prog, errs := visitor.Build(src)
			if len(errs) > 0 {
				t.Fatalf("parse errors: %v", errs)
			}
			mf := prog.Statements[0].(*ast.CreateMicroflowStmt)
			var got bool
			for _, vi := range ValidateMicroflow(mf) {
				if vi.RuleID == "MDL047" {
					got = true
				}
			}
			if got != tc.wantMDL {
				t.Errorf("MDL047 fired=%v, want %v (where: %q)", got, tc.wantMDL, tc.where)
			}
		})
	}
}
