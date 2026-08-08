// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mxcli-formula1 findings #12: `ALTER MODULE X ADD JAR DEPENDENCY (…)` writes the
// coordinate and `list jar dependencies` reports it, but nothing puts the jar on
// the classpath. Measured on 11.12.1: a full `mxbuild --target=deploy` emits a
// build.gradle with no dependencies block and downloads nothing, while
// `mx sync-java-dependencies` fetches it into vendorlib/ — resolution is a
// separate step. Knowing which coordinates are unvendored is what lets a caller
// run that step only when it is needed.
func TestUnvendoredJarDependencies(t *testing.T) {
	dir := t.TempDir()
	vendorlib := filepath.Join(dir, "vendorlib")
	if err := os.MkdirAll(vendorlib, 0o755); err != nil {
		t.Fatal(err)
	}
	// mx writes <artifact>-<version>.jar.
	if err := os.WriteFile(filepath.Join(vendorlib, "commons-lang3-3.14.0.jar"), []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := []JarDependencyRef{
		{Group: "org.apache.commons", Artifact: "commons-lang3", Version: "3.14.0"}, // present
		{Group: "org.duckdb", Artifact: "duckdb_jdbc", Version: "1.5.5.1"},          // missing
		// A different version of a vendored artifact is still missing: the file
		// name carries the version, and the wrong jar is not the right jar.
		{Group: "org.apache.commons", Artifact: "commons-lang3", Version: "3.12.0"},
	}

	got := UnvendoredJarDependencies(dir, deps)
	if len(got) != 2 {
		t.Fatalf("got %d missing, want 2: %v", len(got), got)
	}
	for _, want := range []string{"org.duckdb:duckdb_jdbc:1.5.5.1", "org.apache.commons:commons-lang3:3.12.0"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected %q among the missing, got: %v", want, got)
		}
	}
}

// An incomplete coordinate cannot be checked or resolved, and must not be
// reported as missing — that would send the caller into a sync that cannot help.
func TestUnvendoredJarDependencies_SkipsIncompleteCoordinates(t *testing.T) {
	dir := t.TempDir()
	deps := []JarDependencyRef{
		{Group: "g", Artifact: "", Version: "1.0"},
		{Group: "g", Artifact: "a", Version: ""},
	}
	if got := UnvendoredJarDependencies(dir, deps); len(got) != 0 {
		t.Errorf("expected incomplete coordinates to be skipped, got: %v", got)
	}
}

// No vendorlib/ at all is the fresh-clone case: everything declared is missing.
func TestUnvendoredJarDependencies_NoVendorlib(t *testing.T) {
	deps := []JarDependencyRef{{Group: "g", Artifact: "a", Version: "1.0"}}
	if got := UnvendoredJarDependencies(t.TempDir(), deps); len(got) != 1 {
		t.Errorf("got %v, want the one declared dependency", got)
	}
}

// mx's own failure text has to reach the caller. This whole finding was a
// silent failure, so a sync that does not work must not look like one that did.
func TestSyncJavaDependencies_SurfacesMxFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "NoSuch.mpr")
	err := SyncJavaDependencies(missing, "", "0.0.0-not-a-version", nil)
	if err == nil {
		t.Fatal("expected an error for a project file that does not exist")
	}
	// Either mx is unavailable (named as such) or mx ran and complained; both
	// are errors that say what happened, which is the contract under test.
	msg := err.Error()
	if !strings.Contains(msg, "mx not found") && !strings.Contains(msg, "sync-java-dependencies failed") {
		t.Errorf("error should name the failure, got: %v", err)
	}
}
