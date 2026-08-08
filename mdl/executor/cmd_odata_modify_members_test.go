// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/backend/mock"
	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/domainmodel"
)

// mxcli-formula1 findings #26: re-running a `create or modify odata service`
// after editing a `publish entity` block did not apply the change. Marking a
// member Filterable and re-executing left the served $metadata exactly as it
// was; only `drop odata service` + create picked it up. The modify branch
// updated the service's scalar properties and never touched EntityTypes or
// EntitySets.
//
// Confirmed on 11.12.1 against a real build: the same script produced
// `Label as 'label'` before the fix and `Label as 'label' (Filterable, Sortable)`
// after.
func TestModifyODataService_AppliesPublishedEntities(t *testing.T) {
	svc, mb, h := existingPublishedService()
	var updated *model.PublishedODataService
	mb.UpdatePublishedODataServiceFunc = func(s *model.PublishedODataService) error {
		updated = s
		return nil
	}
	ctx, _ := newMockCtx(t, withBackend(mb), withHierarchy(h))

	stmt := &ast.CreateODataServiceStmt{
		Name:           ast.QualifiedName{Module: "MyModule", Name: "CatalogService"},
		CreateOrModify: true,
		Entities: []*ast.PublishedEntityDef{{
			Entity:      ast.QualifiedName{Module: "MyModule", Name: "Order"},
			ExposedName: "Orders",
			Members: []*ast.PublishedMemberDef{
				{Name: "Label", ExposedName: "label", Filterable: true, Sortable: true},
			},
		}},
	}
	assertNoError(t, createODataService(ctx, stmt))

	if updated == nil {
		t.Fatal("the service was never updated")
	}
	m := findPublishedMember(t, updated, "Label")
	if !m.Filterable || !m.Sortable {
		t.Errorf("member Label: filterable=%v sortable=%v, want both true — "+
			"the modify did not apply the edited publish block", m.Filterable, m.Sortable)
	}
	_ = svc
}

// A modify cannot express role grants (`grant access on odata service …` is a
// separate statement), so it must carry the existing ones through or the build
// fails with "At least one allowed role must be selected".
//
// A guard rather than a reproduction: the loss was reported but did not
// reproduce on 11.12.1 — grants survived a modify on both the fixed and the
// previous build. The invariant holds regardless.
func TestModifyODataService_KeepsRoleGrants(t *testing.T) {
	svc, mb, h := existingPublishedService()
	svc.AllowedModuleRoles = []string{"MyModule.ApiUser"}
	var updated *model.PublishedODataService
	mb.UpdatePublishedODataServiceFunc = func(s *model.PublishedODataService) error {
		updated = s
		return nil
	}
	ctx, _ := newMockCtx(t, withBackend(mb), withHierarchy(h))

	stmt := &ast.CreateODataServiceStmt{
		Name:           ast.QualifiedName{Module: "MyModule", Name: "CatalogService"},
		CreateOrModify: true,
		Path:           "odata/catalog2/",
	}
	assertNoError(t, createODataService(ctx, stmt))

	if updated == nil {
		t.Fatal("the service was never updated")
	}
	if len(updated.AllowedModuleRoles) != 1 || updated.AllowedModuleRoles[0] != "MyModule.ApiUser" {
		t.Errorf("AllowedModuleRoles = %v, want [MyModule.ApiUser]", updated.AllowedModuleRoles)
	}
	if updated.Path != "odata/catalog2/" {
		t.Errorf("Path = %q, want the modify's own change to land too", updated.Path)
	}
}

// existingPublishedService is a one-entity service already in the model, with
// the backend and hierarchy wired so createODataService takes its modify branch.
func existingPublishedService() (*model.PublishedODataService, *mock.MockBackend, *ContainerHierarchy) {
	mod := mkModule("MyModule")
	svc := &model.PublishedODataService{
		ContainerID: mod.ID,
		Name:        "CatalogService",
		Path:        "odata/catalog/",
		ServiceName: "CatalogService",
		EntityTypes: []*model.PublishedEntityType{{
			ExposedName: "Order",
			Entity:      "MyModule.Order",
			Members:     []*model.PublishedMember{{Kind: "attribute", Name: "Label", ExposedName: "label"}},
		}},
		EntitySets: []*model.PublishedEntitySet{{
			ExposedName:    "Orders",
			EntityTypeName: "MyModule.Order",
		}},
	}
	h := mkHierarchy(mod)
	withContainer(h, svc.ContainerID, mod.ID)
	mb := &mock.MockBackend{
		IsConnectedFunc:     func() bool { return true },
		ListModulesFunc:     func() ([]*model.Module, error) { return []*model.Module{mod}, nil },
		GetModuleByNameFunc: func(string) (*model.Module, error) { return mod, nil },
		GetDomainModelFunc: func(model.ID) (*domainmodel.DomainModel, error) {
			return &domainmodel.DomainModel{
				Entities: []*domainmodel.Entity{{
					Name:       "Order",
					Attributes: []*domainmodel.Attribute{{Name: "Label", Type: &domainmodel.StringAttributeType{Length: 120}}},
				}},
			}, nil
		},
		ListPublishedODataServicesFunc: func() ([]*model.PublishedODataService, error) {
			return []*model.PublishedODataService{svc}, nil
		},
	}
	return svc, mb, h
}

func findPublishedMember(t *testing.T, svc *model.PublishedODataService, name string) *model.PublishedMember {
	t.Helper()
	for _, et := range svc.EntityTypes {
		for _, m := range et.Members {
			if m.Name == name {
				return m
			}
		}
	}
	t.Fatalf("published member %q not found", name)
	return nil
}
