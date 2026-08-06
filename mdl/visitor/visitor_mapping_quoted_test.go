// SPDX-License-Identifier: Apache-2.0

package visitor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
)

// TestImportMappingBody_StripsQuotedIdentifiers guards issue #842.
//
// Quoting identifiers is the documented way to avoid MDL keyword collisions, and
// it is stripped generically everywhere else. Inside a mapping body the entity
// and association names were read with ctx.QualifiedName().GetText(), which
// returns the raw parse text — quotes included — so the reference was stored as
// `ZZB."Routing"` and Mendix reported, on an otherwise clean project:
//
//	[error] [CE1613] "The selected entity 'ZZB."Routing"' no longer exists."
//	[error] [CE1613] "The selected attribute 'ZZB."Routing".RouteId' no longer exists."
//
// The attribute half already came through unquoted (identifierOrKeywordText),
// which is what made the stored name a mix of stripped and unstripped parts.
func TestImportMappingBody_StripsQuotedIdentifiers(t *testing.T) {
	prog, errs := Build(`
create import mapping ZZB."IMM_Route"
  with json structure ZZB."JSON_Route"
{
  create ZZB."Routing" {
    "RouteId" = id,
    "RouteName" = name
  }
};`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	stmt, ok := prog.Statements[0].(*ast.CreateImportMappingStmt)
	if !ok {
		t.Fatalf("statement type = %T, want *ast.CreateImportMappingStmt", prog.Statements[0])
	}
	if got, want := stmt.RootElement.Entity, "ZZB.Routing"; got != want {
		t.Errorf("root entity = %q, want %q — quotes must be stripped like everywhere else", got, want)
	}
	for _, child := range stmt.RootElement.Children {
		if child.Attribute == `"RouteId"` || child.Attribute == `"RouteName"` {
			t.Errorf("attribute %q kept its quotes", child.Attribute)
		}
	}
}

// A nested object element carries both an association and an entity; both are
// read from the same raw-text path and both must be stripped.
func TestImportMappingNestedObject_StripsQuotedIdentifiers(t *testing.T) {
	prog, errs := Build(`
create import mapping ZZB."IMM_Order"
  with json structure ZZB."JSON_Order"
{
  create ZZB."Order" {
    "OrderId" = orderId,
    create ZZB."Order_Line"/ZZB."Line" = items {
      "Sku" = sku
    }
  }
};`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	stmt := prog.Statements[0].(*ast.CreateImportMappingStmt)
	var found bool
	for _, child := range stmt.RootElement.Children {
		if child.Association == "" {
			continue
		}
		found = true
		if child.Association != "ZZB.Order_Line" {
			t.Errorf("association = %q, want ZZB.Order_Line", child.Association)
		}
		if child.Entity != "ZZB.Line" {
			t.Errorf("nested entity = %q, want ZZB.Line", child.Entity)
		}
	}
	if !found {
		t.Fatal("no nested object element found")
	}
}

// Export mapping bodies read the same way and have the same defect.
func TestExportMappingBody_StripsQuotedIdentifiers(t *testing.T) {
	prog, errs := Build(`
create export mapping ZZB."EMM_Route"
  with json structure ZZB."JSON_Route"
{
  ZZB."Routing" {
    id = "RouteId",
    name = "RouteName"
  }
};`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	stmt, ok := prog.Statements[0].(*ast.CreateExportMappingStmt)
	if !ok {
		t.Fatalf("statement type = %T, want *ast.CreateExportMappingStmt", prog.Statements[0])
	}
	if got, want := stmt.RootElement.Entity, "ZZB.Routing"; got != want {
		t.Errorf("export root entity = %q, want %q", got, want)
	}
}
