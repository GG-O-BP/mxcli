// SPDX-License-Identifier: Apache-2.0

package visitor

import (
	"reflect"
	"testing"
)

func TestSplitXPathPredicateGroups(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			// The constraint from mendixlabs/mxcli#772, verbatim: a group with a
			// nested bracket followed by two flat ones, newline-separated.
			name: "issue 772 — nested group plus siblings",
			in: "[Reminders.Task_TaskGroup/Reminders.TaskGroup[EndDate = $EndDateLimit]]\n" +
				"[Status != 'Completed']\n[CompletionDate = empty]",
			want: []string{
				"[Reminders.Task_TaskGroup/Reminders.TaskGroup[EndDate = $EndDateLimit]]",
				"[Status != 'Completed']",
				"[CompletionDate = empty]",
			},
		},
		{
			name: "single group",
			in:   "[Status = 'Open']",
			want: []string{"[Status = 'Open']"},
		},
		{
			name: "adjacent groups, no separator",
			in:   "[a = 1][b = 2]",
			want: []string{"[a = 1]", "[b = 2]"},
		},
		{
			// A "][" split would cut this literal in half.
			name: "bracket inside a string literal",
			in:   "[Name = 'a][b'][Status = 'Open']",
			want: []string{"[Name = 'a][b']", "[Status = 'Open']"},
		},
		{
			name: "doubled quote escape inside a literal",
			in:   "[Name = 'it''s]here'][Age = 3]",
			want: []string{"[Name = 'it''s]here']", "[Age = 3]"},
		},
		{name: "empty", in: "", want: nil},
		{name: "not bracketed", in: "Status = 'Open'", want: nil},
		{name: "unbalanced open", in: "[a = 1", want: nil},
		{name: "unbalanced close", in: "[a = 1]]", want: nil},
		{name: "content between groups", in: "[a = 1] and [b = 2]", want: nil},
		{name: "unterminated literal", in: "[Name = 'x]", want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitXPathPredicateGroups(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("SplitXPathPredicateGroups(%q)\n got %#v\nwant %#v", tc.in, got, tc.want)
			}
		})
	}
}

// TestParseXPathConstraint_RejectsPartialParse is the core of #772: the rule matches
// one bracket group, and with the error listeners removed ANTLR parsed the first and
// left the rest on the stream while still reporting success. Callers treated that as
// a full parse and re-rendered only what came back.
func TestParseXPathConstraint_RejectsPartialParse(t *testing.T) {
	multi := "[Status != 'Completed'][CompletionDate = empty]"
	if _, ok := ParseXPathConstraint(multi); ok {
		t.Error("ParseXPathConstraint reported success on a multi-group constraint it only partly consumed")
	}
	// A single group must still parse.
	if _, ok := ParseXPathConstraint("[Status != 'Completed']"); !ok {
		t.Error("ParseXPathConstraint rejected a single well-formed group")
	}
}
