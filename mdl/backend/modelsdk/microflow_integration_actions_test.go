// SPDX-License-Identifier: Apache-2.0

package modelsdkbackend

import (
	"testing"

	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/microflows"
)

// The last three authorable-but-unreadable action types. Two were describe-only
// gaps; TransformJsonAction was worse — the modelsdk writer had no case for it
// either, so the action fell through to `default: return nil` and the enclosing
// ActionActivity was written with NO action at all. `mxcli exec` reported
// success and mxbuild then failed CE0008 "No action defined." — the #850 shape.
// `transform` was simply unusable on the default engine while the legacy writer
// handled it fine.
//
// Round trips rather than reader-only tests: the writers build these elements
// directly with explicit keys, and in two of the three a key diverges from what
// gen would suggest (`VariableName` for an external action's result;
// `QueryParameter` vs `Parameter` for the two REST mapping lists). A reader
// written against hand-authored BSON would assert against my guess instead of
// the writer's actual output.
func TestMicroflowRoundTrip_IntegrationActions(t *testing.T) {
	tests := []struct {
		name   string
		action microflows.MicroflowAction
		verify func(t *testing.T, got microflows.MicroflowAction)
	}{
		{
			name: "TransformJsonAction",
			action: &microflows.TransformJsonAction{
				ErrorHandlingType:  microflows.ErrorHandlingTypeRollback,
				InputVariableName:  "RawJson",
				OutputVariableName: "Shaped",
				Transformation:     "T.MyTransform",
			},
			verify: func(t *testing.T, got microflows.MicroflowAction) {
				a, ok := got.(*microflows.TransformJsonAction)
				if !ok {
					t.Fatalf("got %T, want *microflows.TransformJsonAction", got)
				}
				if a.InputVariableName != "RawJson" || a.OutputVariableName != "Shaped" {
					t.Errorf("variables = {%q, %q}, want {RawJson, Shaped}", a.InputVariableName, a.OutputVariableName)
				}
				if a.Transformation != "T.MyTransform" {
					t.Errorf("Transformation = %q, want T.MyTransform", a.Transformation)
				}
			},
		},
		{
			name: "CallExternalAction",
			action: &microflows.CallExternalAction{
				ErrorHandlingType:    microflows.ErrorHandlingTypeRollback,
				ConsumedODataService: "X.OrdersService",
				Name:                 "PlaceOrder",
				ResultVariableName:   "Result",
				UseReturnVariable:    true,
				ParameterMappings: []*microflows.ExternalActionParameterMapping{
					{ParameterName: "orderId", Argument: "$Id", CanBeEmpty: true},
				},
			},
			verify: func(t *testing.T, got microflows.MicroflowAction) {
				a, ok := got.(*microflows.CallExternalAction)
				if !ok {
					t.Fatalf("got %T, want *microflows.CallExternalAction", got)
				}
				// The result variable is stored under `VariableName`, not
				// `ResultVariableName` — reading the model's own field name loses it.
				if a.ResultVariableName != "Result" {
					t.Errorf("ResultVariableName = %q, want Result (stored under the key VariableName)", a.ResultVariableName)
				}
				if a.ConsumedODataService != "X.OrdersService" || a.Name != "PlaceOrder" {
					t.Errorf("got service=%q name=%q, want X.OrdersService/PlaceOrder", a.ConsumedODataService, a.Name)
				}
				if len(a.ParameterMappings) != 1 {
					t.Fatalf("ParameterMappings = %+v, want exactly one", a.ParameterMappings)
				}
				pm := a.ParameterMappings[0]
				if pm.ParameterName != "orderId" || pm.Argument != "$Id" || !pm.CanBeEmpty {
					t.Errorf("mapping = %+v, want {orderId, $Id, CanBeEmpty}", pm)
				}
			},
		},
		{
			name: "RestOperationCallAction",
			action: &microflows.RestOperationCallAction{
				Operation:      "X.Client.GetOrder",
				OutputVariable: &microflows.RestOutputVar{VariableName: "Response"},
				BodyVariable:   &microflows.RestBodyVar{VariableName: "Payload"},
				ParameterMappings: []*microflows.RestParameterMapping{
					{Parameter: "X.Client.GetOrder.id", Value: "$Id"},
				},
				QueryParameterMappings: []*microflows.RestQueryParameterMapping{
					{Parameter: "X.Client.GetOrder.expand", Value: "'lines'", Included: "Always"},
				},
			},
			verify: func(t *testing.T, got microflows.MicroflowAction) {
				a, ok := got.(*microflows.RestOperationCallAction)
				if !ok {
					t.Fatalf("got %T, want *microflows.RestOperationCallAction", got)
				}
				if a.Operation != "X.Client.GetOrder" {
					t.Errorf("Operation = %q, want X.Client.GetOrder", a.Operation)
				}
				// Output and body are single-child documents, not scalars.
				if a.OutputVariable == nil || a.OutputVariable.VariableName != "Response" {
					t.Errorf("OutputVariable = %+v, want VariableName Response", a.OutputVariable)
				}
				if a.BodyVariable == nil || a.BodyVariable.VariableName != "Payload" {
					t.Errorf("BodyVariable = %+v, want VariableName Payload", a.BodyVariable)
				}
				if len(a.ParameterMappings) != 1 || a.ParameterMappings[0].Parameter != "X.Client.GetOrder.id" {
					t.Errorf("ParameterMappings = %+v, want the path parameter", a.ParameterMappings)
				}
				// The query list stores its name under `QueryParameter`, not
				// `Parameter` — the two lists are NOT symmetric.
				if len(a.QueryParameterMappings) != 1 {
					t.Fatalf("QueryParameterMappings = %+v, want exactly one", a.QueryParameterMappings)
				}
				qm := a.QueryParameterMappings[0]
				if qm.Parameter != "X.Client.GetOrder.expand" || qm.Included != "Always" {
					t.Errorf("query mapping = %+v, want {…expand, Always} (key is QueryParameter, not Parameter)", qm)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			activity := &microflows.ActionActivity{Action: tc.action}
			activity.ID = model.ID("act-1")
			mf := &microflows.Microflow{
				Name: "ACT_Integration",
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
						"the CE0008 \"No action defined.\" shape")
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
