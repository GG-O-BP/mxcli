// SPDX-License-Identifier: Apache-2.0

package widgetobj

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/mendixlabs/mxcli/mdl/backend"
	"github.com/mendixlabs/mxcli/sdk/pages"
)

// TestSerializeColumnClientTemplateParameterFormattingInfo locks in that a
// DataGrid2 dynamic-text column's ClientTemplateParameter serializes the
// parameter's own FormattingInfo — not a hardcoded default. Before ledger #77
// this serializer ignored param.FormattingInfo and always wrote
// DecimalPrecision:2 / GroupDigits:false / DateFormat:Date, so a
// `format (decimalPrecision: 0, groupDigits: true)` block authored on a column
// param was silently dropped and the cell rendered unformatted.
func TestSerializeColumnClientTemplateParameterFormattingInfo(t *testing.T) {
	t.Run("honors per-parameter FormattingInfo", func(t *testing.T) {
		param := &pages.ClientTemplateParameter{
			AttributeRef: "MyModule.Item.Amount",
			FormattingInfo: &pages.FormattingInfo{
				DecimalPrecision: 0,
				GroupDigits:      true,
				DateFormat:       "Date",
				EnumFormat:       "Text",
			},
		}
		got := SerializeColumnClientTemplateParameter(param)
		fi := findField(t, got, "FormattingInfo").(bson.D)

		if dp := findField(t, fi, "DecimalPrecision"); dp != int64(0) {
			t.Errorf("DecimalPrecision = %v, want 0 (param's own value, not hardcoded 2)", dp)
		}
		if gd := findField(t, fi, "GroupDigits"); gd != true {
			t.Errorf("GroupDigits = %v, want true (param's own value, not hardcoded false)", gd)
		}
	})

	t.Run("custom date format", func(t *testing.T) {
		param := &pages.ClientTemplateParameter{
			AttributeRef: "MyModule.Item.DueOn",
			FormattingInfo: &pages.FormattingInfo{
				DateFormat:       "Custom",
				CustomDateFormat: "yyyy-MM-dd",
				DecimalPrecision: 2,
				EnumFormat:       "Text",
			},
		}
		got := SerializeColumnClientTemplateParameter(param)
		fi := findField(t, got, "FormattingInfo").(bson.D)

		if df := findField(t, fi, "DateFormat"); df != "Custom" {
			t.Errorf("DateFormat = %v, want Custom", df)
		}
		if cdf := findField(t, fi, "CustomDateFormat"); cdf != "yyyy-MM-dd" {
			t.Errorf("CustomDateFormat = %v, want yyyy-MM-dd", cdf)
		}
	})

	t.Run("nil FormattingInfo keeps byte-identical defaults", func(t *testing.T) {
		param := &pages.ClientTemplateParameter{AttributeRef: "MyModule.Item.Name"}
		got := SerializeColumnClientTemplateParameter(param)
		fi := findField(t, got, "FormattingInfo").(bson.D)

		if dp := findField(t, fi, "DecimalPrecision"); dp != int64(2) {
			t.Errorf("DecimalPrecision = %v, want 2 (default)", dp)
		}
		if df := findField(t, fi, "DateFormat"); df != "Date" {
			t.Errorf("DateFormat = %v, want Date (default)", df)
		}
		if gd := findField(t, fi, "GroupDigits"); gd != false {
			t.Errorf("GroupDigits = %v, want false (default)", gd)
		}
		if ef := findField(t, fi, "EnumFormat"); ef != "Text" {
			t.Errorf("EnumFormat = %v, want Text (default)", ef)
		}
	})
}

// TestDynamicTextColumnKind locks in that a DataGrid2 dynamic-text column
// (showContentAs=dynamicText, no attribute, no content widgets) is classified
// as itemKindDynamicText and therefore gets the tooltip empty-ClientTemplate
// convention Studio Pro applies — without it the column's tooltip serialized as
// null and the whole widget failed to load with CE0463 (ledger #77).
func TestDynamicTextColumnKind(t *testing.T) {
	spec := map[string]backend.ObjectListItemProperty{
		"showContentAs": {PropertyKey: "showContentAs", Operation: "primitive", PrimitiveVal: "dynamicText"},
	}
	if k := detectObjectListItemKind(spec, nil); k != itemKindDynamicText {
		t.Fatalf("kind = %q, want %q", k, itemKindDynamicText)
	}
	if !shouldEmitEmptyClientTemplate("com.mendix.widget.web.datagrid.Datagrid", "columns", "tooltip", itemKindDynamicText) {
		t.Error("dynamic-text column tooltip must emit an empty ClientTemplate (Studio Pro convention), not null")
	}
	// exportValue stays null for a dynamic-text column (matches the attribute column).
	if shouldEmitEmptyClientTemplate("com.mendix.widget.web.datagrid.Datagrid", "columns", "exportValue", itemKindDynamicText) {
		t.Error("dynamic-text column exportValue must stay null, not an empty ClientTemplate")
	}
}
