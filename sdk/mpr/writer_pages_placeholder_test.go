// SPDX-License-Identifier: Apache-2.0

// mendixlabs/mxcli#760: every mxcli-authored page gained a container nobody asked
// for. The builder wrapped each non-empty layout placeholder in a synthetic
// Forms$DivContainer named "conditionalVisibilityWidget<N>", so creating a single
// button produced a button *and* a container.
//
// The wrapper was never a BSON requirement. Forms$FormCallArgument carries a
// `Widgets` array and a Studio Pro page fills it with its top-level widgets
// directly — verified against Mendix's own output: Administration.Account_Overview in
// a `mx create-project` app has two top-level widgets in one placeholder and zero
// wrappers. The wrapper existed only because pages.LayoutCallArgument declared a
// single `Widget` field.
package mpr

import (
	"testing"

	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/pages"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func placeholderArg(widgets ...pages.Widget) *pages.Page {
	return &pages.Page{
		BaseElement: model.BaseElement{ID: "page1"},
		Name:        "P",
		LayoutCall: &pages.LayoutCall{
			BaseElement: model.BaseElement{ID: "lc1"},
			LayoutName:  "Atlas_Core.Atlas_Default",
			Arguments: []*pages.LayoutCallArgument{{
				BaseElement: model.BaseElement{ID: "arg1"},
				ParameterID: model.ID("Atlas_Core.Atlas_Default.Main"),
				Widgets:     widgets,
			}},
		},
	}
}

func argWidgets(t *testing.T, page *pages.Page) primitive.A {
	t.Helper()
	w := &Writer{}
	raw, err := w.serializePage(page)
	if err != nil {
		t.Fatalf("serializePage: %v", err)
	}
	var m map[string]any
	if err := bson.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	fc := toMap(m["FormCall"])
	if fc == nil {
		t.Fatal("FormCall missing")
	}
	args, _ := fc["Arguments"].(primitive.A)
	if len(args) < 2 {
		t.Fatalf("Arguments = %v, want a marker plus one argument", args)
	}
	arg := toMap(args[1])
	ws, _ := arg["Widgets"].(primitive.A)
	return ws
}

func btn(name string) *pages.ActionButton {
	return &pages.ActionButton{BaseWidget: pages.BaseWidget{
		BaseElement: model.BaseElement{ID: model.ID(name), TypeName: "Forms$ActionButton"},
		Name:        name,
	}}
}

// TestLayoutPlaceholder_WidgetsSerializedDirectly is the regression: whatever widgets
// a placeholder holds must reach BSON as-is, with no synthetic container inserted.
func TestLayoutPlaceholder_WidgetsSerializedDirectly(t *testing.T) {
	tests := []struct {
		name    string
		widgets []pages.Widget
		want    int
	}{
		{"single widget", []pages.Widget{btn("b1")}, 1},
		// The case the wrapper was introduced for: the array holds them side by side.
		{"several widgets", []pages.Widget{btn("b1"), btn("b2"), btn("b3")}, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ws := argWidgets(t, placeholderArg(tc.widgets...))
			// First element is the array version marker.
			if got := len(ws) - 1; got != tc.want {
				t.Fatalf("placeholder holds %d widget(s), want %d — a wrapper would collapse them to 1 (#760)", got, tc.want)
			}
			for _, w := range ws[1:] {
				m := toMap(w)
				if m == nil {
					continue
				}
				if ty := extractString(m["$Type"]); ty == "Forms$DivContainer" {
					t.Errorf("a synthetic DivContainer wrapper is back (#760): %v", m["Name"])
				}
			}
		})
	}
}

// An empty placeholder must still emit the empty Widgets array Mendix expects.
func TestLayoutPlaceholder_EmptyStillEmitsWidgets(t *testing.T) {
	ws := argWidgets(t, placeholderArg())
	if len(ws) != 1 {
		t.Fatalf("empty placeholder Widgets = %v, want just the array marker", ws)
	}
}
