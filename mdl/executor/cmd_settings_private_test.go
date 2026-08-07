// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/backend/mock"
	"github.com/mendixlabs/mxcli/model"
)

// privateConstantBackend serves a Default configuration holding one private and one
// shared constant override.
func privateConstantBackend(wrote *bool) *mock.MockBackend {
	return &mock.MockBackend{
		IsConnectedFunc: func() bool { return true },
		GetProjectSettingsFunc: func() (*model.ProjectSettings, error) {
			ps := &model.ProjectSettings{
				Configuration: &model.ConfigurationSettings{
					Configurations: []*model.ServerConfiguration{{
						Name: "Default",
						ConstantValues: []*model.ConstantValue{
							{ConstantId: "Mod.ApiToken", IsPrivate: true},
							{ConstantId: "Mod.BaseUrl", Value: "https://example.invalid"},
						},
					}},
				},
			}
			ps.RawParts = []map[string]any{{"$Type": "Settings$ConfigurationSettings"}}
			return ps, nil
		},
		UpdateProjectSettingsFunc: func(*model.ProjectSettings) error {
			*wrote = true
			return nil
		},
	}
}

// TestAlterSettingsConstant_RefusesPrivateOverride: setting a value on a private
// override would convert it to a shared one, publishing into version control a
// value the developer deliberately keeps on their workstation. The shared/private
// choice belongs to the constant, so MDL refuses instead of flipping it.
func TestAlterSettingsConstant_RefusesPrivateOverride(t *testing.T) {
	wrote := false
	ctx, _ := newMockCtx(t, withBackend(privateConstantBackend(&wrote)))

	err := alterSettings(ctx, &ast.AlterSettingsStmt{
		Section:    "constant",
		ConfigName: "Default",
		ConstantId: "Mod.ApiToken",
		Value:      "leaked-into-git",
	})
	if err == nil {
		t.Fatal("alterSettings overwrote a private constant override")
	}
	for _, want := range []string{"Mod.ApiToken", "private", "Studio Pro"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
	if wrote {
		t.Error("a refused ALTER still wrote the settings document")
	}
}

// TestAlterSettingsConstant_SharedStillWrites guards the refusal from over-reaching.
func TestAlterSettingsConstant_SharedStillWrites(t *testing.T) {
	wrote := false
	ctx, _ := newMockCtx(t, withBackend(privateConstantBackend(&wrote)))

	if err := alterSettings(ctx, &ast.AlterSettingsStmt{
		Section:    "constant",
		ConfigName: "Default",
		ConstantId: "Mod.BaseUrl",
		Value:      "https://staging.invalid",
	}); err != nil {
		t.Fatalf("alterSettings refused a shared override: %v", err)
	}
	if !wrote {
		t.Error("a valid ALTER did not write the settings document")
	}
}

// TestAlterSettingsConstant_DropPrivateIsAllowed: removing the override entirely
// discards the private/shared choice along with it, which is what the user asked
// for — only converting it in place is refused.
func TestAlterSettingsConstant_DropPrivateIsAllowed(t *testing.T) {
	wrote := false
	ctx, _ := newMockCtx(t, withBackend(privateConstantBackend(&wrote)))

	if err := alterSettings(ctx, &ast.AlterSettingsStmt{
		Section:      "constant",
		ConfigName:   "Default",
		ConstantId:   "Mod.ApiToken",
		DropConstant: true,
	}); err != nil {
		t.Fatalf("alterSettings refused to drop a private override: %v", err)
	}
	if !wrote {
		t.Error("DROP CONSTANT did not write the settings document")
	}
}

// TestDescribeSettings_PrivateOverrideIsNotReExecutable: describe emitted
// `value ”` for a private override, so replaying its own output converted the
// override to a shared empty one.
func TestDescribeSettings_PrivateOverrideIsNotReExecutable(t *testing.T) {
	wrote := false
	ctx, out := newMockCtx(t, withBackend(privateConstantBackend(&wrote)))

	if err := describeSettings(ctx, ""); err != nil {
		t.Fatalf("describeSettings: %v", err)
	}
	got := out.String()

	if strings.Contains(got, "alter settings constant 'Mod.ApiToken'") {
		t.Errorf("describe emitted a re-executable statement for a private override:\n%s", got)
	}
	if !strings.Contains(got, "Mod.ApiToken") || !strings.Contains(got, "private") {
		t.Errorf("describe does not report the private override at all:\n%s", got)
	}
	// The shared override must still round-trip.
	if !strings.Contains(got, "alter settings constant 'Mod.BaseUrl' value 'https://example.invalid'") {
		t.Errorf("describe dropped the shared override:\n%s", got)
	}
}
