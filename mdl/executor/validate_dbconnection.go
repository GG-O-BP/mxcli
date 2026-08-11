// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"fmt"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/linter"
)

// A database connection's connection string, username and password are
// ConstantIdentifier properties: BY_NAME references to a Constant document, with
// no literal alternative. `generated/metamodel` types them as
// `model.QualifiedName` and gen binds all three as `property.ByNameRef`.
//
// The grammar accepts a bare string in those positions too, and writing one puts
// text where Mendix expects a document name. That does not fail the build — it
// fails the LOAD, so the whole project becomes unopenable rather than one
// document becoming invalid:
//
//	StorageLoadException: One or more invalid values were detected while loading
//	the project: Database Connection 'M.Conn' has an invalid value '' for
//	property ConnectionString. The text 'jdbc:postgresql://…' is not a valid
//	ConstantIdentifier.
//
// Verified on Mendix 11.13.0. Same failure class as upstream #854, and the same
// remedy: refuse at author time rather than emit a structurally invalid unit.
//
// Refusing, rather than auto-creating a constant from the literal, is deliberate.
// Minting a document the author did not ask for is a silent side effect, and for
// `password` it would bake a secret into the model as a design-time default —
// exactly what the constant indirection exists to avoid.
var dbConnectionConstantProps = []struct {
	label  string // the MDL keyword, so the message points at what was typed
	value  func(*ast.CreateDatabaseConnectionStmt) string
	isRef  func(*ast.CreateDatabaseConnectionStmt) bool
	sample string
}{
	{
		label:  "connection string",
		value:  func(s *ast.CreateDatabaseConnectionStmt) string { return s.ConnectionString },
		isRef:  func(s *ast.CreateDatabaseConnectionStmt) bool { return s.ConnectionStringIsRef },
		sample: "DbUrl",
	},
	{
		label:  "username",
		value:  func(s *ast.CreateDatabaseConnectionStmt) string { return s.UserName },
		isRef:  func(s *ast.CreateDatabaseConnectionStmt) bool { return s.UserNameIsRef },
		sample: "DbUser",
	},
	{
		label:  "password",
		value:  func(s *ast.CreateDatabaseConnectionStmt) string { return s.Password },
		isRef:  func(s *ast.CreateDatabaseConnectionStmt) bool { return s.PasswordIsRef },
		sample: "DbPassword",
	},
}

// ValidateDatabaseConnection reports literal values written where Mendix stores a
// constant reference. Violations come back in a fixed property order so `check`
// output is deterministic and all three are reported at once.
func ValidateDatabaseConnection(stmt *ast.CreateDatabaseConnectionStmt) []linter.Violation {
	if stmt == nil {
		return nil
	}
	loc := linter.Location{
		DocumentType: "database connection",
		DocumentName: stmt.Name.String(),
	}

	var out []linter.Violation
	for _, p := range dbConnectionConstantProps {
		// An absent property is not an invalid one — all three are optional.
		if p.value(stmt) == "" || p.isRef(stmt) {
			continue
		}
		module := stmt.Name.Module
		if module == "" {
			module = "Module"
		}
		out = append(out, linter.Violation{
			RuleID:   "MDL058",
			Severity: linter.SeverityError,
			Message: fmt.Sprintf("database connection '%s' sets %s to a literal value — Mendix stores it as a "+
				"reference to a constant, and a literal there produces a project that cannot be opened at all "+
				"(StorageLoadException: \"is not a valid ConstantIdentifier\")",
				stmt.Name.String(), p.label),
			Suggestion: fmt.Sprintf("Declare a constant and reference it: "+
				"`create constant %s.%s type String default '…';` then `%s @%s.%s`. "+
				"The indirection is how Mendix keeps the value per-environment rather than in the model.",
				module, p.sample, p.label, module, p.sample),
			Location: loc,
		})
	}
	return out
}
