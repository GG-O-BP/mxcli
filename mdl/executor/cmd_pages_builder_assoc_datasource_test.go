// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/domainmodel"
	"github.com/mendixlabs/mxcli/sdk/pages"
)

// upstream #854 (follow-on): an association DATASOURCE wrote the association
// name into the EntityRefStep exactly as authored. A bare name — the spelling
// attribute paths accept, and the spelling the unresolved-destination guard's
// own error message suggests (`Assoc/Module.Entity`) — therefore reached BSON
// unqualified. Mendix resolves an unqualified AssociationIdentifier to null and
// the loader throws
//
//	ArgumentNullException at EntityRefStep.set_AssociationId
//
// so the .mpr will not open in Studio Pro and `mx check` dies before validating
// anything. Same unopenable-project outcome as the empty DestinationEntity that
// #854 reported; a different property of the same step.
//
// The explicit-destination form is the dangerous one: supplying the destination
// satisfies the guard, so nothing else stood between a bare name and the crash.
func TestAssociationDataSource_QualifiesAssociationName(t *testing.T) {
	const (
		modAID  = model.ID("mod-a")
		modBID  = model.ID("mod-b")
		orderID = model.ID("e-order")
		noteID  = model.ID("e-note")
		lineID  = model.ID("e-line")
	)

	newPB := func() *pageBuilder {
		return &pageBuilder{
			entityContext: "ModA.Order",
			execCache: &executorCache{
				hierarchy: &ContainerHierarchy{moduleNames: map[model.ID]string{
					modAID: "ModA",
					modBID: "ModB",
				}},
				domainModels: []*domainmodel.DomainModel{
					{
						ContainerID: modAID,
						Entities: []*domainmodel.Entity{
							{BaseElement: model.BaseElement{ID: orderID}, Name: "Order"},
							{BaseElement: model.BaseElement{ID: noteID}, Name: "Note"},
						},
						Associations: []*domainmodel.Association{
							{Name: "Order_Note", ParentID: orderID, ChildID: noteID, Type: domainmodel.AssociationTypeReferenceSet},
						},
						CrossAssociations: []*domainmodel.CrossModuleAssociation{
							{Name: "Order_Line", ParentID: orderID, ChildRef: "ModB.Line", Type: domainmodel.AssociationTypeReferenceSet},
						},
					},
					{
						ContainerID: modBID,
						Entities: []*domainmodel.Entity{
							{BaseElement: model.BaseElement{ID: lineID}, Name: "Line"},
						},
					},
				},
			},
		}
	}

	tests := []struct {
		name      string
		reference string
		wantPath  string // EntityPath = "Module.Assoc/Module.DestEntity"
	}{
		{
			name:      "bare same-module association",
			reference: "Order_Note",
			wantPath:  "ModA.Order_Note/ModA.Note",
		},
		{
			name:      "bare cross-module association",
			reference: "Order_Line",
			wantPath:  "ModA.Order_Line/ModB.Line",
		},
		{
			name:      "bare name with explicit destination (the guard's suggested spelling)",
			reference: "Order_Line/ModB.Line",
			wantPath:  "ModA.Order_Line/ModB.Line",
		},
		{
			name:      "already-qualified name is left alone",
			reference: "ModA.Order_Line",
			wantPath:  "ModA.Order_Line/ModB.Line",
		},
		{
			name:      "qualified name with explicit destination",
			reference: "ModA.Order_Line/ModB.Line",
			wantPath:  "ModA.Order_Line/ModB.Line",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ds, childCtx, err := newPB().buildDataSourceV3(&ast.DataSourceV3{
				Type:      "association",
				Reference: tc.reference,
			})
			if err != nil {
				t.Fatalf("buildDataSourceV3(%q) = error %v", tc.reference, err)
			}
			src, ok := ds.(*pages.AssociationSource)
			if !ok {
				t.Fatalf("got %T, want *pages.AssociationSource", ds)
			}
			if src.EntityPath != tc.wantPath {
				t.Errorf("EntityPath = %q, want %q", src.EntityPath, tc.wantPath)
			}
			// The association half must be qualified: an unqualified
			// AssociationIdentifier is what Mendix resolves to null.
			assoc := strings.SplitN(src.EntityPath, "/", 2)[0]
			if !strings.Contains(assoc, ".") {
				t.Errorf("association %q is unqualified — the .mpr will not open", assoc)
			}
			wantCtx := strings.SplitN(tc.wantPath, "/", 2)[1]
			if childCtx != wantCtx {
				t.Errorf("child entity context = %q, want %q", childCtx, wantCtx)
			}
		})
	}
}

// A destination the author supplies explicitly must not be taken on trust: it
// satisfies the empty-DestinationEntity guard, so a misspelled association would
// otherwise be written qualified-but-nonexistent, which Mendix again resolves to
// null. Refuse at author time instead.
func TestAssociationDataSource_RejectsUnknownAssociation(t *testing.T) {
	pb := &pageBuilder{
		entityContext: "ModA.Order",
		execCache: &executorCache{
			hierarchy: &ContainerHierarchy{moduleNames: map[model.ID]string{
				model.ID("mod-a"): "ModA",
			}},
			domainModels: []*domainmodel.DomainModel{{
				ContainerID: model.ID("mod-a"),
				Entities: []*domainmodel.Entity{
					{BaseElement: model.BaseElement{ID: model.ID("e-order")}, Name: "Order"},
				},
			}},
		},
	}

	for _, ref := range []string{"Order_Lnie/ModB.Line", "ModA.Order_Lnie/ModB.Line"} {
		t.Run(ref, func(t *testing.T) {
			_, _, err := pb.buildDataSourceV3(&ast.DataSourceV3{Type: "association", Reference: ref})
			if err == nil {
				t.Fatalf("buildDataSourceV3(%q) succeeded; want a refusal — "+
					"a nonexistent association writes a null AssociationId and the project will not open", ref)
			}
			if !strings.Contains(err.Error(), "Order_Lnie") {
				t.Errorf("error %q does not name the offending association", err)
			}
		})
	}
}
