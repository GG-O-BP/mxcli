// SPDX-License-Identifier: Apache-2.0

package executor

import "testing"

// mxcli-todo findings #14b: a condition kept as a SourceExpr (original text plus
// the parsed tree) skipped the operator lowercasing a rebuilt BinaryExpr gets,
// so `IF A != x AND B != empty` stored `AND` verbatim and mxbuild answered
// CE0117. The same condition with `=` was rebuilt and normalised, which is what
// made it look like `!=` inside a conjunction was unsupported.
func TestNormalizeMendixOperatorCase(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			"the reported case",
			"$Task/Status != M.Status.Done AND $Task/CompletedOn != empty",
			"$Task/Status != M.Status.Done and $Task/CompletedOn != empty",
		},
		{"already lowercase is unchanged", "$a = 1 and $b = 2", "$a = 1 and $b = 2"},
		{"mixed case", "$a = 1 And $b = 2 Or $c = 3", "$a = 1 and $b = 2 or $c = 3"},
		{"NOT", "NOT($a = 1)", "not($a = 1)"},
		{"DIV and MOD", "$a DIV $b MOD 2", "$a div $b mod 2"},

		// Everything that is not an operator must survive byte-identical.
		{
			"a string literal is never touched",
			"$a = 'AND' and $b = 'NOT OR'",
			"$a = 'AND' and $b = 'NOT OR'",
		},
		{
			"an escaped quote does not end the literal early",
			"$a = 'it''s AND then' and $b = 1",
			"$a = 'it''s AND then' and $b = 1",
		},
		{
			"an enum value named And is a member, not an operator",
			"$x/Kind = M.Enum.And and $y = 1",
			"$x/Kind = M.Enum.And and $y = 1",
		},
		{
			"an attribute after / is a member",
			"$Task/Mod = 1 AND $Task/Not = 2",
			"$Task/Mod = 1 and $Task/Not = 2",
		},
		{
			"a variable named $And keeps its case",
			"$And = 1 AND $b = 2",
			"$And = 1 and $b = 2",
		},
		{
			"words merely containing an operator are left alone",
			"$Android = 1 AND $Normal = 2",
			"$Android = 1 and $Normal = 2",
		},
		{"empty input", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeMendixOperatorCase(tt.in); got != tt.want {
				t.Errorf("normalizeMendixOperatorCase(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}
