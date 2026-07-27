// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"
)

// TestValidateCodeActionParams covers the new-finding param-name check: a Java /
// JavaScript action call whose written parameter name doesn't match a declared
// parameter is flagged (case-sensitively), with a did-you-mean on a casing-only
// mismatch. A matching name passes; an empty declared set (backend can't report)
// degrades to no error.
func TestValidateCodeActionParams(t *testing.T) {
	declared := []string{"Username", "Password"}

	t.Run("case-only mismatch flagged with did-you-mean", func(t *testing.T) {
		ref := codeActionCallRef{name: "NanoflowCommons.SignIn", argNames: []string{"username", "Password"}}
		errs := validateCodeActionParams("javascript action", ref, declared)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if !strings.Contains(errs[0], `no parameter "username"`) || !strings.Contains(errs[0], `did you mean "Username"?`) {
			t.Errorf("expected did-you-mean for username, got: %s", errs[0])
		}
		if !strings.Contains(errs[0], "CE1613") {
			t.Errorf("expected CE1613 reference, got: %s", errs[0])
		}
	})

	t.Run("unknown name flagged without did-you-mean", func(t *testing.T) {
		ref := codeActionCallRef{name: "M.A", argNames: []string{"Bogus"}}
		errs := validateCodeActionParams("java action", ref, declared)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if strings.Contains(errs[0], "did you mean") {
			t.Errorf("no casing match exists, should not suggest one: %s", errs[0])
		}
	})

	t.Run("all names match → no error", func(t *testing.T) {
		ref := codeActionCallRef{name: "M.A", argNames: []string{"Username", "Password"}}
		if errs := validateCodeActionParams("javascript action", ref, declared); len(errs) != 0 {
			t.Errorf("expected no errors, got: %v", errs)
		}
	})

	t.Run("empty declared set → degrade to no error", func(t *testing.T) {
		ref := codeActionCallRef{name: "M.A", argNames: []string{"anything"}}
		if errs := validateCodeActionParams("javascript action", ref, nil); len(errs) != 0 {
			t.Errorf("expected graceful skip on unknown params, got: %v", errs)
		}
	})
}
