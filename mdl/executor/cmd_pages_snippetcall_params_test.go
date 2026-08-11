// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/backend/mock"
	"github.com/mendixlabs/mxcli/sdk/pages"
)

// upstream #868: a SNIPPETCALL for a parameterised snippet had no valid spelling.
//
//	omit Params:  → refused, "snippet … requires parameter $Order"
//	give Params:  → accepted, then CE0115 "The arguments that are passed to
//	                snippet … do not match the expected parameters and need to
//	                be refreshed."
//
// Verified against mxbuild 11.13.0, which also shows the bug is narrower than
// reported: passing a REAL page parameter (`params: {Order: $Order}`) builds at
// 0 errors. Only `$currentObject` fails, because it was translated into
//
//	Forms$PageVariable{ PageParameter: "currentObject" }
//
// — a by-name reference to a page parameter that does not exist. "Satisfied by
// the enclosing data context" is not a variable in Mendix; it is the ABSENCE of
// a mapping, which is why Studio Pro's own output has none and why its "Refresh
// snippet parameters" removes what mxcli wrote.
func newSnippetCallTestBuilder(t *testing.T, entityContext string) *pageBuilder {
	t.Helper()
	snippet := &pages.Snippet{
		Name: "S868.OrderActions",
		Parameters: []*pages.SnippetParameter{
			{Name: "Order", EntityName: "S868.Order"},
		},
	}
	return &pageBuilder{
		entityContext: entityContext,
		backend: &mock.MockBackend{
			ListSnippetsFunc: func() ([]*pages.Snippet, error) {
				return []*pages.Snippet{snippet}, nil
			},
		},
	}
}

func TestSnippetCallParams_ContextSatisfiedEmitsNoMapping(t *testing.T) {
	tests := []struct {
		name     string
		supplied []ast.SnippetCallParam
	}{
		{
			name:     "Params omitted entirely",
			supplied: nil,
		},
		{
			name:     "$currentObject named explicitly",
			supplied: []ast.SnippetCallParam{{ParamName: "Order", Variable: "$currentObject"}},
		},
		{
			name:     "currentObject without the $ sigil",
			supplied: []ast.SnippetCallParam{{ParamName: "Order", Variable: "currentObject"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pb := newSnippetCallTestBuilder(t, "S868.Order")
			sc := &pages.SnippetCallWidget{}
			if err := pb.buildSnippetCallParams(sc, "S868.OrderActions", tc.supplied); err != nil {
				t.Fatalf("buildSnippetCallParams: %v", err)
			}
			if len(sc.ParameterMappings) != 0 {
				t.Fatalf("ParameterMappings = %+v, want none — a context-satisfied parameter "+
					"has no mapping in Mendix, and inventing one is CE0115", sc.ParameterMappings)
			}
		})
	}
}

// A real variable still produces a real mapping — that form already built clean
// and must not regress.
func TestSnippetCallParams_RealVariableStillMaps(t *testing.T) {
	pb := newSnippetCallTestBuilder(t, "")
	sc := &pages.SnippetCallWidget{}
	err := pb.buildSnippetCallParams(sc, "S868.OrderActions",
		[]ast.SnippetCallParam{{ParamName: "Order", Variable: "$Order"}})
	if err != nil {
		t.Fatalf("buildSnippetCallParams: %v", err)
	}
	if len(sc.ParameterMappings) != 1 {
		t.Fatalf("ParameterMappings = %+v, want exactly one", sc.ParameterMappings)
	}
	if sc.ParameterMappings[0].ParamName != "Order" || sc.ParameterMappings[0].Argument != "$Order" {
		t.Errorf("mapping = %+v, want {Order, $Order}", sc.ParameterMappings[0])
	}
}

// Omitting Params with NO enclosing context is still an authoring mistake: there
// is nothing to satisfy the parameter, so keep the guidance rather than writing
// an empty list and letting mxbuild explain it later.
func TestSnippetCallParams_NoContextStillRequiresParams(t *testing.T) {
	pb := newSnippetCallTestBuilder(t, "")
	sc := &pages.SnippetCallWidget{}
	err := pb.buildSnippetCallParams(sc, "S868.OrderActions", nil)
	if err == nil {
		t.Fatal("omitting Params outside any data context was accepted; want the requires-parameter error")
	}
	if !strings.Contains(err.Error(), "Order") {
		t.Errorf("error %q does not name the unsatisfied parameter", err)
	}
}

// A context of the WRONG entity cannot satisfy the parameter either.
func TestSnippetCallParams_MismatchedContextRequiresParams(t *testing.T) {
	pb := newSnippetCallTestBuilder(t, "S868.Customer")
	sc := &pages.SnippetCallWidget{}
	if err := pb.buildSnippetCallParams(sc, "S868.OrderActions", nil); err == nil {
		t.Fatal("a context of a different entity was accepted as satisfying the parameter")
	}
}
