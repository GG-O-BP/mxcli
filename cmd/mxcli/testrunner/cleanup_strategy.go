// SPDX-License-Identifier: Apache-2.0

package testrunner

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Cleanup strategies for the @cleanup annotation.
//
// Rollback is the default and always was — the annotation has documented it
// since the runner shipped. It only became real with the test endpoint: the
// endpoint owns the context each test runs in, so it can open a transaction
// around the call and roll it back. The after-startup runner has no such seam,
// which is why the annotation sat parsed-but-unused.
const (
	// CleanupRollback wraps the test in a transaction and rolls it back, so its
	// database writes do not survive. The default.
	CleanupRollback = "rollback"
	// CleanupNone lets the test's writes commit and persist.
	CleanupNone = "none"
)

// cleanupStrategies is the set of accepted @cleanup values.
var cleanupStrategies = map[string]string{
	CleanupRollback: "wrap the test in a transaction and roll it back (default)",
	CleanupNone:     "let the test's writes commit and persist",
}

// validateCleanup rejects an unrecognised @cleanup value.
//
// Silently treating a typo as "not rollback" is the worst outcome available:
// `@cleanup rollbak` would leave the test's data in the database while the run
// still reported a clean pass, and nothing anywhere would say why. An unknown
// value is a mistake in the test file, so it is an error.
func validateCleanup(value string) error {
	if value == "" || cleanupStrategies[value] != "" {
		return nil
	}
	valid := make([]string, 0, len(cleanupStrategies))
	for k := range cleanupStrategies {
		valid = append(valid, k)
	}
	sort.Strings(valid)
	return fmt.Errorf("unknown @cleanup strategy %q (expected one of: %s)", value, strings.Join(valid, ", "))
}

// rollsBack reports whether a test's writes should be rolled back.
//
// An empty strategy means the annotation was absent, which is the default —
// rollback. Anything unrecognised has already been rejected by validateCleanup,
// so this never has to guess.
func rollsBack(tc TestCase) bool {
	return tc.Cleanup == "" || tc.Cleanup == CleanupRollback
}

// reportRollbackFailure explains why a requested rollback did not happen.
//
// Two causes are worth telling apart. The endpoint may not support rollback at
// all — with --attach the app is hosted by whatever mxcli started it, which can
// predate this feature — and that is a different fix from a transaction the
// runtime refused to roll back.
func reportRollbackFailure(w io.Writer, tc TestCase, rr *runResponse) {
	switch {
	case !rr.RollbackRequested:
		fmt.Fprintf(w, "  WARNING: %s ran without rollback — the app is hosting an older test endpoint\n"+
			"           that ignores it. Restart the hosting 'mxcli run --local --test-endpoint'.\n", tc.Name)
	case rr.RollbackError != "":
		fmt.Fprintf(w, "  WARNING: %s could not be rolled back: %s\n", tc.Name, rr.RollbackError)
	default:
		fmt.Fprintf(w, "  WARNING: %s could not be rolled back (no reason reported)\n", tc.Name)
	}
}

// rollbackNote annotates a verbose result line with what happened to the
// transaction.
func rollbackNote(requested bool, rr *runResponse) string {
	switch {
	case !requested:
		return " [committed]"
	case rr.RolledBack:
		return " [rolled back]"
	default:
		return " [ROLLBACK FAILED]"
	}
}

// describeStartup says what the generated after-startup microflow will do,
// naming the project's own microflow when there is one.
//
// This line exists because its absence was a reported trap (mxcli-formula1
// findings #19). The runner printed only that after-startup had been pointed at
// its own microflow; a reader had no way to tell that their app's startup logic
// — a cache load, in that report — was therefore not going to run. The suite
// passed under --attach, where the app boots normally, and failed under --local
// for reasons that had nothing to do with the code under test.
func describeStartup(appAfterStartup string, skipped bool) string {
	base := "After-startup set to " + endpointStartupFlow + " (registers the endpoint; runs no tests"
	switch {
	case appAfterStartup == "":
		return base + "; this project has no after-startup microflow of its own)"
	case skipped:
		return base + "; --skip-app-startup, so " + appAfterStartup + " will NOT run)"
	default:
		return base + ", then runs your " + appAfterStartup + ")"
	}
}
