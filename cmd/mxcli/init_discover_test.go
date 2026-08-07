// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// mxcli-formula1 findings #3: with no .mpr at the target, `mxcli init` used to
// carry on against an invented `project.mpr`, writing tooling that points at a
// file which does not exist. In a solution repo — one app folder per app — that
// is silently the wrong answer, and with two apps there is no right guess.
func TestFindMprFilesInSubdirs(t *testing.T) {
	t.Run("finds one project per subdirectory, sorted", func(t *testing.T) {
		root := t.TempDir()
		for _, name := range []string{"Zeta", "Alpha"} {
			dir := filepath.Join(root, name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name+".mpr"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		got := findMprFilesInSubdirs(root)
		if len(got) != 2 {
			t.Fatalf("got %d candidates, want 2: %v", len(got), got)
		}
		// Sorted, so the refusal message and the suggested command are stable
		// rather than dependent on directory order.
		if filepath.Base(got[0]) != "Alpha.mpr" || filepath.Base(got[1]) != "Zeta.mpr" {
			t.Errorf("candidates are not sorted: %v", got)
		}
	})

	t.Run("ignores dot directories", func(t *testing.T) {
		root := t.TempDir()
		hidden := filepath.Join(root, ".mendix-cache")
		if err := os.MkdirAll(hidden, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hidden, "stale.mpr"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := findMprFilesInSubdirs(root); len(got) != 0 {
			t.Errorf("a dot directory is not a candidate project, got: %v", got)
		}
	})

	t.Run("one level only", func(t *testing.T) {
		root := t.TempDir()
		deep := filepath.Join(root, "apps", "Nested")
		if err := os.MkdirAll(deep, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(deep, "Nested.mpr"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		// A Mendix app keeps its .mpr at its own root; walking deeper would
		// start finding deployment copies and backups.
		if got := findMprFilesInSubdirs(root); len(got) != 0 {
			t.Errorf("expected no candidates two levels down, got: %v", got)
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		if got := findMprFilesInSubdirs(t.TempDir()); len(got) != 0 {
			t.Errorf("got %v, want none", got)
		}
	})
}
