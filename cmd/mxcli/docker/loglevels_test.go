// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"strings"
	"testing"
)

// The runtime rejects anything outside this set with "Unknown LogLevel X", whose
// detail never reaches the HTTP response — so the check happens here.
func TestNormalizeLogLevel(t *testing.T) {
	for in, want := range map[string]string{
		"trace": "TRACE", "TRACE": "TRACE", " Debug ": "DEBUG", "none": "NONE",
	} {
		got, err := NormalizeLogLevel(in)
		if err != nil || got != want {
			t.Errorf("NormalizeLogLevel(%q) = (%q, %v), want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"VERBOSE", "FINE", "", "warn"} {
		if _, err := NormalizeLogLevel(bad); err == nil {
			t.Errorf("NormalizeLogLevel(%q) should fail", bad)
		}
	}
}

// Listing 57 nodes is unusable without a filter; matching is case-insensitive
// and substring, because nobody remembers "ConnectionBus_Synchronize" exactly.
func TestMatchLogNodes(t *testing.T) {
	all := []LogNodeLevel{
		{Node: "ConnectionBus_Queries", Level: "INFO"},
		{Node: "Connector", Level: "INFO"},
		{Node: "Jetty", Level: "INFO"},
	}
	if got := MatchLogNodes(all, ""); len(got) != 3 {
		t.Errorf("an empty filter should match everything, got %d", len(got))
	}
	got := MatchLogNodes(all, "connect")
	if len(got) != 2 {
		t.Fatalf("case-insensitive substring match failed: %+v", got)
	}
	if got[0].Node != "ConnectionBus_Queries" || got[1].Node != "Connector" {
		t.Errorf("unexpected matches: %+v", got)
	}
	if got := MatchLogNodes(all, "nope"); len(got) != 0 {
		t.Errorf("no match expected, got %+v", got)
	}
}

// An empty node list must not reach the admin API as a well-formed no-op.
func TestSetLogLevels_RejectsEmptyInput(t *testing.T) {
	err := SetLogLevels(M2EEOptions{}, nil, false)
	if err == nil || !strings.Contains(err.Error(), "no nodes") {
		t.Errorf("expected a no-nodes error, got %v", err)
	}
}
