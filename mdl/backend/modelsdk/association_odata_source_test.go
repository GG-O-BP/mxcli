// SPDX-License-Identifier: Apache-2.0

package modelsdkbackend

import (
	"testing"

	"github.com/mendixlabs/mxcli/sdk/domainmodel"
)

// RemoteParentNavigationProperty is the only durable link from an association
// back to the OData navigation property it was generated from, and
// CREATE EXTERNAL ENTITIES dedupes a re-import on it. The write path set it and
// the read path dropped it, so the field survived one save and vanished on the
// next load.
//
// The consequence was unbounded: association names must be unique per module, so
// a second entity with a `season` nav property gets `season_2`. On a re-import
// the dedup looked for an association *named* `season` on that parent, found
// `season_2`, and created `season_4`. Two more every run, clean under
// `mx check`, visible only as duplicate links in Studio Pro. One real project
// reached `season_15` before anyone noticed (mxcli-formula1 §50).
func TestAssociation_ODataSourceRoundTrips(t *testing.T) {
	proj := copyFixture(t)
	b := New()
	if err := b.Connect(proj); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = b.Disconnect() })

	mod, err := b.GetModuleByName("MyFirstModule")
	if err != nil || mod == nil {
		t.Fatalf("GetModuleByName: %v", err)
	}
	dm, err := b.GetDomainModel(mod.ID)
	if err != nil {
		t.Fatalf("GetDomainModel: %v", err)
	}

	parent := &domainmodel.Entity{Name: "ZzStanding", Persistable: false}
	child := &domainmodel.Entity{Name: "ZzSeason", Persistable: false}
	if err := b.CreateEntity(dm.ID, parent); err != nil {
		t.Fatalf("CreateEntity parent: %v", err)
	}
	if err := b.CreateEntity(dm.ID, child); err != nil {
		t.Fatalf("CreateEntity child: %v", err)
	}

	// Named with a suffix on purpose: this is exactly the association the
	// re-import failed to recognise, because its name no longer equals the nav
	// property it came from.
	want := &domainmodel.Association{
		Name:                           "season_2",
		ParentID:                       parent.ID,
		ChildID:                        child.ID,
		Type:                           "Reference",
		Owner:                          "Default",
		Source:                         "Rest$ODataRemoteAssociationSource",
		Navigability2:                  "ParentToChild",
		RemoteParentNavigationProperty: "season",
		RemoteChildNavigationProperty:  "standings",
		CreatableFromParent:            true,
		UpdatableFromParent:            true,
	}
	if err := b.CreateAssociation(dm.ID, want); err != nil {
		t.Fatalf("CreateAssociation: %v", err)
	}

	dm2, err := b.GetDomainModel(mod.ID)
	if err != nil {
		t.Fatalf("GetDomainModel(2): %v", err)
	}
	var got *domainmodel.Association
	for _, a := range dm2.Associations {
		if a.Name == "season_2" {
			got = a
			break
		}
	}
	if got == nil {
		t.Fatal("association season_2 not found after reload")
	}

	if got.RemoteParentNavigationProperty != "season" {
		t.Errorf("RemoteParentNavigationProperty = %q, want %q — a re-import "+
			"cannot recognise this association without it, and will duplicate it",
			got.RemoteParentNavigationProperty, "season")
	}
	if got.Source != "Rest$ODataRemoteAssociationSource" {
		t.Errorf("Source = %q, want Rest$ODataRemoteAssociationSource", got.Source)
	}
	if got.RemoteChildNavigationProperty != "standings" {
		t.Errorf("RemoteChildNavigationProperty = %q, want %q",
			got.RemoteChildNavigationProperty, "standings")
	}
	if got.Navigability2 != "ParentToChild" {
		t.Errorf("Navigability2 = %q, want ParentToChild", got.Navigability2)
	}
	if !got.CreatableFromParent || !got.UpdatableFromParent {
		t.Errorf("capability flags lost: CreatableFromParent=%v UpdatableFromParent=%v",
			got.CreatableFromParent, got.UpdatableFromParent)
	}
}

// A plain association must not acquire an OData source it never had — the read
// has to branch on the stored type, not stamp every association.
func TestAssociation_PlainAssociationHasNoODataSource(t *testing.T) {
	proj := copyFixture(t)
	b := New()
	if err := b.Connect(proj); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = b.Disconnect() })

	mod, _ := b.GetModuleByName("MyFirstModule")
	dm, err := b.GetDomainModel(mod.ID)
	if err != nil {
		t.Fatalf("GetDomainModel: %v", err)
	}
	parent := &domainmodel.Entity{Name: "ZzPlainA", Persistable: true}
	child := &domainmodel.Entity{Name: "ZzPlainB", Persistable: true}
	_ = b.CreateEntity(dm.ID, parent)
	_ = b.CreateEntity(dm.ID, child)
	if err := b.CreateAssociation(dm.ID, &domainmodel.Association{
		Name: "ZzPlainB_ZzPlainA", ParentID: child.ID, ChildID: parent.ID,
		Type: "Reference", Owner: "Default",
	}); err != nil {
		t.Fatalf("CreateAssociation: %v", err)
	}

	dm2, _ := b.GetDomainModel(mod.ID)
	for _, a := range dm2.Associations {
		if a.Name != "ZzPlainB_ZzPlainA" {
			continue
		}
		if a.Source != "" || a.RemoteParentNavigationProperty != "" {
			t.Errorf("a plain association was stamped as external: Source=%q nav=%q",
				a.Source, a.RemoteParentNavigationProperty)
		}
		return
	}
	t.Fatal("plain association not found after reload")
}
