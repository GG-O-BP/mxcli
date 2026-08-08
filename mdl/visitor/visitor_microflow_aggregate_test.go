// SPDX-License-Identifier: Apache-2.0

package visitor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
)

// Making SET optional put `$X = <expr>` in front of every
// `VARIABLE EQUALS <function-call>` statement in the grammar, so
// `$Sum = sum($List.Price)` stopped reaching aggregateListStatement and fell
// through to the SET conversion instead — which joined the list and the
// attribute into one name. mxbuild rejected the result:
//
//	[CE0109] "Undefined variable 'ProductList.Price'."
//	[CE0015] "Aggregate function must specify a valid attribute."
//
// Both spellings must produce the same aggregate, whichever rule claims them.
func TestAggregateSplitsListFromAttribute(t *testing.T) {
	cases := []struct {
		name, src string
		wantOp    ast.AggregateListOperationType
		wantAttr  string
	}{
		{"bare sum", "$T = sum($ProductList.Price);", ast.AggregateSum, "Price"},
		{"bare average", "$T = average($ProductList.Price);", ast.AggregateAverage, "Price"},
		{"bare minimum", "$T = minimum($ProductList.Price);", ast.AggregateMinimum, "Price"},
		{"bare maximum", "$T = maximum($ProductList.Price);", ast.AggregateMaximum, "Price"},
		// The SET keyword routes through a different conversion; it was wrong
		// there before the bare form ever reached it.
		{"set sum", "set $T = sum($ProductList.Price);", ast.AggregateSum, "Price"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseSingleAggregate(t, tc.src)
			if got.Operation != tc.wantOp {
				t.Errorf("operation = %v, want %v", got.Operation, tc.wantOp)
			}
			if got.InputVariable != "ProductList" {
				t.Errorf("input variable = %q, want ProductList (the list, without the attribute)", got.InputVariable)
			}
			if got.Attribute != tc.wantAttr {
				t.Errorf("attribute = %q, want %q", got.Attribute, tc.wantAttr)
			}
		})
	}
}

// `sum($List, <expression>)` aggregates a value computed per item. Losing the
// expression leaves an aggregate with nothing to aggregate — CE0015.
func TestAggregateKeepsPerItemExpression(t *testing.T) {
	for _, src := range []string{
		"$T = sum($ProductList, $currentObject/Price * 0.21);",
		"set $T = sum($ProductList, $currentObject/Price * 0.21);",
	} {
		got := parseSingleAggregate(t, src)
		if got.InputVariable != "ProductList" {
			t.Errorf("%s: input variable = %q, want ProductList", src, got.InputVariable)
		}
		if !got.IsExpression || got.Expression == nil {
			t.Errorf("%s: expression dropped (IsExpression=%v, Expression=%v)", src, got.IsExpression, got.Expression)
		}
	}
}

// COUNT takes the list alone and must not acquire an attribute.
func TestAggregateCountTakesTheListAlone(t *testing.T) {
	got := parseSingleAggregate(t, "$N = count($ProductList);")
	if got.Operation != ast.AggregateCount || got.InputVariable != "ProductList" || got.Attribute != "" {
		t.Errorf("got %+v, want COUNT over ProductList with no attribute", got)
	}
}

func parseSingleAggregate(t *testing.T, stmt string) *ast.AggregateListStmt {
	t.Helper()
	src := "create microflow M.A ($ProductList: list of M.Product)\nbegin\n  " + stmt + "\nend;"
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
			if agg, ok := st.(*ast.AggregateListStmt); ok {
				return agg
			}
			// A SET means the statement was swallowed as a plain value
			// assignment — that is the regression, and it reads better as a
			// failure here than as a nil dereference below.
			if set, ok := st.(*ast.MfSetStmt); ok {
				t.Fatalf("%q produced a Change Variable (target %q), not an aggregate", stmt, set.Target)
			}
		}
	}
	t.Fatalf("no AggregateListStmt produced by %q", stmt)
	return nil
}
