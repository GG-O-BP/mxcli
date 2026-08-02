// SPDX-License-Identifier: Apache-2.0

package modelsdkbackend

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/dbconnector"
	"github.com/mendixlabs/mxcli/model"
	"go.mongodb.org/mongo-driver/bson"

	_ "modernc.org/sqlite"
)

// TestCreateDatabaseConnection_QueryTypeSpellingFollowsVersion is the wiring proof
// for the Mendix 11.13 query-type rename. The encoding itself is covered by the
// unit tests in mdl/dbconnector; what this asserts is that the project's version
// actually reaches the writer — a correct mapping nothing consults would still
// have produced the CE5277 that made the 11.13 nightly red.
func TestCreateDatabaseConnection_QueryTypeSpellingFollowsVersion(t *testing.T) {
	tests := []struct {
		name       string
		productVer string
		wantKey    string
		wantValue  any
		absentKey  string
	}{
		{
			name:       "mendix_11_6_writes_legacy_int",
			productVer: "11.6.6",
			wantKey:    dbconnector.QueryTypeKey,
			wantValue:  int64(dbconnector.CustomSQLQueryType),
			absentKey:  dbconnector.TypeKey,
		},
		{
			name:       "mendix_11_13_writes_type_enum",
			productVer: "11.13.0",
			wantKey:    dbconnector.TypeKey,
			wantValue:  dbconnector.TypeSelect,
			absentKey:  dbconnector.QueryTypeKey,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			proj := copyFixture(t)
			setProductVersion(t, proj, tc.productVer)

			b := New()
			if err := b.Connect(proj); err != nil {
				t.Fatalf("connect: %v", err)
			}
			mod, err := b.GetModuleByName("MyFirstModule")
			if err != nil || mod == nil {
				t.Fatalf("GetModuleByName: %v", err)
			}
			conn := &model.DatabaseConnection{
				ContainerID:  mod.ID,
				Name:         "TestDB",
				DatabaseType: "PostgreSQL",
				Queries: []*model.DatabaseQuery{{
					Name:      "GetAll",
					SQL:       "SELECT id FROM t",
					QueryType: dbconnector.CustomSQLQueryType,
				}},
			}
			if err := b.CreateDatabaseConnection(conn); err != nil {
				t.Fatalf("CreateDatabaseConnection: %v", err)
			}
			if err := b.Disconnect(); err != nil {
				t.Fatalf("disconnect: %v", err)
			}

			q := readFirstQuery(t, proj, string(conn.ID))
			if got, ok := q[tc.wantKey]; !ok || got != tc.wantValue {
				t.Errorf("%s = %#v (present=%v), want %#v", tc.wantKey, got, ok, tc.wantValue)
			}
			if v, ok := q[tc.absentKey]; ok {
				t.Errorf("wrote %s = %#v; Mendix %s stores %s",
					tc.absentKey, v, tc.productVer, tc.wantKey)
			}
		})
	}
}

// setProductVersion rewrites the fixture's recorded Mendix version, standing in
// for a project created by that release.
func setProductVersion(t *testing.T, proj, ver string) {
	t.Helper()
	db, err := sql.Open("sqlite", proj)
	if err != nil {
		t.Fatalf("open mpr: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("UPDATE _MetaData SET _ProductVersion = ?", ver); err != nil {
		t.Fatalf("set _ProductVersion: %v", err)
	}
}

// readFirstQuery returns the first Queries entry of a stored DatabaseConnection.
func readFirstQuery(t *testing.T, proj, unitID string) map[string]any {
	t.Helper()
	raw := readUnitBytes(t, proj, unitID)
	var doc bson.M
	if err := bson.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal unit: %v", err)
	}
	arr, ok := doc["Queries"].(bson.A)
	if !ok || len(arr) < 2 {
		t.Fatalf("Queries = %#v, want a marker plus at least one query", doc["Queries"])
	}
	q, ok := arr[1].(bson.D)
	if !ok {
		m, ok2 := arr[1].(bson.M)
		if !ok2 {
			t.Fatalf("query entry has type %T", arr[1])
		}
		return m
	}
	out := make(map[string]any, len(q))
	for _, e := range q {
		out[e.Key] = e.Value
	}
	return out
}

// readUnitBytes returns a unit's stored contents. MPR v2 keeps each unit in its
// own file under mprcontents/; v1 keeps them in the Unit table.
func readUnitBytes(t *testing.T, proj, unitID string) []byte {
	t.Helper()
	dir := filepath.Dir(proj)
	matches, _ := filepath.Glob(filepath.Join(dir, "mprcontents", "*", "*", unitID+".mxunit"))
	if len(matches) == 1 {
		b, err := os.ReadFile(matches[0])
		if err != nil {
			t.Fatalf("read unit file: %v", err)
		}
		return b
	}

	db, err := sql.Open("sqlite", proj)
	if err != nil {
		t.Fatalf("open mpr: %v", err)
	}
	defer db.Close()
	var contents []byte
	row := db.QueryRow("SELECT Contents FROM Unit WHERE hex(UnitID) = hex(?)", unitID)
	if err := row.Scan(&contents); err != nil {
		t.Fatalf("read unit %s: %v", unitID, err)
	}
	return contents
}
