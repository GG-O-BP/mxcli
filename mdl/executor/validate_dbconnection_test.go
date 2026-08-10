// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/linter"
)

// A database connection's ConnectionString / UserName / Password are
// ConstantIdentifier properties — BY_NAME references to a constant, with no
// literal alternative (`generated/metamodel`: all three are `model.QualifiedName`;
// gen binds them as `property.ByNameRef`).
//
// The grammar also accepts a bare string there, and writing one produced a
// project Mendix CANNOT LOAD:
//
//	StorageLoadException: ... has an invalid value '' for property
//	ConnectionString. The text 'jdbc:postgresql://...' is not a valid
//	ConstantIdentifier.
//
// Same class as #854: no build error, because `mx check` dies during load before
// validating anything, taking the whole project down rather than one document.
func TestValidateDatabaseConnection_RejectsLiteralCredentials(t *testing.T) {
	tests := []struct {
		name     string
		stmt     *ast.CreateDatabaseConnectionStmt
		wantProp string
	}{
		{
			name: "literal connection string",
			stmt: &ast.CreateDatabaseConnectionStmt{
				Name:             ast.QualifiedName{Module: "M", Name: "Conn"},
				ConnectionString: "jdbc:postgresql://localhost:5432/app",
			},
			wantProp: "connection string",
		},
		{
			name: "literal username",
			stmt: &ast.CreateDatabaseConnectionStmt{
				Name:     ast.QualifiedName{Module: "M", Name: "Conn"},
				UserName: "app",
			},
			wantProp: "username",
		},
		{
			name: "literal password",
			stmt: &ast.CreateDatabaseConnectionStmt{
				Name:     ast.QualifiedName{Module: "M", Name: "Conn"},
				Password: "secret",
			},
			wantProp: "password",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vs := ValidateDatabaseConnection(tc.stmt)
			if len(vs) == 0 {
				t.Fatalf("no violation for a literal %s — the written project will not open", tc.wantProp)
			}
			v := vs[0]
			if v.Severity != linter.SeverityError {
				t.Errorf("severity = %v, want error: the project cannot be loaded at all", v.Severity)
			}
			if !strings.Contains(strings.ToLower(v.Message), tc.wantProp) {
				t.Errorf("message %q does not name the offending property %q", v.Message, tc.wantProp)
			}
			// The remedy must be actionable: name the constant form.
			if !strings.Contains(v.Suggestion, "@") || !strings.Contains(strings.ToLower(v.Suggestion), "constant") {
				t.Errorf("suggestion %q does not point at the `@Module.Constant` form", v.Suggestion)
			}
		})
	}
}

// The constant-reference form is the one that works, and must stay silent —
// including when only some of the three are set.
func TestValidateDatabaseConnection_AcceptsConstantRefs(t *testing.T) {
	stmt := &ast.CreateDatabaseConnectionStmt{
		Name:                  ast.QualifiedName{Module: "M", Name: "Conn"},
		ConnectionString:      "M.DbUrl",
		ConnectionStringIsRef: true,
		UserName:              "M.DbUser",
		UserNameIsRef:         true,
		Password:              "M.DbPass",
		PasswordIsRef:         true,
	}
	if vs := ValidateDatabaseConnection(stmt); len(vs) != 0 {
		t.Fatalf("constant references flagged: %+v", vs)
	}

	// A connection with none of the three set is also fine — they are optional,
	// and an absent property is not an invalid one.
	bare := &ast.CreateDatabaseConnectionStmt{Name: ast.QualifiedName{Module: "M", Name: "Conn"}}
	if vs := ValidateDatabaseConnection(bare); len(vs) != 0 {
		t.Fatalf("empty connection flagged: %+v", vs)
	}
}

// All three literals in one statement must all be reported, in a stable order —
// fixing one at a time across three exec runs is a poor loop when `check` could
// have said so once.
func TestValidateDatabaseConnection_ReportsEveryLiteral(t *testing.T) {
	stmt := &ast.CreateDatabaseConnectionStmt{
		Name:             ast.QualifiedName{Module: "M", Name: "Conn"},
		ConnectionString: "jdbc:...",
		UserName:         "app",
		Password:         "secret",
	}
	vs := ValidateDatabaseConnection(stmt)
	if len(vs) != 3 {
		t.Fatalf("got %d violations, want 3 (one per literal)", len(vs))
	}
	want := []string{"connection string", "username", "password"}
	for i, w := range want {
		if !strings.Contains(strings.ToLower(vs[i].Message), w) {
			t.Errorf("violation[%d] = %q, want it to name %q (order must be stable)", i, vs[i].Message, w)
		}
	}
}
