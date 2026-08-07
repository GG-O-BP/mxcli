// SPDX-License-Identifier: Apache-2.0

package testrunner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A failing test makes the runner return false, which makes the runtime's
// after-startup action fail, which makes `start` report an error. That is a test
// verdict, not a broken run — the local path must recognise it and let the
// results be parsed rather than aborting with a runtime error.
func TestRunnerReportedVerdict(t *testing.T) {
	tests := []struct {
		name string
		log  string
		want bool
	}{
		{"clean finish", "INFO - MXTEST: MXTEST:END:tests\n", true},
		{"m2ee wording", "The after-startup-action failed with an exception or returned false.\n", true},
		{"runtime log wording", "2026-01-01 ERROR - Core: After-startup action failed.\n", true},
		{"still running", "INFO - MXTEST: MXTEST:RUN:test_1:adds\n", false},
		{"boot died early", "java.lang.OutOfMemoryError\n", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runnerReportedVerdict(tt.log); got != tt.want {
				t.Errorf("runnerReportedVerdict(%q) = %v, want %v", tt.log, got, tt.want)
			}
		})
	}
}

func TestScanTestLog(t *testing.T) {
	tests := []struct {
		name        string
		log         string
		wantDone    bool
		wantFailure bool
	}{
		{"end marker", "MXTEST:END:tests\n", true, false},
		{"after-startup success", "Successfully ran after-startup-action\n", true, false},
		{"failing test", "Core: After-startup action failed.\n", true, false},
		{"runtime failure", "Error starting runtime: boom\n", true, true},
		{"non-boolean runner", "After startup microflow should return a boolean\n", true, true},
		{"mid-run", "MXTEST:RUN:test_1:adds\n", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done, failMsg := scanTestLog(tt.log)
			if done != tt.wantDone {
				t.Errorf("done = %v, want %v", done, tt.wantDone)
			}
			if (failMsg != "") != tt.wantFailure {
				t.Errorf("failMsg = %q, want failure=%v", failMsg, tt.wantFailure)
			}
		})
	}
}

// The runtime log is appended across runs, so a run must read only what it
// wrote — otherwise the previous run's verdict is reported as this one's.
func TestReadFrom_SkipsEarlierRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.log")
	previous := "MXTEST:END:old run\n"
	if err := os.WriteFile(path, []byte(previous), 0o644); err != nil {
		t.Fatal(err)
	}

	offset := fileSize(path)
	if offset != int64(len(previous)) {
		t.Fatalf("offset = %d, want %d", offset, len(previous))
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("MXTEST:START:new run\n")
	f.Close()

	got := readFrom(path, offset)
	if strings.Contains(got, "old run") {
		t.Errorf("read included the previous run: %q", got)
	}
	if !strings.Contains(got, "new run") {
		t.Errorf("read missed this run's output: %q", got)
	}
}

func TestFileSize_MissingFileIsZero(t *testing.T) {
	if got := fileSize(filepath.Join(t.TempDir(), "absent.log")); got != 0 {
		t.Errorf("fileSize of a missing file = %d, want 0", got)
	}
}

func TestWaitForTestLog_ReturnsOnTerminalMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.log")
	content := "MXTEST:START:s\nMXTEST:PASS:test_1\nMXTEST:END:s\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	got, err := waitForTestLog(path, 0, 2*time.Second, &out, false)
	if err != nil {
		t.Fatalf("waitForTestLog: %v", err)
	}
	if !strings.Contains(got, "MXTEST:PASS:test_1") {
		t.Errorf("returned log missing the result line: %q", got)
	}
}

func TestWaitForTestLog_TimesOutWithoutMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.log")
	if err := os.WriteFile(path, []byte("still booting\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	got, err := waitForTestLog(path, 0, 300*time.Millisecond, &out, false)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	// The partial log still comes back, so the caller can show what happened.
	if !strings.Contains(got, "still booting") {
		t.Errorf("timeout dropped the partial log: %q", got)
	}
}
