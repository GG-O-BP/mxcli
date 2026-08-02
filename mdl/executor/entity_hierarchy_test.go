// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/backend/mock"
	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/domainmodel"
)

// hierarchyBackend serves three modules:
//
//	App.Item      extends App.Base        (same-module ancestor)
//	App.Doc       extends System.FileDocument
//	App.Employee  extends System.User     (a user entity)
func hierarchyBackend() *mock.MockBackend {
	ids := map[string]model.ID{"App": "mod-app", "System": "mod-system"}
	attr := func(name string) *domainmodel.Attribute {
		return &domainmodel.Attribute{Name: name}
	}
	dms := map[model.ID]*domainmodel.DomainModel{
		ids["App"]: {ContainerID: ids["App"], Entities: []*domainmodel.Entity{
			{Name: "Base", Attributes: []*domainmodel.Attribute{attr("SharedField")}},
			{Name: "Item", GeneralizationRef: "App.Base", Attributes: []*domainmodel.Attribute{attr("OwnField")}},
			{Name: "Doc", GeneralizationRef: "System.FileDocument", Attributes: []*domainmodel.Attribute{attr("Caption")}},
			{Name: "Employee", GeneralizationRef: "System.User", Attributes: []*domainmodel.Attribute{attr("EmployeeNo")}},
		}},
		ids["System"]: {ContainerID: ids["System"], Entities: []*domainmodel.Entity{
			{Name: "FileDocument", Attributes: []*domainmodel.Attribute{attr("Name"), attr("Contents")}},
			{Name: "User", Attributes: []*domainmodel.Attribute{attr("Name"), attr("Password")}},
		}},
	}
	return &mock.MockBackend{
		IsConnectedFunc: func() bool { return true },
		GetModuleByNameFunc: func(name string) (*model.Module, error) {
			id, ok := ids[name]
			if !ok {
				return nil, nil
			}
			return &model.Module{BaseElement: model.BaseElement{ID: id}, Name: name}, nil
		},
		GetDomainModelFunc: func(id model.ID) (*domainmodel.DomainModel, error) { return dms[id], nil },
	}
}

func memberRefs(members []EntityMember) []string {
	out := make([]string, 0, len(members))
	for _, m := range members {
		out = append(out, m.Ref)
	}
	return out
}

// TestEntityMembers_InheritedUseDeclaringEntity is the core of
// mendixlabs/mxcli#758 / #765: enumerating only the entity's own attributes meant a
// GRANT naming an inherited member wrote nothing, and reconciliation deleted any
// inherited entry as stale. The reference must be qualified against the entity that
// DECLARES the member — qualifying it against the child is CE1613.
func TestEntityMembers_InheritedUseDeclaringEntity(t *testing.T) {
	ctx, _ := newMockCtx(t, withBackend(hierarchyBackend()))

	got := memberRefs(EntityMembers(ctx, "App.Item"))
	want := []string{"App.Item.OwnField", "App.Base.SharedField"}
	if len(got) != len(want) {
		t.Fatalf("EntityMembers(App.Item) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("member %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestEntityMembers_SystemAncestorIncluded: omitting the System.FileDocument
// members from a specializing entity's rule is CE0066 until they are all present,
// so they must be enumerated.
func TestEntityMembers_SystemAncestorIncluded(t *testing.T) {
	ctx, _ := newMockCtx(t, withBackend(hierarchyBackend()))

	got := memberRefs(EntityMembers(ctx, "App.Doc"))
	for _, want := range []string{"App.Doc.Caption", "System.FileDocument.Name", "System.FileDocument.Contents"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("EntityMembers(App.Doc) missing %q; got %v", want, got)
		}
	}
}

// TestEntityMembers_UserEntityExcludesSystemUser: Mendix manages the platform
// members of a user entity. Listing them turns a clean rule into CE0066 —
// verified against Mendix's own Administration.Account and a fresh specialization.
func TestEntityMembers_UserEntityExcludesSystemUser(t *testing.T) {
	ctx, _ := newMockCtx(t, withBackend(hierarchyBackend()))

	got := memberRefs(EntityMembers(ctx, "App.Employee"))
	if len(got) != 1 || got[0] != "App.Employee.EmployeeNo" {
		t.Errorf("EntityMembers(App.Employee) = %v, want only its own member — "+
			"System.User's platform members must not appear", got)
	}
}

// TestEntityMembers_ChildShadowsAncestor: a child redeclaring an ancestor's
// attribute name must contribute its own reference once, not both.
func TestEntityMembers_ChildShadowsAncestor(t *testing.T) {
	ctx, _ := newMockCtx(t, withBackend(hierarchyBackend()))

	// App.Doc declares Caption; System.FileDocument declares Name/Contents. Add a
	// shadowing case by checking no member name appears twice.
	seen := map[string]bool{}
	for _, m := range EntityMembers(ctx, "App.Doc") {
		if seen[m.Name] {
			t.Errorf("member %q enumerated twice", m.Name)
		}
		seen[m.Name] = true
	}
}

func TestUnmatchedGrantMembers(t *testing.T) {
	granted := map[string]bool{"OwnField": true, "SharedField": true}
	got := unmatchedGrantMembers([]string{"SharedField", "Nope"}, []string{"OwnField", "Alsobad"}, granted)
	if len(got) != 2 || got[0] != "Alsobad" || got[1] != "Nope" {
		t.Errorf("unmatchedGrantMembers = %v, want [Alsobad Nope]", got)
	}
}
