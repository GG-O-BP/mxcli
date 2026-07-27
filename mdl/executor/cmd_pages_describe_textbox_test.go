// SPDX-License-Identifier: Apache-2.0

package executor

import "testing"

// TestParseRawWidget_TextBoxPlaceholderAndOnChange covers the DESCRIBE round-trip
// gap: a textbox's PlaceholderTemplate and OnChangeAction must be read back into
// the describe model (previously only Label + Attribute were), so
// `describe page` reflects what `create page` wrote.
func TestParseRawWidget_TextBoxPlaceholderAndOnChange(t *testing.T) {
	ctx, _ := newMockCtx(t)

	raw := map[string]any{
		"$Type": "Forms$TextBox",
		"Name":  "txtQuery",
		// PlaceholderTemplate is a Forms$ClientTemplate: Template.Items[] holds the translation.
		"PlaceholderTemplate": map[string]any{
			"$Type": "Forms$ClientTemplate",
			"Template": map[string]any{
				"$Type": "Texts$Text",
				"Items": []any{
					map[string]any{"$Type": "Texts$Translation", "LanguageCode": "en_US", "Text": "Search all articles"},
				},
			},
		},
		// OnChangeAction is a client action, same shape a button's Action uses:
		// Forms$MicroflowAction with the microflow name under MicroflowSettings.
		"OnChangeAction": map[string]any{
			"$Type": "Forms$MicroflowAction",
			"MicroflowSettings": map[string]any{
				"$Type":     "Forms$MicroflowSettings",
				"Microflow": "MyFirstModule.ACT_Search",
			},
		},
	}

	got := parseRawWidget(ctx, raw)
	if len(got) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(got))
	}
	w := got[0]
	if w.Placeholder != "Search all articles" {
		t.Errorf("Placeholder = %q, want %q", w.Placeholder, "Search all articles")
	}
	if w.OnChange == "" {
		t.Errorf("OnChange should render the client action, got empty")
	}
}
