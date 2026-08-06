// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/backend"
	"github.com/mendixlabs/mxcli/mdl/backend/mock"
	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/domainmodel"
	"github.com/mendixlabs/mxcli/sdk/security"
)

// grantMembersFixture builds a module with two entities joined by a reference set
// and a GRANT harness over it. `owner` selects Default vs Both.
type grantMembersFixture struct {
	ctx      *ExecContext
	captured *backend.EntityAccessRuleParams
}

func newGrantMembersFixture(t *testing.T, owner domainmodel.AssociationOwner, audit bool) *grantMembersFixture {
	t.Helper()

	const (
		issueID = model.ID("e-issue")
		tagID   = model.ID("e-tag")
	)
	mod := mkModule("IT")
	h := mkHierarchy(mod)

	issue := &domainmodel.Entity{
		BaseElement:    model.BaseElement{ID: issueID},
		Name:           "Issue",
		Attributes:     []*domainmodel.Attribute{{Name: "Title"}},
		HasCreatedDate: audit,
		HasChangedDate: audit,
	}
	tag := &domainmodel.Entity{
		BaseElement: model.BaseElement{ID: tagID},
		Name:        "Tag",
		Attributes:  []*domainmodel.Attribute{{Name: "TagName"}},
	}
	dm := &domainmodel.DomainModel{
		BaseElement: model.BaseElement{ID: "dm-it"},
		ContainerID: mod.ID,
		Entities:    []*domainmodel.Entity{issue, tag},
		Associations: []*domainmodel.Association{{
			Name:     "Issue_Tag",
			ParentID: issueID,
			ChildID:  tagID,
			Type:     domainmodel.AssociationTypeReferenceSet,
			Owner:    owner,
		}},
	}

	f := &grantMembersFixture{}
	mb := &mock.MockBackend{
		IsConnectedFunc: func() bool { return true },
		ListModulesFunc: func() ([]*model.Module, error) { return []*model.Module{mod}, nil },
		ListDomainModelsFunc: func() ([]*domainmodel.DomainModel, error) {
			return []*domainmodel.DomainModel{dm}, nil
		},
		GetDomainModelFunc: func(model.ID) (*domainmodel.DomainModel, error) { return dm, nil },
		GetModuleSecurityFunc: func(model.ID) (*security.ModuleSecurity, error) {
			return &security.ModuleSecurity{ModuleRoles: []*security.ModuleRole{{Name: "Admin"}}}, nil
		},
		AddEntityAccessRuleFunc: func(p backend.EntityAccessRuleParams) error {
			cp := p
			f.captured = &cp
			return nil
		},
		ReconcileMemberAccessesFunc: func(model.ID, string) (int, error) { return 0, nil },
	}
	ctx, _ := newMockCtx(t, withBackend(mb), withHierarchy(h))
	f.ctx = ctx
	return f
}

func grantStmt(entity string, rights ...ast.EntityAccessRight) *ast.GrantEntityAccessStmt {
	return &ast.GrantEntityAccessStmt{
		Entity: ast.QualifiedName{Module: "IT", Name: entity},
		Roles:  []ast.QualifiedName{{Module: "IT", Name: "Admin"}},
		Rights: rights,
	}
}

func (f *grantMembersFixture) associationRights(assocRef string) (string, bool) {
	if f.captured == nil {
		return "", false
	}
	for _, ma := range f.captured.MemberAccesses {
		if ma.AssociationRef == assocRef {
			return ma.AccessRights, true
		}
	}
	return "", false
}

// TestGrantEntityAccess_BothOwnerAssociationOnToSide pins issuetracker #20: with
// `OWNER Both` the association is a member of BOTH ends, so the TO entity's rule
// needs a MemberAccess entry too. Emitting it only on the FROM side left the TO
// entity's rule incomplete, which Mendix reports as CE0066 "Entity access is out
// of date" — and made partial coverage worse than none.
func TestGrantEntityAccess_BothOwnerAssociationOnToSide(t *testing.T) {
	f := newGrantMembersFixture(t, domainmodel.AssociationOwnerBoth, false)

	// Tag is the TO side of IT.Issue_Tag.
	if err := execGrantEntityAccess(f.ctx, grantStmt("Tag",
		ast.EntityAccessRight{Type: ast.EntityAccessWriteAll})); err != nil {
		t.Fatalf("grant failed: %v", err)
	}
	rights, ok := f.associationRights("IT.Issue_Tag")
	if !ok {
		t.Fatalf("no MemberAccess for IT.Issue_Tag on the TO entity — the rule is incomplete (CE0066); got %+v", f.captured.MemberAccesses)
	}
	if rights != "ReadWrite" {
		t.Errorf("association rights = %q, want ReadWrite (the rule default)", rights)
	}
}

