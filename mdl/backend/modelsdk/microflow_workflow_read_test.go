// SPDX-License-Identifier: Apache-2.0

package modelsdkbackend

import (
	"testing"

	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/microflows"
)

// The eight workflow call actions were authorable in MDL and had DESCRIBE
// formatters, but the modelsdk reader — the default engine — had no case for
// any of them. Each read back with a nil Action, so a shipped feature was
// write-only: DESCRIBE showed a placeholder and a describe→edit→exec cycle
// deleted the activity.
//
// These are round trips rather than reader-only tests on purpose. The writer
// builds these elements directly (newElem + explicit keys) and gen disagrees
// with it in at least one place per "get" action — gen binds the result variable
// as `VariableName`, the model stores `OutputVariableName` — so a reader written
// against the gen accessors would return "" and a reader-only test seeded with
// hand-written BSON would never notice. Pinning the two sides against each other
// is the only check that means anything here.
func TestMicroflowRoundTrip_WorkflowActions(t *testing.T) {
	tests := []struct {
		name   string
		action microflows.MicroflowAction
		verify func(t *testing.T, got microflows.MicroflowAction)
	}{
		{
			name: "WorkflowCallAction",
			action: &microflows.WorkflowCallAction{
				OutputVariableName:      "WfResult",
				UseReturnVariable:       true,
				Workflow:                "W.ApproveOrder",
				WorkflowContextVariable: "Order",
				ErrorHandlingType:       microflows.ErrorHandlingTypeRollback,
			},
			verify: func(t *testing.T, got microflows.MicroflowAction) {
				a, ok := got.(*microflows.WorkflowCallAction)
				if !ok {
					t.Fatalf("got %T, want *microflows.WorkflowCallAction", got)
				}
				if a.OutputVariableName != "WfResult" {
					t.Errorf("OutputVariableName = %q, want WfResult — gen binds this key as "+
						"VariableName, so reading it through the accessor loses it", a.OutputVariableName)
				}
				if a.Workflow != "W.ApproveOrder" || a.WorkflowContextVariable != "Order" || !a.UseReturnVariable {
					t.Errorf("got %+v, want the authored workflow/context/return flag", a)
				}
			},
		},
		{
			name: "GetWorkflowDataAction",
			action: &microflows.GetWorkflowDataAction{
				OutputVariableName: "Data",
				Workflow:           "W.ApproveOrder",
				WorkflowVariable:   "Wf",
			},
			verify: func(t *testing.T, got microflows.MicroflowAction) {
				a, ok := got.(*microflows.GetWorkflowDataAction)
				if !ok {
					t.Fatalf("got %T, want *microflows.GetWorkflowDataAction", got)
				}
				if a.OutputVariableName != "Data" || a.Workflow != "W.ApproveOrder" || a.WorkflowVariable != "Wf" {
					t.Errorf("got %+v, want {Data, W.ApproveOrder, Wf}", a)
				}
			},
		},
		{
			name: "GetWorkflowsAction",
			action: &microflows.GetWorkflowsAction{
				OutputVariableName:          "Workflows",
				WorkflowContextVariableName: "Order",
			},
			verify: func(t *testing.T, got microflows.MicroflowAction) {
				a, ok := got.(*microflows.GetWorkflowsAction)
				if !ok {
					t.Fatalf("got %T, want *microflows.GetWorkflowsAction", got)
				}
				if a.OutputVariableName != "Workflows" || a.WorkflowContextVariableName != "Order" {
					t.Errorf("got %+v, want {Workflows, Order}", a)
				}
			},
		},
		{
			name: "GetWorkflowActivityRecordsAction",
			action: &microflows.GetWorkflowActivityRecordsAction{
				OutputVariableName: "Records",
				WorkflowVariable:   "Wf",
			},
			verify: func(t *testing.T, got microflows.MicroflowAction) {
				a, ok := got.(*microflows.GetWorkflowActivityRecordsAction)
				if !ok {
					t.Fatalf("got %T, want *microflows.GetWorkflowActivityRecordsAction", got)
				}
				if a.OutputVariableName != "Records" || a.WorkflowVariable != "Wf" {
					t.Errorf("got %+v, want {Records, Wf}", a)
				}
			},
		},
		{
			name: "OpenWorkflowAction",
			action: &microflows.OpenWorkflowAction{
				WorkflowVariable: "Wf",
			},
			verify: func(t *testing.T, got microflows.MicroflowAction) {
				a, ok := got.(*microflows.OpenWorkflowAction)
				if !ok {
					t.Fatalf("got %T, want *microflows.OpenWorkflowAction", got)
				}
				if a.WorkflowVariable != "Wf" {
					t.Errorf("WorkflowVariable = %q, want Wf", a.WorkflowVariable)
				}
			},
		},
		{
			name: "LockWorkflowAction, all workflows",
			action: &microflows.LockWorkflowAction{
				PauseAllWorkflows: true,
			},
			verify: func(t *testing.T, got microflows.MicroflowAction) {
				a, ok := got.(*microflows.LockWorkflowAction)
				if !ok {
					t.Fatalf("got %T, want *microflows.LockWorkflowAction", got)
				}
				if !a.PauseAllWorkflows {
					t.Error("PauseAllWorkflows = false, want true")
				}
				if a.Workflow != "" || a.WorkflowVariable != "" {
					t.Errorf("selection = {%q, %q}, want neither — the writer omits "+
						"WorkflowSelection when all workflows are targeted, so its absence is meaningful",
						a.Workflow, a.WorkflowVariable)
				}
			},
		},
		{
			name: "LockWorkflowAction, one workflow by name",
			action: &microflows.LockWorkflowAction{
				Workflow: "W.ApproveOrder",
			},
			verify: func(t *testing.T, got microflows.MicroflowAction) {
				a, ok := got.(*microflows.LockWorkflowAction)
				if !ok {
					t.Fatalf("got %T, want *microflows.LockWorkflowAction", got)
				}
				if a.Workflow != "W.ApproveOrder" || a.WorkflowVariable != "" {
					t.Errorf("selection = {%q, %q}, want the name form only", a.Workflow, a.WorkflowVariable)
				}
			},
		},
		{
			name: "UnlockWorkflowAction, one workflow by variable",
			action: &microflows.UnlockWorkflowAction{
				WorkflowVariable: "Wf",
			},
			verify: func(t *testing.T, got microflows.MicroflowAction) {
				a, ok := got.(*microflows.UnlockWorkflowAction)
				if !ok {
					t.Fatalf("got %T, want *microflows.UnlockWorkflowAction", got)
				}
				// The object form stores the variable under WorkflowDefinitionVariable,
				// not WorkflowVariable — reading the wrong key loses the target.
				if a.WorkflowVariable != "Wf" || a.Workflow != "" {
					t.Errorf("selection = {%q, %q}, want the object form only", a.Workflow, a.WorkflowVariable)
				}
			},
		},
		{
			name: "WorkflowOperationAction, pause",
			action: &microflows.WorkflowOperationAction{
				Operation: &microflows.PauseOperation{WorkflowVariable: "Wf"},
			},
			verify: func(t *testing.T, got microflows.MicroflowAction) {
				a, ok := got.(*microflows.WorkflowOperationAction)
				if !ok {
					t.Fatalf("got %T, want *microflows.WorkflowOperationAction", got)
				}
				op, ok := a.Operation.(*microflows.PauseOperation)
				if !ok {
					t.Fatalf("Operation = %T, want *microflows.PauseOperation", a.Operation)
				}
				if op.WorkflowVariable != "Wf" {
					t.Errorf("WorkflowVariable = %q, want Wf", op.WorkflowVariable)
				}
			},
		},
		{
			name: "WorkflowOperationAction, abort carries a reason",
			action: &microflows.WorkflowOperationAction{
				Operation: &microflows.AbortOperation{WorkflowVariable: "Wf", Reason: "cancelled by user"},
			},
			verify: func(t *testing.T, got microflows.MicroflowAction) {
				a, ok := got.(*microflows.WorkflowOperationAction)
				if !ok {
					t.Fatalf("got %T, want *microflows.WorkflowOperationAction", got)
				}
				op, ok := a.Operation.(*microflows.AbortOperation)
				if !ok {
					t.Fatalf("Operation = %T, want *microflows.AbortOperation", a.Operation)
				}
				// Abort is the one variant with extra arity; dispatching on $Type is
				// what keeps the reason attached to it and off the others.
				if op.Reason != "cancelled by user" {
					t.Errorf("Reason = %q, want 'cancelled by user'", op.Reason)
				}
				if op.WorkflowVariable != "Wf" {
					t.Errorf("WorkflowVariable = %q, want Wf", op.WorkflowVariable)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			activity := &microflows.ActionActivity{Action: tc.action}
			activity.ID = model.ID("act-1")
			mf := &microflows.Microflow{
				Name: "ACT_Workflow",
				ObjectCollection: &microflows.MicroflowObjectCollection{
					Objects: []microflows.MicroflowObject{activity},
				},
			}
			mf.ID = model.ID("mf-1")

			got := roundTripMicroflow(t, mf)

			var found microflows.MicroflowAction
			for _, obj := range got.ObjectCollection.Objects {
				aa, ok := obj.(*microflows.ActionActivity)
				if !ok {
					continue
				}
				if aa.Action == nil {
					t.Fatal("ActionActivity round-tripped with a nil Action — " +
						"this is the placeholder shape DESCRIBE cannot render")
				}
				found = aa.Action
			}
			if found == nil {
				t.Fatal("no action survived the round trip")
			}
			tc.verify(t, found)
		})
	}
}
