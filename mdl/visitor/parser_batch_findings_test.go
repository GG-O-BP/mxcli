// SPDX-License-Identifier: Apache-2.0

package visitor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
)

// TestRetrieveSortByQuotedQualified guards FINDINGS #13: a quoted, qualified
// `sort by` attribute in a RETRIEVE must store the bare dotted form. Keeping the
// quotes produced a reference that only failed on write ("attribute does not
// belong to entity").
func TestRetrieveSortByQuotedQualified(t *testing.T) {
	input := `create microflow M.DS () returns list of M.Thing as $rows
begin
  retrieve $rows from M.Thing sort by "M"."Thing"."Code" asc;
  return $rows;
end;`
	prog, errs := Build(input)
	if len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	mf := prog.Statements[0].(*ast.CreateMicroflowStmt)
	var got string
	for _, s := range mf.Body {
		if r, ok := s.(*ast.RetrieveStmt); ok && len(r.SortColumns) > 0 {
			got = r.SortColumns[0].Attribute
		}
	}
	if got != "M.Thing.Code" {
		t.Errorf("sort attribute = %q, want %q (unquoted dotted)", got, "M.Thing.Code")
	}
}

// TestUserRoleQuotingConsistency guards FINDINGS #5: DESCRIBE and DROP USER ROLE
// both accept bare and quoted names, so the two commands are consistent.
func TestUserRoleQuotingConsistency(t *testing.T) {
	cases := []struct {
		input    string
		wantName string
	}{
		{"describe user role Administrator;", "Administrator"},
		{"describe user role 'Administrator';", "Administrator"},
		{"drop user role User;", "User"},
		{"drop user role 'User';", "User"},
	}
	for _, tc := range cases {
		prog, errs := Build(tc.input)
		if len(errs) > 0 {
			t.Errorf("%q: unexpected parse errors: %v", tc.input, errs)
			continue
		}
		switch s := prog.Statements[0].(type) {
		case *ast.DescribeStmt:
			if s.Name.Name != tc.wantName {
				t.Errorf("%q: describe role name = %q, want %q", tc.input, s.Name.Name, tc.wantName)
			}
		case *ast.DropUserRoleStmt:
			if s.Name != tc.wantName {
				t.Errorf("%q: drop role name = %q, want %q", tc.input, s.Name, tc.wantName)
			}
		default:
			t.Errorf("%q: unexpected statement type %T", tc.input, prog.Statements[0])
		}
	}
}
