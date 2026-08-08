// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
)

// mxcli-formula1 findings #21: `execute database query … dynamic $Sql` reached
// the runtime as the string literal '$Sql', so DuckDB was asked to execute the
// four characters:
//
//	ERROR - ExternalDatabaseConnector: Parser Error: syntax error at or near "$"
//
// The builder quoted anything not already starting with a quote, and the AST
// kept no literal-vs-expression flag, so it could not tell them apart. This
// blocked runtime-built SQL — query pushdown — outright.
func TestDynamicQueryExpressionIsNotQuoted(t *testing.T) {
	cases := []struct {
		name string
		stmt *ast.ExecuteDatabaseQueryStmt
		want string
	}{
		{
			"a variable passes through untouched",
			&ast.ExecuteDatabaseQueryStmt{DynamicQuery: "$Sql", DynamicQueryIsExpression: true},
			"$Sql",
		},
		{
			"so does a built expression",
			&ast.ExecuteDatabaseQueryStmt{DynamicQuery: "'SELECT * FROM t LIMIT ' + toString($Limit)", DynamicQueryIsExpression: true},
			"'SELECT * FROM t LIMIT ' + toString($Limit)",
		},
		{
			"a literal is still quoted, because the field holds an expression",
			&ast.ExecuteDatabaseQueryStmt{DynamicQuery: "SELECT * FROM t"},
			"'SELECT * FROM t'",
		},
		{
			"a literal's own quotes are doubled, Mendix-style",
			&ast.ExecuteDatabaseQueryStmt{DynamicQuery: "SELECT 'a'"},
			"'SELECT ''a'''",
		},
		{
			"no dynamic query stays empty rather than becoming two quotes",
			&ast.ExecuteDatabaseQueryStmt{},
			"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dynamicQueryExpression(tc.stmt); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
