// SPDX-License-Identifier: Apache-2.0

package modelsdkbackend

import (
	"testing"

	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/microflows"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// EXECUTE DATABASE QUERY is authorable in MDL and has a DESCRIBE formatter, but
// the modelsdk reader — the default engine — had no case for it, so it read back
// with a nil Action and described as "-- Empty action". Write-only: you could
// author the activity and never get it back (#863 follow-up).
//
// Note the storage $Type is DatabaseConnector$ExecuteDatabaseQueryAction, not
// Microflows$ — the action lives in its own sub-metamodel.
func TestActionFromGen_ExecuteDatabaseQuery(t *testing.T) {
	act := decodeAction(t, bson.D{
		{Key: "$ID", Value: "q-1"},
		{Key: "$Type", Value: "DatabaseConnector$ExecuteDatabaseQueryAction"},
		{Key: "ErrorHandlingType", Value: "Custom"},
		{Key: "OutputVariableName", Value: "Rows"},
		{Key: "Query", Value: "Mod.Conn.GetOrders"},
		{Key: "DynamicQuery", Value: ""},
		// Versioned arrays: element 0 is the marker, not a mapping.
		{Key: "ParameterMappings", Value: bson.A{int32(2),
			bson.D{
				{Key: "$Type", Value: "DatabaseConnector$QueryParameterMapping"},
				{Key: "ParameterName", Value: "Status"},
				{Key: "Value", Value: "$Status"},
			},
		}},
		{Key: "ConnectionParameterMappings", Value: bson.A{int32(2),
			bson.D{
				{Key: "$Type", Value: "DatabaseConnector$ConnectionParameterMapping"},
				{Key: "ParameterName", Value: "Url"},
				{Key: "Value", Value: "$Url"},
			},
		}},
	})

	q, ok := act.(*microflows.ExecuteDatabaseQueryAction)
	if !ok {
		t.Fatalf("actionFromGen → %T, want *microflows.ExecuteDatabaseQueryAction "+
			"(nil renders \"-- Empty action\")", act)
	}
	if q.OutputVariableName != "Rows" {
		t.Errorf("OutputVariableName = %q, want Rows", q.OutputVariableName)
	}
	if q.Query != "Mod.Conn.GetOrders" {
		t.Errorf("Query = %q, want Mod.Conn.GetOrders", q.Query)
	}
	if q.ErrorHandlingType != microflows.ErrorHandlingTypeCustom {
		t.Errorf("ErrorHandlingType = %q, want Custom", q.ErrorHandlingType)
	}
	if len(q.ParameterMappings) != 1 {
		t.Fatalf("ParameterMappings = %+v, want exactly 1 — the array's leading "+
			"version marker must not be read as a mapping, nor swallow the real one", q.ParameterMappings)
	}
	if q.ParameterMappings[0].ParameterName != "Status" || q.ParameterMappings[0].Value != "$Status" {
		t.Errorf("ParameterMappings[0] = %+v, want {Status, $Status}", q.ParameterMappings[0])
	}
	if len(q.ConnectionParameterMappings) != 1 {
		t.Fatalf("ConnectionParameterMappings = %+v, want exactly 1", q.ConnectionParameterMappings)
	}
	if q.ConnectionParameterMappings[0].ParameterName != "Url" {
		t.Errorf("ConnectionParameterMappings[0] = %+v, want ParameterName Url", q.ConnectionParameterMappings[0])
	}
}

// A dynamic (raw SQL) query overrides the named one; both keys are read so a
// DESCRIBE can tell the two forms apart.
func TestActionFromGen_ExecuteDatabaseQuery_Dynamic(t *testing.T) {
	act := decodeAction(t, bson.D{
		{Key: "$ID", Value: "q-2"},
		{Key: "$Type", Value: "DatabaseConnector$ExecuteDatabaseQueryAction"},
		{Key: "Query", Value: "Mod.Conn.Base"},
		{Key: "DynamicQuery", Value: "'select * from orders'"},
		{Key: "OutputVariableName", Value: "Rows"},
	})
	q, ok := act.(*microflows.ExecuteDatabaseQueryAction)
	if !ok {
		t.Fatalf("actionFromGen → %T, want *microflows.ExecuteDatabaseQueryAction", act)
	}
	if q.DynamicQuery != "'select * from orders'" {
		t.Errorf("DynamicQuery = %q, want the raw SQL expression", q.DynamicQuery)
	}
	if q.Query != "Mod.Conn.Base" {
		t.Errorf("Query = %q, want Mod.Conn.Base", q.Query)
	}
}

// Round trip through the writer the reader must mirror. The writer is the
// authority on the storage keys here (it builds the element directly rather than
// through the gen setters), so reader and writer are pinned against each other
// rather than against a hand-written guess at the shape.
func TestMicroflowRoundTrip_ExecuteDatabaseQuery(t *testing.T) {
	act := &microflows.ExecuteDatabaseQueryAction{
		ErrorHandlingType:  microflows.ErrorHandlingTypeRollback,
		OutputVariableName: "Rows",
		Query:              "Mod.Conn.GetOrders",
		ParameterMappings: []*microflows.DatabaseQueryParameterMapping{
			{ParameterName: "Status", Value: "$Status"},
		},
		ConnectionParameterMappings: []*microflows.DatabaseConnectionParameterMapping{
			{ParameterName: "Url", Value: "$Url"},
		},
	}
	act.ID = model.ID("q-1")
	activity := &microflows.ActionActivity{Action: act}
	activity.ID = model.ID("act-1")

	mf := &microflows.Microflow{
		Name: "ACT_Query",
		ObjectCollection: &microflows.MicroflowObjectCollection{
			Objects: []microflows.MicroflowObject{activity},
		},
	}
	mf.ID = model.ID("mf-1")

	got := roundTripMicroflow(t, mf)

	var found *microflows.ExecuteDatabaseQueryAction
	for _, obj := range got.ObjectCollection.Objects {
		aa, ok := obj.(*microflows.ActionActivity)
		if !ok {
			continue
		}
		if aa.Action == nil {
			t.Fatal("ActionActivity round-tripped with a nil Action")
		}
		if q, ok := aa.Action.(*microflows.ExecuteDatabaseQueryAction); ok {
			found = q
		}
	}
	if found == nil {
		t.Fatal("no ExecuteDatabaseQueryAction survived the round trip")
	}
	if found.Query != "Mod.Conn.GetOrders" || found.OutputVariableName != "Rows" {
		t.Errorf("got Query=%q Output=%q, want Mod.Conn.GetOrders / Rows", found.Query, found.OutputVariableName)
	}
	if len(found.ParameterMappings) != 1 || found.ParameterMappings[0].ParameterName != "Status" {
		t.Errorf("ParameterMappings = %+v, want one {Status, $Status}", found.ParameterMappings)
	}
	if len(found.ConnectionParameterMappings) != 1 || found.ConnectionParameterMappings[0].ParameterName != "Url" {
		t.Errorf("ConnectionParameterMappings = %+v, want one {Url, $Url}", found.ConnectionParameterMappings)
	}
}
