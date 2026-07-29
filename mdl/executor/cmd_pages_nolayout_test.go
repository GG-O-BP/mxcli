// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
)

// TestBuildPageV3_WidgetsWithoutLayout guards the layout-drop bug: a page with body
// widgets but no Layout: clause has no LayoutCall to place them into, so the widgets
// were silently dropped and Mendix rejected the page (CE1613). It must now be a clear
// error instead.
func TestBuildPageV3_WidgetsWithoutLayout(t *testing.T) {
	s := &ast.CreatePageStmtV3{
		Name: ast.QualifiedName{Module: "M", Name: "NoLayout"},
		Widgets: []*ast.WidgetV3{
			{Type: "container", Name: "c1", Children: []*ast.WidgetV3{
				{Type: "dynamictext", Name: "dt", Properties: map[string]any{"Content": "x"}},
			}},
		},
	}
	_, err := newPopupPageBuilder().buildPageV3(s)
	if err == nil {
		t.Fatal("expected an error for a page with widgets but no Layout:, got nil (widgets would be dropped)")
	}
	if !strings.Contains(err.Error(), "no Layout") {
		t.Errorf("error = %q, want it to mention the missing Layout: clause", err.Error())
	}
}

// TestBuildPageV3_NoWidgetsNoLayout confirms the guard does NOT fire for an empty
// page (no widgets to drop) — matching the existing popup-defaults test behavior.
func TestBuildPageV3_NoWidgetsNoLayout(t *testing.T) {
	s := &ast.CreatePageStmtV3{Name: ast.QualifiedName{Module: "M", Name: "Empty"}}
	if _, err := newPopupPageBuilder().buildPageV3(s); err != nil {
		t.Errorf("empty layout-less page should build without error, got %v", err)
	}
}
