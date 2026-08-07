// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// fakeServe stands in for `mxbuild --serve`, recording the build request it was
// sent so a test can assert on what actually went over the wire.
func fakeServe(t *testing.T) (*ServeServer, *BuildRequest) {
	t.Helper()
	var got BuildRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"Success"}`)
	}))
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parsing test server URL: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parsing test server port: %v", err)
	}
	return &ServeServer{Host: u.Hostname(), Port: port}, &got
}

// TestBuildAbsolutizesProjectPath pins the fix for MxBuild's "the project file
// path should be an absolute path" rejection. `mxcli run` used to absolutize at
// the CLI layer, which left `mxcli test --local` hitting the raw error through
// StartLocalApp; doing it in Build covers every caller.
func TestBuildAbsolutizesProjectPath(t *testing.T) {
	dir := t.TempDir()
	mpr := filepath.Join(dir, "App.mpr")
	if err := os.WriteFile(mpr, []byte("x"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	// Run from the project directory so "App.mpr" is a valid relative path.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(cwd) })

	srv, got := fakeServe(t)
	if _, err := srv.Build(BuildRequest{Target: TargetDeploy, ProjectFilePath: "App.mpr"}); err != nil {
		t.Fatalf("Build: %v", err)
	}

	if !filepath.IsAbs(got.ProjectFilePath) {
		t.Fatalf("MxBuild was sent a relative path %q; it rejects those", got.ProjectFilePath)
	}
	// EvalSymlinks because macOS /tmp is a symlink to /private/tmp.
	wantResolved, _ := filepath.EvalSymlinks(mpr)
	gotResolved, _ := filepath.EvalSymlinks(got.ProjectFilePath)
	if gotResolved != wantResolved {
		t.Errorf("sent %q, want %q", gotResolved, wantResolved)
	}
}

// TestBuildLeavesAnAbsolutePathAlone guards against the resolution mangling a
// path that was already correct.
func TestBuildLeavesAnAbsolutePathAlone(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "App.mpr")
	srv, got := fakeServe(t)
	if _, err := srv.Build(BuildRequest{Target: TargetDeploy, ProjectFilePath: abs}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got.ProjectFilePath != abs {
		t.Errorf("absolute path was rewritten: got %q, want %q", got.ProjectFilePath, abs)
	}
}

// TestBuildDefaultsTargetToDeploy pins the pre-existing default, which the
// absolutization now sits next to.
func TestBuildDefaultsTargetToDeploy(t *testing.T) {
	srv, got := fakeServe(t)
	if _, err := srv.Build(BuildRequest{ProjectFilePath: filepath.Join(t.TempDir(), "App.mpr")}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got.Target != TargetDeploy {
		t.Errorf("Target = %q, want %q", got.Target, TargetDeploy)
	}
}

// TestLocalAppOptionsAbsolutizeProjectPath pins that DeployDir is not derived
// from a relative project path.
func TestLocalAppOptionsAbsolutizeProjectPath(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(cwd) })

	o := LocalAppOptions{ProjectPath: "App.mpr"}
	o.applyDefaults()

	if !filepath.IsAbs(o.ProjectPath) {
		t.Errorf("ProjectPath = %q, want an absolute path", o.ProjectPath)
	}
	if !filepath.IsAbs(o.DeployDir) {
		t.Errorf("DeployDir = %q, want an absolute path", o.DeployDir)
	}
}
