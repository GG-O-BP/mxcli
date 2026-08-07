// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"bytes"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/backend/mock"
	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/domainmodel"
)

// publishCtx wires a module with one entity of the given persistability, and
// returns a context plus a pointer to whatever CreatePublishedODataService is
// handed.
func publishCtx(t *testing.T, entityName string, persistable bool) (*ExecContext, **model.PublishedODataService, *bytes.Buffer) {
	t.Helper()
	mod := mkModule("Probe")
	h := mkHierarchy(mod)
	dm := &domainmodel.DomainModel{
		BaseElement: model.BaseElement{ID: nextID("dm")},
		ContainerID: mod.ID,
		Entities: []*domainmodel.Entity{
			{
				BaseElement: model.BaseElement{ID: nextID("ent")},
				Name:        entityName,
				Persistable: persistable,
				Attributes: []*domainmodel.Attribute{
					{BaseElement: model.BaseElement{ID: nextID("attr")}, Name: "RowKey", Type: &domainmodel.StringAttributeType{}},
				},
			},
		},
	}

	var created *model.PublishedODataService
	mb := &mock.MockBackend{
		IsConnectedFunc:    func() bool { return true },
		ListModulesFunc:    func() ([]*model.Module, error) { return []*model.Module{mod}, nil },
		GetDomainModelFunc: func(model.ID) (*domainmodel.DomainModel, error) { return dm, nil },
		ListPublishedODataServicesFunc: func() ([]*model.PublishedODataService, error) {
			return nil, nil
		},
		CreatePublishedODataServiceFunc: func(svc *model.PublishedODataService) error {
			created = svc
			return nil
		},
	}
	ctx, buf := newMockCtx(t, withBackend(mb), withHierarchy(h))
	return ctx, &created, buf
}

func publishStmt(entityName string) *ast.CreateODataServiceStmt {
	return &ast.CreateODataServiceStmt{
		Name:         ast.QualifiedName{Module: "Probe", Name: "ProbeApi"},
		Path:         "odata/probe/",
		Version:      "1.0.0",
		ODataVersion: "OData4",
		Namespace:    "Probe.Api",
		Entities: []*ast.PublishedEntityDef{
			{
				Entity:      ast.QualifiedName{Module: "Probe", Name: entityName},
				ExposedName: "Rows",
				ReadMode:    "MICROFLOW Probe.Read_Rows",
			},
		},
	}
}

// mxcli-formula1 findings #10.1: Name (the document) and ServiceName (the name
// in the OData metadata document) are different properties, and CREATE only set
// the first — so every published service created purely from MDL failed the
// build with CE0729 "The service name should not be empty", which `mxcli check`
// cannot see. The CONSUMED path has defaulted this for CE0339 all along.
func TestCreateODataService_DefaultsServiceNameToDocumentName(t *testing.T) {
	ctx, created, _ := publishCtx(t, "Row", true)
	if err := createODataService(ctx, publishStmt("Row")); err != nil {
		t.Fatal(err)
	}
	if *created == nil {
		t.Fatal("expected the service to be created")
	}
	if got := (*created).ServiceName; got != "ProbeApi" {
		t.Errorf("ServiceName = %q, want the document name %q", got, "ProbeApi")
	}
}

// An explicit ServiceName still wins — the default fills a gap, it does not
// override the author.
func TestCreateODataService_ExplicitServiceNameWins(t *testing.T) {
	ctx, created, _ := publishCtx(t, "Row", true)
	stmt := publishStmt("Row")
	stmt.ServiceName = "PublicName"
	if err := createODataService(ctx, stmt); err != nil {
		t.Fatal(err)
	}
	if got := (*created).ServiceName; got != "PublicName" {
		t.Errorf("ServiceName = %q, want %q", got, "PublicName")
	}
}
