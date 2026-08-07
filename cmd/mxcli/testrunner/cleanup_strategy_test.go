// SPDX-License-Identifier: Apache-2.0

package testrunner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRollsBack(t *testing.T) {
	tests := []struct {
		name    string
		cleanup string
		want    bool
	}{
		{"absent annotation defaults to rollback", "", true},
		{"explicit rollback", CleanupRollback, true},
		{"explicit none", CleanupNone, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rollsBack(TestCase{Cleanup: tt.cleanup}); got != tt.want {
				t.Errorf("rollsBack(%q) = %v, want %v", tt.cleanup, got, tt.want)
			}
		})
	}
}

// TestRollbackIsTheDefault pins the contract the annotation has always
// documented: a test with no @cleanup rolls back. It went unimplemented until
// the endpoint gave the runner a context of its own to open a transaction on.
func TestRollbackIsTheDefault(t *testing.T) {
	if !rollsBack(TestCase{}) {
		t.Error("a test with no @cleanup annotation does not roll back")
	}
}

func TestValidateCleanup(t *testing.T) {
	for _, ok := range []string{"", CleanupRollback, CleanupNone} {
		if err := validateCleanup(ok); err != nil {
			t.Errorf("validateCleanup(%q) rejected a valid strategy: %v", ok, err)
		}
	}
}

// TestValidateCleanupRejectsATypo pins the reason this validation exists at all.
// Treating an unrecognised value as "not rollback" would leave the test's data
// in the database while the run still reported a clean pass — the worst
// available outcome, because nothing anywhere would say why.
func TestValidateCleanupRejectsATypo(t *testing.T) {
	err := validateCleanup("rollbak")
	if err == nil {
		t.Fatal("a misspelled strategy was accepted; it would silently skip the rollback")
	}
	if !strings.Contains(err.Error(), "rollbak") {
		t.Errorf("error %q does not quote the offending value", err)
	}
	// The message has to say what IS allowed, or the user is left guessing.
	for _, want := range []string{CleanupRollback, CleanupNone} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not list the valid strategy %q", err, want)
		}
	}
}

// TestParserRejectsABadCleanup pins that the rejection happens at parse time, so
// --list catches it too and no runtime is booted for a test file that cannot be
// run correctly.
func TestParserRejectsABadCleanup(t *testing.T) {
	body := `/**
 * @test something
 * @cleanup rollbak
 */
$r = CALL MICROFLOW Mod.A();
/
`
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.test.mdl")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := ParseTestFile(path); err == nil {
		t.Fatal("a .test.mdl with a misspelled @cleanup parsed without error")
	}
}

// TestMarkdownParserRejectsABadCleanup covers the other file format — the two
// parsers are separate code paths and the first version of this validation only
// reached one of them.
func TestMarkdownParserRejectsABadCleanup(t *testing.T) {
	body := "```mdl-test\n/**\n * @test something\n * @cleanup rollbak\n */\n$r = CALL MICROFLOW Mod.A();\n```\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.test.md")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := ParseTestFile(path); err == nil {
		t.Fatal("a .test.md with a misspelled @cleanup parsed without error")
	}
}

func TestEndpointJavaSupportsRollback(t *testing.T) {
	for _, want := range []string{
		`"1".equals(request.getParameter("rollback"))`,
		"ctx.startTransaction();",
		"ctx.rollbackTransaction();",
	} {
		if !strings.Contains(endpointJava, want) {
			t.Errorf("the handler is missing %q", want)
		}
	}
}

// TestEndpointRollbackIsInAFinallyBlock pins that a test which throws still gets
// its transaction rolled back. Without the finally, a failing test would be
// exactly the one that leaves its half-written data behind.
func TestEndpointRollbackIsInAFinallyBlock(t *testing.T) {
	execute := strings.Index(endpointJava, "Core.microflowCall(mf).execute")
	finallyIdx := strings.Index(endpointJava, "} finally {")
	rollbackIdx := strings.Index(endpointJava, "ctx.rollbackTransaction();")

	if finallyIdx < 0 {
		t.Fatal("the execution is not wrapped in try/finally")
	}
	if !(execute < finallyIdx && finallyIdx < rollbackIdx) {
		t.Errorf("the rollback is not in the finally block after execution (execute=%d finally=%d rollback=%d)",
			execute, finallyIdx, rollbackIdx)
	}
}

// TestEndpointStartsTheTransactionBeforeExecuting pins the ordering: a
// transaction opened after the microflow ran would roll back nothing.
func TestEndpointStartsTheTransactionBeforeExecuting(t *testing.T) {
	start := strings.Index(endpointJava, "ctx.startTransaction();")
	execute := strings.Index(endpointJava, "Core.microflowCall(mf).execute")
	if start < 0 || execute < 0 {
		t.Fatal("a landmark is missing")
	}
	if start > execute {
		t.Error("the transaction is started after the microflow runs, so it would roll back nothing")
	}
}

// TestEndpointReportsRollbackOutcome pins that a rollback which fails is
// reported rather than swallowed — otherwise the data stays and the run still
// says PASS.
func TestEndpointReportsRollbackOutcome(t *testing.T) {
	for _, want := range []string{`\"rolledBack\":`, `\"rollbackRequested\":`, `\"rollbackError\":`} {
		if !strings.Contains(endpointJava, want) {
			t.Errorf("the response does not carry %s", want)
		}
	}
}

func TestReportRollbackFailureDistinguishesCauses(t *testing.T) {
	tc := TestCase{Name: "some test"}

	tests := []struct {
		name string
		resp runResponse
		want string
	}{
		{
			name: "an endpoint that ignores the parameter",
			resp: runResponse{RollbackRequested: false},
			want: "older test endpoint",
		},
		{
			name: "a runtime that refused",
			resp: runResponse{RollbackRequested: true, RollbackError: "transaction already ended"},
			want: "transaction already ended",
		},
		{
			name: "no reason given",
			resp: runResponse{RollbackRequested: true},
			want: "no reason reported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			reportRollbackFailure(&buf, tc, &tt.resp)
			if !strings.Contains(buf.String(), tt.want) {
				t.Errorf("warning %q does not mention %q", buf.String(), tt.want)
			}
			if !strings.Contains(buf.String(), tc.Name) {
				t.Errorf("warning %q does not name the test", buf.String())
			}
		})
	}
}

func TestRollbackNote(t *testing.T) {
	tests := []struct {
		name      string
		requested bool
		resp      runResponse
		want      string
	}{
		{"committed", false, runResponse{}, "committed"},
		{"rolled back", true, runResponse{RolledBack: true}, "rolled back"},
		{"failed", true, runResponse{}, "ROLLBACK FAILED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rollbackNote(tt.requested, &tt.resp); !strings.Contains(got, tt.want) {
				t.Errorf("rollbackNote = %q, want it to contain %q", got, tt.want)
			}
		})
	}
}
