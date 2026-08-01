// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/backend/mock"
	"github.com/mendixlabs/mxcli/model"
)

// captureSettingsBackend is settingsBackend with the written document kept, so a
// test can assert on the configuration the handler produced.
func captureSettingsBackend(out **model.ProjectSettings) *mock.MockBackend {
	var wrote bool
	b := settingsBackend(&wrote)
	b.UpdateProjectSettingsFunc = func(ps *model.ProjectSettings) error {
		*out = ps
		return nil
	}
	return b
}

// TestCreateConfiguration_DefaultsMatchStudioPro is the regression test for
// mendixlabs/mxcli#759: CREATE CONFIGURATION hardcoded DatabaseType "HSQLDB",
// which is not a member of the Mendix DatabaseType enumeration ("Hsqldb" is), and
// left ServerPortNumber at 0 so the new configuration had no admin port.
func TestCreateConfiguration_DefaultsMatchStudioPro(t *testing.T) {
	var written *model.ProjectSettings
	ctx, _ := newMockCtx(t, withBackend(captureSettingsBackend(&written)))

	if err := createConfiguration(ctx, &ast.CreateConfigurationStmt{Name: "Acceptance"}); err != nil {
		t.Fatalf("createConfiguration: %v", err)
	}

	cfg := configByName(t, written, "Acceptance")
	if cfg.DatabaseType != "Hsqldb" {
		t.Errorf("DatabaseType = %q, want %q (the enum member Studio Pro stores)",
			cfg.DatabaseType, "Hsqldb")
	}
	if cfg.HttpPortNumber != 8080 {
		t.Errorf("HttpPortNumber = %d, want 8080", cfg.HttpPortNumber)
	}
	if cfg.ServerPortNumber != 8090 {
		t.Errorf("ServerPortNumber = %d, want 8090", cfg.ServerPortNumber)
	}
}

// TestCreateConfiguration_CanonicalisesDatabaseType covers the user-supplied half:
// a recognised value is stored in the enum's spelling whatever case it was typed
// in, and an unrecognised one is refused instead of written through.
func TestCreateConfiguration_CanonicalisesDatabaseType(t *testing.T) {
	tests := []struct {
		given string
		want  string // "" = must be rejected
	}{
		{given: "PostgreSql", want: "PostgreSql"},
		{given: "postgresql", want: "PostgreSql"},
		{given: "SQLSERVER", want: "SqlServer"},
		{given: "hsqldb", want: "Hsqldb"},
		{given: "HSQLDB", want: "Hsqldb"},
		{given: "Postgres", want: ""},
		{given: "", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.given, func(t *testing.T) {
			var written *model.ProjectSettings
			ctx, _ := newMockCtx(t, withBackend(captureSettingsBackend(&written)))

			err := createConfiguration(ctx, &ast.CreateConfigurationStmt{
				Name:       "Acceptance",
				Properties: map[string]any{"DatabaseType": tc.given},
			})
			if tc.want == "" {
				if err == nil {
					t.Fatalf("createConfiguration accepted DatabaseType %q", tc.given)
				}
				if !strings.Contains(err.Error(), "DatabaseType") {
					t.Errorf("error does not name the property: %v", err)
				}
				if written != nil {
					t.Error("a rejected CREATE CONFIGURATION still wrote the settings document")
				}
				return
			}
			if err != nil {
				t.Fatalf("createConfiguration: %v", err)
			}
			if got := configByName(t, written, "Acceptance").DatabaseType; got != tc.want {
				t.Errorf("DatabaseType = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAlterConfiguration_CanonicalisesDatabaseType covers the same on the ALTER
// path, which stored whatever string it was given.
func TestAlterConfiguration_CanonicalisesDatabaseType(t *testing.T) {
	var written *model.ProjectSettings
	ctx, _ := newMockCtx(t, withBackend(captureSettingsBackend(&written)))

	err := alterSettings(ctx, &ast.AlterSettingsStmt{
		Section:    "configuration",
		ConfigName: "Default",
		Properties: map[string]any{"DatabaseType": "oracle"},
	})
	if err != nil {
		t.Fatalf("alterSettings: %v", err)
	}
	if got := configByName(t, written, "Default").DatabaseType; got != "Oracle" {
		t.Errorf("DatabaseType = %q, want %q", got, "Oracle")
	}

	written = nil
	err = alterSettings(ctx, &ast.AlterSettingsStmt{
		Section:    "configuration",
		ConfigName: "Default",
		Properties: map[string]any{"DatabaseType": "MongoDB"},
	})
	if err == nil {
		t.Fatal("alterSettings accepted DatabaseType \"MongoDB\"")
	}
	if written != nil {
		t.Error("a rejected ALTER still wrote the settings document")
	}
}

func configByName(t *testing.T, ps *model.ProjectSettings, name string) *model.ServerConfiguration {
	t.Helper()
	if ps == nil || ps.Configuration == nil {
		t.Fatal("no settings document written")
	}
	for _, cfg := range ps.Configuration.Configurations {
		if cfg.Name == name {
			return cfg
		}
	}
	t.Fatalf("configuration %q not in the written settings", name)
	return nil
}
