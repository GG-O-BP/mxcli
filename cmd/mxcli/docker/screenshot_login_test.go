// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mxcli-todo findings #16: the unlicensed local runtime caps concurrent
// sessions, and the login page reports that as "Sign in failed" — pointing at
// the credentials. The real reason is only in the runtime log, so a failed
// sign-in has to go and read it.
func TestLoginFailureHint(t *testing.T) {
	dir := t.TempDir()

	capped := filepath.Join(dir, "capped.log")
	if err := os.WriteFile(capped, []byte(
		"INFO - Core: Starting\n"+
			"ERROR - Security: Maximum number of sessions exceeded! (You are currently using a trial license)\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	hint := loginFailureHint(capped)
	if hint == "" {
		t.Fatal("expected a hint when the log shows the session cap")
	}
	// The hint is only worth printing if it names the cause and the way out.
	for _, want := range []string{sessionCapMarker, "Sign in failed", "run --local"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint should mention %q, got:\n%s", want, hint)
		}
	}

	quiet := filepath.Join(dir, "quiet.log")
	if err := os.WriteFile(quiet, []byte("INFO - Core: Starting\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loginFailureHint(quiet); got != "" {
		t.Errorf("expected no hint for an ordinary log, got: %s", got)
	}

	// A disabled (`-`), unset, or missing log must not turn into an error path:
	// the login failure itself is what the caller reports.
	for _, path := range []string{"", "-", filepath.Join(dir, "absent.log")} {
		if got := loginFailureHint(path); got != "" {
			t.Errorf("loginFailureHint(%q) = %q, want empty", path, got)
		}
	}
}

// The marker can sit far back in a long-running log; only the tail is read, so
// check the tail window actually holds recent lines.
func TestReadLogTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.log")
	body := strings.Repeat("filler line that is here only to push the file past the window\n", 2000)
	if err := os.WriteFile(path, []byte(body+"the last line\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tail, err := readLogTail(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) > 1024 {
		t.Errorf("read %d bytes, want at most 1024", len(tail))
	}
	if !strings.Contains(tail, "the last line") {
		t.Error("tail should contain the end of the file")
	}

	// A file smaller than the window is read whole, not skipped.
	small := filepath.Join(t.TempDir(), "small.log")
	if err := os.WriteFile(small, []byte("short\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := readLogTail(small, 1024); err != nil || got != "short\n" {
		t.Errorf("readLogTail(small) = %q, %v", got, err)
	}
}
