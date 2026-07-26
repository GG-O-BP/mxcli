// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/backend/mock"
	"github.com/mendixlabs/mxcli/mdl/visitor"
	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/domainmodel"
)

var errBoom = errors.New("boom")

// TestExecuteProgramContinueOnError verifies that continue-on-error attempts
// every statement (rather than halting on the first failure), counts outcomes,
// and reports each failure with its statement number. Findings #10.
func TestExecuteProgramContinueOnError(t *testing.T) {
	// A connected backend that can answer `show entities` (empty) but has no
	// `ListEnumerations` configured — so `show enumerations` fails.
	mb := &mock.MockBackend{
		IsConnectedFunc:      func() bool { return true },
		ListModulesFunc:      func() ([]*model.Module, error) { return nil, nil },
		ListDomainModelsFunc: func() ([]*domainmodel.DomainModel, error) { return nil, nil },
		// `show enumerations` fails; `show entities` succeeds.
		ListEnumerationsFunc: func() ([]*model.Enumeration, error) { return nil, errBoom },
	}
	e := New(&bytes.Buffer{})
	e.backend = mb

	prog, errs := visitor.Build("show entities;\nshow enumerations;\nshow entities;")
	if len(errs) > 0 {
		t.Fatalf("parse error: %v", errs[0])
	}

	var report bytes.Buffer
	res, err := e.ExecuteProgramContinueOnError(prog, &report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Total != 3 || res.Succeeded != 2 || res.Failed != 1 {
		t.Fatalf("got %+v, want Total=3 Succeeded=2 Failed=1", res)
	}
	// The middle statement failed and was reported with its 1-based index; the
	// third statement still ran (no halt), which is why Succeeded==2.
	if !strings.Contains(report.String(), "statement 2:") {
		t.Errorf("expected failure report for statement 2, got:\n%s", report.String())
	}
}

// TestExecuteProgramContinueOnError_HaltsOnExit verifies that an `exit`/`quit`
// statement still stops the run and surfaces ErrExit, even in continue mode.
func TestExecuteProgramContinueOnError_HaltsOnExit(t *testing.T) {
	e := New(&bytes.Buffer{})
	// Not connected: `show entities` fails, but `exit` must stop the run.
	prog, errs := visitor.Build("show entities;\nexit;\nshow entities;")
	if len(errs) > 0 {
		t.Fatalf("parse error: %v", errs[0])
	}

	var report bytes.Buffer
	res, err := e.ExecuteProgramContinueOnError(prog, &report)
	if err == nil || !strings.Contains(err.Error(), ErrExit.Error()) {
		t.Fatalf("expected ErrExit, got %v", err)
	}
	// Statement 1 failed (not connected) and was reported; the run then stopped
	// at `exit` before reaching statement 3.
	if res.Total != 2 {
		t.Errorf("expected 2 statements attempted before exit, got %d", res.Total)
	}
}
