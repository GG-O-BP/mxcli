// SPDX-License-Identifier: Apache-2.0

package linter_test

import (
	"path/filepath"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/catalog"
	"github.com/mendixlabs/mxcli/mdl/linter"
)

// TestWidgets_ProjectsMicroflowNanoflowRef guards findings #35: CATALOG.WIDGETS
// records the action/datasource flow of a widget, but the linter's Widget
// projection dropped MicroflowRef/NanoflowRef, so a custom rule could not detect
// a microflow-datasource ListView (no database pushdown). The fields must now be
// carried through from the catalog into the Widget struct.
func TestWidgets_ProjectsMicroflowNanoflowRef(t *testing.T) {
	cat, err := catalog.NewFromFile(filepath.Join(t.TempDir(), "cat.db"))
	if err != nil {
		t.Fatalf("NewFromFile: %v", err)
	}
	defer cat.Close()
	db := cat.CatalogDB()

	if _, err := db.Exec(
		`INSERT INTO modules_data (Id, Name, ProjectId, SnapshotId) VALUES (?,?,?,?)`,
		"mod-1", "Sales", "default", "s1",
	); err != nil {
		t.Fatalf("insert module: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO widgets_data
			(Id, Name, WidgetType, ContainerId, ContainerQualifiedName, ContainerType,
			 ModuleName, EntityRef, AttributeRef, MicroflowRef, NanoflowRef, ProjectId, SnapshotId)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"w-1", "lvOrders", "listview", "c-1", "Sales.Order_Overview", "page",
		"Sales", "Sales.Order", "", "Sales.DS_Orders", "", "default", "s1",
	); err != nil {
		t.Fatalf("insert widget: %v", err)
	}

	ctx := linter.NewLintContext(cat, nil)
	var found *linter.Widget
	for w := range ctx.Widgets() {
		if w.ID == "w-1" {
			ww := w
			found = &ww
			break
		}
	}
	if found == nil {
		t.Fatal("widget w-1 not returned by ctx.Widgets()")
	}
	if found.MicroflowRef != "Sales.DS_Orders" {
		t.Errorf("MicroflowRef = %q, want %q", found.MicroflowRef, "Sales.DS_Orders")
	}
	if found.NanoflowRef != "" {
		t.Errorf("NanoflowRef = %q, want empty", found.NanoflowRef)
	}
}
