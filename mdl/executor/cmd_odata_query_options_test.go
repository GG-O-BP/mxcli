// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/visitor"

	"github.com/mendixlabs/mxcli/mdl/ast"
)

// mxcli-formula1 findings #10.3: Countable, SkipSupported and TopSupported were
// hardcoded true in the BSON writer with no MDL to turn any of them off. That is
// not a cosmetic gap — Countable forces every read-microflow-backed resource to
// declare a System.ODataResponse parameter and compute a count, which over a
// 27533-row CSV scan is not free.
func TestPublishEntityQueryOptions_ParseAndCarry(t *testing.T) {
	script := `
create module Q;
create non-persistent entity Q.Row (K: string(20));
create odata service Q.Api (
  path: 'odata/q/',
  version: '1.0.0',
  ODataVersion: OData4,
  namespace: 'Q.Api'
)
authentication basic
{
  publish entity Q.Row as 'Rows' (
    ReadMode: microflow Q.Read,
    Countable: No,
    TopSupported: No
  )
  expose ( K as 'k' (KEY) );
};
`
	prog, errs := visitor.Build(script)
	if len(errs) > 0 {
		t.Fatalf("parsing the script: %v", errs)
	}

	var def *ast.PublishedEntityDef
	for _, stmt := range prog.Statements {
		if s, ok := stmt.(*ast.CreateODataServiceStmt); ok && len(s.Entities) == 1 {
			def = s.Entities[0]
		}
	}
	if def == nil {
		t.Fatal("expected one published entity")
	}

	if def.Countable == nil || *def.Countable {
		t.Errorf("Countable = %v, want an explicit false", def.Countable)
	}
	if def.TopSupported == nil || *def.TopSupported {
		t.Errorf("TopSupported = %v, want an explicit false", def.TopSupported)
	}
	// Unmentioned stays nil, which the writer turns into Mendix's default of
	// true — distinguishing "unset" from "false" is the whole point.
	if def.SkipSupported != nil {
		t.Errorf("SkipSupported = %v, want nil for an unmentioned property", *def.SkipSupported)
	}

	// And the executor carries them onto the entity set it builds.
	_, es := astEntityDefToModel(nil, def)
	if es.Countable == nil || *es.Countable {
		t.Errorf("entity set Countable = %v, want an explicit false", es.Countable)
	}
	if es.SkipSupported != nil {
		t.Error("entity set SkipSupported should stay unset")
	}
}
