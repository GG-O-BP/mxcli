// SPDX-License-Identifier: Apache-2.0

package widgets

import (
	"testing"

	"github.com/mendixlabs/mxcli/sdk/widgets/mpk"
)

// Sibling of modelsdk/widgets' test of the same name. AugmentTemplate's early
// return was written for the add/remove work and skipped the value-level
// reconciliation that follows it, so a template whose keys already match the
// installed package was never reconciled at all (mendixlabs/mxcli#716).
//
// This copy carries only syncDefinitionAttrs — the five reconcile passes added
// to modelsdk under #600 (enum values, property metadata, ValueType scalars,
// the AllowUpload envelope, PropertyType order) were never ported here, which
// is why the legacy engine still reports CE0463 on Data Widgets 3.10. Asserting
// the one pass this copy does have keeps the guard honest in both engines.
func TestAugmentTemplate_MatchingKeysStillReconcilesValues(t *testing.T) {
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
	var vt map[string]any
	for _, pt := range objType["PropertyTypes"].([]any) {
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
		t.Errorf("Required = %v, want true — syncDefinitionAttrs did not run", vt["Required"])
	}
}
