// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"fmt"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/backend/mock"
	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/domainmodel"
)

// upstream #829: an expression navigating an association must name the target
// entity fully qualified —
//
//	$Issue/MA.Issue_Person/MB.Person/FullName
//
// mxcli inserts that step itself, so the author may write the association alone.
// Two ways it went wrong, both ending in CE0117 "Error(s) in expression." with
// nothing said at exec or check time:
//
//  1. CROSS-MODULE: lookupAssociation walked only dm.Associations. A
//     cross-module association lives in dm.CrossAssociations (remote end is the
//     BY_NAME ChildRef), so the lookup failed and NOTHING was inserted — the
//     author's short form was written through unchanged. This is the same
//     two-list trap as #854 and issuetracker #19.
//
//  2. SAME-MODULE, entity named: the guard only skipped insertion when the next
//     segment was already QUALIFIED. An author writing the bare entity name got
//     the resolved step inserted *in addition* to their own, producing
//     `MA.Issue_Tag/MA.Tag/Tag/Label` — four segments, equally invalid.
//
// Verified against mxbuild 11.13.0: each shape is one CE0117, and the corrected
// output builds clean.
func TestResolvePathSegments_QualifiesAssociationTarget(t *testing.T) {
	fb := newAssocPathTestBuilder(t)

	tests := []struct {
		name string
		path []string
		want []string
	}{
		{
			name: "cross-module, association only",
			path: []string{"MA.Issue_Person", "FullName"},
			want: []string{"MA.Issue_Person", "MB.Person", "FullName"},
		},
		{
			name: "cross-module, author named the entity bare (#829 as reported)",
			path: []string{"MA.Issue_Person", "Person", "FullName"},
			want: []string{"MA.Issue_Person", "MB.Person", "FullName"},
		},
		{
			name: "cross-module, already fully qualified — left alone",
			path: []string{"MA.Issue_Person", "MB.Person", "FullName"},
			want: []string{"MA.Issue_Person", "MB.Person", "FullName"},
		},
		{
			name: "same-module, association only",
			path: []string{"MA.Issue_Tag", "Label"},
			want: []string{"MA.Issue_Tag", "MA.Tag", "Label"},
		},
		{
			name: "same-module, author named the entity bare — must not duplicate",
			path: []string{"MA.Issue_Tag", "Tag", "Label"},
			want: []string{"MA.Issue_Tag", "MA.Tag", "Label"},
		},
		{
			name: "same-module, already fully qualified",
			path: []string{"MA.Issue_Tag", "MA.Tag", "Label"},
			want: []string{"MA.Issue_Tag", "MA.Tag", "Label"},
		},
		{
			name: "plain attribute path is untouched",
			path: []string{"Subject"},
			want: []string{"Subject"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fb.resolvePathSegments(tc.path)
			if len(got) != len(tc.want) {
				t.Fatalf("resolvePathSegments(%v) = %v, want %v", tc.path, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("resolvePathSegments(%v) = %v, want %v", tc.path, got, tc.want)
				}
			}
		})
	}
}

// An association whose target cannot be resolved must leave the path alone
// rather than invent a qualifier — a wrong entity name is as invalid as a
// missing one, and the author can still be right about a shape mxcli cannot see.
func TestResolvePathSegments_UnknownAssociationIsLeftAlone(t *testing.T) {
	fb := newAssocPathTestBuilder(t)
	path := []string{"MA.Nope_Assoc", "Field"}
	got := fb.resolvePathSegments(path)
	if len(got) != 2 || got[0] != "MA.Nope_Assoc" || got[1] != "Field" {
		t.Fatalf("resolvePathSegments(%v) = %v, want it unchanged", path, got)
	}
}

// newAssocPathTestBuilder wires a flowBuilder over a two-module domain model:
// MA holds Issue and Tag plus a same-module association, and a CROSS-module
// association to MB.Person — the two lists a lookup has to search.
func newAssocPathTestBuilder(t *testing.T) *flowBuilder {
	t.Helper()

	const (
		maID    = model.ID("mod-ma")
		issueID = model.ID("e-issue")
		tagID   = model.ID("e-tag")
	)

	mb := &mock.MockBackend{
		GetModuleByNameFunc: func(name string) (*model.Module, error) {
			if name == "MA" {
				return &model.Module{BaseElement: model.BaseElement{ID: maID}, Name: "MA"}, nil
			}
			return nil, fmt.Errorf("no module %q", name)
		},
		GetDomainModelFunc: func(moduleID model.ID) (*domainmodel.DomainModel, error) {
			if moduleID != maID {
				return nil, fmt.Errorf("no domain model for %q", moduleID)
			}
			return &domainmodel.DomainModel{
				ContainerID: maID,
				Entities: []*domainmodel.Entity{
					{BaseElement: model.BaseElement{ID: issueID}, Name: "Issue", Persistable: true},
					{BaseElement: model.BaseElement{ID: tagID}, Name: "Tag", Persistable: true},
				},
				Associations: []*domainmodel.Association{
					{Name: "Issue_Tag", ParentID: issueID, ChildID: tagID, Type: domainmodel.AssociationTypeReference},
				},
				CrossAssociations: []*domainmodel.CrossModuleAssociation{
					{Name: "Issue_Person", ParentID: issueID, ChildRef: "MB.Person", Type: domainmodel.AssociationTypeReference},
				},
			}, nil
		},
	}

	return &flowBuilder{backend: mb}
}
