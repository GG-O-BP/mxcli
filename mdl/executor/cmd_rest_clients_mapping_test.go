// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
)

// TestBuildRestClientOperation_NormalizesTypeTokens pins the contract documented
// on model.RestClientOperation: BodyType/ResponseType are upper-case tokens.
// The visitor yields the lower-case source text, and every consumer (both
// serializers, the REST-call microflow builder) compares against the upper-case
// spelling — so passing the AST value through unchanged silently disabled them
// and dropped the mapping (#843).
func TestBuildRestClientOperation_NormalizesTypeTokens(t *testing.T) {
	tests := []struct {
		name             string
		def              *ast.RestOperationDef
		wantBodyType     string
		wantResponseType string
	}{
		{
			name:             "scalar response",
			def:              &ast.RestOperationDef{Name: "Get", ResponseType: "json"},
			wantResponseType: "JSON",
		},
		{
			name:             "scalar body",
			def:              &ast.RestOperationDef{Name: "Post", BodyType: "template", ResponseType: "none"},
			wantBodyType:     "TEMPLATE",
			wantResponseType: "NONE",
		},
		{
			name: "response mapping",
			def: &ast.RestOperationDef{
				Name: "GetRoute",
				ResponseMapping: &ast.RestMappingDef{
					Entity:  ast.QualifiedName{Module: "Mod", Name: "Routing"},
					Entries: []ast.RestMappingEntry{{Left: "RoutingCode", Right: "routing_code"}},
				},
			},
			wantResponseType: "MAPPING",
		},
		{
			name: "body mapping",
			def: &ast.RestOperationDef{
				Name: "PostRoute",
				BodyMapping: &ast.RestMappingDef{
					Entity:  ast.QualifiedName{Module: "Mod", Name: "Routing"},
					Entries: []ast.RestMappingEntry{{Left: "routing_code", Right: "RoutingCode"}},
				},
			},
			wantBodyType: "EXPORT_MAPPING",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, err := buildRestClientOperation(tt.def)
			if err != nil {
				t.Fatalf("buildRestClientOperation: %v", err)
			}
			if op.BodyType != tt.wantBodyType {
				t.Errorf("BodyType = %q, want %q", op.BodyType, tt.wantBodyType)
			}
			if op.ResponseType != tt.wantResponseType {
				t.Errorf("ResponseType = %q, want %q", op.ResponseType, tt.wantResponseType)
			}
		})
	}
}

// TestBuildRestClientOperation_RejectsMappingWithoutBody covers the syntax the
// reporter actually wrote: `Response: mapping Mod.IMM_R10`, pointing at an
// import mapping *document*. Mendix has no response handler that references one,
// so the clause contributed nothing and the operation was written out as
// Rest$NoResponseHandling — silently. It must be refused, with the inline form
// spelled out.
func TestBuildRestClientOperation_RejectsMappingWithoutBody(t *testing.T) {
	tests := []struct {
		name      string
		def       *ast.RestOperationDef
		wantParts []string
	}{
		{
			name: "response",
			def: &ast.RestOperationDef{
				Name:            "SearchRoutes",
				ResponseMapping: &ast.RestMappingDef{Entity: ast.QualifiedName{Module: "ZZB", Name: "IMM_R10"}},
			},
			wantParts: []string{"Response", "ZZB.IMM_R10", "Response: mapping Module.Entity {"},
		},
		{
			name: "body",
			def: &ast.RestOperationDef{
				Name:        "PostRoute",
				BodyMapping: &ast.RestMappingDef{Entity: ast.QualifiedName{Module: "ZZB", Name: "EXM_R10"}},
			},
			wantParts: []string{"Body", "ZZB.EXM_R10", "Body: mapping Module.Entity {"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, err := buildRestClientOperation(tt.def)
			if err == nil {
				t.Fatalf("expected an error, got op %+v", op)
			}
			for _, want := range tt.wantParts {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err.Error(), want)
				}
			}
		})
	}
}
