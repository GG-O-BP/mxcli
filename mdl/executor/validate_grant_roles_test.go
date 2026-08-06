// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/linter"
)

// ValidateGrantRoles is what `mxcli check` calls. It must fire without a
// project: the check compares two names already present in the script, so
// requiring -p would withhold an answer mxcli can always give (#836).
func TestValidateGrantRoles_ReportsCrossModuleWithoutProject(t *testing.T) {
	prog := &ast.Program{Statements: []ast.Statement{
		&ast.CreateModuleStmt{Name: "ZKT27B"},
		&ast.GrantMicroflowAccessStmt{
			Microflow: ast.QualifiedName{Module: "ZKT27B", Name: "MF_Test"},
			Roles:     []ast.QualifiedName{{Module: "ZKT27A", Name: "Role1"}},
		},
	}}
	got := ValidateGrantRoles(prog)
	if len(got) != 1 {
		t.Fatalf("got %d violations, want 1", len(got))
	}
	if got[0].RuleID != "MDL-GRANT01" {
		t.Errorf("RuleID = %q, want MDL-GRANT01", got[0].RuleID)
	}
	if got[0].Severity != linter.SeverityError {
		t.Errorf("Severity = %v, want error — this fails the build, not a style nit", got[0].Severity)
	}
	if !strings.Contains(got[0].Message, "CE0148") {
		t.Errorf("message should name CE0148, got: %s", got[0].Message)
	}
	if got[0].Suggestion == "" {
		t.Error("a violation the user must act on needs a suggestion")
	}
}

// A same-module grant must not be reported — the guard has to be silent on the
// correct form, or it just trains people to ignore it.
func TestValidateGrantRoles_SameModuleIsClean(t *testing.T) {
	prog := &ast.Program{Statements: []ast.Statement{
		&ast.GrantMicroflowAccessStmt{
			Microflow: ast.QualifiedName{Module: "ZKT27B", Name: "MF_Test"},
			Roles:     []ast.QualifiedName{{Module: "ZKT27B", Name: "RoleB"}},
		},
		&ast.GrantPageAccessStmt{
			Page:  ast.QualifiedName{Module: "Sales", Name: "Overview"},
			Roles: []ast.QualifiedName{{Module: "Sales", Name: "User"}, {Module: "Sales", Name: "Admin"}},
		},
	}}
	if got := ValidateGrantRoles(prog); len(got) != 0 {
		t.Errorf("same-module grants must not be reported, got %d: %+v", len(got), got)
	}
}
