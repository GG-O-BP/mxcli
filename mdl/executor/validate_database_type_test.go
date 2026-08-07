// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/linter"
	"github.com/mendixlabs/mxcli/mdl/visitor"
)

// mxcli-formula1 findings #6: the skill documented `Redshift` (not in Studio
// Pro's picker on any version checked) and omitted `BYOD` (the only way to use
// a JDBC driver Mendix ships no entry for). mxcli writes the string straight
// through and mxbuild does not check it — `type 'Redshift'` builds 0 errors on
// 11.12.1 — so a wrong value hides behind a green build.
func TestValidateDatabaseConnectionType(t *testing.T) {
	script := func(dbType string) string {
		return `
create module D;
create constant D.Cs type string default 'jdbc:x';
create constant D.U type string default 'u';
create constant D.P type string default 'p';
create database connection D.Conn
type '` + dbType + `'
connection string @D.Cs
username @D.U
password @D.P
begin
end;
`
	}

	tests := []struct {
		dbType   string
		wantWarn bool
	}{
		{"PostgreSQL", false},
		{"MSSQL", false},
		{"Oracle", false},
		{"MySQL", false},
		{"Snowflake", false},
		{"BYOD", false},
		// Matching is case-insensitive: the picker's id is the canonical
		// spelling, but a different casing is not a different type.
		{"postgresql", false},
		// Both of these were in the skill's table and neither is real.
		{"Redshift", true},
		{"SQLServer", true},
		{"Postgres", true},
	}

	for _, tt := range tests {
		t.Run(tt.dbType, func(t *testing.T) {
			prog, errs := visitor.Build(script(tt.dbType))
			if len(errs) > 0 {
				t.Fatalf("parsing the script: %v", errs)
			}
			got := ValidateDatabaseConnectionType(prog)
			if tt.wantWarn {
				if len(got) != 1 {
					t.Fatalf("expected 1 warning for %q, got %d: %v", tt.dbType, len(got), got)
				}
				if got[0].RuleID != "MDL-DB01" {
					t.Errorf("rule = %q, want MDL-DB01", got[0].RuleID)
				}
				// A warning, not an error: the set is version-specific and
				// mxcli cannot prove a value wrong on a version it has not seen.
				if got[0].Severity != linter.SeverityWarning {
					t.Errorf("severity = %v, want warning", got[0].Severity)
				}
				// BYOD is the way out for a driver Mendix has no entry for, so
				// the suggestion has to name it.
				if !strings.Contains(got[0].Suggestion, "BYOD") {
					t.Errorf("suggestion should point at BYOD, got: %s", got[0].Suggestion)
				}
			} else if len(got) != 0 {
				t.Errorf("expected no warning for %q, got: %v", tt.dbType, got)
			}
		})
	}
}
