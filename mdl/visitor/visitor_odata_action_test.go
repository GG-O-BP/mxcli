// SPDX-License-Identifier: Apache-2.0

package visitor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
)

// odataActionService parses a service whose body is the given block.
func odataActionService(t *testing.T, body string) *ast.CreateODataServiceStmt {
	t.Helper()
	prog, errs := Build(`create odata service T.Api (
  path: 'odata/t/', version: '1.0.0', ODataVersion: OData4,
  namespace: 'T.Api', ServiceName: 'Api'
)
{
` + body + `
};`)
	if len(errs) > 0 {
		t.Fatalf("parse: %v", errs)
	}
	svc, ok := prog.Statements[0].(*ast.CreateODataServiceStmt)
	if !ok {
		t.Fatalf("expected CreateODataServiceStmt, got %T", prog.Statements[0])
	}
	return svc
}

// mxcli-formula1 §47.1: MDL could not declare an OData action at all.
// `createODataServiceStatement` admitted publishEntityBlock* and nothing else,
// so every variant of `publish microflow …` was a parse error at
// `missing ENTITY` — and the published $metadata had no ActionImport.
func TestPublishMicroflow_Parses(t *testing.T) {
	svc := odataActionService(t,
		"  publish microflow T.RecordPrediction as 'RecordPrediction'\n"+
			"    expose ( DriverId as 'driverId', Points as 'points' );")
	if len(svc.Microflows) != 1 {
		t.Fatalf("expected 1 published microflow, got %d", len(svc.Microflows))
	}
	mf := svc.Microflows[0]
	if mf.Microflow.String() != "T.RecordPrediction" {
		t.Errorf("Microflow = %q", mf.Microflow.String())
	}
	if mf.ExposedName != "RecordPrediction" {
		t.Errorf("ExposedName = %q", mf.ExposedName)
	}
	if len(mf.Parameters) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(mf.Parameters))
	}
	if mf.Parameters[0].Name != "DriverId" || mf.Parameters[0].ExposedName != "driverId" {
		t.Errorf("param 0 = %+v", mf.Parameters[0])
	}
	if mf.Parameters[1].Name != "Points" || mf.Parameters[1].ExposedName != "points" {
		t.Errorf("param 1 = %+v", mf.Parameters[1])
	}
}

// No expose clause means every parameter under its own name — resolved against
// the microflow at execution time, so nothing is restated here.
func TestPublishMicroflow_NoExposeClauseMeansAllParameters(t *testing.T) {
	svc := odataActionService(t, "  publish microflow T.DoThing;")
	if len(svc.Microflows) != 1 {
		t.Fatalf("expected 1 published microflow, got %d", len(svc.Microflows))
	}
	mf := svc.Microflows[0]
	if !mf.ExposeAll {
		t.Error("an omitted expose clause must mean all parameters")
	}
	if len(mf.Parameters) != 0 {
		t.Errorf("no parameters should be selected explicitly: %+v", mf.Parameters)
	}
	if mf.ExposedName != "" {
		t.Errorf("ExposedName = %q, want empty so the executor can default it to the microflow name", mf.ExposedName)
	}
}

// A parameter can be marked optional; Mendix stores that as CanBeEmpty.
func TestPublishMicroflow_CanBeEmptyOption(t *testing.T) {
	svc := odataActionService(t,
		"  publish microflow T.DoThing expose ( Note (CanBeEmpty) );")
	mf := svc.Microflows[0]
	if len(mf.Parameters) != 1 || !mf.Parameters[0].CanBeEmpty {
		t.Errorf("CanBeEmpty was not captured: %+v", mf.Parameters)
	}
}

// Actions and entity sets coexist in one service, which is the normal case:
// the entity sets are the reads and the actions are the writes.
func TestPublishMicroflow_AlongsideEntities(t *testing.T) {
	svc := odataActionService(t,
		"  publish entity T.Row as 'Rows' (\n"+
			"    ReadMode: source, InsertMode: not_supported,\n"+
			"    UpdateMode: not_supported, DeleteMode: not_supported\n"+
			"  )\n"+
			"  expose ( K as 'k' (KEY) );\n"+
			"\n"+
			"  publish microflow T.DoThing as 'DoThing';")
	if len(svc.Entities) != 1 {
		t.Errorf("expected 1 published entity, got %d", len(svc.Entities))
	}
	if len(svc.Microflows) != 1 {
		t.Errorf("expected 1 published microflow, got %d", len(svc.Microflows))
	}
}
