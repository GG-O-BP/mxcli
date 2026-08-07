// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"testing"

	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/domainmodel"
)

// mxcli-todo findings #12: Mendix stores a page's attribute reference against
// the entity that DECLARES the attribute. mxcli qualified it with the entity in
// context, so a binding to an inherited attribute produced a dangling reference
// and mxbuild failed with
//
//	[CE1613] "The selected attribute 'TaskBoard.Person.FullName' no longer exists."
//
// Both `mxcli check` (including --references) and `mxcli lint` passed.
//
// The fixture mirrors the reporter's model: Person extends Administration.Account
// (which declares FullName/Email) and adds IsAvailable of its own; Task points at
// Person through an association.
func inheritancePB(entityContext string) *pageBuilder {
	const (
		appID     = model.ID("mod-app")
		adminID   = model.ID("mod-admin")
		personID  = model.ID("e-person")
		taskID    = model.ID("e-task")
		accountID = model.ID("e-account")
	)
	return &pageBuilder{
		entityContext: entityContext,
		execCache: &executorCache{
			hierarchy: &ContainerHierarchy{moduleNames: map[model.ID]string{
				appID:   "TaskBoard",
				adminID: "Administration",
			}},
			domainModels: []*domainmodel.DomainModel{
				{
					ContainerID: appID,
					Entities: []*domainmodel.Entity{
						{
							BaseElement:       model.BaseElement{ID: personID},
							Name:              "Person",
							GeneralizationRef: "Administration.Account",
							Attributes: []*domainmodel.Attribute{
								{Name: "IsAvailable"},
							},
						},
						{
							BaseElement: model.BaseElement{ID: taskID},
							Name:        "Task",
							Attributes:  []*domainmodel.Attribute{{Name: "Title"}},
						},
					},
					CrossAssociations: []*domainmodel.CrossModuleAssociation{
						{Name: "Task_Assignee", ParentID: taskID, ChildRef: "TaskBoard.Person", Type: domainmodel.AssociationTypeReference},
					},
					Associations: []*domainmodel.Association{
						{Name: "Task_Assignee", ParentID: taskID, ChildID: personID, Type: domainmodel.AssociationTypeReference},
					},
				},
				{
					ContainerID: adminID,
					Entities: []*domainmodel.Entity{
						{
							BaseElement: model.BaseElement{ID: accountID},
							Name:        "Account",
							Attributes: []*domainmodel.Attribute{
								{Name: "FullName"},
								{Name: "Email"},
							},
						},
					},
				},
			},
		},
	}
}

func TestResolveAttributePath_InheritedAttribute(t *testing.T) {
	pb := inheritancePB("TaskBoard.Person")

	// Inherited: must be qualified with the declaring entity, not the context.
	if got := pb.resolveAttributePath("FullName"); got != "Administration.Account.FullName" {
		t.Errorf("inherited attribute: got %q, want Administration.Account.FullName", got)
	}
	// Own: unchanged.
	if got := pb.resolveAttributePath("IsAvailable"); got != "TaskBoard.Person.IsAvailable" {
		t.Errorf("own attribute: got %q, want TaskBoard.Person.IsAvailable", got)
	}
	// Unknown name: no invented qualification, today's behaviour is kept.
	if got := pb.resolveAttributePath("Nonexistent"); got != "TaskBoard.Person.Nonexistent" {
		t.Errorf("unknown attribute: got %q, want the context qualification", got)
	}
	// Already qualified: untouched.
	if got := pb.resolveAttributePath("Other.Entity.Attr"); got != "Other.Entity.Attr" {
		t.Errorf("qualified attribute was rewritten: %q", got)
	}
}

// The same rule applies to the final attribute of an association path — the
// reporter's table had this failing too, and it goes through a different
// resolver.
func TestResolveAssociationAttributePath_InheritedFinalAttribute(t *testing.T) {
	pb := inheritancePB("TaskBoard.Task")

	finalQN, steps, ok := pb.resolveAssociationAttributePath("Task_Assignee/FullName")
	if !ok {
		t.Fatal("expected the association path to resolve")
	}
	if finalQN != "Administration.Account.FullName" {
		t.Errorf("inherited final attribute: got %q, want Administration.Account.FullName", finalQN)
	}
	if len(steps) != 1 || steps[0].DestinationEntity != "TaskBoard.Person" {
		t.Errorf("steps = %+v, want one hop to TaskBoard.Person", steps)
	}

	// An own attribute on the same destination keeps the destination entity.
	finalQN, _, ok = pb.resolveAssociationAttributePath("Task_Assignee/IsAvailable")
	if !ok || finalQN != "TaskBoard.Person.IsAvailable" {
		t.Errorf("own final attribute: got %q (ok=%v), want TaskBoard.Person.IsAvailable", finalQN, ok)
	}
}

func TestDeclaringEntityFor(t *testing.T) {
	pb := inheritancePB("TaskBoard.Person")

	tests := []struct {
		entity, attr string
		want         string
		wantOK       bool
	}{
		{"TaskBoard.Person", "IsAvailable", "TaskBoard.Person", true},
		{"TaskBoard.Person", "FullName", "Administration.Account", true},
		{"TaskBoard.Person", "Email", "Administration.Account", true},
		// Case-insensitive, since MDL identifiers are matched that way elsewhere.
		{"TaskBoard.Person", "fullname", "Administration.Account", true},
		{"TaskBoard.Person", "Missing", "", false},
		{"Unknown.Entity", "Attr", "", false},
		{"", "Attr", "", false},
		{"TaskBoard.Person", "", "", false},
	}
	for _, tt := range tests {
		got, ok := pb.declaringEntityFor(tt.entity, tt.attr)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("declaringEntityFor(%q, %q) = (%q, %v), want (%q, %v)",
				tt.entity, tt.attr, got, ok, tt.want, tt.wantOK)
		}
	}
}

// A generalization cycle must not hang the walk. Mendix cannot express one, but
// a corrupt or partially-written model could.
func TestDeclaringEntityFor_CycleTerminates(t *testing.T) {
	const modID = model.ID("mod")
	pb := &pageBuilder{
		execCache: &executorCache{
			hierarchy: &ContainerHierarchy{moduleNames: map[model.ID]string{modID: "M"}},
			domainModels: []*domainmodel.DomainModel{{
				ContainerID: modID,
				Entities: []*domainmodel.Entity{
					{Name: "A", GeneralizationRef: "M.B"},
					{Name: "B", GeneralizationRef: "M.A"},
				},
			}},
		},
	}
	if _, ok := pb.declaringEntityFor("M.A", "Whatever"); ok {
		t.Error("expected no declaring entity for a cyclic chain")
	}
}
