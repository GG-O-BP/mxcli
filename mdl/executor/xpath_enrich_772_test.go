// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"
)

// TestEnrichXPathGroups_KeepsEveryGroup is the regression test for
// mendixlabs/mxcli#772. The stored constraint holds three sibling predicate groups;
// enriching the whole string at once parsed only the first, re-rendered just that,
// and dropped the other two — so `describe` showed a materially less restrictive
// query than the project contains, with no warning.
func TestEnrichXPathGroups_KeepsEveryGroup(t *testing.T) {
	enumAttrs := map[string]string{"Status": "Reminders.TaskStatus"}

	// Exactly the shape from the issue: a group with a nested bracket, then two
	// flat ones, newline-separated.
	const stored = "[Reminders.Task_TaskGroup/Reminders.TaskGroup[EndDate = $EndDateLimit]]\n" +
		"[Status != 'Completed']\n[CompletionDate = empty]"

	got := enrichXPathGroups(stored, enumAttrs)

	for _, want := range []string{
		"Reminders.Task_TaskGroup/Reminders.TaskGroup[EndDate = $EndDateLimit]",
		"CompletionDate = empty",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("enrichXPathGroups dropped %q\ngot: %s", want, got)
		}
	}
	// The enum comparison must be enriched to a qualified value — in the *second*
	// group, which is the one the old code never reached.
	if !strings.Contains(got, "Status != Reminders.TaskStatus.Completed") {
		t.Errorf("enum enrichment not applied to a later group\ngot: %s", got)
	}
	if strings.Contains(got, "'Completed'") {
		t.Errorf("enum literal left unenriched\ngot: %s", got)
	}
}

// TestEnrichXPathGroups_PassesThroughUnparseable: a constraint mxcli cannot split
// into groups is returned untouched rather than mangled or dropped.
func TestEnrichXPathGroups_PassesThroughUnparseable(t *testing.T) {
	enumAttrs := map[string]string{"Status": "Mod.S"}
	for _, in := range []string{
		"Status = 'Open'",     // no brackets
		"[Status = 'Open'",    // unbalanced
		"[a = 1] and [b = 2]", // content between groups
	} {
		if got := enrichXPathGroups(in, enumAttrs); got != in {
			t.Errorf("enrichXPathGroups(%q) = %q, want it returned verbatim", in, got)
		}
	}
}

// TestEnrichXPathGroups_SingleGroupStillEnriched guards the fix from regressing the
// original single-group behaviour.
func TestEnrichXPathGroups_SingleGroupStillEnriched(t *testing.T) {
	got := enrichXPathGroups("[Status = 'Open']", map[string]string{"Status": "Mod.S"})
	if got != "[Status = Mod.S.Open]" {
		t.Errorf("enrichXPathGroups = %q, want %q", got, "[Status = Mod.S.Open]")
	}
}
