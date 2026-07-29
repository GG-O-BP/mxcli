// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/sdk/microflows"
)

// containsActionKind builds a flow graph for a single `contains` list-operation
// statement and reports which activity the builder emitted for it.
func containsActionKind(t *testing.T, declaredVars, varTypes map[string]string, s *ast.ListOperationStmt) (isChangeVar, isListOp bool) {
	t.Helper()
	fb := &flowBuilder{
		posX:         100,
		posY:         100,
		spacing:      HorizontalSpacing,
		declaredVars: declaredVars,
		varTypes:     varTypes,
		measurer:     &layoutMeasurer{varTypes: varTypes},
	}
	oc := fb.buildFlowGraph([]ast.MicroflowStatement{s}, &ast.MicroflowReturnType{Type: ast.DataType{Kind: ast.TypeBoolean}})
	if errs := fb.GetErrors(); len(errs) > 0 {
		t.Fatalf("unexpected build errors: %v", errs)
	}
	for _, obj := range oc.Objects {
		act, ok := obj.(*microflows.ActionActivity)
		if !ok {
			continue
		}
		switch act.Action.(type) {
		case *microflows.ChangeVariableAction:
			isChangeVar = true
		case *microflows.ListOperationAction:
			isListOp = true
		}
	}
	return isChangeVar, isListOp
}

// TestBuildContains_StringVsList covers ledger finding #53 at the flow-builder
// layer. When both `contains` arguments are plain variables the visitor cannot
// tell a string contains from a list contains, so it emits a ListOperationStmt.
// The builder disambiguates: a String-typed input becomes a Change Variable
// action carrying the `contains(...)` expression (a List operation activity on
// strings fails the Mendix build with CE0023/CE0097/CE0111), while a list-typed
// input stays a List operation activity.
func TestBuildContains_StringVsList(t *testing.T) {
	t.Run("string input becomes change variable", func(t *testing.T) {
		isChangeVar, isListOp := containsActionKind(t,
			map[string]string{"Hay": "String", "Needle": "String", "Match": "Boolean"},
			map[string]string{},
			&ast.ListOperationStmt{
				Operation:      ast.ListOpContains,
				InputVariable:  "Hay",
				SecondVariable: "Needle",
				OutputVariable: "Match",
			})
		if !isChangeVar {
			t.Error("string contains must emit a Change Variable action")
		}
		if isListOp {
			t.Error("string contains must NOT emit a List operation activity")
		}
	})

	t.Run("list input stays a list operation", func(t *testing.T) {
		isChangeVar, isListOp := containsActionKind(t,
			map[string]string{},
			map[string]string{"Items": "List of M.Item", "One": "M.Item"},
			&ast.ListOperationStmt{
				Operation:      ast.ListOpContains,
				InputVariable:  "Items",
				SecondVariable: "One",
				OutputVariable: "Found",
			})
		if isChangeVar {
			t.Error("list contains must NOT emit a Change Variable action")
		}
		if !isListOp {
			t.Error("list contains must emit a List operation activity")
		}
	})
}
