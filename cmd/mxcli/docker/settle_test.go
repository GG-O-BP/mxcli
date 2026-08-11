// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// mxcli-todo findings #7: mxbuild sits beside the mx binary `mxcli new` already
// resolved, so the settle build should not need a second download — including on
// macOS/Windows, where the resolved mx comes from a Studio Pro install that no
// version cache knows about.
func TestResolveMxBuildForSettle_NextToMx(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("binary names differ on Windows; the Linux/macOS layout is what this checks")
	}
	dir := t.TempDir()
	mxPath := filepath.Join(dir, "mx")
	mxbuildPath := filepath.Join(dir, "mxbuild")
	for _, p := range []string{mxPath, mxbuildPath} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if got := resolveMxBuildForSettle(mxPath, "11.12.1"); got != mxbuildPath {
		t.Errorf("got %q, want %q", got, mxbuildPath)
	}
}

// With no mxbuild beside mx and nothing cached for the version, there is nothing
// to run — the caller must get "" and report a warning, not run a random binary.
func TestResolveMxBuildForSettle_NotFound(t *testing.T) {
	dir := t.TempDir()
	mxPath := filepath.Join(dir, "mx")
	if err := os.WriteFile(mxPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A version string no cache directory can match.
	if got := resolveMxBuildForSettle(mxPath, "0.0.0-not-a-version"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestLastLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"fewer lines than asked for", "a\nb\n", 5, "a\nb"},
		{"truncates to the tail", "a\nb\nc\nd\n", 2, "c\nd"},
		{"empty", "", 3, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lastLines(tt.in, tt.n); got != tt.want {
				t.Errorf("lastLines(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
			}
		})
	}
}

// A settle build that cannot run must return an error the caller can print as a
// warning — never panic, and never take the project creation down with it.
func TestSettleGeneratedSources_NoMxBuild(t *testing.T) {
	dir := t.TempDir()
	mpr := filepath.Join(dir, "App.mpr")
	if err := os.WriteFile(mpr, []byte("not a real project"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := SettleGeneratedSources(mpr, filepath.Join(dir, "mx"), "0.0.0-not-a-version", os.Stdout)
	if err == nil {
		t.Fatal("expected an error when mxbuild cannot be found")
	}
	if !strings.Contains(err.Error(), "mxbuild") {
		t.Errorf("error should name what is missing, got: %v", err)
	}
}
