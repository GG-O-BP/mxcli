// SPDX-License-Identifier: Apache-2.0

// Package dbconnector holds the version-dependent storage rules for the Mendix
// Database Connector documents, shared by both write engines so the two cannot
// drift apart: the codec engine (mdl/backend/modelsdk) and the legacy engine
// (sdk/mpr).
//
// Today that is one rule — how a query's type is stored — but it is the same
// shape of problem as mdl/settingsoverlay: a property Mendix renamed and
// re-encoded between minor versions, where writing the wrong spelling produces a
// document the target version rejects.
package dbconnector

import "strings"

// The two spellings of a DatabaseQuery's type. They differ in value encoding as
// well as in name, so the key alone does not determine what to write.
//
// Mendix 11.13 replaced the integer QueryType with a string enum under a new
// `Type` key. Its own one-time conversion (ExternalDatabaseConnectionQueryTypeConversion)
// rewrites `QueryType: 1` to `Type: "Select"` and drops the old key entirely.
const (
	TypeKey      = "Type"      // Mendix 11.13+, string enum
	QueryTypeKey = "QueryType" // Mendix <= 11.12, int (1 = custom SQL)
)

// Members of the 11.13 query-type enumeration. An absent `Type` reads as Unknown,
// which is what CE5277 ("Please re-run and save the query to fix the error")
// reports on every Execute-database-query activity pointing at such a query.
const (
	TypeSelect    = "Select"
	TypeNonSelect = "NonSelect"
	TypeUnknown   = "Unknown"
)

// CustomSQLQueryType is the legacy integer mxcli writes for a hand-written query
// (the only kind MDL can author); Mendix's converter maps it to TypeSelect.
const CustomSQLQueryType = 1

// StoresTypeEnum reports whether a project of the given Mendix major.minor stores
// the query type under the 11.13+ `Type` key rather than the legacy integer
// `QueryType`. Callers with no version information pass 0, 0.
//
// An unknown version is therefore treated as pre-11.13. That is the safe default:
// the legacy key is what every version through 11.12 expects, and 11.13 repairs a
// document carrying it via its one-time conversion — whereas writing `Type` onto
// an older project invents a property that version's metamodel does not define,
// which is the shape Studio Pro refuses to open.
//
// The two engines share this one predicate rather than each spelling out the
// version test, so a future rename cannot be fixed in one engine and missed in
// the other.
func StoresTypeEnum(major, minor int) bool {
	return major > 11 || (major == 11 && minor >= 13)
}

// TypeForSQL classifies a query by its statement so a freshly authored query gets
// a usable type instead of Unknown.
//
// Studio Pro derives the real value by executing the query and inspecting the
// result set, which mxcli cannot do — it never connects to the database. Reading
// the leading keyword is the closest honest approximation, and it is strictly
// better than what Mendix's own converter does to a pre-11.13 project (it marks
// every migrated query Select regardless of statement). A query whose type the
// heuristic gets wrong is corrected the same way CE5277 asks for: re-run and save
// it in Studio Pro.
func TypeForSQL(sql string) string {
	switch firstKeyword(sql) {
	case "":
		return TypeUnknown
	case "select", "with", "show", "describe", "explain", "values", "table":
		return TypeSelect
	default:
		return TypeNonSelect
	}
}

// firstKeyword returns the lower-cased first word of a SQL statement, skipping
// leading whitespace, line comments and block comments.
func firstKeyword(sql string) string {
	s := sql
	for {
		// A parenthesised statement — `(SELECT ...)` — leads with the paren.
		s = strings.TrimLeft(s, " \t\r\n(")
		switch {
		case strings.HasPrefix(s, "--"):
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = s[i+1:]
				continue
			}
			return ""
		case strings.HasPrefix(s, "/*"):
			if i := strings.Index(s[2:], "*/"); i >= 0 {
				s = s[i+4:]
				continue
			}
			return ""
		}
		break
	}
	end := strings.IndexFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == '(' || r == ';'
	})
	if end < 0 {
		end = len(s)
	}
	return strings.ToLower(s[:end])
}

// TypeToWrite returns the `Type` value for a query: the one already stored when
// the query round-trips through mxcli (Studio Pro may have set a value the SQL
// heuristic would not derive), otherwise one derived from the SQL.
func TypeToWrite(stored, sql string) string {
	if stored != "" {
		return stored
	}
	return TypeForSQL(sql)
}

// LegacyQueryTypeFor maps a stored 11.13 `Type` back to the legacy integer, for
// the semantic model's QueryType field. Every authored query is custom SQL;
// only an Unknown type has no legacy counterpart.
func LegacyQueryTypeFor(typeName string) int {
	if typeName == TypeUnknown || typeName == "" {
		return 0
	}
	return CustomSQLQueryType
}
