// SPDX-License-Identifier: Apache-2.0

package visitor

import "testing"

// mxcli-formula1 suggested issue 11: extractOriginalText reads the raw source
// between two token positions — which is what preserves an expression's spacing,
// and also drags in the comments the lexer sent to a hidden channel. A `--`
// comment between two operands was stored inside the Mendix expression and the
// build failed CE0117 "Error(s) in expression". Verified end to end: the same
// script went from 1 error to 0 against mxbuild 11.12.1.
func TestStripMDLComments(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"nothing to do", "'a' + 'b'", "'a' + 'b'"},
		{
			"line comment between operands",
			"'a' +\n  -- explain\n  'b'",
			"'a' +\n  \n  'b'",
		},
		{"trailing line comment", "$x + 1 -- why", "$x + 1 "},
		{"block comment", "$x /* mid */ + 1", "$x   + 1"},

		// A Mendix string may legitimately contain these. Removing them would
		// silently corrupt the value, which is worse than the bug being fixed.
		{"-- inside a string literal", "'a--b'", "'a--b'"},
		{"/* inside a string literal", "'a/*b'", "'a/*b'"},
		{"comment marker after a string", "'a--b' -- real", "'a--b' "},
		{
			"escaped quote keeps the string open",
			"'it''s -- fine'",
			"'it''s -- fine'",
		},

		// A comment becomes whitespace, never nothing: gluing the operands
		// together would change the expression rather than clean it.
		{"tokens must not weld", "1 --c\n+ 2", "1 \n+ 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripMDLComments(tt.in); got != tt.want {
				t.Errorf("stripMDLComments(%q)\n got: %q\nwant: %q", tt.in, got, tt.want)
			}
		})
	}
}

// An unterminated block comment is a parse error the parser reports; the
// stripper must not emit half of it into the model.
func TestStripMDLComments_UnterminatedBlock(t *testing.T) {
	if got := stripMDLComments("$x + /* oops"); got != "$x + " {
		t.Errorf("got %q", got)
	}
}
