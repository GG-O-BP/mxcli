// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/linter"
)

// `mxcli init` seeds a lint config excluding System. On a blank app the System
// module accounted for ~96% of all lint findings — entities you cannot
// document, give access rules to, or rename — which buried the findings about
// the developer's own code (issuetracker finding #9).
func TestWriteDefaultLintConfig(t *testing.T) {
	t.Run("creates the config and excludes System", func(t *testing.T) {
		dir := t.TempDir()

		path, created := writeDefaultLintConfig(dir)
		if !created {
			t.Fatal("expected the config to be created in an empty project")
		}
		if want := filepath.Join(dir, ".claude", "lint-config.yaml"); path != want {
			t.Errorf("path = %q, want %q", path, want)
		}

		// It must parse as a real config, not just look right.
		cfg, err := linter.LoadConfig(path)
		if err != nil {
			t.Fatalf("the seeded config does not load: %v", err)
		}
		if len(cfg.ExcludeModules) != 1 || cfg.ExcludeModules[0] != "System" {
			t.Errorf("ExcludeModules = %v, want [System]", cfg.ExcludeModules)
		}

		// And lint must actually find it where it looks.
		if found := linter.FindConfigFile(dir); found != path {
			t.Errorf("FindConfigFile = %q, want %q — lint would not pick it up", found, path)
		}
	})

	t.Run("never overwrites an existing config", func(t *testing.T) {
		// init is re-runnable and the config is meant to be edited, so a second
		// run must not discard the developer's changes.
		for _, existing := range []string{
			filepath.Join(".claude", "lint-config.yaml"),
			"lint-config.yaml",
			".lint-config.yaml",
		} {
			t.Run(existing, func(t *testing.T) {
				dir := t.TempDir()
				full := filepath.Join(dir, existing)
				if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
					t.Fatal(err)
				}
				const mine = "excludeModules: [MyOwnModule]\n"
				if err := os.WriteFile(full, []byte(mine), 0644); err != nil {
					t.Fatal(err)
				}

				if _, created := writeDefaultLintConfig(dir); created {
					t.Error("reported creating a config when one already existed")
				}
				got, err := os.ReadFile(full)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != mine {
					t.Errorf("existing config was modified:\n%s", got)
				}
				// It must not have written a competing config elsewhere either.
				other := filepath.Join(dir, ".claude", "lint-config.yaml")
				if other != full {
					if _, err := os.Stat(other); err == nil {
						t.Error("wrote a second, competing config at .claude/lint-config.yaml")
					}
				}
			})
		}
	})
}

// An exclude beats --modules (LintContext.IsExcluded checks the exclude set
// first), so once init ships a config excluding System, `lint -m System`
// returns nothing. intersect drives the warning that explains why.
func TestIntersect(t *testing.T) {
	tests := []struct {
		name string
		want []string
		have []string
		out  []string
	}{
		{name: "shadowed module detected", want: []string{"System"}, have: []string{"System"}, out: []string{"System"}},
		{name: "unshadowed module ignored", want: []string{"MyModule"}, have: []string{"System"}, out: nil},
		{
			name: "only the overlap, in the caller's order",
			want: []string{"MyModule", "System", "Atlas_Core"},
			have: []string{"Atlas_Core", "System"},
			out:  []string{"System", "Atlas_Core"},
		},
		{name: "duplicates collapse", want: []string{"System", "System"}, have: []string{"System"}, out: []string{"System"}},
		{name: "no filter", want: nil, have: []string{"System"}, out: nil},
		{name: "no excludes", want: []string{"System"}, have: nil, out: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := intersect(tc.want, tc.have)
			if strings.Join(got, ",") != strings.Join(tc.out, ",") {
				t.Errorf("intersect(%v, %v) = %v, want %v", tc.want, tc.have, got, tc.out)
			}
		})
	}
}
