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

func attrTypeFor(kind string) domainmodel.AttributeType {
	if kind == "Boolean" {
		return &domainmodel.BooleanAttributeType{}
	}
	return &domainmodel.StringAttributeType{}
}

// typedHierarchyBackend adds attribute types and an inherited Boolean, for the
// type-resolution tests.
func typedHierarchyBackend() *mock.MockBackend {
	ids := map[string]model.ID{"App": "mod-app", "System": "mod-system"}
	typed := func(name, kind string) *domainmodel.Attribute {
		return &domainmodel.Attribute{Name: name, Type: attrTypeFor(kind)}
	}
	dms := map[model.ID]*domainmodel.DomainModel{
		ids["App"]: {ContainerID: ids["App"], Entities: []*domainmodel.Entity{
			{Name: "Base", Attributes: []*domainmodel.Attribute{
				typed("SharedField", "String"), typed("Flag", "Boolean")}},
			{Name: "Item", GeneralizationRef: "App.Base", Attributes: []*domainmodel.Attribute{
				typed("OwnField", "String")}},
			{Name: "Doc", GeneralizationRef: "System.FileDocument", Attributes: []*domainmodel.Attribute{
				typed("Category", "String")}},
		}},
		ids["System"]: {ContainerID: ids["System"], Entities: []*domainmodel.Entity{
			{Name: "FileDocument", Attributes: []*domainmodel.Attribute{typed("Name", "String")}},
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

// TestResolveMemberRef_DeclaringEntity is the mapping half of the #765 umbrella
// (mendixlabs/mxcli#703): a mapping element bound to an inherited attribute was
// qualified against the entity being mapped, which is CE1613 "The selected
// attribute no longer exists" and leaves the field unmapped in Studio Pro.
func TestResolveMemberRef_DeclaringEntity(t *testing.T) {
	b := typedHierarchyBackend()

	tests := []struct {
		entity, member, want string
		ok                   bool
	}{
		{"App.Item", "OwnField", "App.Item.OwnField", true},
		{"App.Item", "SharedField", "App.Base.SharedField", true}, // inherited
		{"App.Doc", "Name", "System.FileDocument.Name", true},     // inherited from System
		{"App.Item", "Nonexistent", "", false},
	}
	for _, tc := range tests {
		got, ok := ResolveMemberRef(b, tc.entity, tc.member)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ResolveMemberRef(%s, %s) = (%q, %v), want (%q, %v)",
				tc.entity, tc.member, got, ok, tc.want, tc.ok)
		}
	}
}

// TestResolveMemberType_FollowsChain: an inherited attribute's type was not found
// on the entity itself, so the mapping element defaulted to String — giving an
// inherited Boolean or DateTime the wrong DataType (#703).
func TestResolveMemberType_FollowsChain(t *testing.T) {
	b := typedHierarchyBackend()

	if got := ResolveMemberType(b, "App.Item", "OwnField"); got != "String" {
		t.Errorf("own attribute type = %q, want String", got)
	}
	if got := ResolveMemberType(b, "App.Item", "Flag"); got != "Boolean" {
		t.Errorf("inherited attribute type = %q, want Boolean — "+
			"defaulting to String is what mistyped mapping elements", got)
	}
	if got := ResolveMemberType(b, "App.Item", "Nope"); got != "" {
		t.Errorf("unresolvable member type = %q, want empty", got)
	}
}

// TestResolveAttributeType_InheritedAttribute covers the mapping call site rather
// than the resolver: resolveAttributeType scanned only the entity's own attributes
// and fell through to its "String" default, so a mapping element bound to an
// inherited Boolean or DateTime got the wrong DataType (mendixlabs/mxcli#703).
func TestResolveAttributeType_InheritedAttribute(t *testing.T) {
	b := typedHierarchyBackend()

	if got := resolveAttributeType("App.Item", "OwnField", b); got != "String" {
		t.Errorf("own attribute = %q, want String", got)
	}
	if got := resolveAttributeType("App.Item", "Flag", b); got != "Boolean" {
		t.Errorf("inherited attribute = %q, want Boolean — the String default is the bug", got)
	}
	// An unresolvable member still falls back to the documented default.
	if got := resolveAttributeType("App.Item", "Nope", b); got != "String" {
		t.Errorf("unresolvable attribute = %q, want the String fallback", got)
	}
}
