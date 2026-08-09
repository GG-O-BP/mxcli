// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

// Both spellings exist because both read naturally, and the two-argument form is
// what anyone types first.
func TestParseLogSetArgs_BothForms(t *testing.T) {
	got, err := parseLogSetArgs([]string{"ConnectionBus_Queries", "TRACE"})
	if err != nil {
		t.Fatalf("two-arg form: %v", err)
	}
	if len(got) != 1 || got[0].Node != "ConnectionBus_Queries" || got[0].Level != "TRACE" {
		t.Errorf("two-arg form parsed as %+v", got)
	}

	got, err = parseLogSetArgs([]string{"A=TRACE", "B=debug"})
	if err != nil {
		t.Fatalf("pair form: %v", err)
	}
	if len(got) != 2 || got[0].Node != "A" || got[1].Node != "B" || got[1].Level != "debug" {
		t.Errorf("pair form parsed as %+v", got)
	}
}

// `log set A=TRACE B DEBUG` has two readings and neither is obviously right, so
// it is refused rather than guessed at.
func TestParseLogSetArgs_RefusesMixedForms(t *testing.T) {
	_, err := parseLogSetArgs([]string{"A=TRACE", "B", "DEBUG"})
	if err == nil {
		t.Fatal("mixed forms should be refused")
	}
	if !strings.Contains(err.Error(), "do not mix") {
		t.Errorf("the error should say why: %v", err)
	}
}

// A bad level is caught before the round trip, with the valid set named. The
// runtime's own rejection ("Unknown LogLevel VERBOSE") reaches the caller as a
// bare AdminException whose detail is only in the runtime log.
func TestParseLogSetArgs_RejectsAnUnknownLevelLocally(t *testing.T) {
	for _, args := range [][]string{
		{"Connector", "VERBOSE"},
		{"Connector=VERBOSE"},
	} {
		_, err := parseLogSetArgs(args)
		if err == nil {
			t.Fatalf("%v: an unknown level should be refused", args)
		}
		if !strings.Contains(err.Error(), "TRACE") {
			t.Errorf("%v: the error should list the valid levels, got %v", args, err)
		}
	}
}

// Levels are case-insensitive on input and upper-cased on the wire, since that
// is what the runtime accepts.
func TestParseLogSetArgs_LevelCasing(t *testing.T) {
	got, err := parseLogSetArgs([]string{"Connector", "trace"})
	if err != nil {
		t.Fatalf("lower-case level rejected: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
}

// A single argument is neither form; say so rather than reaching the runtime
// with half a request.
func TestParseLogSetArgs_RejectsIncompleteInput(t *testing.T) {
	if _, err := parseLogSetArgs([]string{"Connector"}); err == nil {
		t.Error("a lone node name should be refused")
	}
	if _, err := parseLogSetArgs([]string{"=TRACE"}); err == nil {
		t.Error("an empty node name should be refused")
	}
	if _, err := parseLogSetArgs([]string{"Connector="}); err == nil {
		t.Error("an empty level should be refused")
	}
}
