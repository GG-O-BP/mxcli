// SPDX-License-Identifier: Apache-2.0

// Check-time (no-project) validation of page-creation order within one script.
// The executor resolves a widget's page reference at the moment it writes the
// page, so a button pointing at a page created further down the same script
// fails partway through `exec` — after earlier statements have already been
// written. `--references` catches this (validateForwardPageRefs), but that needs
// a project; the ordering is a property of the script alone whenever the target
// is created by a plain CREATE, so plain `mxcli check` can catch it too.
// (mxcli-todo findings #9.)
package executor

import (
	"fmt"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/linter"
)

// ValidateScriptPageOrder flags (MDL-PAGE01) a page or snippet whose widgets
// reference a page that a LATER statement creates with a plain CREATE.
//
// The "plain CREATE" condition is what makes this sound without a project: a
// plain CREATE fails if the page already exists, so a script containing one
// asserts the page does not exist yet — the earlier reference therefore cannot
// resolve against the project either. `CREATE OR MODIFY` / `CREATE OR REPLACE`
// carry no such assertion, so those stay for `--references`, which can look.
func ValidateScriptPageOrder(prog *ast.Program) []linter.Violation {
	if prog == nil {
		return nil
	}

	var out []linter.Violation
	for i, stmt := range prog.Statements {
		var widgets []*ast.WidgetV3
		var label string
		switch s := stmt.(type) {
		case *ast.CreatePageStmtV3:
			widgets, label = s.Widgets, "page "+s.Name.String()
		case *ast.CreateSnippetStmtV3:
			widgets, label = s.Widgets, "snippet "+s.Name.String()
		default:
			continue
		}

		refs := &widgetRefCollector{}
		refs.collectFromWidgets(widgets)
		refs.dedupe()
		for _, ref := range refs.pages {
			if createdEarlier(prog, ref, i) || !plainCreateAfter(prog, ref, i) {
				continue
			}
			out = append(out, linter.Violation{
				RuleID:   "MDL-PAGE01",
				Severity: linter.SeverityError,
				Message: fmt.Sprintf(
					"%s references page %s before it is created — the executor resolves page references in statement order, so this fails partway through `exec`",
					label, ref),
				Suggestion: fmt.Sprintf(
					"Move the CREATE PAGE %s statement above this one. If the two pages link to each other, no ordering satisfies both: create one without the linking widget and add it afterwards with ALTER PAGE … INSERT.",
					ref),
			})
		}
	}
	return out
}

// createdEarlier reports whether ref is created by any page statement at an
// index below fromIdx — in which case the reference resolves fine.
func createdEarlier(prog *ast.Program, ref string, fromIdx int) bool {
	for j := 0; j < fromIdx; j++ {
		if s, ok := prog.Statements[j].(*ast.CreatePageStmtV3); ok && s.Name.String() == ref {
			return true
		}
	}
	return false
}

// plainCreateAfter reports whether ref is created after fromIdx by a CREATE that
// is neither OR MODIFY nor OR REPLACE.
func plainCreateAfter(prog *ast.Program, ref string, fromIdx int) bool {
	for j := fromIdx + 1; j < len(prog.Statements); j++ {
		s, ok := prog.Statements[j].(*ast.CreatePageStmtV3)
		if !ok || s.Name.Module == "" || s.Name.String() != ref {
			continue
		}
		if !s.IsModify && !s.IsReplace {
			return true
		}
	}
	return false
}
