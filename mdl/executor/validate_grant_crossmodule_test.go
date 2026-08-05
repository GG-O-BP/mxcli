// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
)

// TestValidateStatement_CrossModuleGrant guards issue #836.
//
// `mxcli check --references` reported "All references valid / Check passed!"
// for a script whose GRANT names a role from a different module than the
// document. Execution then failed with the CE0148 guard — but only after the
// preceding statements had already been applied, because mxcli does not run a
// script in a single transaction. The whole point of --references is to catch
// this before anything is written.
//
// The guard existed (checkDocumentAccessRolesSameModule) and was called from
// all five exec paths; it was simply never reached from the validate path.
func TestValidateStatement_CrossModuleGrant(t *testing.T) {
	crossModule := []ast.QualifiedName{{Module: "ZKT27A", Name: "Role1"}}
	sameModule := []ast.QualifiedName{{Module: "ZKT27B", Name: "RoleB"}}

	cases := []struct {
		name  string
		cross ast.Statement
		same  ast.Statement
	}{
		{
			"microflow",
			&ast.GrantMicroflowAccessStmt{Microflow: ast.QualifiedName{Module: "ZKT27B", Name: "MF_Test"}, Roles: crossModule},
			&ast.GrantMicroflowAccessStmt{Microflow: ast.QualifiedName{Module: "ZKT27B", Name: "MF_Test"}, Roles: sameModule},
		},
		{
			"nanoflow",
			&ast.GrantNanoflowAccessStmt{Nanoflow: ast.QualifiedName{Module: "ZKT27B", Name: "NF_Test"}, Roles: crossModule},
			&ast.GrantNanoflowAccessStmt{Nanoflow: ast.QualifiedName{Module: "ZKT27B", Name: "NF_Test"}, Roles: sameModule},
		},
		{
			"page",
			&ast.GrantPageAccessStmt{Page: ast.QualifiedName{Module: "ZKT27B", Name: "P_Test"}, Roles: crossModule},
			&ast.GrantPageAccessStmt{Page: ast.QualifiedName{Module: "ZKT27B", Name: "P_Test"}, Roles: sameModule},
		},
		{
			"odata service",
			&ast.GrantODataServiceAccessStmt{Service: ast.QualifiedName{Module: "ZKT27B", Name: "Svc"}, Roles: crossModule},
			&ast.GrantODataServiceAccessStmt{Service: ast.QualifiedName{Module: "ZKT27B", Name: "Svc"}, Roles: sameModule},
		},
		{
			"published rest service",
			&ast.GrantPublishedRestServiceAccessStmt{Service: ast.QualifiedName{Module: "ZKT27B", Name: "Svc"}, Roles: crossModule},
			&ast.GrantPublishedRestServiceAccessStmt{Service: ast.QualifiedName{Module: "ZKT27B", Name: "Svc"}, Roles: sameModule},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCrossModuleGrant(tc.cross)
			if err == nil {
				t.Fatal("cross-module grant should be reported by validation, not only at exec time")
			}
			if !strings.Contains(err.Error(), "CE0148") {
				t.Errorf("error should name CE0148 so the user can search for it, got: %v", err)
			}
			if err := validateCrossModuleGrant(tc.same); err != nil {
				t.Errorf("same-module grant must stay valid, got: %v", err)
			}
		})
	}
}

// Statements that are not grants must be ignored by the check.
func TestValidateCrossModuleGrant_IgnoresOtherStatements(t *testing.T) {
	if err := validateCrossModuleGrant(&ast.CreateEntityStmt{}); err != nil {
		t.Errorf("non-grant statement should be ignored, got: %v", err)
	}
}
