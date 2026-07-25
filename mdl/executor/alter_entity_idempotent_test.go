// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/backend/mock"
	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/domainmodel"
)

// idempotentTestCtx builds an ExecContext over a Sudoku.Game entity carrying the
// given attributes, wired so execAlterEntity can resolve and (optionally) update it.
func idempotentTestCtx(t *testing.T, attrs ...string) (*ExecContext, *bool) {
	t.Helper()
	mod := mkModule("Sudoku")
	game := &domainmodel.Entity{
		BaseElement: model.BaseElement{ID: nextID("ent")},
		ContainerID: nextID("dm"),
		Name:        "Game",
		Persistable: true,
	}
	for _, a := range attrs {
		game.Attributes = append(game.Attributes, &domainmodel.Attribute{Name: a})
	}
	dm := &domainmodel.DomainModel{
		BaseElement: model.BaseElement{ID: nextID("dm")},
		ContainerID: mod.ID,
		Entities:    []*domainmodel.Entity{game},
	}
	h := mkHierarchy(mod)
	withContainer(h, dm.ID, mod.ID)
	withContainer(h, game.ContainerID, dm.ID)

	updated := false
	mb := &mock.MockBackend{
		IsConnectedFunc:      func() bool { return true },
		ListModulesFunc:      func() ([]*model.Module, error) { return []*model.Module{mod}, nil },
		ListDomainModelsFunc: func() ([]*domainmodel.DomainModel, error) { return []*domainmodel.DomainModel{dm}, nil },
		GetDomainModelFunc:   func(id model.ID) (*domainmodel.DomainModel, error) { return dm, nil },
		UpdateEntityFunc:     func(dmID model.ID, e *domainmodel.Entity) error { updated = true; return nil },
	}
	ctx, _ := newMockCtx(t, withBackend(mb), withHierarchy(h))
	return ctx, &updated
}

// TestAlterEntityAddAttributeIfNotExists verifies ADD ATTRIBUTE IF NOT EXISTS is
// a no-op (no error) when the attribute already exists, and still adds when it
// doesn't. Findings #10.
func TestAlterEntityAddAttributeIfNotExists(t *testing.T) {
	// Already exists → skipped, no error, no write.
	ctx, updated := idempotentTestCtx(t, "PuzzleNo")
	err := execAlterEntity(ctx, &ast.AlterEntityStmt{
		Name:        ast.QualifiedName{Module: "Sudoku", Name: "Game"},
		Operation:   ast.AlterEntityAddAttribute,
		Attribute:   &ast.Attribute{Name: "PuzzleNo", Type: ast.DataType{Kind: ast.TypeInteger}},
		IfNotExists: true,
	})
	assertNoError(t, err)
	if *updated {
		t.Error("expected no UpdateEntity when the attribute already exists")
	}
	out := ctx.Output.(interface{ String() string }).String()
	if !strings.Contains(out, "already exists") || !strings.Contains(out, "skipped") {
		t.Errorf("expected a skip notice, got: %q", out)
	}

	// Missing → added.
	ctx2, updated2 := idempotentTestCtx(t, "PuzzleNo")
	err = execAlterEntity(ctx2, &ast.AlterEntityStmt{
		Name:        ast.QualifiedName{Module: "Sudoku", Name: "Game"},
		Operation:   ast.AlterEntityAddAttribute,
		Attribute:   &ast.Attribute{Name: "Score", Type: ast.DataType{Kind: ast.TypeInteger}},
		IfNotExists: true,
	})
	assertNoError(t, err)
	if !*updated2 {
		t.Error("expected UpdateEntity when adding a new attribute")
	}
}

// TestAlterEntityAddAttributeDuplicateStillErrors verifies that WITHOUT the
// guard, adding a duplicate attribute is still an error (the guard is opt-in).
func TestAlterEntityAddAttributeDuplicateStillErrors(t *testing.T) {
	ctx, _ := idempotentTestCtx(t, "PuzzleNo")
	err := execAlterEntity(ctx, &ast.AlterEntityStmt{
		Name:      ast.QualifiedName{Module: "Sudoku", Name: "Game"},
		Operation: ast.AlterEntityAddAttribute,
		Attribute: &ast.Attribute{Name: "PuzzleNo", Type: ast.DataType{Kind: ast.TypeInteger}},
	})
	if err == nil {
		t.Fatal("expected an already-exists error without IF NOT EXISTS")
	}
}

// TestAlterEntityDropAttributeIfExists verifies DROP ATTRIBUTE IF EXISTS is a
// no-op (no error) when the attribute is already gone. Findings #10.
func TestAlterEntityDropAttributeIfExists(t *testing.T) {
	ctx, updated := idempotentTestCtx(t, "PuzzleNo")
	err := execAlterEntity(ctx, &ast.AlterEntityStmt{
		Name:          ast.QualifiedName{Module: "Sudoku", Name: "Game"},
		Operation:     ast.AlterEntityDropAttribute,
		AttributeName: "OldCol",
		IfExists:      true,
	})
	assertNoError(t, err)
	if *updated {
		t.Error("expected no UpdateEntity when the attribute is already absent")
	}
	out := ctx.Output.(interface{ String() string }).String()
	if !strings.Contains(out, "not found") || !strings.Contains(out, "skipped") {
		t.Errorf("expected a skip notice, got: %q", out)
	}
}

// TestAlterEntityDropAttributeMissingStillErrors verifies that WITHOUT the guard,
// dropping a missing attribute is still an error.
func TestAlterEntityDropAttributeMissingStillErrors(t *testing.T) {
	ctx, _ := idempotentTestCtx(t, "PuzzleNo")
	err := execAlterEntity(ctx, &ast.AlterEntityStmt{
		Name:          ast.QualifiedName{Module: "Sudoku", Name: "Game"},
		Operation:     ast.AlterEntityDropAttribute,
		AttributeName: "OldCol",
	})
	if err == nil {
		t.Fatal("expected a not-found error without IF EXISTS")
	}
}
