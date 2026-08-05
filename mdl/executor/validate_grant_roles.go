// SPDX-License-Identifier: Apache-2.0

// Check-time (no-project) validation for document-access GRANT statements.
// Mendix stores document access as references to the document's OWN module roles
// only, so granting a page/microflow/nanoflow/service access to a role from a
// different module builds with CE0148 ("reselect roles"). The comparison is
// purely between the two qualified names in the statement, so it needs no
// project — see issue #836.
package executor

import (
	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/linter"
)

// ValidateGrantRoles reports (MDL-GRANT01) a GRANT that names a module role from
// a different module than the document it targets.
//
// This lives in the no-project pass rather than the --references pass on
// purpose: the check compares two names already present in the script, so
// requiring -p would withhold an answer mxcli can always give. It also means a
// plain `mxcli check` catches it, not only `check --references`.
func ValidateGrantRoles(prog *ast.Program) []linter.Violation {
	var out []linter.Violation
	for _, stmt := range prog.Statements {
		if err := validateCrossModuleGrant(stmt); err != nil {
			out = append(out, linter.Violation{
				RuleID:   "MDL-GRANT01",
				Severity: linter.SeverityError,
				Message:  err.Error(),
				Suggestion: "Grant a module role from the document's own module, then map the user role to it " +
					"(`alter user role <UserRole> add <DocModule>.<Role>`).",
			})
		}
	}
	return out
}
