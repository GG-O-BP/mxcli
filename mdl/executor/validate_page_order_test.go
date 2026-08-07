// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/visitor"
)

// mxcli-todo findings #9: a page whose button targets a page created further
// down the same script passed `mxcli check` and then failed partway through
// `exec` — after earlier statements had already been written to the .mpr.
// `--references` caught it, but only with a project; the ordering is knowable
// from the script alone whenever the target is created by a plain CREATE.
func TestValidateScriptPageOrder(t *testing.T) {
	const board = `
create page ORD.Board
(
  title: 'Board',
  layout: Atlas_Core.Atlas_Default
)
{
  container ctnMain {
    linkbutton btnNew (caption: 'New', action: SHOW_PAGE ORD.TaskEdit)
  }
}
`
	const edit = `
create page ORD.TaskEdit
(
  title: 'Edit',
  layout: Atlas_Core.Atlas_Default
)
{
  container ctnEdit {
    dynamictext txtEdit (content: 'edit')
  }
}
`

	tests := []struct {
		name     string
		script   string
		wantFlag bool
	}{
		{"forward reference to a plain CREATE", "create module ORD;\n" + board + edit, true},
		{"target created first", "create module ORD;\n" + edit + board, false},
		// CREATE OR MODIFY asserts nothing about whether the page already exists,
		// so the reference may well resolve against the project. Only
		// --references, which can look, may judge that one.
		{
			"forward reference to a CREATE OR MODIFY is left alone",
			"create module ORD;\n" + board + strings.Replace(edit, "create page", "create or modify page", 1),
			false,
		},
		{"a page with no page references", "create module ORD;\n" + edit, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog, errs := visitor.Build(tt.script)
			if len(errs) > 0 {
				t.Fatalf("parsing the script: %v", errs)
			}
			got := ValidateScriptPageOrder(prog)
			if tt.wantFlag {
				if len(got) != 1 {
					t.Fatalf("expected exactly 1 violation, got %d: %v", len(got), got)
				}
				if got[0].RuleID != "MDL-PAGE01" {
					t.Errorf("rule = %q, want MDL-PAGE01", got[0].RuleID)
				}
				// The message is only useful if it names both pages and the fix.
				for _, want := range []string{"ORD.Board", "ORD.TaskEdit"} {
					if !strings.Contains(got[0].Message, want) {
						t.Errorf("message should mention %q, got: %s", want, got[0].Message)
					}
				}
				if !strings.Contains(got[0].Suggestion, "ALTER PAGE") {
					t.Errorf("suggestion should cover the cyclic case, got: %s", got[0].Suggestion)
				}
			} else if len(got) != 0 {
				t.Errorf("expected no violations, got: %v", got)
			}
		})
	}
}

func TestValidateScriptPageOrder_NilProgram(t *testing.T) {
	if got := ValidateScriptPageOrder(nil); got != nil {
		t.Errorf("expected nil for a nil program, got %v", got)
	}
}
