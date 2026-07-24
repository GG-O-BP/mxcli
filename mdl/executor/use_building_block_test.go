// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/backend/mock"
	"github.com/mendixlabs/mxcli/mdl/visitor"
	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/pages"
)

// bbRawWidgets returns a minimal building-block widget tree (a container holding
// a dynamictext) in the raw BSON shape that getBuildingBlockWidgetsFromRaw and
// parseRawWidget expect.
func bbRawWidgets() map[string]any {
	return map[string]any{
		"Widgets": []any{
			map[string]any{
				"$Type":      "Forms$DivContainer",
				"Name":       "container",
				"Appearance": map[string]any{"Class": "card"},
				"Widgets": []any{
					map[string]any{
						"$Type":      "Forms$DynamicText",
						"Name":       "cardtitle",
						"Appearance": map[string]any{"Class": "card-title"},
						"Content": map[string]any{
							"Template": map[string]any{
								"Items": []any{
									map[string]any{"Text": "Card Title", "LanguageCode": "en_US"},
								},
							},
						},
					},
				},
			},
		},
	}
}

// TestExpandBuildingBlock_Mock exercises USE BUILDING BLOCK expansion end to end
// through the executor: resolve the block, render its widgets to MDL, re-parse,
// and apply the prefix rename. Mirrors the fragment expansion tests.
func TestExpandBuildingBlock_Mock(t *testing.T) {
	mod := mkModule("MyModule")
	bb := mkBuildingBlock(mod.ID, "Card")

	h := mkHierarchy(mod)
	withContainer(h, bb.ContainerID, mod.ID)

	mb := &mock.MockBackend{
		IsConnectedFunc: func() bool { return true },
		ListBuildingBlocksFunc: func() ([]*pages.BuildingBlock, error) {
			return []*pages.BuildingBlock{bb}, nil
		},
		GetRawUnitFunc: func(id model.ID) (map[string]any, error) {
			return bbRawWidgets(), nil
		},
	}

	ctx, _ := newMockCtx(t, withBackend(mb), withHierarchy(h))

	pb := &pageBuilder{ctx: ctx, backend: mb}

	w := &ast.WidgetV3{
		Type:       "USE_BUILDING_BLOCK",
		Name:       "MyModule.Card",
		Properties: map[string]interface{}{"Prefix": "p_"},
	}

	result, err := pb.expandIfFragment(w)
	if err != nil {
		t.Fatalf("expandIfFragment failed: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 top-level widget, got %d", len(result))
	}

	// Top-level widget is the prefixed container.
	container := result[0]
	if container.Type != "container" {
		t.Errorf("expected container type, got %q", container.Type)
	}
	if container.Name != "p_container" {
		t.Errorf("expected prefixed container name 'p_container', got %q", container.Name)
	}
	if len(container.Children) != 1 {
		t.Fatalf("expected 1 child widget, got %d", len(container.Children))
	}

	// Child is the prefixed dynamictext copied from the block.
	child := container.Children[0]
	if child.Type != "dynamictext" {
		t.Errorf("expected dynamictext child, got %q", child.Type)
	}
	if child.Name != "p_cardtitle" {
		t.Errorf("expected prefixed child name 'p_cardtitle', got %q", child.Name)
	}
}

// TestExpandBuildingBlock_NoPrefix verifies expansion without an `as` prefix
// keeps the block's original widget names.
func TestExpandBuildingBlock_NoPrefix(t *testing.T) {
	mod := mkModule("MyModule")
	bb := mkBuildingBlock(mod.ID, "Card")

	h := mkHierarchy(mod)
	withContainer(h, bb.ContainerID, mod.ID)

	mb := &mock.MockBackend{
		IsConnectedFunc: func() bool { return true },
		ListBuildingBlocksFunc: func() ([]*pages.BuildingBlock, error) {
			return []*pages.BuildingBlock{bb}, nil
		},
		GetRawUnitFunc: func(id model.ID) (map[string]any, error) {
			return bbRawWidgets(), nil
		},
	}

	ctx, _ := newMockCtx(t, withBackend(mb), withHierarchy(h))
	pb := &pageBuilder{ctx: ctx, backend: mb}

	w := &ast.WidgetV3{
		Type:       "USE_BUILDING_BLOCK",
		Name:       "MyModule.Card",
		Properties: map[string]interface{}{"Prefix": ""},
	}

	result, err := pb.expandIfFragment(w)
	if err != nil {
		t.Fatalf("expandIfFragment failed: %v", err)
	}
	if len(result) != 1 || result[0].Name != "container" {
		t.Fatalf("expected unprefixed 'container', got %+v", result)
	}
}

// TestExpandBuildingBlock_NotFound verifies a missing block errors cleanly.
func TestExpandBuildingBlock_NotFound(t *testing.T) {
	mod := mkModule("MyModule")
	h := mkHierarchy(mod)

	mb := &mock.MockBackend{
		IsConnectedFunc: func() bool { return true },
		ListBuildingBlocksFunc: func() ([]*pages.BuildingBlock, error) {
			return []*pages.BuildingBlock{}, nil
		},
	}

	ctx, _ := newMockCtx(t, withBackend(mb), withHierarchy(h))
	pb := &pageBuilder{ctx: ctx, backend: mb}

	w := &ast.WidgetV3{
		Type:       "USE_BUILDING_BLOCK",
		Name:       "MyModule.Missing",
		Properties: map[string]interface{}{"Prefix": ""},
	}

	_, err := pb.expandIfFragment(w)
	if err == nil {
		t.Fatal("expected error for missing building block")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

// TestUseBuildingBlockParses verifies the grammar+visitor produce the sentinel
// widget with the qualified name and prefix.
func TestUseBuildingBlockParses(t *testing.T) {
	input := `define fragment Wrap as {
		use building block Atlas_Web_Content.Card as cust_
	};`

	prog, errs := visitor.Build(input)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	def, ok := prog.Statements[0].(*ast.DefineFragmentStmt)
	if !ok {
		t.Fatalf("expected DefineFragmentStmt, got %T", prog.Statements[0])
	}
	if len(def.Widgets) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(def.Widgets))
	}
	w := def.Widgets[0]
	if w.Type != "USE_BUILDING_BLOCK" {
		t.Errorf("expected USE_BUILDING_BLOCK, got %q", w.Type)
	}
	if w.Name != "Atlas_Web_Content.Card" {
		t.Errorf("expected 'Atlas_Web_Content.Card', got %q", w.Name)
	}
	if p, _ := w.Properties["Prefix"].(string); p != "cust_" {
		t.Errorf("expected prefix 'cust_', got %q", p)
	}
}
