// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/backend/mock"
	"github.com/mendixlabs/mxcli/model"
)

// mxcli-formula1 findings #10.5: DESCRIBE emitted a form it could not parse.
// The project's review checklist asks DESCRIBE to produce re-executable MDL, so
// three separate slips each broke that: a stored mode spelling with no MDL
// equivalent, the entity TYPE's exposed name where the entity SET's belongs
// (silently renaming the set on a re-exec), and fully-qualified member names in
// an `expose (...)` clause that takes bare ones.
func TestDescribeODataService_EmitsReExecutableMDL(t *testing.T) {
	mod := mkModule("F1")
	svc := &model.PublishedODataService{
		BaseElement:  model.BaseElement{ID: nextID("pos")},
		ContainerID:  mod.ID,
		Name:         "LapApi",
		ServiceName:  "LapApi",
		Path:         "odata/f1/",
		Version:      "1.0.0",
		ODataVersion: "OData4",
		Namespace:    "F1.Laps",
		EntityTypes: []*model.PublishedEntityType{{
			Entity: "F1.Lap",
			// The TYPE is exposed singular, the SET plural — Studio Pro's own
			// convention, and the reason printing the wrong one is invisible
			// until someone re-executes the output.
			ExposedName: "Lap",
			Members: []*model.PublishedMember{
				{Kind: "attribute", Name: "F1.Lap.LapKey", ExposedName: "lapKey", IsPartOfKey: true},
				{Kind: "attribute", Name: "F1.Lap.Driver", ExposedName: "driver", Filterable: true},
			},
		}},
		EntitySets: []*model.PublishedEntitySet{{
			ExposedName:    "Laps",
			EntityTypeName: "F1.Lap",
			ReadMode:       "CallMicroflow:F1.Read_Laps",
			InsertMode:     "NotSupported",
			Countable:      boolPtr(false),
		}},
	}
	h := mkHierarchy(mod)
	withContainer(h, svc.ContainerID, mod.ID)

	mb := &mock.MockBackend{
		IsConnectedFunc: func() bool { return true },
		ListPublishedODataServicesFunc: func() ([]*model.PublishedODataService, error) {
			return []*model.PublishedODataService{svc}, nil
		},
	}
	ctx, buf := newMockCtx(t, withBackend(mb), withHierarchy(h))
	assertNoError(t, describeODataService(ctx, ast.QualifiedName{Module: "F1", Name: "LapApi"}))
	out := buf.String()

	for _, want := range []string{
		"publish entity F1.Lap as 'Laps'",  // the SET name, not the TYPE name
		"ReadMode: microflow F1.Read_Laps", // not "CallMicroflow:F1.Read_Laps"
		"LapKey as 'lapKey' (KEY)",         // bare member, documented modifier
		"driver",                           // the second member survives
		"Countable: No",                    // a turned-off option is printed
	} {
		if !strings.Contains(out, want) {
			t.Errorf("describe output should contain %q, got:\n%s", want, out)
		}
	}

	for _, unwanted := range []string{
		"CallMicroflow:", // parses as nothing
		"F1.Lap.LapKey",  // qualified member in an expose clause
		"as 'Lap'",       // the entity type name in the AS position
		"IsPartOfKey",    // parses, but KEY is the documented spelling
		"SkipSupported",  // unset options stay unprinted
		"TopSupported",
	} {
		if strings.Contains(out, unwanted) {
			t.Errorf("describe output should not contain %q, got:\n%s", unwanted, out)
		}
	}
}

func TestODataModeToMDL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"CallMicroflow:M.Read", "microflow M.Read"},
		{"MICROFLOW M.Read", "microflow M.Read"},
		{"microflow M.Read", "microflow M.Read"},
		// Everything else is already an MDL spelling and must pass through.
		{"source", "source"},
		{"NotSupported", "NotSupported"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := odataModeToMDL(tt.in); got != tt.want {
			t.Errorf("odataModeToMDL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBareMemberName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"F1.Lap.LapKey", "LapKey"},
		{"Lap.LapKey", "LapKey"},
		{"LapKey", "LapKey"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := bareMemberName(tt.in); got != tt.want {
			t.Errorf("bareMemberName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
