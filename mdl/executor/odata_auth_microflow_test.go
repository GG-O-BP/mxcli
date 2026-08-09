// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/visitor"
	"github.com/mendixlabs/mxcli/model"
)

// authService parses a service with the given authentication clause.
func authService(t *testing.T, clause string) *ast.CreateODataServiceStmt {
	t.Helper()
	script := `create odata service T.Api (
  path: 'odata/t/', version: '1.0.0', ODataVersion: OData4,
  namespace: 'T.Api', ServiceName: 'Api'
)
` + clause + `
{
  publish entity T.Row as 'Rows' (
    ReadMode: source, InsertMode: not_supported,
    UpdateMode: not_supported, DeleteMode: not_supported
  )
  expose ( K as 'k' (KEY) );
};`
	prog, errs := visitor.Build(script)
	if len(errs) > 0 {
		t.Fatalf("parse: %v", errs)
	}
	svc, ok := prog.Statements[0].(*ast.CreateODataServiceStmt)
	if !ok {
		t.Fatalf("expected CreateODataServiceStmt, got %T", prog.Statements[0])
	}
	return svc
}

// mxcli-formula1 §40: the grammar always accepted the qualified name; the
// visitor discarded it. So `authentication microflow M.Auth` parsed, checked and
// executed while the model got a Microflow method with no microflow — and
// Mendix refused to build it (CE0333). Custom authentication is the fix for the
// per-request BCrypt cost, so this gap kept a real optimisation out of reach of
// a project whose model is written as MDL.
func TestODataAuth_CapturesTheMicroflowName(t *testing.T) {
	svc := authService(t, "authentication microflow T.Authenticate")
	if svc.AuthMicroflow != "T.Authenticate" {
		t.Errorf("AuthMicroflow = %q, want T.Authenticate — the name was discarded", svc.AuthMicroflow)
	}
	if len(svc.AuthenticationTypes) != 1 || svc.AuthenticationTypes[0] != "Microflow" {
		t.Errorf("AuthenticationTypes = %v, want [Microflow]", svc.AuthenticationTypes)
	}
}

// The method list is comma-separated, so the microflow must attach to its own
// entry and not swallow the neighbours.
func TestODataAuth_MicroflowAlongsideOtherMethods(t *testing.T) {
	svc := authService(t, "authentication basic, microflow T.Authenticate, guest")
	if svc.AuthMicroflow != "T.Authenticate" {
		t.Errorf("AuthMicroflow = %q", svc.AuthMicroflow)
	}
	want := []string{"Basic", "Microflow", "Guest"}
	if strings.Join(svc.AuthenticationTypes, ",") != strings.Join(want, ",") {
		t.Errorf("AuthenticationTypes = %v, want %v", svc.AuthenticationTypes, want)
	}
}

// Methods that carry no target must stay unaffected.
func TestODataAuth_NoMicroflowForOtherMethods(t *testing.T) {
	svc := authService(t, "authentication basic, session")
	if svc.AuthMicroflow != "" {
		t.Errorf("AuthMicroflow = %q, want empty", svc.AuthMicroflow)
	}
}

// The name is optional in the grammar, so the incomplete form still parses —
// and is exactly what produced CE0333. Check has to catch it.
func TestODataAuth_FlagsMicroflowWithNoName(t *testing.T) {
	svc := authService(t, "authentication microflow")
	if svc.AuthMicroflow != "" {
		t.Fatalf("AuthMicroflow = %q, want empty for this fixture", svc.AuthMicroflow)
	}
	prog := &ast.Program{Statements: []ast.Statement{svc}}
	vs := ValidateODataAuth(prog)
	if len(vs) != 1 || vs[0].RuleID != "MDL-ODATA04" {
		t.Fatalf("expected one MDL-ODATA04, got %+v", vs)
	}
	if !strings.Contains(vs[0].Suggestion, "CE0333") {
		t.Errorf("the suggestion should name the build error it prevents: %q", vs[0].Suggestion)
	}
}

// A named microflow must not be flagged — otherwise the rule punishes the fix
// it recommends.
func TestODataAuth_SilentWhenTheMicroflowIsNamed(t *testing.T) {
	svc := authService(t, "authentication microflow T.Authenticate")
	if vs := ValidateODataAuth(&ast.Program{Statements: []ast.Statement{svc}}); len(vs) != 0 {
		t.Errorf("a named microflow must not be flagged: %+v", vs)
	}
}

// Nor must a service that never asked for custom authentication.
func TestODataAuth_SilentForOtherMethods(t *testing.T) {
	svc := authService(t, "authentication basic, session")
	if vs := ValidateODataAuth(&ast.Program{Statements: []ast.Statement{svc}}); len(vs) != 0 {
		t.Errorf("basic/session must not be flagged: %+v", vs)
	}
}

// DESCRIBE emits re-executable MDL. Emitting the microflow as a comment
// (`-- Auth Microflow: …`) made the output look complete while replaying into a
// service Mendix rejects — the same shape as §39.
func TestDescribeODataService_RoundTripsTheAuthMicroflow(t *testing.T) {
	got := odataAuthClause(&model.PublishedODataService{
		AuthenticationTypes: []string{"Microflow"},
		AuthMicroflow:       "T.Authenticate",
	})
	if got != "Microflow T.Authenticate" {
		t.Errorf("got %q, want %q", got, "Microflow T.Authenticate")
	}
}

func TestDescribeODataService_AuthClauseKeepsOtherMethods(t *testing.T) {
	got := odataAuthClause(&model.PublishedODataService{
		AuthenticationTypes: []string{"Basic", "Microflow"},
		AuthMicroflow:       "T.Authenticate",
	})
	if got != "Basic, Microflow T.Authenticate" {
		t.Errorf("got %q", got)
	}
}

// A stored microflow with no matching method would otherwise vanish from the
// output; append it rather than lose it.
func TestDescribeODataService_AuthMicroflowWithoutAMethodIsStillEmitted(t *testing.T) {
	got := odataAuthClause(&model.PublishedODataService{
		AuthenticationTypes: []string{"Basic"},
		AuthMicroflow:       "T.Authenticate",
	})
	if got != "Basic, Microflow T.Authenticate" {
		t.Errorf("got %q", got)
	}
}

// The whole point: what DESCRIBE prints must parse back to the same service.
func TestDescribeODataService_AuthClauseReParses(t *testing.T) {
	clause := odataAuthClause(&model.PublishedODataService{
		AuthenticationTypes: []string{"Basic", "Microflow"},
		AuthMicroflow:       "T.Authenticate",
	})
	svc := authService(t, "authentication "+clause)
	if svc.AuthMicroflow != "T.Authenticate" {
		t.Errorf("round trip lost the microflow: %q", svc.AuthMicroflow)
	}
	if strings.Join(svc.AuthenticationTypes, ",") != "Basic,Microflow" {
		t.Errorf("round trip changed the methods: %v", svc.AuthenticationTypes)
	}
}
