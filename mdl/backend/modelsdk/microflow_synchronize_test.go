// SPDX-License-Identifier: Apache-2.0

package modelsdkbackend

import (
	"testing"

	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/microflows"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// upstream #863: SynchronizeAction had no reader case, so it rendered as
// "-- Empty action". The enum values are the platform's, taken from the Mendix
// Model SDK's SynchronizationType — NOT Studio Pro's UI wording, which calls
// Specific "Selected object(s)".
func TestActionFromGen_Synchronize(t *testing.T) {
	tests := []struct {
		name      string
		doc       bson.D
		wantType  microflows.SynchronizationType
		wantVars  []string
		wantError microflows.ErrorHandlingType
	}{
		{
			name: "All",
			doc: bson.D{
				{Key: "$Type", Value: "Microflows$SynchronizeAction"},
				{Key: "Type", Value: "All"},
				{Key: "ErrorHandlingType", Value: "Rollback"},
			},
			wantType:  microflows.SynchronizationTypeAll,
			wantError: microflows.ErrorHandlingTypeRollback,
		},
		{
			name: "Unsynchronized",
			doc: bson.D{
				{Key: "$Type", Value: "Microflows$SynchronizeAction"},
				{Key: "Type", Value: "Unsynchronized"},
			},
			wantType: microflows.SynchronizationTypeUnsynchronized,
		},
		{
			name: "Specific reads the VariableNames array",
			doc: bson.D{
				{Key: "$Type", Value: "Microflows$SynchronizeAction"},
				{Key: "Type", Value: "Specific"},
				// Versioned array: element 0 is the marker, not data.
				{Key: "VariableNames", Value: bson.A{int32(1), "Order", "Lines"}},
			},
			wantType: microflows.SynchronizationTypeSpecific,
			wantVars: []string{"Order", "Lines"},
		},
		{
			name: "absent Type means All",
			doc: bson.D{
				{Key: "$Type", Value: "Microflows$SynchronizeAction"},
			},
			wantType: microflows.SynchronizationTypeAll,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := append(bson.D{{Key: "$ID", Value: "sync-1"}}, tc.doc...)
			act := decodeAction(t, doc)
			sync, ok := act.(*microflows.SynchronizeAction)
			if !ok {
				t.Fatalf("actionFromGen → %T, want *microflows.SynchronizeAction "+
					"(nil renders \"-- Empty action\")", act)
			}
			if sync.SyncType != tc.wantType {
				t.Errorf("SyncType = %q, want %q", sync.SyncType, tc.wantType)
			}
			if len(sync.VariableNames) != len(tc.wantVars) {
				t.Fatalf("VariableNames = %v, want %v", sync.VariableNames, tc.wantVars)
			}
			for i, v := range tc.wantVars {
				if sync.VariableNames[i] != v {
					t.Errorf("VariableNames[%d] = %q, want %q", i, sync.VariableNames[i], v)
				}
			}
			if tc.wantError != "" && sync.ErrorHandlingType != tc.wantError {
				t.Errorf("ErrorHandlingType = %q, want %q", sync.ErrorHandlingType, tc.wantError)
			}
		})
	}
}

// The write side, including the detail that cost a build: a Mendix array's first
// element is a version marker, and the reader treats element 0 as the version
// rather than as data. Written without it, a one-element VariableNames read back
// as empty and mxbuild failed CE2004 "No variables to synchronize have been
// selected." — with the variable plainly visible in the BSON dump.
func TestMicroflowRoundTrip_Synchronize(t *testing.T) {
	for _, tc := range []struct {
		name     string
		syncType microflows.SynchronizationType
		vars     []string
	}{
		{"All", microflows.SynchronizationTypeAll, nil},
		{"Unsynchronized", microflows.SynchronizationTypeUnsynchronized, nil},
		{"Specific", microflows.SynchronizationTypeSpecific, []string{"Order", "Lines"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			act := &microflows.SynchronizeAction{
				SyncType:          tc.syncType,
				VariableNames:     tc.vars,
				ErrorHandlingType: microflows.ErrorHandlingTypeRollback,
			}
			act.ID = model.ID("sync-1")
			activity := &microflows.ActionActivity{Action: act}
			activity.ID = model.ID("act-1")

			mf := &microflows.Microflow{
				Name: "NF_Sync",
				ObjectCollection: &microflows.MicroflowObjectCollection{
					Objects: []microflows.MicroflowObject{activity},
				},
			}
			mf.ID = model.ID("mf-1")

			got := roundTripMicroflow(t, mf)

			var found *microflows.SynchronizeAction
			for _, obj := range got.ObjectCollection.Objects {
				aa, ok := obj.(*microflows.ActionActivity)
				if !ok {
					continue
				}
				if aa.Action == nil {
					t.Fatal("ActionActivity round-tripped with a nil Action")
				}
				if s, ok := aa.Action.(*microflows.SynchronizeAction); ok {
					found = s
				}
			}
			if found == nil {
				t.Fatal("no SynchronizeAction survived the round trip")
			}
			if found.SyncType != tc.syncType {
				t.Errorf("SyncType = %q, want %q", found.SyncType, tc.syncType)
			}
			if len(found.VariableNames) != len(tc.vars) {
				t.Fatalf("VariableNames = %v, want %v — a missing array version marker "+
					"reads back empty and mxbuild fails CE2004", found.VariableNames, tc.vars)
			}
			for i, v := range tc.vars {
				if found.VariableNames[i] != v {
					t.Errorf("VariableNames[%d] = %q, want %q", i, found.VariableNames[i], v)
				}
			}
		})
	}
}
