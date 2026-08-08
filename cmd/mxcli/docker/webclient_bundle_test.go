// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeBundle creates a deployment whose web client is bundled.
func writeBundle(t *testing.T, deployDir, content string) {
	t.Helper()
	dist := filepath.Join(deployDir, "web", "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.js"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mxcli-formula1 §35: the boot's Gradle package pass repopulates deployment/web
// and removes dist/, deleting the bundle a previous step of the same command
// wrote. The app then serves a 200 HTML shell and a black screen — invisible to
// check, build, the runtime log and curl.
func TestEnsureWebClientBundle_RebundlesAfterThePackagingWipe(t *testing.T) {
	deployDir := t.TempDir()
	writeBundle(t, deployDir, "// bundled at 15:15:31")

	// What the Gradle package pass does at 15:16:22.
	if err := os.RemoveAll(filepath.Join(deployDir, "web", "dist")); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	called := 0
	rebuilt, err := ensureWebClientBundle(deployDir, &out, func() error {
		called++
		writeBundle(t, deployDir, "// re-bundled after boot")
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rebuilt || called != 1 {
		t.Fatalf("the wipe was not repaired: rebuilt=%v calls=%d", rebuilt, called)
	}
	if !WebClientBundled(deployDir) {
		t.Error("the app would still serve a blank page")
	}
	// Silence is what made this survive several restarts, so the repair is stated.
	if !strings.Contains(out.String(), "re-bundling") {
		t.Errorf("the re-bundle was not reported:\n%s", out.String())
	}
}

// A bundle that survived the boot must not be rebuilt — the guard costs a stat
// in the common case, which is what makes it affordable at every boot.
func TestEnsureWebClientBundle_LeavesASurvivingBundleAlone(t *testing.T) {
	deployDir := t.TempDir()
	writeBundle(t, deployDir, "// bundled, and Gradle had nothing to do")

	var out bytes.Buffer
	called := 0
	rebuilt, err := ensureWebClientBundle(deployDir, &out, func() error {
		called++
		return nil
	})
	if err != nil || rebuilt || called != 0 {
		t.Fatalf("re-bundled a bundle that was already there: rebuilt=%v calls=%d err=%v", rebuilt, called, err)
	}
	if out.Len() != 0 {
		t.Errorf("the quiet path should stay quiet, got:\n%s", out.String())
	}
}

// A failed re-bundle must name the consequence. The runtime is up and its
// services answer, so "the app is running" is not the whole truth.
func TestEnsureWebClientBundle_FailureNamesTheBlankPage(t *testing.T) {
	deployDir := t.TempDir()

	_, err := ensureWebClientBundle(deployDir, nil, func() error {
		return errors.New("rollup exploded")
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "blank page") || !strings.Contains(err.Error(), "rollup exploded") {
		t.Errorf("error should carry both the cause and the symptom, got: %v", err)
	}
}

// An empty dist/index.js is the same blank page as a missing one — a truncated
// write must not read as "bundled".
func TestWebClientBundled_RejectsAnEmptyBundle(t *testing.T) {
	deployDir := t.TempDir()
	writeBundle(t, deployDir, "")
	if WebClientBundled(deployDir) {
		t.Error("an empty bundle should not count as bundled")
	}

	// And a directory called index.js is not a bundle either.
	other := t.TempDir()
	if err := os.MkdirAll(filepath.Join(other, "web", "dist", "index.js"), 0o755); err != nil {
		t.Fatal(err)
	}
	if WebClientBundled(other) {
		t.Error("a directory named index.js should not count as bundled")
	}
}

// The second way in: a headless boot (mxcli test --local) destroys a bundle a
// previous `run --local` left behind. Tests do not need it, so the loss is
// reported rather than paid for — but it must not be silent, which is what let
// a test run between a boot and a browser look like a rendering bug.
func TestReportLostWebClientBundle(t *testing.T) {
	deployDir := t.TempDir()
	var out bytes.Buffer

	// Destroyed by this boot: say so, and name the remedy.
	if !ReportLostWebClientBundle(deployDir, true, &out) {
		t.Error("a destroyed bundle should be reported")
	}
	if !strings.Contains(out.String(), "blank page") || !strings.Contains(out.String(), "mxcli run --local") {
		t.Errorf("the note should carry the symptom and the fix:\n%s", out.String())
	}

	// Never there to begin with: not this boot's doing, so nothing to say.
	out.Reset()
	if ReportLostWebClientBundle(deployDir, false, &out) || out.Len() > 0 {
		t.Errorf("an absent bundle this boot did not destroy should be silent:\n%s", out.String())
	}

	// Survived: silent.
	out.Reset()
	writeBundle(t, deployDir, "// survived")
	if ReportLostWebClientBundle(deployDir, true, &out) || out.Len() > 0 {
		t.Errorf("a surviving bundle should be silent:\n%s", out.String())
	}
}
