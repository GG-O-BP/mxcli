// SPDX-License-Identifier: Apache-2.0

//go:build integration

package docker

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mendixlabs/mxcli/sdk/mpr"
)

// TestBuild_PreservesMPRv2StorageFormat is the end-to-end guard for
// mendixlabs/mxcli#808, the counterpart to TestCheck_PreservesMPRv2StorageFormat
// (#763). Build's pre-check step ran its own bare `mx update-widgets`, which
// rewrites an MPRv2 project into the self-contained MPRv1 format — inlining every
// unit into the .mpr and deleting mprcontents/. #764 protected Check only, so
// `mxcli docker build`, `docker run` and `docker reload` kept converting projects
// while reporting success.
//
// DryRun stops Build immediately after the check step, so this exercises the whole
// buggy path (update-widgets + mx check) without paying for a full MxBuild. The
// deferred restore still runs on the DryRun return.
//
// Requires a resolvable mx/MxBuild and a JDK 21 (provided by the CI integration
// job); skips otherwise.
func TestBuild_PreservesMPRv2StorageFormat(t *testing.T) {
	mxPath, err := ResolveMx("")
	if err != nil {
		t.Skipf("mx not resolvable: %v", err)
	}
	if _, err := resolveJDK21(); err != nil {
		t.Skipf("JDK 21 not resolvable: %v", err)
	}

	// Scaffold a fresh project. `mx create-project` (no template arg) writes App.mpr
	// into the working directory and produces MPRv2 storage.
	dir := t.TempDir()
	scaffold := exec.Command(mxPath, "create-project")
	scaffold.Dir = dir
	if out, err := scaffold.CombinedOutput(); err != nil {
		t.Skipf("mx create-project failed, cannot scaffold fixture: %v\n%s", err, out)
	}
	mprPath := filepath.Join(dir, "App.mpr")
	if _, err := os.Stat(mprPath); err != nil {
		t.Skipf("mx create-project did not produce App.mpr: %v", err)
	}

	// Precondition: the fixture must be MPRv2, or the test proves nothing.
	if v := mprStorageVersion(t, mprPath); v != mpr.MPRVersionV2 {
		t.Skipf("scaffolded project is %v, not MPRv2 — nothing to protect", v)
	}

	var stdout bytes.Buffer
	if err := Build(BuildOptions{
		ProjectPath: mprPath,
		DryRun:      true,
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("Build failed: %v\nstdout:\n%s", err, stdout.String())
	}

	// Postcondition: still MPRv2. Without the fix, update-widgets would have left it
	// MPRv1 with mprcontents/ deleted.
	if v := mprStorageVersion(t, mprPath); v != mpr.MPRVersionV2 {
		t.Errorf("Build converted the project to %v; the MPRv2 storage format must be preserved (#808)", v)
	}
	if _, err := os.Stat(filepath.Join(dir, "mprcontents")); err != nil {
		t.Errorf("mprcontents/ missing after Build, storage format was not preserved: %v", err)
	}
}
