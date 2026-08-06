// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"testing"

	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/domainmodel"
)

// issuetracker #19: an association whose target lives in ANOTHER module is a
// DomainModels$CrossAssociation and is stored in a separate list, where only the
// local (FROM) end is BY_ID and the remote end is the BY_NAME ChildRef.
// associationEndpoints searched only dm.Associations, so every cross-module hop
// came back unresolvable; a widget bound to `Issue_Assignee/Name` then fell back
// to a flat attribute path and mxbuild failed CE1613 "The selected attribute
// 'IT.Issue.Issue_Assignee/Name' no longer exists".
//
// The reporter framed this as a System-module limitation, but a plain second app
// module reproduces it identically — the trigger is cross-module, not System.
func TestResolveAssociationAttributePath_CrossModule(t *testing.T) {
	const (
		modID    = model.ID("mod-it")
		otherID  = model.ID("mod-other")
		issueID  = model.ID("e-issue")
		projID   = model.ID("e-project")
		personID = model.ID("e-person")
	)

	newPB := func() *pageBuilder {
		return &pageBuilder{
			entityContext: "IT.Issue",
			execCache: &executorCache{
				hierarchy: &ContainerHierarchy{moduleNames: map[model.ID]string{
					modID:   "IT",
					otherID: "Other",
				}},
				domainModels: []*domainmodel.DomainModel{
					{
						ContainerID: modID,
						Entities: []*domainmodel.Entity{
							{BaseElement: model.BaseElement{ID: issueID}, Name: "Issue"},
							{BaseElement: model.BaseElement{ID: projID}, Name: "Project"},
						},
						Associations: []*domainmodel.Association{
							{Name: "Issue_Project", ParentID: issueID, ChildID: projID, Type: domainmodel.AssociationTypeReference},
						},
						CrossAssociations: []*domainmodel.CrossModuleAssociation{
							// Target in another app module.
							{Name: "Issue_Person", ParentID: issueID, ChildRef: "Other.Person", Type: domainmodel.AssociationTypeReference},
							// Target in the platform's System module.
							{Name: "Issue_Assignee", ParentID: issueID, ChildRef: "System.User", Type: domainmodel.AssociationTypeReference},
						},
					},
					{
						ContainerID: otherID,
						Entities: []*domainmodel.Entity{
							{BaseElement: model.BaseElement{ID: personID}, Name: "Person"},
						},
					},
				},
			},
		}
	}

	tests := []struct {
		name       string
		path       string
		wantFinal  string
		wantAssoc  string
		wantDestEn string
	}{
		{
			name:       "same-module hop (regression guard)",
			path:       "Issue_Project/Code",
			wantFinal:  "IT.Project.Code",
			wantAssoc:  "IT.Issue_Project",
			wantDestEn: "IT.Project",
		},
		{
			name:       "cross-module hop into another app module",
			path:       "Issue_Person/FullName",
			wantFinal:  "Other.Person.FullName",
			wantAssoc:  "IT.Issue_Person",
			wantDestEn: "Other.Person",
		},
		{
			name:       "cross-module hop into System",
			path:       "Issue_Assignee/Name",
			wantFinal:  "System.User.Name",
			wantAssoc:  "IT.Issue_Assignee",
			wantDestEn: "System.User",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			finalQN, steps, ok := newPB().resolveAssociationAttributePath(tc.path)
			if !ok {
				t.Fatalf("path %q was dropped (ok=false) — the binding falls back to a flat path and mxbuild fails CE1613", tc.path)
			}
			if finalQN != tc.wantFinal {
				t.Errorf("finalQN = %q, want %q", finalQN, tc.wantFinal)
			}
			if len(steps) != 1 {
				t.Fatalf("steps = %+v, want exactly one hop", steps)
			}
			if steps[0].Association != tc.wantAssoc || steps[0].DestinationEntity != tc.wantDestEn {
				t.Errorf("step = %+v, want {Association: %s, DestinationEntity: %s}",
					steps[0], tc.wantAssoc, tc.wantDestEn)
			}
		})
	}

	// The destination resolver used by DATASOURCE bindings reads the same two
	// lists and must agree with the step resolver above.
	t.Run("datasource destination resolves cross-module", func(t *testing.T) {
		pb := newPB()
		if got := pb.resolveAssociationDestination("IT.Issue_Assignee", "IT.Issue"); got != "System.User" {
			t.Errorf("resolveAssociationDestination = %q, want System.User", got)
		}
		if got := pb.resolveAssociationDestination("IT.Issue_Person", "IT.Issue"); got != "Other.Person" {
			t.Errorf("resolveAssociationDestination = %q, want Other.Person", got)
		}
	})
}

// issuetracker #19 (related quirk): `add attribute CreatedDate: AutoCreatedDate`
// is the spelling mxcli REQUIRES — it rejects any other declared name and tells
// you to use this one — but the member is stored as `createdDate`. Binding a
// widget to the name you just declared failed CE1613 while the undocumented
// lowercase form worked.
func TestStoredSystemMemberName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"CreatedDate", "createdDate"},
		{"createdDate", "createdDate"},
		{"CREATEDDATE", "createdDate"},
		{"ChangedDate", "changedDate"},
		{"ChangedBy", "changedBy"},
		{"Owner", "owner"},
		// Ordinary attributes are untouched — including one that merely contains
		// an audit-member name.
		{"Title", "Title"},
		{"CreatedDateLocal", "CreatedDateLocal"},
		// Already-qualified paths and association paths are left to their own
		// resolvers; rewriting a segment here would corrupt them.
		{"IT.Issue.CreatedDate", "IT.Issue.CreatedDate"},
		{"Issue_Assignee/CreatedDate", "Issue_Assignee/CreatedDate"},
		{"$currentObject/CreatedDate", "$currentObject/CreatedDate"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := storedSystemMemberName(tc.in); got != tc.want {
			t.Errorf("storedSystemMemberName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
