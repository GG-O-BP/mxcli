// SPDX-License-Identifier: Apache-2.0

package visitor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
)

// mxcli-formula1 findings #32: MOVE accepted seven doctypes and rejected the
// rest at parse time (`no viable alternative at input 'MOVEJAVA'`). Neither
// CREATE JAVA ACTION nor CREATE ODATA SERVICE takes a folder clause either, so
// those documents could never leave the module root from MDL — five of that
// backend's documents were stuck there.
func TestMoveStatement_JavaActionAndODataService(t *testing.T) {
	cases := []struct {
		src        string
		wantType   ast.DocumentType
		wantName   string
		wantFolder string
	}{
		{"move java action Mv.Helper to folder 'Support';", ast.DocumentTypeJavaAction, "Helper", "Support"},
		{"move odata service Mv.Api to folder 'Api/Published';", ast.DocumentTypeODataService, "Api", "Api/Published"},
		// The doctypes that already worked must keep working.
		{"move microflow Mv.Flow to folder 'Live';", ast.DocumentTypeMicroflow, "Flow", "Live"},
		{"move database connection Mv.Db to folder 'Warehouse';", ast.DocumentTypeDatabaseConnection, "Db", "Warehouse"},
	}

	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			prog, errs := Build(tc.src)
			if len(errs) > 0 {
				t.Fatalf("parse errors: %v", errs)
			}
			var got *ast.MoveStmt
			for _, s := range prog.Statements {
				if m, ok := s.(*ast.MoveStmt); ok {
					got = m
				}
			}
			if got == nil {
				t.Fatal("no MoveStmt produced")
			}
			if got.DocumentType != tc.wantType {
				t.Errorf("DocumentType = %q, want %q", got.DocumentType, tc.wantType)
			}
			if got.Name.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", got.Name.Name, tc.wantName)
			}
			if got.Folder != tc.wantFolder {
				t.Errorf("Folder = %q, want %q", got.Folder, tc.wantFolder)
			}
		})
	}
}

// MOVE FOLDER is told apart from a document move by the absence of a doctype
// keyword, so adding two more keywords must not make a folder move look like a
// document move.
func TestMoveStatement_FolderStillDistinct(t *testing.T) {
	prog, errs := Build("move folder Mv.Old to folder 'New';")
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	for _, s := range prog.Statements {
		if m, ok := s.(*ast.MoveStmt); ok {
			t.Fatalf("MOVE FOLDER produced a document MoveStmt (%q)", m.DocumentType)
		}
	}
}
