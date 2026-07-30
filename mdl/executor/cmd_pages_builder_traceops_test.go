// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/model"
)

func newBuilderForWidgetTest() *pageBuilder {
	return &pageBuilder{
		widgetScope:      map[string]model.ID{},
		paramScope:       map[string]model.ID{},
		paramEntityNames: map[string]string{},
		localVariables:   map[string]bool{},
	}
}

// TestBuildDynamicTextV3_EmptyContent guards traceops #9: `Content: ”` must
// produce an EMPTY template with no parameters — not an orphaned `{1}`
// placeholder, which Mendix rejects with CE0720 ("Place holder index 1 is
// greater than 0").
func TestBuildDynamicTextV3_EmptyContent(t *testing.T) {
	pb := newBuilderForWidgetTest()
	w := &ast.WidgetV3{Name: "x1", Type: "dynamictext", Properties: map[string]any{"Content": ""}}
	dt, err := pb.buildDynamicTextV3(w)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := dt.Content.Template.Translations["en_US"]; got != "" {
		t.Errorf("empty Content template = %q, want \"\" (no orphaned {1})", got)
	}
	if len(dt.Content.Parameters) != 0 {
		t.Errorf("empty Content params = %d, want 0", len(dt.Content.Parameters))
	}
}

// TestBuildDynamicTextV3_DollarDigitLiteral guards traceops #10: `Content: '$318'`
// (a `$` followed by digits) is NOT a valid variable reference and must be kept
// as literal content — not turned into an unbound `{1}` parameter (which failed
// the build with CE0402/CE1613 "attribute '$318' no longer exists").
func TestBuildDynamicTextV3_DollarDigitLiteral(t *testing.T) {
	pb := newBuilderForWidgetTest()
	w := &ast.WidgetV3{Name: "s4v", Type: "dynamictext", Properties: map[string]any{"Content": "$318"}}
	dt, err := pb.buildDynamicTextV3(w)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := dt.Content.Template.Translations["en_US"]; got != "$318" {
		t.Errorf("literal Content template = %q, want \"$318\"", got)
	}
	if len(dt.Content.Parameters) != 0 {
		t.Errorf("literal '$318' params = %d, want 0 (not an unbound {1})", len(dt.Content.Parameters))
	}
}

// TestBuildDynamicTextV3_VariableStillBinds ensures the #10 fix didn't break a
// genuine variable reference: `$currentObject/Attr` and `$var` must still
// auto-generate a `{1}` template with the value as a parameter.
func TestBuildDynamicTextV3_VariableStillBinds(t *testing.T) {
	pb := newBuilderForWidgetTest()
	pb.localVariables["v"] = true
	w := &ast.WidgetV3{Name: "d", Type: "dynamictext", Properties: map[string]any{"Content": "$v"}}
	dt, err := pb.buildDynamicTextV3(w)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := dt.Content.Template.Translations["en_US"]; got != "{1}" {
		t.Errorf("variable Content template = %q, want \"{1}\"", got)
	}
	if len(dt.Content.Parameters) != 1 {
		t.Errorf("variable Content params = %d, want 1", len(dt.Content.Parameters))
	}
}

func TestIsDynamicTextVariableRef(t *testing.T) {
	cases := map[string]bool{
		"$var":                  true,
		"$widget.Attr":          true,
		"$currentObject/A/Name": true,
		"$_x":                   true,
		"$318":                  false, // dollar + digit — literal, not a variable (#10)
		"$":                     false,
		"plain":                 false,
		"":                      false,
	}
	for in, want := range cases {
		if got := isDynamicTextVariableRef(in); got != want {
			t.Errorf("isDynamicTextVariableRef(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestBuildListViewV3_PageSize guards traceops #17: an explicit PageSize must be
// honored, not silently discarded (the writer hardcoded 20 regardless of the
// parsed AST value).
func TestBuildListViewV3_PageSize(t *testing.T) {
	pb := newBuilderForWidgetTest()
	w := &ast.WidgetV3{Name: "lv", Type: "listview", Properties: map[string]any{"PageSize": 200}}
	lv, err := pb.buildListViewV3(w)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if lv.PageSize != 200 {
		t.Errorf("PageSize = %d, want 200", lv.PageSize)
	}

	// No PageSize property → default 20.
	pb2 := newBuilderForWidgetTest()
	w2 := &ast.WidgetV3{Name: "lv2", Type: "listview", Properties: map[string]any{}}
	lv2, err := pb2.buildListViewV3(w2)
	if err != nil {
		t.Fatalf("build default: %v", err)
	}
	if lv2.PageSize != 20 {
		t.Errorf("default PageSize = %d, want 20", lv2.PageSize)
	}
}
