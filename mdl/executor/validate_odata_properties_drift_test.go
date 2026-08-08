// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/visitor"
)

// MDL-ODATA01's hint listed six publish-entity properties long after the visitor
// grew three more (Countable / SkipSupported / TopSupported), so the message told
// users that accepted properties were unknown. The lists are separate by design —
// the visitor decides, the hint only displays — but they must not drift, and
// nothing was checking (mxcli-formula1 #15).
//
// The visitor is the authority: every name the hint advertises is fed through it
// and must not come back as unknown.
func TestKnownODataProps_MatchTheVisitor(t *testing.T) {
	cases := []struct {
		what  string
		props []string
		build func(prop string) string
	}{
		{"odata service", knownODataServiceProps, func(p string) string {
			return fmt.Sprintf("create odata service M.S (%s: 'x');", p)
		}},
		{"publish entity", knownPublishEntityProps, func(p string) string {
			return fmt.Sprintf("create odata service M.S (Path: 'p/')\n{\n  publish entity M.E as 'Es' (%s: 'x')\n  expose (A);\n};", p)
		}},
		{"odata client", knownODataClientProps, func(p string) string {
			return fmt.Sprintf("create odata client M.C (%s: 'x');", p)
		}},
		{"external entity", knownExternalEntityProps, func(p string) string {
			return fmt.Sprintf("create external entity M.E from odata client M.C (%s: 'x');", p)
		}},
	}

	for _, tc := range cases {
		for _, prop := range tc.props {
			t.Run(tc.what+"/"+prop, func(t *testing.T) {
				prog, errs := visitor.Build(tc.build(prop))
				if len(errs) > 0 {
					t.Fatalf("%q does not parse in a %s: %v", prop, tc.what, errs)
				}
				if unknown := collectUnknownProps(prog); len(unknown) > 0 {
					t.Errorf("the hint advertises %q but the visitor discards it (unknown: %v)", prop, unknown)
				}
			})
		}
	}
}

// The direction that actually broke: a property the AST carries but the hint does
// not advertise. Countable/SkipSupported/TopSupported were added as fields, the
// visitor learned to set them, and the hint was never updated — so the message
// told users three accepted properties were unknown.
//
// The AST struct is the single source here: every field is a property unless it
// is listed as structural, so adding one and forgetting the hint fails this test
// rather than shipping a wrong message.
func TestKnownODataProps_CoverEveryASTField(t *testing.T) {
	cases := []struct {
		what       string
		typ        reflect.Type
		advertised []string
		structural map[string]bool
	}{
		{
			"publish entity", reflect.TypeOf(ast.PublishedEntityDef{}), knownPublishEntityProps,
			map[string]bool{"Entity": true, "ExposedName": true, "Members": true, "UnknownProperties": true},
		},
		{
			"external entity", reflect.TypeOf(ast.CreateExternalEntityStmt{}), knownExternalEntityProps,
			map[string]bool{
				"Name": true, "ServiceRef": true, "Attributes": true, "Documentation": true,
				"CreateOrModify": true, "UnknownProperties": true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.what, func(t *testing.T) {
			have := map[string]bool{}
			for _, p := range tc.advertised {
				have[strings.ToLower(p)] = true
			}
			for i := 0; i < tc.typ.NumField(); i++ {
				f := tc.typ.Field(i)
				if tc.structural[f.Name] || !f.IsExported() {
					continue
				}
				// Flags that only record whether a sibling was set are not
				// properties in their own right.
				if strings.HasSuffix(f.Name, "IsLiteral") || strings.HasSuffix(f.Name, "Set") ||
					strings.HasSuffix(f.Name, "IsExpression") {
					continue
				}
				if !have[strings.ToLower(f.Name)] {
					t.Errorf("%s carries %s but MDL-ODATA01 does not advertise it — "+
						"a user typing that property is told it is unknown", tc.what, f.Name)
				}
			}
		})
	}
}

// The converse: a name nothing accepts must still be reported, or the test above
// would pass against a visitor that silently swallowed everything.
func TestKnownODataProps_UnknownIsStillFlagged(t *testing.T) {
	prog, errs := visitor.Build("create odata client M.C (NotAProperty: 'x');")
	if len(errs) > 0 {
		t.Fatalf("parse: %v", errs)
	}
	unknown := collectUnknownProps(prog)
	if len(unknown) != 1 || !strings.EqualFold(unknown[0], "NotAProperty") {
		t.Errorf("unknown = %v, want [NotAProperty]", unknown)
	}
}

func collectUnknownProps(prog *ast.Program) []string {
	var out []string
	for _, stmt := range prog.Statements {
		switch s := stmt.(type) {
		case *ast.CreateODataServiceStmt:
			out = append(out, s.UnknownProperties...)
			for _, e := range s.Entities {
				if e != nil {
					out = append(out, e.UnknownProperties...)
				}
			}
		case *ast.CreateODataClientStmt:
			out = append(out, s.UnknownProperties...)
		case *ast.CreateExternalEntityStmt:
			out = append(out, s.UnknownProperties...)
		}
	}
	return out
}
