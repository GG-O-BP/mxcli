// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"testing"

	"github.com/mendixlabs/mxcli/sdk/microflows"
	"github.com/mendixlabs/mxcli/sdk/workflows"
)

// TestNormalizeWorkflowContextExpr guards the CE0117 half of the FINDINGS #39
// regression: the 11.9+ CallMicroflowActivity is case-sensitive, so a user-written
// `$workflowContext` must be normalized to the context parameter name
// `$WorkflowContext`.
func TestNormalizeWorkflowContextExpr(t *testing.T) {
	cases := map[string]string{
		"$workflowContext":       "$WorkflowContext",
		"$WORKFLOWCONTEXT":       "$WorkflowContext",
		"$WorkflowContext":       "$WorkflowContext",
		"$workflowContext/Field": "$WorkflowContext/Field",
		"$Other":                 "$Other",
		"'literal'":              "'literal'",
	}
	for in, want := range cases {
		if got := normalizeWorkflowContextExpr(in); got != want {
			t.Errorf("normalizeWorkflowContextExpr(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDefaultCallMicroflowOutcomes guards the CE6686 half of the FINDINGS #39
// regression: default outcomes must match the target microflow's return type —
// Boolean → two BooleanConditionOutcomes, anything else → a single default.
func TestDefaultCallMicroflowOutcomes(t *testing.T) {
	boolMF := &microflows.Microflow{Name: "ACT", ReturnType: microflows.BooleanType{}}
	outs := defaultCallMicroflowOutcomes(boolMF)
	if len(outs) != 2 {
		t.Fatalf("boolean return: got %d outcomes, want 2 (true/false)", len(outs))
	}
	sawTrue, sawFalse := false, false
	for _, o := range outs {
		b, ok := o.(*workflows.BooleanConditionOutcome)
		if !ok {
			t.Fatalf("boolean return: outcome %T, want *BooleanConditionOutcome", o)
		}
		sawTrue = sawTrue || b.Value
		sawFalse = sawFalse || !b.Value
	}
	if !sawTrue || !sawFalse {
		t.Errorf("boolean return: want both true and false outcomes, got true=%v false=%v", sawTrue, sawFalse)
	}

	// Void / nil / non-branching → single VoidConditionOutcome.
	for _, mf := range []*microflows.Microflow{nil, {ReturnType: microflows.VoidType{}}, {ReturnType: microflows.StringType{}}} {
		outs := defaultCallMicroflowOutcomes(mf)
		if len(outs) != 1 {
			t.Fatalf("non-boolean return %v: got %d outcomes, want 1", mf, len(outs))
		}
		if _, ok := outs[0].(*workflows.VoidConditionOutcome); !ok {
			t.Errorf("non-boolean return: outcome %T, want *VoidConditionOutcome", outs[0])
		}
	}
}
