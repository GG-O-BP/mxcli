// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/visitor"
)

// mxcli-formula1 findings, suggested issue 8: the OData property switches had
// no default, so a typo was accepted by the parser, dropped by the visitor, and
// the model was quietly missing what the author asked for. The ALTER path has
// always answered "unknown OData service property".
func TestValidateODataProperties(t *testing.T) {
	const prologue = "create module T;\ncreate non-persistent entity T.Row (K: string(20));\n"

	service := func(props, entityProps string) string {
		return prologue + `
create odata service T.Api (
  path: 'odata/t/',
  version: '1.0.0',
  ODataVersion: OData4,
  namespace: 'T.Api'` + props + `
)
authentication basic
{
  publish entity T.Row as 'Rows' (
    ReadMode: source` + entityProps + `
  )
  expose ( K as 'k' (KEY) );
};
`
	}

	tests := []struct {
		name     string
		script   string
		wantN    int
		wantText []string
	}{
		{"clean service", service("", ""), 0, nil},
		{
			"misspelt service property",
			service(",\n  ServiceNam: 'Api'", ""),
			1,
			// The guess is the whole point: the known-property list alone still
			// leaves the reader diffing two spellings by eye.
			[]string{"ServiceNam", "ServiceName"},
		},
		{
			"unknown publish-entity property",
			service("", ",\n    ReadMicroflow: microflow T.Read"),
			1,
			[]string{"ReadMicroflow", "ReadMode"},
		},
		{
			"both at once",
			service(",\n  Pth: 'x'", ",\n    PgSize: 20"),
			2,
			nil,
		},
		// Property matching is case-insensitive in the visitor, so a different
		// casing is NOT a typo and must not be reported. (The finding listed
		// `Pagesize:` as silently dropped; it is not — it is accepted.)
		{"casing is not a typo", service("", ",\n    Pagesize: 20"), 0, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog, errs := visitor.Build(tt.script)
			if len(errs) > 0 {
				t.Fatalf("parsing the script: %v", errs)
			}
			got := ValidateODataProperties(prog)
			if len(got) != tt.wantN {
				t.Fatalf("got %d violations, want %d: %v", len(got), tt.wantN, got)
			}
			for _, want := range tt.wantText {
				found := false
				for _, v := range got {
					if strings.Contains(v.Message+v.Suggestion, want) {
						found = true
					}
				}
				if !found {
					t.Errorf("expected a violation mentioning %q, got: %v", want, got)
				}
			}
			for _, v := range got {
				if v.RuleID != "MDL-ODATA01" {
					t.Errorf("rule = %q, want MDL-ODATA01", v.RuleID)
				}
			}
		})
	}
}

func TestValidateODataProperties_ClientAndExternalEntity(t *testing.T) {
	script := `
create module T;
create odata client T.Api (
  Version: '1.0',
  ODataVersion: OData4,
  MetadataUrl: 'https://example.com/$metadata',
  Timout: 300
);
`
	prog, errs := visitor.Build(script)
	if len(errs) > 0 {
		t.Fatalf("parsing the script: %v", errs)
	}
	got := ValidateODataProperties(prog)
	if len(got) != 1 {
		t.Fatalf("got %d violations, want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0].Suggestion, "Timeout") {
		t.Errorf("expected a Timeout suggestion, got: %s", got[0].Suggestion)
	}
}

func TestClosestProperty(t *testing.T) {
	known := []string{"ReadMode", "InsertMode", "PageSize"}
	tests := []struct{ in, want string }{
		{"ReadMod", "ReadMode"},   // one deletion
		{"Readmodes", "ReadMode"}, // one insertion, different case
		{"PageSiz", "PageSize"},   // prefix
		{"Countable", ""},         // nothing close — no guess is better than a wrong one
		{"", "ReadMode"},          // empty is a prefix of everything; harmless
	}
	for _, tt := range tests {
		if got := closestProperty(tt.in, known); got != tt.want {
			t.Errorf("closestProperty(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
