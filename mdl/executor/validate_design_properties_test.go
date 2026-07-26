// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/visitor"
)

// testThemeRegistry is a small in-memory registry: a Dropdown, a ColorPicker,
// and a Toggle on DivContainer (what MDL `container` resolves to).
func testThemeRegistry() *ThemeRegistry {
	return &ThemeRegistry{WidgetProperties: map[string][]ThemeProperty{
		"DivContainer": {
			{Name: "Background color", Type: "Dropdown", Options: []ThemeOption{{Name: "Brand Primary"}, {Name: "Default"}}},
			{Name: "Text alignment", Type: "ToggleButtonGroup", Options: []ThemeOption{{Name: "Left"}, {Name: "Center"}, {Name: "Right"}}},
			{Name: "Card style", Type: "Toggle"},
		},
	}}
}

// TestAstDesignPropToValue_Typed verifies the write path picks the BSON value
// type from the theme registry (ColorPicker/ToggleButtonGroup → custom) rather
// than always defaulting flat values to option. Typed design properties.
func TestAstDesignPropToValue_Typed(t *testing.T) {
	props := []ThemeProperty{
		{Name: "Background color", Type: "Dropdown"},
		{Name: "Text alignment", Type: "ToggleButtonGroup"},
		{Name: "Accent", Type: "ColorPicker"},
	}
	cases := []struct {
		key, val, wantType string
	}{
		{"Background color", "Brand Primary", "option"},
		{"Text alignment", "Center", "custom"},
		{"Accent", "#ff0000", "custom"},
		{"Unknown Key", "x", "option"}, // not in registry → default option
	}
	for _, c := range cases {
		dp, ok := astDesignPropToValue(ast.DesignPropertyEntryV3{Key: c.key, Value: c.val}, props)
		if !ok {
			t.Fatalf("%s: expected ok", c.key)
		}
		if dp.ValueType != c.wantType {
			t.Errorf("%s=%q: ValueType=%q, want %q", c.key, c.val, dp.ValueType, c.wantType)
		}
	}
	// on/off still map to toggle/skip regardless of metadata.
	if dp, ok := astDesignPropToValue(ast.DesignPropertyEntryV3{Key: "Card style", Value: "on"}, props); !ok || dp.ValueType != "toggle" {
		t.Errorf("on → toggle, got %+v ok=%v", dp, ok)
	}
	if _, ok := astDesignPropToValue(ast.DesignPropertyEntryV3{Key: "Card style", Value: "off"}, props); ok {
		t.Error("off should be skipped")
	}
	// Without metadata the flat default stays option (backward compatible).
	if dp, _ := astDesignPropToValue(ast.DesignPropertyEntryV3{Key: "Text alignment", Value: "Center"}, nil); dp.ValueType != "option" {
		t.Errorf("no metadata → option, got %q", dp.ValueType)
	}
}

func designPropViolations(t *testing.T, src string, reg *ThemeRegistry) map[string]string {
	t.Helper()
	prog, errs := visitor.Build(src)
	if len(errs) > 0 {
		t.Fatalf("parse error: %v", errs[0])
	}
	out := map[string]string{}
	for _, stmt := range prog.Statements {
		for _, v := range ValidateDesignPropertiesForStatement(stmt, reg) {
			out[v.RuleID] = v.Message + " || " + v.Suggestion
		}
	}
	return out
}

// TestValidateDesignProperties_UnknownKeyAndBadValue covers MDL-WIDGET11 (unknown
// design-property key) and MDL-WIDGET12 (value not allowed, listing allowed values).
func TestValidateDesignProperties_UnknownKeyAndBadValue(t *testing.T) {
	reg := testThemeRegistry()

	// Unknown key.
	v := designPropViolations(t, `create page M.P (layout: Atlas_Core.Atlas_Default) {
  container c1 (designproperties: ['Nonexistent': 'x']) {}
}`, reg)
	if _, ok := v["MDL-WIDGET11"]; !ok {
		t.Errorf("expected MDL-WIDGET11 for unknown key, got %v", v)
	}

	// Bad value on a known Dropdown → MDL-WIDGET12 listing allowed values.
	v = designPropViolations(t, `create page M.P (layout: Atlas_Core.Atlas_Default) {
  container c1 (designproperties: ['Background color': 'Bogus']) {}
}`, reg)
	msg, ok := v["MDL-WIDGET12"]
	if !ok {
		t.Fatalf("expected MDL-WIDGET12 for bad value, got %v", v)
	}
	if !strings.Contains(msg, "Brand Primary") || !strings.Contains(msg, "Default") {
		t.Errorf("expected allowed values listed, got: %s", msg)
	}

	// Valid key + valid value + valid toggle → no violations.
	v = designPropViolations(t, `create page M.P (layout: Atlas_Core.Atlas_Default) {
  container c1 (designproperties: ['Background color': 'Brand Primary', 'Card style': on]) {}
}`, reg)
	if len(v) != 0 {
		t.Errorf("expected no violations for valid design properties, got %v", v)
	}
}

// TestValidateDesignProperties_WrongCaseHint verifies the case-sensitivity hint.
func TestValidateDesignProperties_WrongCaseHint(t *testing.T) {
	reg := testThemeRegistry()
	v := designPropViolations(t, `create page M.P (layout: Atlas_Core.Atlas_Default) {
  container c1 (designproperties: ['card style': on]) {}
}`, reg)
	msg, ok := v["MDL-WIDGET11"]
	if !ok {
		t.Fatalf("expected MDL-WIDGET11 for wrong-case key, got %v", v)
	}
	if !strings.Contains(msg, "case-sensitive") || !strings.Contains(msg, "Card style") {
		t.Errorf("expected case-sensitivity hint naming 'Card style', got: %s", msg)
	}
}

// TestValidateDesignProperties_UnknownWidgetSkipped verifies a widget type with
// no registry metadata (e.g. a pluggable widget) is not flagged.
func TestValidateDesignProperties_UnknownWidgetSkipped(t *testing.T) {
	// Registry only knows DataGrid; the container has no entry → skip.
	prog, errs := visitor.Build(`create page M.P (layout: Atlas_Core.Atlas_Default) {
  container c1 (designproperties: ['Something': 'x']) {}
}`)
	if len(errs) > 0 {
		t.Fatalf("parse error: %v", errs[0])
	}
	// Swap the widget's resolved key out of the registry by using an empty registry.
	empty := &ThemeRegistry{WidgetProperties: map[string][]ThemeProperty{"DataGrid": {{Name: "X", Type: "Toggle"}}}}
	var n int
	for _, stmt := range prog.Statements {
		n += len(ValidateDesignPropertiesForStatement(stmt, empty))
	}
	if n != 0 {
		t.Errorf("expected no violations when widget type has no registry metadata, got %d", n)
	}
}
