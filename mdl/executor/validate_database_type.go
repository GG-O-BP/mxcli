// SPDX-License-Identifier: Apache-2.0

// Check-time (no-project) validation of CREATE DATABASE CONNECTION's TYPE.
//
// mxcli writes the type string straight through to BSON, and mxbuild accepts
// anything: `type 'Redshift'` builds 0 errors on 11.12.1 and is simply not a
// database type Mendix has. The values are the ones Studio Pro's own connector
// editor offers, read out of the shipped bundle at
// modeler/ide-client/database-connector-editor/ (verified identical on 11.10.0,
// 11.12.1 and — per mxcli-formula1 findings #6 — 11.13.0).
package executor

import (
	"fmt"
	"strings"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/linter"
)

// databaseConnectionTypes is Studio Pro's picker, id -> label.
var databaseConnectionTypes = []struct{ ID, Label string }{
	{"MSSQL", "Microsoft SQL"},
	{"MySQL", "MySQL"},
	{"Oracle", "Oracle"},
	{"PostgreSQL", "PostgreSQL"},
	{"Snowflake", "Snowflake"},
	{"BYOD", "Other — bring your own JDBC driver"},
}

// ValidateDatabaseConnectionType warns (MDL-DB01) when a CREATE DATABASE
// CONNECTION names a type Studio Pro does not offer.
//
// A warning rather than an error: the set is version-specific, and mxbuild does
// not reject an unknown value, so mxcli cannot prove one wrong on a Mendix
// version it has not seen. Saying nothing is worse — the build is green and the
// connection simply does not work.
func ValidateDatabaseConnectionType(prog *ast.Program) []linter.Violation {
	if prog == nil {
		return nil
	}
	var out []linter.Violation
	for _, stmt := range prog.Statements {
		s, ok := stmt.(*ast.CreateDatabaseConnectionStmt)
		if !ok || s.DatabaseType == "" {
			continue
		}
		if knownDatabaseConnectionType(s.DatabaseType) {
			continue
		}
		out = append(out, linter.Violation{
			RuleID:   "MDL-DB01",
			Severity: linter.SeverityWarning,
			Message: fmt.Sprintf("database connection %s: type %q is not one Studio Pro offers — it is written to the model as-is and mxbuild does not check it, so the build stays green and the connection does not work",
				s.Name.String(), s.DatabaseType),
			Suggestion: fmt.Sprintf("Use one of: %s. For a JDBC driver Mendix has no entry for, use 'BYOD' — it skips the driver-presence check and takes the connection string as given.",
				strings.Join(databaseConnectionTypeIDs(), ", ")),
		})
	}
	return out
}

func knownDatabaseConnectionType(name string) bool {
	for _, t := range databaseConnectionTypes {
		if strings.EqualFold(t.ID, name) {
			return true
		}
	}
	return false
}

func databaseConnectionTypeIDs() []string {
	ids := make([]string, 0, len(databaseConnectionTypes))
	for _, t := range databaseConnectionTypes {
		ids = append(ids, "'"+t.ID+"'")
	}
	return ids
}
