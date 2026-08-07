// SPDX-License-Identifier: Apache-2.0

package visitor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
)

// mxcli-formula1 findings #13: `$N = 0;` did not parse — `no viable alternative
// at input '$N=0'`, an error naming the token rather than the missing keyword —
// while `DECLARE $N Integer = 0;` did, and every other assignment in MDL already
// worked bare (`$X = HEAD($List)`, `$X = execute database query …`). SET is now
// optional, so the bare form everyone reaches for means the same thing.
func TestBareAssignmentMatchesSetStatement(t *testing.T) {
	cases := []struct {
		name       string
		bare, full string
		wantTarget string
	}{
		{"integer literal", "$Total = 5;", "set $Total = 5;", "Total"},
		// A negative literal is the case that made this look like an
		// expression-parsing bug rather than a missing statement form.
		{"negative literal", "$Total = -1;", "set $Total = -1;", "Total"},
		{"expression over itself", "$Total = $Total + 1;", "set $Total = $Total + 1;", "Total"},
		{"string literal", "$Name = 'Hello';", "set $Name = 'Hello';", "Name"},
		// A plain variable target is stored without the sigil; an attribute
		// path keeps it. Both spellings must agree on whichever it is.
		{"attribute path", "$Order/Status = 'Pending';", "set $Order/Status = 'Pending';", "$Order/Status"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bare := parseSingleSet(t, tc.bare)
			full := parseSingleSet(t, tc.full)

			if bare.Target != tc.wantTarget {
				t.Errorf("bare target = %q, want %q", bare.Target, tc.wantTarget)
			}
			// Same statement, not merely both-parse: the two spellings must
			// produce the same AST or they are two features, not one.
			if bare.Target != full.Target {
				t.Errorf("target differs: bare %q vs SET %q", bare.Target, full.Target)
			}
		})
	}
}

// The keyword form must keep working — this is an addition, not a replacement,
// and existing scripts are full of it.
func TestSetKeywordStillParses(t *testing.T) {
	if got := parseSingleSet(t, "set $Total = 7;"); got.Target != "Total" {
		t.Errorf("target = %q, want Total", got.Target)
	}
}

// parseSingleSet parses one statement inside a microflow body and returns the
// MfSetStmt it produced.
func parseSingleSet(t *testing.T, stmt string) *ast.MfSetStmt {
	t.Helper()
	src := "create microflow M.ACT ($Order: M.O)\nbegin\n  declare $Total integer = 0;\n  declare $Name string;\n  " + stmt + "\nend;"
	prog, errs := Build(src)
	if len(errs) > 0 {
		t.Fatalf("parse errors for %q: %v", stmt, errs)
	}
	for _, s := range prog.Statements {
		cm, ok := s.(*ast.CreateMicroflowStmt)
		if !ok {
			continue
		}
		for _, st := range cm.Body {
			if set, ok := st.(*ast.MfSetStmt); ok {
				return set
			}
		}
	}
	t.Fatalf("no MfSetStmt produced by %q", stmt)
	return nil
}
