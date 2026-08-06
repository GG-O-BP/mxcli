// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
)

// MDL-SEC01 gives `mxcli check` parity with the executor's refusal. Without it
// a script naming an audit member with non-default rights passed `check` and
// only failed once run against a project — the round-trip `check` exists to
// avoid, and the gap CI caught on issuetracker #20.
func TestValidateGrantEntityAccess_AuditMemberRights(t *testing.T) {
	grant := func(rights ...ast.EntityAccessRight) *ast.GrantEntityAccessStmt {
		return &ast.GrantEntityAccessStmt{
			Entity: ast.QualifiedName{Module: "IT", Name: "Doc"},
			Roles:  []ast.QualifiedName{{Module: "IT", Name: "Admin"}},
			Rights: rights,
		}
	}

	tests := []struct {
		name      string
		stmt      *ast.GrantEntityAccessStmt
		wantRule  bool
		wantNamed string
	}{
		{
			name: "read-only audit member under a ReadWrite default is rejected",
			stmt: grant(
				ast.EntityAccessRight{Type: ast.EntityAccessWriteAll},
				ast.EntityAccessRight{Type: ast.EntityAccessReadMembers, Members: []string{"createdDate"}}),
			wantRule:  true,
			wantNamed: "createdDate",
		},
		{
			name: "write on an audit member under a ReadOnly default is rejected",
			stmt: grant(
				ast.EntityAccessRight{Type: ast.EntityAccessReadAll},
				ast.EntityAccessRight{Type: ast.EntityAccessWriteMembers, Members: []string{"changedDate"}}),
			wantRule:  true,
			wantNamed: "changedDate",
		},
		{
			name: "quoted name is still recognised",
			stmt: grant(
				ast.EntityAccessRight{Type: ast.EntityAccessWriteAll},
				ast.EntityAccessRight{Type: ast.EntityAccessReadMembers, Members: []string{`"createdDate"`}}),
			wantRule: true,
		},
		{
			// Naming a member at the rule's own default is a no-op, not an error —
			// it is how you spell "yes, I know this member exists".
			name: "audit member matching the default is allowed",
			stmt: grant(
				ast.EntityAccessRight{Type: ast.EntityAccessReadAll},
				ast.EntityAccessRight{Type: ast.EntityAccessReadMembers, Members: []string{"createdDate", "changedDate"}}),
			wantRule: false,
		},
		{
			name: "ordinary attributes are never flagged",
			stmt: grant(
				ast.EntityAccessRight{Type: ast.EntityAccessWriteAll},
				ast.EntityAccessRight{Type: ast.EntityAccessReadMembers, Members: []string{"DocTitle", "CreatedDateLocal"}}),
			wantRule: false,
		},
		{
			name:     "wildcards alone are fine",
			stmt:     grant(ast.EntityAccessRight{Type: ast.EntityAccessReadAll}),
			wantRule: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateGrantEntityAccess(tc.stmt)
			if tc.wantRule {
				if len(got) == 0 {
					t.Fatal("expected MDL-SEC01, got none — check would pass a script exec refuses")
				}
				if got[0].RuleID != "MDL-SEC01" {
					t.Errorf("RuleID = %q, want MDL-SEC01", got[0].RuleID)
				}
				if !strings.Contains(got[0].Message, "CE0066") {
					t.Errorf("message should name the build error it prevents: %s", got[0].Message)
				}
				if tc.wantNamed != "" && !strings.Contains(got[0].Message, tc.wantNamed) {
					t.Errorf("message should name %q: %s", tc.wantNamed, got[0].Message)
				}
			} else if len(got) != 0 {
				t.Errorf("expected no violation, got %+v", got)
			}
		})
	}
}
