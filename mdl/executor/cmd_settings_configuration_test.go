// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/backend/mock"
	"github.com/mendixlabs/mxcli/model"
)

// mxcli-formula1 findings #8: `alter settings configuration 'X'` is the write
// form, but the read form was a parse error, and the `show` summary omitted
// ApplicationRootUrl — so the obvious command for "did my root URL land?" could
// not answer it.
func settingsCtxWithConfigs(t *testing.T) *ExecContext {
	t.Helper()
	ps := &model.ProjectSettings{
		Configuration: &model.ConfigurationSettings{
			Configurations: []*model.ServerConfiguration{
				{
					Name: "Default", DatabaseType: "Hsqldb", DatabaseName: "default",
					HttpPortNumber: 8080, ApplicationRootUrl: "http://backend.local:8080/",
				},
				{Name: "Test", DatabaseType: "PostgreSQL", DatabaseName: "test", HttpPortNumber: 8180},
			},
		},
	}
	mb := &mock.MockBackend{
		IsConnectedFunc:        func() bool { return true },
		GetProjectSettingsFunc: func() (*model.ProjectSettings, error) { return ps, nil },
	}
	ctx, _ := newMockCtx(t, withBackend(mb))
	return ctx
}

func TestDescribeSettingsConfiguration_ByName(t *testing.T) {
	ctx := settingsCtxWithConfigs(t)
	buf := ctx.Output.(*bytes.Buffer)
	if err := describeSettings(ctx, "Default"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.Contains(out, "alter settings configuration 'Default'") {
		t.Errorf("expected the named configuration, got:\n%s", out)
	}
	if !strings.Contains(out, "ApplicationRootUrl = 'http://backend.local:8080/'") {
		t.Errorf("expected the root URL, got:\n%s", out)
	}
	// Naming one configuration means one configuration, not all of them.
	if strings.Contains(out, "'Test'") {
		t.Errorf("expected only the named configuration, got:\n%s", out)
	}
}

// Case-insensitive, like the rest of MDL's name matching.
func TestDescribeSettingsConfiguration_CaseInsensitive(t *testing.T) {
	ctx := settingsCtxWithConfigs(t)
	if err := describeSettings(ctx, "default"); err != nil {
		t.Fatalf("expected a case-insensitive match: %v", err)
	}
}

// A wrong name has to say which ones exist, or the user is guessing.
func TestDescribeSettingsConfiguration_UnknownNameListsAvailable(t *testing.T) {
	ctx := settingsCtxWithConfigs(t)
	err := describeSettings(ctx, "Nope")
	if err == nil {
		t.Fatal("expected an error for an unknown configuration")
	}
	for _, want := range []string{"Nope", "Default", "Test"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

// No name still prints everything, as before.
func TestDescribeSettings_AllConfigurations(t *testing.T) {
	ctx := settingsCtxWithConfigs(t)
	buf := ctx.Output.(*bytes.Buffer)
	if err := describeSettings(ctx, ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"'Default'", "'Test'"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected every configuration, %q missing from:\n%s", want, out)
		}
	}
}

func TestShowSettings_SummaryCarriesRootURL(t *testing.T) {
	ctx := settingsCtxWithConfigs(t)
	buf := ctx.Output.(*bytes.Buffer)
	if err := listSettings(ctx); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "url=http://backend.local:8080/") {
		t.Errorf("summary should carry the root URL, got:\n%s", out)
	}
	// An empty DatabaseUrl leaves a gap where a value should be; it is omitted
	// rather than rendered as a bare comma.
	if strings.Contains(out, "Hsqldb, , ") {
		t.Errorf("summary should skip an empty DatabaseUrl, got:\n%s", out)
	}
}
