// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"testing"

	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/domainmodel"
)

// f1CachedModel mirrors the shape that made the bug unbounded: three entities
// each with a `season` navigation property. Association names are unique per
// module, so only the first can be called `season`; the others carry a numeric
// suffix and no longer match the nav property they came from.
func f1CachedModel() *domainmodel.DomainModel {
	races := &domainmodel.Entity{Name: "Races"}
	races.ID = model.ID("e-races")
	ds := &domainmodel.Entity{Name: "DriverStandings"}
	ds.ID = model.ID("e-ds")
	cs := &domainmodel.Entity{Name: "ConstructorStandings"}
	cs.ID = model.ID("e-cs")
	season := &domainmodel.Entity{Name: "Seasons"}
	season.ID = model.ID("e-season")

	mk := func(id, name string, parent model.ID, nav string) *domainmodel.Association {
		a := &domainmodel.Association{
			Name: name, ParentID: parent, ChildID: season.ID,
			RemoteParentNavigationProperty: nav,
		}
		a.ID = model.ID(id)
		return a
	}

	return &domainmodel.DomainModel{
		Entities: []*domainmodel.Entity{races, ds, cs, season},
		Associations: []*domainmodel.Association{
			mk("a1", "season", races.ID, "season"),
			mk("a2", "season_2", ds.ID, "season"),
			mk("a3", "season_3", cs.ID, "season"),
		},
	}
}

// The dedup must recognise a suffixed association as the nav property it was
// generated from. Matching on the association name alone cannot: `season_2` is
// not `season`, so a re-import recreates it as `season_4`, then `season_6`, for
// ever (mxcli-formula1 §50 — one project reached season_15).
func TestIndexExistingAssociations_SuffixedAssociationMatchesItsNavProperty(t *testing.T) {
	byName, byNav := indexExistingAssociations(f1CachedModel())

	// This is the lookup the import performs, for each parent that has a
	// `season` nav property. All three must be recognised as already imported.
	for _, parent := range []string{"Races", "DriverStandings", "ConstructorStandings"} {
		k := assocKey{parent, "season"}
		if !byNav[k] && !byName[k] {
			t.Errorf("%s.season is not recognised as already imported — a re-import "+
				"will create a duplicate with a fresh suffix", parent)
		}
	}

	// Specifically: the two suffixed ones are matched via the nav index, not the
	// name index. If that ever inverts, the name match is doing work it cannot
	// actually do and the guard above would pass for the wrong reason.
	for _, parent := range []string{"DriverStandings", "ConstructorStandings"} {
		if byName[assocKey{parent, "season"}] {
			t.Errorf("%s: name index unexpectedly holds the unsuffixed name", parent)
		}
		if !byNav[assocKey{parent, "season"}] {
			t.Errorf("%s: nav index missing — this is the entry that prevents the duplicate", parent)
		}
	}
}

// Legacy data (and Studio Pro-authored associations) may carry no nav property.
// The name index must still cover the unsuffixed case, so those do not duplicate
// either.
func TestIndexExistingAssociations_FallsBackToNameWhenNavAbsent(t *testing.T) {
	dm := f1CachedModel()
	for _, a := range dm.Associations {
		a.RemoteParentNavigationProperty = ""
	}
	byName, byNav := indexExistingAssociations(dm)

	if len(byNav) != 0 {
		t.Errorf("nav index should be empty when no association carries a nav property: %v", byNav)
	}
	if !byName[assocKey{"Races", "season"}] {
		t.Error("the unsuffixed association is not matched by name either")
	}
}

// An association whose parent is not in this domain model (a cross-module
// association) must not index under an empty parent name, which would collide
// with every other orphan and could suppress a legitimate import.
func TestIndexExistingAssociations_SkipsAssociationsWithUnknownParent(t *testing.T) {
	dm := f1CachedModel()
	orphan := &domainmodel.Association{
		Name: "elsewhere", ParentID: model.ID("not-in-this-module"),
		RemoteParentNavigationProperty: "elsewhere",
	}
	orphan.ID = model.ID("a9")
	dm.Associations = append(dm.Associations, orphan)

	byName, byNav := indexExistingAssociations(dm)
	if byName[assocKey{"", "elsewhere"}] || byNav[assocKey{"", "elsewhere"}] {
		t.Error("an association with an unresolvable parent was indexed under the empty name")
	}
}
