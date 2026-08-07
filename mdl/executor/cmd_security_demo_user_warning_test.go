// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/sdk/security"
)

// mxcli-todo findings #15: with Security Level Off — what a blank mxcli template
// ships with — the runtime creates no accounts, so demo users sit in the model
// and never appear in the app. CREATE DEMO USER reported success and SHOW
// PROJECT SECURITY said "Demo Users Enabled: true", with nothing to explain the
// empty Administration.Account table.
func TestWarnDemoUsersInert(t *testing.T) {
	tests := []struct {
		level    string
		wantWarn bool
	}{
		{security.SecurityLevelOff, true},
		{security.SecurityLevelPrototype, false},
		{security.SecurityLevelProduction, false},
	}
	for _, tt := range tests {
		var buf bytes.Buffer
		warnDemoUsersInert(&ExecContext{Output: &buf}, tt.level)
		got := buf.String()
		if tt.wantWarn {
			if !strings.Contains(got, "security level is Off") {
				t.Errorf("level %q: expected a warning, got %q", tt.level, got)
			}
			if !strings.Contains(got, "alter project security level prototype") {
				t.Errorf("level %q: the warning must name the fix, got %q", tt.level, got)
			}
		} else if got != "" {
			t.Errorf("level %q: expected no warning, got %q", tt.level, got)
		}
	}
}
