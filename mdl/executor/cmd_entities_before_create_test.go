// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/backend/mock"
	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/microflows"
)

// mxcli-todo findings #14a: Mendix passes no object to a BEFORE CREATE handler —
// the object does not exist yet — so wiring a microflow that takes parameters
// there builds as CE7247 "Microflow should not have parameters". `mxcli check`
// could not see it, and the model was written before anyone found out.
func TestCheckBeforeCreateHandlerHasNoParameters(t *testing.T) {
	withParams := &microflows.Microflow{
		Name:       "OCH_SetDefaults",
		Parameters: []*microflows.MicroflowParameter{{Name: "Task"}},
	}
	noParams := &microflows.Microflow{Name: "OCH_Audit"}

	tests := []struct {
		name      string
		moment    string
		event     string
		mf        *microflows.Microflow
		wantError bool
	}{
		{"before create with parameters is refused", "Before", "Create", withParams, true},
		{"before create without parameters is fine", "Before", "Create", noParams, false},
		// Every other moment/event receives the object, so parameters are correct there.
		{"after create with parameters is fine", "After", "Create", withParams, false},
		{"before commit with parameters is fine", "Before", "Commit", withParams, false},
		{"before delete with parameters is fine", "Before", "Delete", withParams, false},
		// MDL keywords are case-insensitive; the guard must not be.
		{"lowercase spelling is still matched", "before", "create", withParams, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &ExecContext{Backend: &mock.MockBackend{
				GetMicroflowFunc: func(model.ID) (*microflows.Microflow, error) { return tt.mf, nil },
			}}
			def := ast.EventHandlerDef{Moment: tt.moment, Event: tt.event}
			err := checkBeforeCreateHandlerHasNoParameters(ctx, def, "M.OCH_SetDefaults", model.ID("mf-1"))

			if tt.wantError {
				if err == nil {
					t.Fatal("expected the handler to be refused")
				}
				// The message has to carry the build error and the way out, or
				// the user is no better off than with CE7247 alone.
				for _, want := range []string{"CE7247", "AFTER CREATE", "$Task"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error should mention %q, got: %v", want, err)
					}
				}
			} else if err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

// A microflow created earlier in the same script is not readable back yet. The
// guard must skip rather than refuse — mxbuild still catches the real case, and
// failing on an unreadable microflow would break legitimate scripts.
func TestCheckBeforeCreateHandler_UnreadableMicroflowIsSkipped(t *testing.T) {
	ctx := &ExecContext{Backend: &mock.MockBackend{
		GetMicroflowFunc: func(model.ID) (*microflows.Microflow, error) {
			return nil, errUnreadable
		},
	}}
	def := ast.EventHandlerDef{Moment: "Before", Event: "Create"}
	if err := checkBeforeCreateHandlerHasNoParameters(ctx, def, "M.MF", model.ID("mf-1")); err != nil {
		t.Errorf("expected the guard to skip an unreadable microflow, got: %v", err)
	}
}

var errUnreadable = &unreadableError{}

type unreadableError struct{}

func (e *unreadableError) Error() string { return "not readable yet" }
