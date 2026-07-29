// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/model"
)

// TestValidateLanguageCode guards FINDINGS #6: setting DefaultLanguageCode to a
// language not configured in the project must be rejected up front, because the
// write otherwise "succeeds" and the next mx check dies with a NullReferenceException.
func TestValidateLanguageCode(t *testing.T) {
	ls := &model.LanguageSettings{
		Languages: []model.Language{{Code: "en_US"}, {Code: "de_DE"}},
	}

	// Configured code → accepted.
	if err := validateLanguageCode(ls, "en_US"); err != nil {
		t.Errorf("validateLanguageCode(en_US) = %v, want nil", err)
	}

	// Unconfigured code → rejected, and the message lists the available codes.
	err := validateLanguageCode(ls, "nl_NL")
	if err == nil {
		t.Fatalf("validateLanguageCode(nl_NL) = nil, want rejection")
	}
	if msg := err.Error(); !strings.Contains(msg, "en_US") || !strings.Contains(msg, "de_DE") {
		t.Errorf("error message %q should list available codes en_US, de_DE", msg)
	}

	// No language list available → skip validation (avoid false rejection).
	if err := validateLanguageCode(&model.LanguageSettings{}, "nl_NL"); err != nil {
		t.Errorf("validateLanguageCode with empty list = %v, want nil (skip)", err)
	}
	if err := validateLanguageCode(nil, "nl_NL"); err != nil {
		t.Errorf("validateLanguageCode(nil) = %v, want nil (skip)", err)
	}
}
