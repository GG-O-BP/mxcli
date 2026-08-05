// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
)

// TestFormattingInfoFromParamFormat: a format block starts from the Mendix
// defaults and applies only the keys the user set (ledger #75).
func TestFormattingInfoFromParamFormat(t *testing.T) {
	if fi := formattingInfoFromParamFormat(nil); fi != nil {
		t.Errorf("nil format must yield nil FormattingInfo (keeps default write path), got %+v", fi)
	}
	if fi := formattingInfoFromParamFormat(&ast.ParamFormatV3{}); fi != nil {
		t.Errorf("empty format must yield nil, got %+v", fi)
	}

	f := &ast.ParamFormatV3{Props: []ast.ParamFormatProp{
		{Key: "decimalprecision", Value: "4"},
		{Key: "groupdigits", Value: "true"},
	}}
	fi := formattingInfoFromParamFormat(f)
	if fi == nil {
		t.Fatal("expected FormattingInfo")
	}
	if fi.DecimalPrecision != 4 || !fi.GroupDigits {
		t.Errorf("decimal/group = %d/%v, want 4/true", fi.DecimalPrecision, fi.GroupDigits)
	}
	// Untouched fields keep the defaults (so the writer stays schema-aligned).
	if fi.DateFormat != "Date" || fi.EnumFormat != "Text" {
		t.Errorf("defaults not preserved: DateFormat=%q EnumFormat=%q", fi.DateFormat, fi.EnumFormat)
	}

	dt := formattingInfoFromParamFormat(&ast.ParamFormatV3{Props: []ast.ParamFormatProp{
		{Key: "dateformat", Value: "Custom"}, {Key: "customdateformat", Value: "dd-MM-yyyy"},
	}})
	if dt.DateFormat != "Custom" || dt.CustomDateFormat != "dd-MM-yyyy" {
		t.Errorf("custom date: %q / %q", dt.DateFormat, dt.CustomDateFormat)
	}
}

func dtWidget(params []ast.ParamAssignmentV3, props map[string]any) *ast.WidgetV3 {
	all := map[string]any{"ContentParams": params}
	for k, v := range props {
		all[k] = v
	}
	return &ast.WidgetV3{Name: "txt", Type: "dynamictext", Properties: all}
}

func TestValidateDynamicTextFormatting(t *testing.T) {
	fmtBlock := func(props ...ast.ParamFormatProp) []ast.ParamAssignmentV3 {
		return []ast.ParamAssignmentV3{{Index: 1, Value: "Amount", Format: &ast.ParamFormatV3{Props: props}}}
	}
	tests := []struct {
		name    string
		w       *ast.WidgetV3
		wantSub string // "" = no violations expected
	}{
		{"valid decimal", dtWidget(fmtBlock(
			ast.ParamFormatProp{Key: "decimalprecision", Value: "2"},
			ast.ParamFormatProp{Key: "groupdigits", Value: "true"}), nil), ""},
		{"unknown key", dtWidget(fmtBlock(
			ast.ParamFormatProp{Key: "decimalprecison", Value: "2"}), nil), "unknown format key"},
		{"bad decimal", dtWidget(fmtBlock(
			ast.ParamFormatProp{Key: "decimalprecision", Value: "x"}), nil), "non-negative integer"},
		{"bad dateformat", dtWidget(fmtBlock(
			ast.ParamFormatProp{Key: "dateformat", Value: "Nope"}), nil), "dateFormat must be one of"},
		{"bad enumformat", dtWidget(fmtBlock(
			ast.ParamFormatProp{Key: "enumformat", Value: "Nope"}), nil), "enumFormat must be"},
		{"custom without Custom", dtWidget(fmtBlock(
			ast.ParamFormatProp{Key: "customdateformat", Value: "yyyy"}), nil), "requires `dateFormat: Custom`"},
		{"widget-level format key", dtWidget(nil, map[string]any{"decimalPrecision": 2}), "per-parameter format"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateDynamicTextFormatting(tt.w, "page P")
			if tt.wantSub == "" {
				if len(got) != 0 {
					t.Fatalf("expected no violations, got %d: %+v", len(got), got)
				}
				return
			}
			if len(got) == 0 {
				t.Fatalf("expected a violation containing %q, got none", tt.wantSub)
			}
			found := false
			for _, v := range got {
				if v.RuleID == "MDL-WIDGET18" && strings.Contains(v.Message, tt.wantSub) {
					found = true
				}
			}
			if !found {
				t.Errorf("no MDL-WIDGET18 with %q; got %+v", tt.wantSub, got)
			}
		})
	}
}

func TestFormatParamFormatSuffix(t *testing.T) {
	// Default FormattingInfo → empty suffix (unformatted params round-trip as before).
	def := map[string]any{"FormattingInfo": map[string]any{
		"DecimalPrecision": int64(2), "GroupDigits": false, "DateFormat": "Date", "EnumFormat": "Text", "CustomDateFormat": "",
	}}
	if s := formatParamFormatSuffix(def); s != "" {
		t.Errorf("default FormattingInfo must yield empty suffix, got %q", s)
	}
	non := map[string]any{"FormattingInfo": map[string]any{
		"DecimalPrecision": int64(4), "GroupDigits": true, "DateFormat": "DateTime", "EnumFormat": "Text", "CustomDateFormat": "",
	}}
	s := formatParamFormatSuffix(non)
	for _, want := range []string{" format (", "decimalPrecision: 4", "groupDigits: true", "dateFormat: DateTime"} {
		if !strings.Contains(s, want) {
			t.Errorf("suffix %q missing %q", s, want)
		}
	}
}
