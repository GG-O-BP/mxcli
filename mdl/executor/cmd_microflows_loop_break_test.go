// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/sdk/microflows"
)

// TestBuilder_ConditionalBreakLastInLoop_HasFalseFlow pins ledger #52: a
// `loop { if cond then break }` where the IF is the last statement built an
// ExclusiveSplit with ONLY its `true` outgoing flow (→ BreakEvent); the `false`
// case was deferred to a following statement that never came, so mx check
// reported CE0079 "the 'false' condition value should be configured on an
// outgoing sequence flow". The false branch must now be wired to a ContinueEvent
// ("didn't break → next iteration").
func TestBuilder_ConditionalBreakLastInLoop_HasFalseFlow(t *testing.T) {
	body := []ast.MicroflowStatement{
		&ast.LoopStmt{
			ListVariable: "L",
			LoopVariable: "R",
			Body: []ast.MicroflowStatement{
				&ast.IfStmt{
					Condition: &ast.LiteralExpr{Kind: ast.LiteralBoolean, Value: true},
					ThenBody:  []ast.MicroflowStatement{&ast.BreakStmt{}},
				},
			},
		},
	}

	fb := &flowBuilder{posX: 100, posY: 100, spacing: HorizontalSpacing, varTypes: map[string]string{"L": "List of M.R"}}
	col := fb.buildFlowGraph(body, nil)

	// The loop's internal flows are lifted to the top-level collection.
	var splitID string
	hasBreak, hasContinue := false, false
	for _, o := range col.Objects {
		switch obj := o.(type) {
		case *microflows.LoopedActivity:
			for _, inner := range obj.ObjectCollection.Objects {
				switch inner.(type) {
				case *microflows.ExclusiveSplit:
					splitID = string(inner.(*microflows.ExclusiveSplit).ID)
				case *microflows.BreakEvent:
					hasBreak = true
				case *microflows.ContinueEvent:
					hasContinue = true
				}
			}
		}
	}
	// LoopedActivity may store its objects in the inner collection; also scan
	// there for events if the split lives inside it.
	if splitID == "" {
		t.Fatal("no ExclusiveSplit found in the loop body")
	}
	if !hasBreak {
		t.Error("expected a BreakEvent in the loop body")
	}
	if !hasContinue {
		t.Error("expected a synthesized ContinueEvent for the split's false branch (ledger #52)")
	}

	trueCount, falseCount := 0, 0
	for _, f := range col.Flows {
		if string(f.OriginID) != splitID {
			continue
		}
		switch cv := f.CaseValue.(type) {
		case *microflows.ExpressionCase:
			if cv.Expression == "true" {
				trueCount++
			} else if cv.Expression == "false" {
				falseCount++
			}
		case microflows.EnumerationCase:
			if cv.Value == "true" {
				trueCount++
			} else if cv.Value == "false" {
				falseCount++
			}
		}
	}
	if trueCount != 1 {
		t.Errorf("split should have exactly 1 true flow (→ break), got %d", trueCount)
	}
	if falseCount != 1 {
		t.Errorf("split should have exactly 1 false flow (→ continue), got %d — a decision with no false flow is CE0079", falseCount)
	}
}
