// SPDX-License-Identifier: Apache-2.0

package modelsdkbackend

import (
	"testing"

	"github.com/mendixlabs/mxcli/modelsdk/codec"
	"github.com/mendixlabs/mxcli/modelsdk/element"
	"github.com/mendixlabs/mxcli/sdk/workflows"
)

// buildCallMicroflowWorkflowGen builds the gen element tree for a workflow whose
// flow contains a single "call microflow" activity.
func buildCallMicroflowWorkflowGen() element.Element {
	wf := &workflows.Workflow{
		Name:         "SnFlow",
		WorkflowName: "Sn Flow",
		Parameter:    &workflows.WorkflowParameter{EntityRef: "MyFirstModule.Ctx"},
		Flow: &workflows.Flow{
			Activities: []workflows.WorkflowActivity{
				&workflows.StartWorkflowActivity{BaseWorkflowActivity: workflows.BaseWorkflowActivity{Name: "Start"}},
				&workflows.CallMicroflowTask{
					BaseWorkflowActivity: workflows.BaseWorkflowActivity{Name: "Call", Caption: "Call"},
					Microflow:            "MyFirstModule.ACT_Do",
				},
				&workflows.EndWorkflowActivity{BaseWorkflowActivity: workflows.BaseWorkflowActivity{Name: "End"}},
			},
		},
	}
	return workflowToGen(wf)
}

func countTypeName(root element.Element, typ string) int {
	n := 0
	element.Walk(root, func(e element.Element) bool {
		if e.TypeName() == typ {
			n++
		}
		return true
	})
	return n
}

// TestApplyCallMicroflowStorageName verifies the version-gated $Type rewrite
// (FINDINGS #39): pre-11.9 keeps CallMicroflowTask; 11.9+ rewrites to
// CallMicroflowActivity. Writing the wrong name makes the runtime fail to load
// the whole model, and both mxcli check and mx check pass regardless — so this is
// the only guard.
func TestApplyCallMicroflowStorageName(t *testing.T) {
	// useActivity=false (pre-11.9): tree keeps the legacy CallMicroflowTask name.
	g := buildCallMicroflowWorkflowGen()
	applyCallMicroflowStorageName(g, false)
	if got := countTypeName(g, callMicroflowTaskType); got != 1 {
		t.Errorf("pre-11.9: CallMicroflowTask count = %d, want 1", got)
	}
	if got := countTypeName(g, callMicroflowActivityType); got != 0 {
		t.Errorf("pre-11.9: CallMicroflowActivity count = %d, want 0", got)
	}

	// useActivity=true (11.9+): the activity is rewritten to CallMicroflowActivity.
	g2 := buildCallMicroflowWorkflowGen()
	applyCallMicroflowStorageName(g2, true)
	if got := countTypeName(g2, callMicroflowActivityType); got != 1 {
		t.Errorf("11.9+: CallMicroflowActivity count = %d, want 1", got)
	}
	if got := countTypeName(g2, callMicroflowTaskType); got != 0 {
		t.Errorf("11.9+: CallMicroflowTask count = %d, want 0", got)
	}

	// The 11.9+ tree must still encode cleanly — the codec looks up TypeDefaults
	// and list markers by $Type, so both must be registered under the new name.
	if _, err := (&codec.Encoder{}).Encode(g2); err != nil {
		t.Fatalf("encode with CallMicroflowActivity name: %v", err)
	}
}

// TestUseCallMicroflowActivityName_Pre119 checks the version gate against the
// vendored fixture (Mendix 11.6.6, i.e. < 11.9): it must select the legacy name.
func TestUseCallMicroflowActivityName_Pre119(t *testing.T) {
	proj := copyFixture(t)
	b := New()
	if err := b.Connect(proj); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = b.Disconnect() })

	if b.useCallMicroflowActivityName() {
		t.Errorf("fixture is Mendix 11.6.6 (< 11.9); useCallMicroflowActivityName() = true, want false")
	}
}

// TestWorkflowParameterMappingRoundTrip guards FINDINGS #42: a call-microflow
// activity's parameter mappings must survive create → read, so DESCRIBE WORKFLOW
// re-emits the `with (...)` clause instead of silently dropping it.
func TestWorkflowParameterMappingRoundTrip(t *testing.T) {
	proj := copyFixture(t)
	b := New()
	if err := b.Connect(proj); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = b.Disconnect() })

	mod, err := b.GetModuleByName("MyFirstModule")
	if err != nil || mod == nil {
		t.Fatalf("GetModuleByName: %v", err)
	}
	wf := &workflows.Workflow{
		ContainerID:  mod.ID,
		Name:         "PmFlow",
		WorkflowName: "Pm Flow",
		Parameter:    &workflows.WorkflowParameter{EntityRef: "MyFirstModule.Ctx"},
		Flow: &workflows.Flow{Activities: []workflows.WorkflowActivity{
			&workflows.StartWorkflowActivity{BaseWorkflowActivity: workflows.BaseWorkflowActivity{Name: "Start"}},
			&workflows.CallMicroflowTask{
				BaseWorkflowActivity: workflows.BaseWorkflowActivity{Name: "Call", Caption: "Call"},
				Microflow:            "MyFirstModule.ACT_Do",
				ParameterMappings: []*workflows.ParameterMapping{
					{Parameter: "Item", Expression: "$workflowContext"},
				},
			},
			&workflows.EndWorkflowActivity{BaseWorkflowActivity: workflows.BaseWorkflowActivity{Name: "End"}},
		}},
	}
	if err := b.CreateWorkflow(wf); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	b2 := New()
	if err := b2.Connect(proj); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	t.Cleanup(func() { _ = b2.Disconnect() })
	all, err := b2.ListWorkflows()
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	var cm *workflows.CallMicroflowTask
	for _, w := range all {
		if w.Name != "PmFlow" || w.Flow == nil {
			continue
		}
		for _, act := range w.Flow.Activities {
			if c, ok := act.(*workflows.CallMicroflowTask); ok {
				cm = c
			}
		}
	}
	if cm == nil {
		t.Fatal("call-microflow activity not found after round-trip")
	}
	if len(cm.ParameterMappings) != 1 {
		t.Fatalf("ParameterMappings count = %d, want 1 (mapping dropped on read)", len(cm.ParameterMappings))
	}
	if cm.ParameterMappings[0].Expression != "$workflowContext" {
		t.Errorf("mapping expression = %q, want %q", cm.ParameterMappings[0].Expression, "$workflowContext")
	}
}
