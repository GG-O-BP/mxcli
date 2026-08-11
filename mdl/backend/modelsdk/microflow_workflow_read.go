// SPDX-License-Identifier: Apache-2.0

package modelsdkbackend

import (
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/modelsdk/element"
	genMf "github.com/mendixlabs/mxcli/modelsdk/gen/microflows"
	"github.com/mendixlabs/mxcli/sdk/microflows"
)

// workflowActionFromGen reconstructs the workflow-related microflow actions —
// the inverse of workflowMicroflowActionToGen, and read against the SAME storage
// keys that serializer writes rather than the gen accessors.
//
// That distinction is load-bearing here: gen binds the result variable of every
// "get" action as `VariableName`, while the model stores `OutputVariableName`.
// Reading through the accessor returns "" and the variable silently disappears
// from DESCRIBE, so these read from raw wherever the two disagree (the same trap
// as ExecuteDatabaseQueryAction).
//
// Without these cases each action read back with a nil Action and rendered as a
// placeholder, so a shipped, authorable feature was write-only: `describe` could
// not show it and a describe→edit→exec cycle deleted it.
func workflowActionFromGen(el element.Element) microflows.MicroflowAction {
	switch a := el.(type) {
	case *genMf.WorkflowCallAction:
		raw := a.Raw()
		out := &microflows.WorkflowCallAction{
			ErrorHandlingType:       microflows.ErrorHandlingType(a.ErrorHandlingType()),
			OutputVariableName:      rawStr(raw, "OutputVariableName"),
			UseReturnVariable:       a.UseReturnVariable(),
			Workflow:                a.WorkflowQualifiedName(),
			WorkflowContextVariable: a.WorkflowContextVariable(),
		}
		out.ID = model.ID(a.ID())
		return out

	case *genMf.GetWorkflowDataAction:
		raw := a.Raw()
		out := &microflows.GetWorkflowDataAction{
			ErrorHandlingType:  microflows.ErrorHandlingType(a.ErrorHandlingType()),
			OutputVariableName: rawStr(raw, "OutputVariableName"),
			Workflow:           a.WorkflowQualifiedName(),
			WorkflowVariable:   a.WorkflowVariable(),
		}
		out.ID = model.ID(a.ID())
		return out

	case *genMf.GetWorkflowsAction:
		raw := a.Raw()
		out := &microflows.GetWorkflowsAction{
			ErrorHandlingType:           microflows.ErrorHandlingType(a.ErrorHandlingType()),
			OutputVariableName:          rawStr(raw, "OutputVariableName"),
			WorkflowContextVariableName: a.WorkflowContextVariableName(),
		}
		out.ID = model.ID(a.ID())
		return out

	case *genMf.GetWorkflowActivityRecordsAction:
		raw := a.Raw()
		out := &microflows.GetWorkflowActivityRecordsAction{
			ErrorHandlingType:  microflows.ErrorHandlingType(a.ErrorHandlingType()),
			OutputVariableName: rawStr(raw, "OutputVariableName"),
			WorkflowVariable:   a.WorkflowVariable(),
		}
		out.ID = model.ID(a.ID())
		return out

	case *genMf.WorkflowOperationAction:
		out := &microflows.WorkflowOperationAction{
			ErrorHandlingType: microflows.ErrorHandlingType(a.ErrorHandlingType()),
			Operation:         workflowOperationFromRaw(a.Raw()),
		}
		out.ID = model.ID(a.ID())
		return out

	case *genMf.OpenWorkflowAction:
		out := &microflows.OpenWorkflowAction{
			ErrorHandlingType: microflows.ErrorHandlingType(a.ErrorHandlingType()),
			WorkflowVariable:  a.WorkflowVariable(),
		}
		out.ID = model.ID(a.ID())
		return out

	case *genMf.LockWorkflowAction:
		raw := a.Raw()
		out := &microflows.LockWorkflowAction{
			ErrorHandlingType: microflows.ErrorHandlingType(a.ErrorHandlingType()),
			PauseAllWorkflows: a.PauseAllWorkflows(),
		}
		// The selection is absent by design when all workflows are targeted —
		// the writer omits it, so its absence is meaningful rather than missing.
		out.Workflow, out.WorkflowVariable = workflowSelectionFromRaw(raw)
		out.ID = model.ID(a.ID())
		return out

	case *genMf.UnlockWorkflowAction:
		raw := a.Raw()
		out := &microflows.UnlockWorkflowAction{
			ErrorHandlingType:        microflows.ErrorHandlingType(a.ErrorHandlingType()),
			ResumeAllPausedWorkflows: a.ResumeAllPausedWorkflows(),
		}
		out.Workflow, out.WorkflowVariable = workflowSelectionFromRaw(raw)
		out.ID = model.ID(a.ID())
		return out
	}
	return nil
}

// workflowOperationFromRaw reconstructs the polymorphic Operation sub-element of
// a WorkflowOperationAction. Dispatch is on the stored `$Type`: the variants
// differ in arity — only Abort carries a Reason — so reading fields without
// branching first would invent a reason on the others.
func workflowOperationFromRaw(raw bson.Raw) microflows.WorkflowOperation {
	opDoc, ok := raw.Lookup("Operation").DocumentOK()
	if !ok {
		return nil
	}
	variable := rawStr(opDoc, "WorkflowVariable")
	switch rawStr(opDoc, "$Type") {
	case "Microflows$AbortOperation":
		op := &microflows.AbortOperation{WorkflowVariable: variable}
		if reason, ok := opDoc.Lookup("Reason").DocumentOK(); ok {
			op.Reason = rawStr(reason, "Text")
		}
		return op
	case "Microflows$ContinueOperation":
		return &microflows.ContinueOperation{WorkflowVariable: variable}
	case "Microflows$PauseOperation":
		return &microflows.PauseOperation{WorkflowVariable: variable}
	case "Microflows$RestartOperation":
		return &microflows.RestartOperation{WorkflowVariable: variable}
	case "Microflows$RetryOperation":
		return &microflows.RetryOperation{WorkflowVariable: variable}
	case "Microflows$UnpauseOperation":
		return &microflows.UnpauseOperation{WorkflowVariable: variable}
	}
	return nil
}

// workflowSelectionFromRaw reads a Workflows$WorkflowDefinition*Selection back
// into the (workflow, workflowVariable) pair the model keeps. Exactly one of the
// two is set, matching workflowSelectionToGen; the object form stores the
// variable under `WorkflowDefinitionVariable`, not `WorkflowVariable`.
func workflowSelectionFromRaw(raw bson.Raw) (workflow, workflowVariable string) {
	selDoc, ok := raw.Lookup("WorkflowSelection").DocumentOK()
	if !ok {
		return "", ""
	}
	switch rawStr(selDoc, "$Type") {
	case "Workflows$WorkflowDefinitionNameSelection":
		return rawStr(selDoc, "Workflow"), ""
	case "Workflows$WorkflowDefinitionObjectSelection":
		return "", rawStr(selDoc, "WorkflowDefinitionVariable")
	}
	return "", ""
}
