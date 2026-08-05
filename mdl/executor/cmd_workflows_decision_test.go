package executor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
)

// TestBuildExclusiveSplit_NormalizesWorkflowContext guards issue #845.
//
// The workflow context parameter is stored as "WorkflowContext", and Mendix
// expressions are case-sensitive on 11.9+, so a user-written `$workflowContext`
// is an undefined variable. autoBindCallMicroflow already normalizes CALL
// MICROFLOW `WITH` expressions (FINDINGS #39), but DECISION expressions were
// stored verbatim, so the lowercase form reached the .mpr and mx check reported
//
//	[error] [CE0117] "Error(s) in expression." at Decision 'Decision'
//
// The inconsistency is what made this hard to spot: the same `$workflowContext`
// spelling works in a WITH clause and fails in a DECISION.
func TestBuildExclusiveSplit_NormalizesWorkflowContext(t *testing.T) {
	cases := map[string]string{
		"$workflowContext/IsExclusive": "$WorkflowContext/IsExclusive",
		"$WORKFLOWCONTEXT/Total > 100": "$WorkflowContext/Total > 100",
		"$WorkflowContext/IsExclusive": "$WorkflowContext/IsExclusive",
		"$Other/Field":                 "$Other/Field",
	}
	for in, want := range cases {
		act := buildExclusiveSplit(&ast.WorkflowDecisionNode{Expression: in})
		if act.Expression != want {
			t.Errorf("buildExclusiveSplit(%q).Expression = %q, want %q", in, act.Expression, want)
		}
	}
}

// TestBuildWaitForTimer_NormalizesWorkflowContext covers the same defect in the
// sibling expression site: a WAIT FOR TIMER delay may reference a date attribute
// on the workflow context, and was likewise stored verbatim.
func TestBuildWaitForTimer_NormalizesWorkflowContext(t *testing.T) {
	act := buildWaitForTimer(&ast.WorkflowWaitForTimerNode{
		DelayExpression: "$workflowContext/DueDate",
	})
	if want := "$WorkflowContext/DueDate"; act.DelayExpression != want {
		t.Errorf("buildWaitForTimer.DelayExpression = %q, want %q", act.DelayExpression, want)
	}
}
