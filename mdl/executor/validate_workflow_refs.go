// SPDX-License-Identifier: Apache-2.0

// Reference-based (project-connected) validation for workflows. Runs under
// `check --references`, where the target microflows can be introspected. See
// validate_workflow.go for the syntax-only (no-project) checks.
package executor

import (
	"fmt"

	"github.com/mendixlabs/mxcli/mdl/ast"
)

// validateWorkflowParameterMappings checks that each workflow "call microflow"
// activity maps every parameter of its target microflow. Mendix rejects an
// unmapped parameter (CE6677 — "should accept parameters ...") and the workflow
// would fail at the activity, but mxcli check passed it (FINDINGS #40). Microflows
// created in the same script are skipped (not yet queryable); a target microflow
// that isn't in the project is left to the missing-reference check.
func validateWorkflowParameterMappings(ctx *ExecContext, s *ast.CreateWorkflowStmt, sc *scriptContext) []string {
	if ctx == nil || ctx.Backend == nil {
		return nil
	}
	h, err := getHierarchy(ctx)
	if err != nil {
		return nil
	}
	mfs, err := ctx.Backend.ListMicroflows()
	if err != nil {
		return nil
	}
	paramsByMF := make(map[string][]string, len(mfs))
	for _, mf := range mfs {
		names := make([]string, 0, len(mf.Parameters))
		for _, p := range mf.Parameters {
			names = append(names, p.Name)
		}
		paramsByMF[h.GetQualifiedName(mf.ContainerID, mf.Name)] = names
	}

	var errs []string
	walkWorkflowActivities(s.Activities, func(act ast.WorkflowActivityNode) {
		cm, ok := act.(*ast.WorkflowCallMicroflowNode)
		if !ok {
			return
		}
		mfQN := cm.Microflow.String()
		if sc != nil && sc.microflows[mfQN] {
			return // created in the same script — cannot introspect its parameters
		}
		want, known := paramsByMF[mfQN]
		if !known {
			return // target microflow not in project; the missing-ref check covers it
		}
		mapped := make(map[string]bool, len(cm.ParameterMappings))
		for _, pm := range cm.ParameterMappings {
			mapped[pm.Parameter] = true
		}
		for _, name := range want {
			if !mapped[name] {
				errs = append(errs, fmt.Sprintf(
					"call microflow '%s': parameter '%s' is not mapped — Mendix requires every parameter of a workflow call-microflow to be mapped (add `with (%s = ...)`)",
					mfQN, name, name))
			}
		}
	})
	return errs
}
