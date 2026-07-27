// SPDX-License-Identifier: Apache-2.0

package executor

import "testing"

// TestFullOnlyTablesCoverage guards the "plain REFRESH leaves the analytic tables
// empty, 0 rows, no error" papercut: every catalog table populated only in FULL
// mode must be flagged as requiring `refresh catalog full`, so a query in fast
// mode warns instead of silently returning nothing. xpath_expressions was missing
// (activities/refs were covered) — a query against it returned empty with no hint.
func TestFullOnlyTablesCoverage(t *testing.T) {
	// These views are populated only when the catalog is built in FULL mode
	// (builder_microflows.go activities, builder_references.go refs,
	// builder_xpath.go xpath_expressions, builder_pages.go widgets).
	fullOnly := []string{"activities", "refs", "xpath_expressions", "widgets"}
	for _, tbl := range fullOnly {
		if got := tableRequiredMode(tbl); got != "refresh catalog full" {
			t.Errorf("tableRequiredMode(%q) = %q, want %q — a fast-mode query would silently return 0 rows", tbl, got, "refresh catalog full")
		}
	}

	// A fast-mode table must NOT be flagged (no false warning).
	if got := tableRequiredMode("entities"); got != "refresh catalog" {
		t.Errorf("tableRequiredMode(\"entities\") = %q, want %q", got, "refresh catalog")
	}
}
