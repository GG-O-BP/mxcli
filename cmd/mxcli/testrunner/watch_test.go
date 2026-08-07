// SPDX-License-Identifier: Apache-2.0

package testrunner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateOptions(t *testing.T) {
	tests := []struct {
		name    string
		opts    RunOptions
		wantErr string
	}{
		{name: "no watch is always fine", opts: RunOptions{}},
		{name: "watch with local", opts: RunOptions{Watch: true, Local: true}},
		{
			name:    "watch without local",
			opts:    RunOptions{Watch: true},
			wantErr: "--watch requires --local",
		},
		{
			name:    "watch with the legacy runner",
			opts:    RunOptions{Watch: true, Local: true, LegacyRunner: true},
			wantErr: "--watch cannot be combined with --legacy-runner",
		},
		{
			name:    "watch with skip-build",
			opts:    RunOptions{Watch: true, Local: true, SkipBuild: true},
			wantErr: "--watch cannot be combined with --skip-build",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOptions(tt.opts)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// writeTestFile writes a test file with a controlled mtime.
func writeTestFile(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("/** @test x */\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}

// TestTestFilesMTimeSeesAnEditInsideADirectory pins the reason the directory is
// walked rather than stat'ed: on Linux a directory's own mtime does not move
// when an existing entry is edited in place, which is the common case.
func TestTestFilesMTimeSeesAnEditInsideADirectory(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-2 * time.Hour)
	f := filepath.Join(dir, "a.test.mdl")
	writeTestFile(t, f, old)
	// Pin the directory itself to the old time, so only the file can move.
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatalf("chtimes dir: %v", err)
	}

	before := testFilesMTime([]string{dir})

	edited := time.Now()
	writeTestFile(t, f, edited)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatalf("chtimes dir: %v", err)
	}

	after := testFilesMTime([]string{dir})
	if !after.After(before) {
		t.Errorf("editing a file in a watched directory produced no change signal (before=%v after=%v)", before, after)
	}
}

func TestTestFilesMTimeAcceptsAFilePath(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.test.mdl")
	want := time.Now().Add(-time.Hour).Truncate(time.Second)
	writeTestFile(t, f, want)

	if got := testFilesMTime([]string{f}); !got.Truncate(time.Second).Equal(want) {
		t.Errorf("mtime = %v, want %v", got, want)
	}
}

// TestTestFilesMTimeIgnoresNonTestFiles keeps an unrelated file in the tests
// directory — a README, an editor swap file — from re-triggering the loop.
func TestTestFilesMTimeIgnoresNonTestFiles(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-2 * time.Hour)
	writeTestFile(t, filepath.Join(dir, "a.test.mdl"), old)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatalf("chtimes dir: %v", err)
	}
	before := testFilesMTime([]string{dir})

	noise := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(noise, []byte("scratch"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	now := time.Now()
	if err := os.Chtimes(noise, now, now); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	// Hold the directory back so only the noise file could move the signal.
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatalf("chtimes dir: %v", err)
	}

	if after := testFilesMTime([]string{dir}); after.After(before) {
		t.Error("a non-test file in the tests directory moved the change signal")
	}
}

// TestTestFilesMTimeSeesADeletion pins that removing a test is a change. The
// walk cannot see a file that is gone, so the directory's own mtime — which does
// move on a deletion — has to be folded in.
func TestTestFilesMTimeSeesADeletion(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-2 * time.Hour)
	keep := filepath.Join(dir, "a.test.mdl")
	gone := filepath.Join(dir, "b.test.mdl")
	writeTestFile(t, keep, old)
	writeTestFile(t, gone, old)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatalf("chtimes dir: %v", err)
	}
	before := testFilesMTime([]string{dir})

	if err := os.Remove(gone); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if after := testFilesMTime([]string{dir}); !after.After(before) {
		t.Errorf("deleting a test file produced no change signal (before=%v after=%v)", before, after)
	}
}

// TestStaleTestFlowsDropsRemovedTests pins the correctness hazard of re-running:
// CREATE OR REPLACE updates a test that changed but says nothing about one that
// was deleted, whose microflow would otherwise linger and keep reporting a pass.
func TestStaleTestFlowsDropsRemovedTests(t *testing.T) {
	old := &TestSuite{Tests: []TestCase{{ID: "test_1"}, {ID: "test_2"}, {ID: "test_3"}}}
	new := &TestSuite{Tests: []TestCase{{ID: "test_1"}, {ID: "test_2"}}}

	got := staleTestFlows(old, new)
	want := []string{"DROP MICROFLOW MxTest.Test_test_3"}
	if len(got) != len(want) || (len(got) > 0 && got[0] != want[0]) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStaleTestFlowsNoneWhenSuiteGrew(t *testing.T) {
	old := &TestSuite{Tests: []TestCase{{ID: "test_1"}}}
	new := &TestSuite{Tests: []TestCase{{ID: "test_1"}, {ID: "test_2"}}}

	if got := staleTestFlows(old, new); len(got) != 0 {
		t.Errorf("got %q, want no drops when tests were only added", got)
	}
}

// TestStaleTestFlowsDropsAllWhenEmptied covers deleting the last test in a file:
// every previously-injected flow has to come out.
func TestStaleTestFlowsDropsAllWhenEmptied(t *testing.T) {
	old := &TestSuite{Tests: []TestCase{{ID: "test_1"}, {ID: "test_2"}}}
	new := &TestSuite{}

	if got := staleTestFlows(old, new); len(got) != 2 {
		t.Errorf("got %d drops %q, want 2", len(got), got)
	}
}

func TestStaleTestFlowsHandlesNoPriorSuite(t *testing.T) {
	if got := staleTestFlows(nil, &TestSuite{Tests: []TestCase{{ID: "test_1"}}}); got != nil {
		t.Errorf("got %q, want nil for a first injection", got)
	}
}