// Regression guard: with the default owner the association belongs to the FROM
// side only, and adding it to the TO side is itself a CE0066.
func TestGrantEntityAccess_DefaultOwnerAssociationNotOnToSide(t *testing.T) {
	f := newGrantMembersFixture(t, domainmodel.AssociationOwnerDefault, false)

	if err := execGrantEntityAccess(f.ctx, grantStmt("Tag",
		ast.EntityAccessRight{Type: ast.EntityAccessWriteAll})); err != nil {
		t.Fatalf("grant failed: %v", err)
	}
	if _, ok := f.associationRights("IT.Issue_Tag"); ok {
		t.Errorf("TO entity must not carry the association for an OWNER Default association; got %+v", f.captured.MemberAccesses)
	}
}

// The FROM side keeps its entry under either owner mode.
func TestGrantEntityAccess_AssociationAlwaysOnFromSide(t *testing.T) {
	for _, owner := range []domainmodel.AssociationOwner{
		domainmodel.AssociationOwnerDefault,
		domainmodel.AssociationOwnerBoth,
	} {
		f := newGrantMembersFixture(t, owner, false)
		if err := execGrantEntityAccess(f.ctx, grantStmt("Issue",
			ast.EntityAccessRight{Type: ast.EntityAccessWriteAll})); err != nil {
			t.Fatalf("owner %s: grant failed: %v", owner, err)
		}
		if _, ok := f.associationRights("IT.Issue_Tag"); !ok {
			t.Errorf("owner %s: FROM entity lost its association MemberAccess", owner)
		}
	}
}

// TestGrantEntityAccess_ToSideAssociationNamedExplicitly: naming the TO-side
// association was rejected as "entity has no member(s) Issue_Tag". It is a
// member, so it must be accepted and its per-member rights honoured.
func TestGrantEntityAccess_ToSideAssociationNamedExplicitly(t *testing.T) {
	f := newGrantMembersFixture(t, domainmodel.AssociationOwnerBoth, false)

	err := execGrantEntityAccess(f.ctx, grantStmt("Tag",
		ast.EntityAccessRight{Type: ast.EntityAccessWriteAll},
		ast.EntityAccessRight{Type: ast.EntityAccessReadMembers, Members: []string{"Issue_Tag"}}))
	if err != nil {
		t.Fatalf("naming the TO-side association was rejected: %v", err)
	}
	if rights, _ := f.associationRights("IT.Issue_Tag"); rights != "ReadOnly" {
		t.Errorf("association rights = %q, want ReadOnly (the explicit read grant)", rights)
	}
}

// TestGrantEntityAccess_AuditMembers: audit members are entity FLAGS, not
// entries in entity.Attributes, so naming one was rejected as "no member" —
// wrong, Mendix does consider them members. But Mendix stores no MemberAccess
// for them (mxbuild rejects a rule that carries one with CE0066), so their
// access can only come from the rule's default. Naming one is therefore
// accepted; asking for rights that differ from the default is refused rather
// than silently dropped.
func TestGrantEntityAccess_AuditMembers(t *testing.T) {
	t.Run("named at the rule default is accepted and emits no entry", func(t *testing.T) {
		f := newGrantMembersFixture(t, domainmodel.AssociationOwnerDefault, true)
		err := execGrantEntityAccess(f.ctx, grantStmt("Issue",
			ast.EntityAccessRight{Type: ast.EntityAccessReadAll},
			ast.EntityAccessRight{Type: ast.EntityAccessReadMembers, Members: []string{"createdDate", "changedDate"}}))
		if err != nil {
			t.Fatalf("naming an audit member was rejected: %v", err)
		}
		for _, ma := range f.captured.MemberAccesses {
			if strings.HasSuffix(ma.AttributeRef, ".createdDate") || strings.HasSuffix(ma.AttributeRef, ".changedDate") {
				t.Errorf("emitted a MemberAccess for an audit member (%s) — mxbuild rejects this with CE0066", ma.AttributeRef)
			}
		}
	})

	t.Run("rights differing from the default are refused with a reason", func(t *testing.T) {
		f := newGrantMembersFixture(t, domainmodel.AssociationOwnerDefault, true)
		err := execGrantEntityAccess(f.ctx, grantStmt("Issue",
			ast.EntityAccessRight{Type: ast.EntityAccessWriteAll},
			ast.EntityAccessRight{Type: ast.EntityAccessReadMembers, Members: []string{"createdDate"}}))
		if err == nil {
			t.Fatal("read-only on an audit member under a ReadWrite default must be refused, not silently dropped")
		}
		for _, want := range []string{"createdDate", "audit member", "CE0066"} {
			assertContainsStr(t, err.Error(), want)
		}
		// The old message claimed the member did not exist.
		if strings.Contains(err.Error(), "has no member") {
			t.Errorf("error still claims the audit member does not exist: %s", err.Error())
		}
	})

	t.Run("an unknown member is still an error", func(t *testing.T) {
		f := newGrantMembersFixture(t, domainmodel.AssociationOwnerDefault, true)
		err := execGrantEntityAccess(f.ctx, grantStmt("Issue",
			ast.EntityAccessRight{Type: ast.EntityAccessReadMembers, Members: []string{"NoSuchMember"}}))
		assertError(t, err)
		assertContainsStr(t, err.Error(), "NoSuchMember")
	})
}
