// SPDX-License-Identifier: Apache-2.0

package syntax

import (
	"strings"
	"testing"
)

// `mxcli syntax` is the reference an agent reads before writing MDL, and nothing
// checks it against the parser — so a spelling the parser has dropped can sit in
// it indefinitely. Two did, and both cost a build round-trip to discover
// (mxcli-todo findings #8).
//
// This pins the corrections. It is a spelling guard, not a parse: the snippets
// are fragments (a DATAVIEW body, a property line) that do not stand alone as
// statements, so they cannot simply be fed to the parser.
func TestSyntaxDocs_NoRetiredSpellings(t *testing.T) {
	retired := []struct {
		text   string
		reason string
	}{
		{
			"Binds:",
			"the parser rejects it: \"'Binds:' is no longer supported, use 'Attribute:' instead\"",
		},
		{
			"MICROFLOW Module.MF()",
			"a zero-argument microflow DATASOURCE takes no parentheses (unlike RETRIEVE/CALL, where they are normal)",
		},
	}

	for _, f := range All() {
		for _, r := range retired {
			for field, text := range map[string]string{"Syntax": f.Syntax, "Example": f.Example} {
				if strings.Contains(text, r.text) {
					t.Errorf("syntax topic %q, %s field, still shows %q — %s", f.Path, field, r.text, r.reason)
				}
			}
		}
	}
}
