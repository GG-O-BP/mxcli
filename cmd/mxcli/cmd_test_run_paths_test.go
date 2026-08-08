// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// mxcli-formula1 findings #13: `mxcli test tests/ -p app/App.mpr` failed with
// "no such file or directory" for a tests/ sitting right next to the .mpr,
// because the path resolved against the process CWD only. Defensible alone, but
// mxcli otherwise encourages naming the project instead of standing in its
// directory, so the two conventions collided.
func TestResolveTestPaths(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "app")
	if err := os.MkdirAll(filepath.Join(projectDir, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(projectDir, "App.mpr")
	if err := os.WriteFile(projectPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("falls back to the project directory", func(t *testing.T) {
		got := resolveTestPaths([]string{"tests"}, projectPath)
		want := filepath.Join(projectDir, "tests")
		if len(got) != 1 || got[0] != want {
			t.Errorf("got %v, want [%s]", got, want)
		}
	})

	t.Run("the working directory still wins", func(t *testing.T) {
		// A tests/ in both places must resolve to the one the user is standing
		// in — that is what every other tool does, and silently preferring the
		// project's copy would run the wrong suite.
		cwdTests := filepath.Join(root, "tests")
		if err := os.MkdirAll(cwdTests, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(root)

		got := resolveTestPaths([]string{"tests"}, projectPath)
		if len(got) != 1 || got[0] != "tests" {
			t.Errorf("got %v, want the CWD-relative [tests]", got)
		}
	})

	t.Run("an absolute path is untouched", func(t *testing.T) {
		abs := filepath.Join(projectDir, "tests")
		got := resolveTestPaths([]string{abs}, projectPath)
		if len(got) != 1 || got[0] != abs {
			t.Errorf("got %v, want [%s]", got, abs)
		}
	})

	t.Run("a path that exists nowhere keeps what was typed", func(t *testing.T) {
		// The error must name what the user wrote, not a rewritten path they
		// never mentioned.
		got := resolveTestPaths([]string{"nope"}, projectPath)
		if len(got) != 1 || got[0] != "nope" {
			t.Errorf("got %v, want [nope]", got)
		}
	})

	t.Run("no project means no rewriting", func(t *testing.T) {
		got := resolveTestPaths([]string{"tests"}, "")
		if len(got) != 1 || got[0] != "tests" {
			t.Errorf("got %v, want [tests]", got)
		}
	})
}
