// SPDX-License-Identifier: Apache-2.0

package widgets

import (
	"testing"

	"github.com/mendixlabs/mxcli/modelsdk/widgets/mpk"
)

// AugmentTemplate does two unrelated jobs: it syncs the property SET (add/remove
// keys), and it reconciles the VALUES of properties that already match. Only the
// first depends on the sets differing.
//
// A guard written for the add/remove work returned early whenever the sets were
// already in sync, which skipped every value-level pass. Data Widgets 3.10's
// drop-down filter has exactly the same 25 keys as the embedded 11.6-era
// template, so its augmentation silently did nothing and it kept a stale
// Required plus a missing AllowUpload envelope field — CE0463 on every freshly
// authored drop-down filter (mendixlabs/mxcli#716).
//
// The template here deliberately matches the definition key-for-key, so the test
// fails the moment that early return comes back.
func TestAugmentTemplate_MatchingKeysStillReconcilesValues(t *testing.T) {
	ResetPlaceholderCounter()

	tmpl := &WidgetTemplate{
		WidgetID: "test.Widget",
		Type: map[string]any{
			"ObjectType": map[string]any{
				"PropertyTypes": []any{
					float64(2),
					map[string]any{
						"$ID":         "pt0001",
						"$Type":       "CustomWidgets$WidgetPropertyType",
						"PropertyKey": "caption",
						"ValueType": map[string]any{
							"$ID":   "vt0001",
							"$Type": "CustomWidgets$WidgetValueType",
							"Type":  "Attribute",
							// Stale: the installed package declares required="true".
							"Required": false,
							// AllowUpload absent: the template predates the field.
						},
					},
				},
			},
		},
		Object: map[string]any{
			"Properties": []any{
				float64(2),
				map[string]any{
					"$ID":         "p0001",
					"$Type":       "CustomWidgets$WidgetValue",
					"TypePointer": "pt0001",
				},
			},
		},
	}

	// Same key set — nothing to add, nothing to remove, no nested children.
	def := &mpk.WidgetDefinition{
		ID: "test.Widget",
		Properties: []mpk.PropertyDef{
			{Key: "caption", Type: "attribute", Required: true},
		},
	}

	if err := AugmentTemplate(tmpl, def); err != nil {
		t.Fatalf("AugmentTemplate: %v", err)
	}

	objType := tmpl.Type["ObjectType"].(map[string]any)
	propTypes := objType["PropertyTypes"].([]any)

	var vt map[string]any
	for _, pt := range propTypes {
		m, ok := pt.(map[string]any)
		if !ok {
			continue
		}
		if m["PropertyKey"] == "caption" {
			vt, _ = m["ValueType"].(map[string]any)
		}
	}
	if vt == nil {
		t.Fatal("caption PropertyType or its ValueType went missing")
	}

	if req, _ := vt["Required"].(bool); !req {
		t.Errorf("Required = %v, want true — the value-level reconciliation did not run", vt["Required"])
	}
	if _, ok := vt["AllowUpload"]; !ok {
		t.Error("AllowUpload missing — completeValueTypeEnvelope did not run")
	}
}
