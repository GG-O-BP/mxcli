// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"testing"

	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/domainmodel"
)

// TestReconcileDroppedIndexes guards ledger finding #39: a CREATE OR MODIFY that
// drops an indexed attribute must not leave an orphaned index (which crashes
// `mx check`). When the statement lists no indexes, existing indexes are carried
// forward with columns for dropped attributes pruned and empty indexes removed.
func TestReconcileDroppedIndexes(t *testing.T) {
	idA, idB, idC := model.ID("a"), model.ID("b"), model.ID("c")
	existing := func() *domainmodel.Entity {
		return &domainmodel.Entity{
			Attributes: []*domainmodel.Attribute{{BaseElement: model.BaseElement{ID: idA}, Name: "A"}, {BaseElement: model.BaseElement{ID: idB}, Name: "B"}, {BaseElement: model.BaseElement{ID: idC}, Name: "C"}},
			Indexes: []*domainmodel.Index{
				{AttributeIDs: []model.ID{idA, idB}},
				{AttributeIDs: []model.ID{idC}},
			},
		}
	}

	t.Run("drop C: its single-column index is removed, composite kept+pruned", func(t *testing.T) {
		newEnt := &domainmodel.Entity{Attributes: []*domainmodel.Attribute{{BaseElement: model.BaseElement{ID: idA}, Name: "A"}, {BaseElement: model.BaseElement{ID: idB}, Name: "B"}}}
		dropped := reconcileDroppedIndexes(newEnt, existing())
		if dropped != 1 {
			t.Errorf("dropped = %d, want 1", dropped)
		}
		if len(newEnt.Indexes) != 1 || len(newEnt.Indexes[0].AttributeIDs) != 2 {
			t.Fatalf("expected 1 kept index over [A,B], got %+v", newEnt.Indexes)
		}
	})

	t.Run("drop B: composite index pruned to [A]", func(t *testing.T) {
		newEnt := &domainmodel.Entity{Attributes: []*domainmodel.Attribute{{BaseElement: model.BaseElement{ID: idA}, Name: "A"}, {BaseElement: model.BaseElement{ID: idC}, Name: "C"}}}
		reconcileDroppedIndexes(newEnt, existing())
		// index over [A,B] → [A]; index over [C] kept.
		if len(newEnt.Indexes) != 2 {
			t.Fatalf("expected 2 indexes, got %d", len(newEnt.Indexes))
		}
		if len(newEnt.Indexes[0].AttributeIDs) != 1 || newEnt.Indexes[0].AttributeIDs[0] != idA {
			t.Errorf("first index should be pruned to [A], got %v", newEnt.Indexes[0].AttributeIDs)
		}
	})

	t.Run("statement provides its own indexes: no carry-forward", func(t *testing.T) {
		newEnt := &domainmodel.Entity{
			Attributes: []*domainmodel.Attribute{{BaseElement: model.BaseElement{ID: idA}, Name: "A"}},
			Indexes:    []*domainmodel.Index{{AttributeIDs: []model.ID{idA}}},
		}
		if dropped := reconcileDroppedIndexes(newEnt, existing()); dropped != 0 {
			t.Errorf("dropped = %d, want 0 (statement indexes replace wholesale)", dropped)
		}
		if len(newEnt.Indexes) != 1 {
			t.Errorf("statement index list must be left intact, got %d", len(newEnt.Indexes))
		}
	})
}

// TestIsViewEntity guards ledger finding #41: view entities cannot participate in
// associations (CE6771), so the checker must recognize one. The canonical marker
// is the OqlViewEntitySource source type; OqlQuery / SourceDocumentRef are
// fallbacks for read paths that populate only one.
func TestIsViewEntity(t *testing.T) {
	cases := []struct {
		name string
		ent  *domainmodel.Entity
		want bool
	}{
		{"nil", nil, false},
		{"plain persistent entity", &domainmodel.Entity{Name: "Customer"}, false},
		{"view via Source", &domainmodel.Entity{Name: "VCat", Source: "DomainModels$OqlViewEntitySource"}, true},
		{"view via OqlQuery", &domainmodel.Entity{Name: "VCat", OqlQuery: "select 1"}, true},
		{"view via SourceDocumentRef", &domainmodel.Entity{Name: "VCat", SourceDocumentRef: "Mod.VCatVe"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isViewEntity(c.ent); got != c.want {
				t.Errorf("isViewEntity = %v, want %v", got, c.want)
			}
		})
	}
}
