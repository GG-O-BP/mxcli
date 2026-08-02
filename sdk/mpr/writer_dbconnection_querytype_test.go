// SPDX-License-Identifier: Apache-2.0

package mpr

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/dbconnector"
	"github.com/mendixlabs/mxcli/model"
	"go.mongodb.org/mongo-driver/bson"
)

// TestSerializeDBQuery_QueryTypeSpellingFollowsVersion covers the Mendix 11.13
// rename of the query-type property. 11.13 replaced the integer `QueryType` with
// the string enum `Type`; a query carrying only the legacy key reads as Unknown,
// which mxbuild reports as CE5277 ("Please re-run and save the query to fix the
// error") on every Execute-database-query activity pointing at it.
//
// Exactly one spelling must be written: the other is a property the target
// version's metamodel does not define, which is the shape Studio Pro refuses to
// open.
func TestSerializeDBQuery_QueryTypeSpellingFollowsVersion(t *testing.T) {
	tests := []struct {
		name      string
		typeEnum  bool
		wantKey   string
		wantValue any
		absentKey string
	}{
		{
			name:      "mendix_11_12_writes_legacy_int",
			typeEnum:  false,
			wantKey:   dbconnector.QueryTypeKey,
			wantValue: int64(dbconnector.CustomSQLQueryType),
			absentKey: dbconnector.TypeKey,
		},
		{
			name:      "mendix_11_13_writes_type_enum",
			typeEnum:  true,
			wantKey:   dbconnector.TypeKey,
			wantValue: dbconnector.TypeSelect,
			absentKey: dbconnector.QueryTypeKey,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := &model.DatabaseQuery{
				Name:      "GetAll",
				SQL:       "SELECT driverId FROM drivers",
				QueryType: dbconnector.CustomSQLQueryType,
			}
			doc := docMap(serializeDBQuery(q, tc.typeEnum))

			if got, ok := doc[tc.wantKey]; !ok || got != tc.wantValue {
				t.Errorf("%s = %#v (present=%v), want %#v", tc.wantKey, got, ok, tc.wantValue)
			}
			if v, ok := doc[tc.absentKey]; ok {
				t.Errorf("wrote %s = %#v; this Mendix version stores %s",
					tc.absentKey, v, tc.wantKey)
			}
		})
	}
}

// TestSerializeDBQuery_TypeFromStatement: mxcli never connects to the database, so
// it derives the 11.13 type from the statement rather than leaving it Unknown.
func TestSerializeDBQuery_TypeFromStatement(t *testing.T) {
	tests := []struct {
		sql    string
		stored string
		want   string
	}{
		{sql: "SELECT 1", want: dbconnector.TypeSelect},
		{sql: "UPDATE drivers SET forename = 'x'", want: dbconnector.TypeNonSelect},
		// A value Studio Pro derived by running the query outranks the heuristic.
		{sql: "EXEC dbo.GetRows", stored: dbconnector.TypeSelect, want: dbconnector.TypeSelect},
	}
	for _, tc := range tests {
		q := &model.DatabaseQuery{Name: "Q", SQL: tc.sql, QueryTypeName: tc.stored}
		if got := docMap(serializeDBQuery(q, true))[dbconnector.TypeKey]; got != tc.want {
			t.Errorf("Type for %q (stored %q) = %#v, want %q", tc.sql, tc.stored, got, tc.want)
		}
	}
}

// TestParseDBQuery_ReadsEitherSpelling guards the read half: a project written by
// 11.13 has no QueryType at all, and reading 0 there would write Unknown straight
// back on the next ALTER.
func TestParseDBQuery_ReadsEitherSpelling(t *testing.T) {
	legacy := parseDBQuery(map[string]any{
		"Name":                   "Q",
		"Query":                  "SELECT 1",
		dbconnector.QueryTypeKey: int32(dbconnector.CustomSQLQueryType),
		"$Type":                  "DatabaseConnector$DatabaseQuery",
	})
	if legacy.QueryType != dbconnector.CustomSQLQueryType || legacy.QueryTypeName != "" {
		t.Errorf("legacy parse = %d/%q, want %d/\"\"",
			legacy.QueryType, legacy.QueryTypeName, dbconnector.CustomSQLQueryType)
	}

	modern := parseDBQuery(map[string]any{
		"Name":              "Q",
		"Query":             "UPDATE t SET a = 1",
		dbconnector.TypeKey: dbconnector.TypeNonSelect,
		"$Type":             "DatabaseConnector$DatabaseQuery",
	})
	if modern.QueryTypeName != dbconnector.TypeNonSelect {
		t.Errorf("QueryTypeName = %q, want %q", modern.QueryTypeName, dbconnector.TypeNonSelect)
	}
	if modern.QueryType != dbconnector.CustomSQLQueryType {
		t.Errorf("QueryType = %d, want %d", modern.QueryType, dbconnector.CustomSQLQueryType)
	}
}

// docMap flattens a bson.D into a lookup keyed by property name.
func docMap(d bson.D) map[string]any {
	out := make(map[string]any, len(d))
	for _, e := range d {
		out[e.Key] = e.Value
	}
	return out
}
