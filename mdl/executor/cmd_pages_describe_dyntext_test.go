// SPDX-License-Identifier: Apache-2.0

package executor

import "testing"

// TestUnwrapToStringAttrParam covers the non-string-attribute DESCRIBE round-trip
// fix: a ContentParam auto-generated as toString($currentObject/Attr) (or
// toString($param/Attr)) must render back as the bare attribute (or $param.Attr),
// while any other expression is left untouched.
func TestUnwrapToStringAttrParam(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"toString($currentObject/CountAll)", "CountAll", true},
		{"toString($currentObject/Feedline.Article_Source/Slug)", "Feedline.Article_Source/Slug", true},
		{"toString($View/CountAll)", "$View.CountAll", true},
		// Not the exact auto-generated form → left as-is.
		{"toString($currentObject/CountAll) + ' items'", "", false},
		{"formatDateTime($currentObject/Created, 'yyyy')", "", false},
		{"$currentObject/CountAll", "", false},
		{"'literal'", "", false},
	}
	for _, c := range cases {
		got, ok := unwrapToStringAttrParam(c.in)
		if ok != c.wantOK || (ok && got != c.want) {
			t.Errorf("unwrapToStringAttrParam(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

// TestParseRawWidget_DynamicTextNonStringAttribute drives the describe reader with
// a dynamictext whose ContentParam is the toString(...) form Mendix stores for a
// non-string attribute, and asserts the describe param is the bare attribute name
// (so re-applying the DESCRIBE output doesn't fail CE1613).
func TestParseRawWidget_DynamicTextNonStringAttribute(t *testing.T) {
	ctx, _ := newMockCtx(t)
	raw := map[string]any{
		"$Type": "Forms$DynamicText",
		"Name":  "num",
		"Content": map[string]any{
			"$Type": "Forms$ClientTemplate",
			"Template": map[string]any{
				"$Type": "Texts$Text",
				"Items": []any{
					map[string]any{"$Type": "Texts$Translation", "LanguageCode": "en_US", "Text": "{1}"},
				},
			},
			"Parameters": []any{
				map[string]any{"$Type": "Forms$ClientTemplateParameter", "Expression": "toString($currentObject/CountAll)"},
			},
		},
	}
	got := parseRawWidget(ctx, raw)
	if len(got) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(got))
	}
	if len(got[0].Parameters) != 1 || got[0].Parameters[0] != "CountAll" {
		t.Errorf("ContentParam = %v, want [CountAll] (bare attribute, not the toString expression)", got[0].Parameters)
	}
}
