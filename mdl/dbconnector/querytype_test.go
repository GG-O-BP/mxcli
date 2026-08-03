// SPDX-License-Identifier: Apache-2.0

package dbconnector

import "testing"

// TestStoresTypeEnum pins the version boundary. Mendix 11.13 replaced the integer
// QueryType with the `Type` string enum; writing the wrong one produces either a
// query the target rejects (CE5277 on every activity using it) or a property the
// older metamodel does not define.
func TestStoresTypeEnum(t *testing.T) {
	tests := []struct {
		major, minor int
		want         bool
	}{
		{0, 0, false}, // unknown version — fall back to the legacy key
		{10, 24, false},
		{11, 6, false},
		{11, 12, false},
		{11, 13, true},
		{11, 20, true},
		{12, 0, true},
	}
	for _, tc := range tests {
		if got := StoresTypeEnum(tc.major, tc.minor); got != tc.want {
			t.Errorf("StoresTypeEnum(%d, %d) = %v, want %v", tc.major, tc.minor, got, tc.want)
		}
	}
}

func TestTypeForSQL(t *testing.T) {
	tests := []struct {
		sql  string
		want string
	}{
		{"SELECT driverId FROM drivers", TypeSelect},
		{"  select 1", TypeSelect},
		{"WITH d AS (SELECT 1) SELECT * FROM d", TypeSelect},
		{"(SELECT 1)", TypeSelect},
		{"UPDATE drivers SET forename = 'x'", TypeNonSelect},
		{"INSERT INTO drivers (forename) VALUES ('y')", TypeNonSelect},
		{"DELETE FROM drivers WHERE driverId = 2", TypeNonSelect},
		{"EXEC dbo.RefreshCache", TypeNonSelect},
		{"", TypeUnknown},
		{"   \n\t ", TypeUnknown},
		// Leading comments must not be mistaken for the statement.
		{"-- fetch everyone\nSELECT 1", TypeSelect},
		{"/* header */ UPDATE t SET a = 1", TypeNonSelect},
		{"-- only a comment", TypeUnknown},
	}
	for _, tc := range tests {
		if got := TypeForSQL(tc.sql); got != tc.want {
			t.Errorf("TypeForSQL(%q) = %q, want %q", tc.sql, got, tc.want)
		}
	}
}

// TestTypeToWrite_PrefersStored: Studio Pro derives the type by executing the
// query, so it can hold a value the SQL heuristic would not derive. A round-trip
// through mxcli must not overwrite it.
func TestTypeToWrite_PrefersStored(t *testing.T) {
	if got := TypeToWrite(TypeSelect, "EXEC dbo.GetRows"); got != TypeSelect {
		t.Errorf("TypeToWrite overwrote the stored value: got %q", got)
	}
	if got := TypeToWrite("", "EXEC dbo.GetRows"); got != TypeNonSelect {
		t.Errorf("TypeToWrite(unstored) = %q, want %q", got, TypeNonSelect)
	}
}

func TestLegacyQueryTypeFor(t *testing.T) {
	tests := map[string]int{
		TypeSelect:    CustomSQLQueryType,
		TypeNonSelect: CustomSQLQueryType,
		TypeUnknown:   0,
		"":            0,
	}
	for in, want := range tests {
		if got := LegacyQueryTypeFor(in); got != want {
			t.Errorf("LegacyQueryTypeFor(%q) = %d, want %d", in, got, want)
		}
	}
}
