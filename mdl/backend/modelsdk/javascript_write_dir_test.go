// SPDX-License-Identifier: Apache-2.0

package modelsdkbackend

import (
	"path/filepath"
	"strings"
	"testing"
)

// Mendix stores JavaScript action sources under a LOWERCASED module directory —
// a blank Mendix 11 app ships javascriptsource/nanoflowcommons/,
// /datawidgets/, /webactions/ and /feedbackmodule/ for modules named
// NanoflowCommons, DataWidgets, WebActions and FeedbackModule.
//
// Writing to javascriptsource/<ModuleName>/ instead means mxbuild finds no
// source at the path it looks in, generates a stub whose body is
// `throw new Error("JavaScript action was not implemented")`, and bundles that.
// The action then parses, passes `mxcli check`, builds cleanly, and throws the
// moment a user clicks the button.
//
// It only reproduces on a case-sensitive filesystem, which is why it survived:
// on macOS and Windows the two spellings are the same directory.
func TestJSActionSourceDirIsLowercased(t *testing.T) {
	b := &Backend{path: filepath.Join("app", "App.mpr")}

	cases := map[string]string{
		"ThemeProof":      "themeproof",
		"NanoflowCommons": "nanoflowcommons",
		"MyFirstModule":   "myfirstmodule",
		"already_lower":   "already_lower",
	}
	for module, wantDir := range cases {
		got := b.jsActionSourceDir(module)
		want := filepath.Join("app", "javascriptsource", wantDir, "actions")
		if got != want {
			t.Errorf("jsActionSourceDir(%q) = %q, want %q", module, got, want)
		}
		if strings.Contains(got, module) && module != wantDir {
			t.Errorf("jsActionSourceDir(%q) kept the module casing: %q", module, got)
		}
	}
}
