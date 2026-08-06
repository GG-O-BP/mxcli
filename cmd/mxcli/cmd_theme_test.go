// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/cmd/mxcli/theme"
	"github.com/spf13/cobra"
)

// runTheme drives the real cobra command rather than the package API. That
// distinction is the whole point of this file: the bug these tests cover lived
// in the command's argument handling, not in the theme package, so a test that
// calls theme.Resolve directly would keep passing while the CLI stayed broken.
func runTheme(t *testing.T, args ...string) (string, error) {
	t.Helper()
	for _, c := range []*cobra.Command{themeApplyCmd, themeRemoveCmd} {
		resetCmdFlags(c)
	}

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(append([]string{"theme"}, args...))
	err := rootCmd.ExecuteContext(context.Background())
	return out.String(), err
}

// themeProject fakes the parts of a Mendix project a theme touches.
func themeProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for path, body := range map[string]string{
		"App.mpr":                              "",
		"themesource/atlas_core/web/main.scss": "// atlas\n",
		"theme/web/main.scss":                  "@import \"custom-variables\";\n@import \"theme-dark\";\n",
		"theme/web/custom-variables.scss":      ":root {\n  --brand-primary: #264ae5;\n}\n",
	} {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func installedThemes(t *testing.T, dir string) []string {
	t.Helper()
	got, err := theme.Installed(dir)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// `mxcli theme remove -p app.mpr` — the invocation the docs show — used to
// target the default theme regardless of what was installed. On a project
// themed with anything else it removed nothing, reported every file as
// unchanged and exited 0, leaving the theme fully in place.
func TestThemeRemove_BareInvocationRemovesTheInstalledTheme(t *testing.T) {
	dir := themeProject(t)
	if _, err := runTheme(t, "apply", "ledger", "-p", dir); err != nil {
		t.Fatalf("apply ledger: %v", err)
	}
	if got := installedThemes(t, dir); len(got) != 1 || got[0] != "ledger" {
		t.Fatalf("setup failed: installed = %v", got)
	}

	if _, err := runTheme(t, "remove", "-p", dir); err != nil {
		t.Fatalf("bare remove: %v", err)
	}

	if got := installedThemes(t, dir); len(got) != 0 {
		t.Errorf("bare remove left %v installed", got)
	}
	main := filepath.Join(dir, "theme", "web", "main.scss")
	body, err := os.ReadFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "mxcli:theme") {
		t.Errorf("main.scss still carries a theme block:\n%s", body)
	}
}

// Removing from a project that has no theme is a mistake worth reporting, not
// a no-op that exits 0.
func TestThemeRemove_UnthemedProjectErrors(t *testing.T) {
	dir := themeProject(t)

	_, err := runTheme(t, "remove", "-p", dir)
	if err == nil {
		t.Fatal("expected an error removing from an unthemed project")
	}
	if !strings.Contains(err.Error(), "no mxcli theme found") {
		t.Errorf("error should say what is missing, got: %v", err)
	}
}

// A bare `apply` refreshes what is installed. Silently switching a ledger
// project to the default is as surprising as the remove bug was.
func TestThemeApply_BareInvocationRefreshesTheInstalledTheme(t *testing.T) {
	dir := themeProject(t)
	if _, err := runTheme(t, "apply", "console", "-p", dir); err != nil {
		t.Fatalf("apply console: %v", err)
	}

	if _, err := runTheme(t, "apply", "-p", dir); err != nil {
		t.Fatalf("bare apply: %v", err)
	}

	got := installedThemes(t, dir)
	if len(got) != 1 || got[0] != "console" {
		t.Errorf("bare apply changed the installed theme to %v, want [console]", got)
	}
}

// An unthemed project is exactly when falling back to the default is right.
func TestThemeApply_BareInvocationInstallsTheDefaultWhenThereIsNone(t *testing.T) {
	dir := themeProject(t)

	if _, err := runTheme(t, "apply", "-p", dir); err != nil {
		t.Fatalf("bare apply: %v", err)
	}
	got := installedThemes(t, dir)
	if len(got) != 1 || got[0] != theme.DefaultName {
		t.Errorf("installed = %v, want [%s]", got, theme.DefaultName)
	}
}

// Switching themes must leave exactly one theme behind in every file it
// touches — including the shared Atlas map, where the outgoing block used to
// survive and the incoming one was appended beside it.
func TestThemeApply_SwitchingLeavesNoOrphanBlocks(t *testing.T) {
	dir := themeProject(t)
	for _, name := range []string{"signal", "ledger", "console"} {
		if _, err := runTheme(t, "apply", name, "-p", dir); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if got := installedThemes(t, dir); len(got) != 1 || got[0] != name {
			t.Fatalf("after apply %s, installed = %v", name, got)
		}
	}

	// And a bare remove then leaves nothing, which is the combination the
	// report flagged: switch once, then run the documented removal.
	if _, err := runTheme(t, "remove", "-p", dir); err != nil {
		t.Fatalf("bare remove after switching: %v", err)
	}
	if got := installedThemes(t, dir); len(got) != 0 {
		t.Errorf("remove after switching left %v", got)
	}
}
