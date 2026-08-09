// SPDX-License-Identifier: Apache-2.0

package visitor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
)

// navMenuItems parses a CREATE NAVIGATION with the given menu body and returns
// its top-level items.
func navMenuItems(t *testing.T, menuBody string) []ast.NavMenuItemDef {
	t.Helper()
	prog, errs := Build("CREATE NAVIGATION Responsive MENU (\n" + menuBody + "\n);")
	if len(errs) > 0 {
		t.Fatalf("parsing the menu: %v", errs)
	}
	stmt, ok := prog.Statements[0].(*ast.AlterNavigationStmt)
	if !ok {
		t.Fatalf("expected AlterNavigationStmt, got %T", prog.Statements[0])
	}
	return stmt.MenuItems
}

// mxcli-formula1 §9: a menu item's icon could not be expressed at all. It is a
// reference into the model, so it is written as a qualifiedName like every other
// reference — not as a string. Atlas icon names carry hyphens, which IDENTIFIER
// cannot lex, so that segment is double-quoted the same way a keyword-colliding
// name is.
func TestNavMenuItem_ParsesAQuotedHyphenatedIconSegment(t *testing.T) {
	items := navMenuItems(t,
		`MENU ITEM 'Dashboard' PAGE M.Dash ICON Atlas_Core.Atlas."align-center";`)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Icon != "Atlas_Core.Atlas.align-center" {
		t.Errorf("Icon = %q, want the quotes stripped from the hyphenated segment", items[0].Icon)
	}
	if items[0].Page == nil || items[0].Page.Name != "Dash" {
		t.Error("the ICON clause must not displace the PAGE target")
	}
}

// Most Atlas names are plain identifiers and need no quoting at all — that is
// the common case and it must read like any other reference.
func TestNavMenuItem_ParsesABareIconName(t *testing.T) {
	items := navMenuItems(t, "MENU ITEM 'Dashboard' PAGE M.Dash ICON Atlas_Core.Atlas.home;")
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Icon != "Atlas_Core.Atlas.home" {
		t.Errorf("Icon = %q", items[0].Icon)
	}
	if items[0].Page == nil || items[0].Page.Name != "Dash" {
		t.Error("the ICON clause must not displace the PAGE target")
	}
}

// The sub-menu alternative takes an icon too, before its parenthesised body.
func TestNavMenuItem_ParsesTheIconOnASubMenu(t *testing.T) {
	items := navMenuItems(t,
		"MENU 'Reports' ICON Atlas_Core.Atlas.folder (\n"+
			"  MENU ITEM 'Monthly' PAGE M.Monthly;\n"+
			");")
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Icon != "Atlas_Core.Atlas.folder" {
		t.Errorf("sub-menu Icon = %q", items[0].Icon)
	}
	if len(items[0].Items) != 1 || items[0].Items[0].Caption != "Monthly" {
		t.Errorf("the icon swallowed the sub-items: %+v", items[0].Items)
	}
}

// The clause is optional. The target and the icon are both qualifiedNames in one
// indexed list, so the visitor reads them positionally and has to be pinned
// against mistaking the PAGE target for an icon.
func TestNavMenuItem_NoIconClauseLeavesIconEmpty(t *testing.T) {
	items := navMenuItems(t, "MENU ITEM 'Dashboard' PAGE M.Dash;")
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Icon != "" {
		t.Errorf("Icon = %q, want empty when no ICON clause is written", items[0].Icon)
	}
}

// An icon on an item with no action at all still parses: ICON is independent of
// the optional PAGE/MICROFLOW target.
func TestNavMenuItem_IconWithoutATarget(t *testing.T) {
	items := navMenuItems(t, "MENU ITEM 'Placeholder' ICON Atlas_Core.Atlas.home;")
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Icon != "Atlas_Core.Atlas.home" {
		t.Errorf("Icon = %q", items[0].Icon)
	}
	if items[0].Page != nil || items[0].Microflow != nil {
		t.Error("no target was written, so none must be built")
	}
}
