// SPDX-License-Identifier: Apache-2.0

//go:build integration

package executor

import (
	"sort"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/backend"
	modelsdkbackend "github.com/mendixlabs/mxcli/mdl/backend/modelsdk"
	mprbackend "github.com/mendixlabs/mxcli/mdl/backend/mpr"
	"github.com/mendixlabs/mxcli/mdl/visitor"
	"github.com/mendixlabs/mxcli/model"
	"go.mongodb.org/mongo-driver/bson"
)

// parityScript creates a published service and grants it a role. GRANT is a
// separate statement — it cannot be expressed in the service body — which is
// exactly why a writer that drops AllowedModuleRoles is unrecoverable from the
// script.
const parityScript = `
create module PTest;
create entity PTest.Cust (Email: string(200) unique error 'u' required error 'r');
create module role PTest.Admin;

CREATE MICROFLOW PTest.DoThing ($Note: string)
  RETURNS Boolean AS $Ok
BEGIN
  declare $Ok Boolean = true;
  RETURN $Ok;
END;

create odata service PTest.Api (
  path: 'odata/p/', version: '1.0.0', ODataVersion: OData4,
  namespace: 'PTest.Api', ServiceName: 'Api'
)
authentication basic
{
  publish entity PTest.Cust as 'Custs' (
    ReadMode: source, InsertMode: not_supported,
    UpdateMode: not_supported, DeleteMode: not_supported
  )
  expose ( Email as 'email' (KEY) );

  -- An OData action. Both writers must serialize the Microflows part list;
  -- neither referenced PublishedMicroflow before mxcli-formula1 §47.
  publish microflow PTest.DoThing as 'DoThing' expose ( Note as 'note' );
};
grant access on odata service PTest.Api to PTest.Admin;

-- The modify is the load-bearing step. GRANT patches the stored document, so a
-- create+grant alone looks fine on both engines; it is the wholesale
-- re-serialization on modify that deletes whatever the writer omits.
create or modify odata service PTest.Api (
  path: 'odata/p/', version: '1.0.1', ODataVersion: OData4,
  namespace: 'PTest.Api', ServiceName: 'Api'
)
authentication basic
{
  publish entity PTest.Cust as 'Custs' (
    ReadMode: source, InsertMode: not_supported,
    UpdateMode: not_supported, DeleteMode: not_supported
  )
  expose ( Email as 'email' (KEY) );

  publish microflow PTest.DoThing as 'DoThing' expose ( Note as 'note' );
};
`

// storedODataService runs parityScript on a fresh project through the given
// engine and returns the service's stored BSON document.
func storedODataService(t *testing.T, factory func() backend.FullBackend) map[string]any {
	t.Helper()
	env := setupTestEnvWithBackend(t, factory)
	defer env.teardown()

	prog, errs := visitor.Build(parityScript)
	if len(errs) > 0 {
		t.Fatalf("parse: %v", errs[0])
	}
	if err := env.executor.ExecuteProgram(prog); err != nil {
		t.Fatalf("execute: %v", err)
	}

	svcs, err := env.executor.Backend().ListPublishedODataServices()
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	var id model.ID
	for _, s := range svcs {
		if s.Name == "Api" {
			id = s.ID
		}
	}
	if id == "" {
		t.Fatal("the service was not created")
	}
	raw, err := env.executor.Backend().GetRawUnit(id)
	if err != nil {
		t.Fatalf("read raw unit: %v", err)
	}
	return raw
}

// The two OData writers must serialize the same key set.
//
// mxcli-formula1 §41: the §26 role-grant fix went into sdk/mpr/writer_odata.go,
// and the modelsdk writer had no reference to AllowedModuleRoles at all — so a
// `create or modify odata service` on that engine kept revoking the grants for a
// whole release. Nothing noticed, because every existing test exercised one
// writer or the other, never the two against each other.
//
// The doctype gate cannot stand in for this. A published service with no allowed
// roles is 0 errors under `mx check` on Mendix 11.12 (measured, not assumed), so
// the build is blind to the symptom; the loss is only visible in the stored
// document. That is why this asserts on BSON rather than on a check result.
//
// Asserted as the general property, not the one field: whatever legacy writes,
// modelsdk writes too.
func TestODataService_EngineWriteParity(t *testing.T) {
	legacy := storedODataService(t, func() backend.FullBackend { return mprbackend.New() })
	msdk := storedODataService(t, func() backend.FullBackend { return modelsdkbackend.New() })

	var missing []string
	for k := range legacy {
		if _, ok := msdk[k]; !ok {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("modelsdk drops %d key(s) the mpr engine writes: %v\n"+
			"The document is serialized wholesale, so a key the serializer omits is "+
			"not left alone — it is deleted on the next modify.", len(missing), missing)
	}

	// The reported field, by value rather than presence: an empty marker array
	// satisfies a key-set check while still having revoked the grant.
	for name, doc := range map[string]map[string]any{"legacy": legacy, "modelsdk": msdk} {
		roles, ok := doc["AllowedModuleRoles"]
		if !ok {
			t.Errorf("%s: AllowedModuleRoles absent", name)
			continue
		}
		// The two readers hand back the same array under different static
		// types (bson.A on one path, a plain []any on the other), so accept both
		// rather than pinning an incidental difference.
		var arr []any
		switch v := roles.(type) {
		case bson.A:
			arr = v
		case []any:
			arr = v
		default:
			t.Errorf("%s: AllowedModuleRoles is %T, want an array", name, roles)
			continue
		}
		if len(arr) != 2 {
			t.Errorf("%s: AllowedModuleRoles = %v, want marker + the granted role", name, arr)
			continue
		}
		if arr[1] != "PTest.Admin" {
			t.Errorf("%s: granted role = %v, want PTest.Admin", name, arr[1])
		}
	}
}
