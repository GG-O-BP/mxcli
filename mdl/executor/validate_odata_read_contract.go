// SPDX-License-Identifier: Apache-2.0

// Check-time validation of what a microflow-backed OData resource promises.
//
// A read microflow cannot refuse a request. Mendix hands it the raw request and
// returns whatever comes back, and — unlike an OData action or an
// insert/update/delete microflow — the read capability has no System.HttpResponse
// parameter, so there is no way to answer 400. Its only exits are to throw (a
// blunt 500) or to return data.
//
// That makes the read path's contract declarative: whatever it cannot do at
// request time has to be stated up front, in the published metadata. These rules
// check the statement against the microflow it names.
//
// Both fire on one provable condition — the microflow does not take a
// System.HttpRequest parameter — because without the request it cannot see a
// key, a $filter, a $top or a $skip at all. A microflow that does take the
// request gets the benefit of the doubt: proving *which* options it parses would
// need real analysis, and a rule that guesses is a rule people switch off.
//
// mxcli-formula1 §37 (the KEY promise) and §20 (capabilities Mendix does not
// apply itself).
package executor

import (
	"fmt"
	"strings"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/linter"
)

const (
	// httpRequestType is the parameter that carries the query string.
	httpRequestType = "System.HttpRequest"
	// odataDocsCustomResponse is the reference for what each capability may do.
	odataDocsCustomResponse = "https://docs.mendix.com/refguide/published-odata-entity/#custom-http-response"
)

// ValidateODataReadContract flags read microflows that cannot keep the promises
// their published resource makes for them.
func ValidateODataReadContract(prog *ast.Program) []linter.Violation {
	if prog == nil {
		return nil
	}
	flows := microflowsByName(prog)

	var out []linter.Violation
	for _, stmt := range prog.Statements {
		svc, ok := stmt.(*ast.CreateODataServiceStmt)
		if !ok {
			continue
		}
		for _, e := range svc.Entities {
			if e == nil {
				continue
			}
			mf, named := readModeMicroflow(e.ReadMode)
			if !named {
				continue // ReadFromDatabase and friends: Mendix does the work
			}
			// Only a microflow defined in this same script can be inspected. A
			// reference to one that already exists in the project is not
			// evidence of anything, so say nothing.
			decl, found := flows[strings.ToLower(mf)]
			if !found || takesHTTPRequest(decl) {
				continue
			}
			where := fmt.Sprintf("publish entity %s as %q in %s",
				e.Entity.String(), e.ExposedName, svc.Name.String())
			out = append(out, keyPromiseViolations(where, e, mf)...)
			out = append(out, capabilityViolations(where, e, mf)...)
		}
	}
	return out
}

// keyPromiseViolations flags a declared KEY the read microflow is never told
// about (MDL-ODATA02).
func keyPromiseViolations(where string, e *ast.PublishedEntityDef, mf string) []linter.Violation {
	var keys []string
	for _, m := range e.Members {
		if m == nil || !m.IsPartOfKey {
			continue
		}
		// The exposed name is what a client filters on, so that is the name to
		// put in the message; fall back to the model name when none was given.
		name := m.ExposedName
		if name == "" {
			name = m.Name
		}
		keys = append(keys, name)
	}
	if len(keys) == 0 {
		return nil
	}
	return []linter.Violation{{
		RuleID:   "MDL-ODATA02",
		Severity: linter.SeverityWarning,
		Message: fmt.Sprintf("%s: declares KEY %s, but %s never sees the request (no %s parameter) and so cannot answer a lookup by that key",
			where, strings.Join(keys, ", "), mf, httpRequestType),
		Suggestion: fmt.Sprintf(
			"A client holding a row re-reads it by key on its own — Mendix's OData client sends `?$filter=%s eq '…'`. "+
				"With no branch for it the request falls through to the collection default, the client adopts the FIRST row as that object's identity, and there is no error: valid collection, right count, 200. "+
				"Give %s a `$Request: %s` parameter and branch key → filter → default. "+
				"Dropping the KEY is NOT an alternative: Mendix requires a published entity to have one "+
				"(CE6585 \"Published entity must have a key defined\"), so the lookup has to be answered.",
			keys[0], mf, httpRequestType),
	}}
}

// capabilityViolations flags query options advertised to clients that the read
// microflow cannot implement (MDL-ODATA03).
func capabilityViolations(where string, e *ast.PublishedEntityDef, mf string) []linter.Violation {
	// nil means "not specified", which publishes Mendix's default of true — so
	// silence is a claim, and that is exactly what makes this worth flagging.
	var claimed []string
	if boolOrTrueAST(e.TopSupported) {
		claimed = append(claimed, "TopSupported")
	}
	if boolOrTrueAST(e.SkipSupported) {
		claimed = append(claimed, "SkipSupported")
	}
	if len(claimed) == 0 {
		return nil
	}
	return []linter.Violation{{
		RuleID:   "MDL-ODATA03",
		Severity: linter.SeverityWarning,
		Message: fmt.Sprintf("%s: advertises %s, but %s never sees the request (no %s parameter), so no paging is applied",
			where, strings.Join(claimed, " and "), mf, httpRequestType),
		Suggestion: fmt.Sprintf(
			"Mendix applies no query options to a read-microflow resource — it returns exactly what the microflow returns — so these annotations describe %s, not the platform. "+
				"A client asking for $top=5 gets the whole collection with a 200 and believes it received a page. "+
				"Either parse them from `$Request/Uri`, or declare `TopSupported: No, SkipSupported: No`. "+
				"Declaring No is the read path's substitute for the 400 it cannot send: the read capability has no System.HttpResponse parameter (%s).",
			mf, odataDocsCustomResponse),
	}}
}

// microflowsByName indexes the microflows this script defines, lower-cased.
func microflowsByName(prog *ast.Program) map[string]*ast.CreateMicroflowStmt {
	out := map[string]*ast.CreateMicroflowStmt{}
	for _, stmt := range prog.Statements {
		if mf, ok := stmt.(*ast.CreateMicroflowStmt); ok {
			out[strings.ToLower(mf.Name.String())] = mf
		}
	}
	return out
}

// readModeMicroflow extracts the microflow name from a ReadMode, and reports
// whether the mode names one at all.
//
// The visitor stores this as `MICROFLOW Module.Name`, upper-cased, so the prefix
// is matched case-insensitively — a case-sensitive check silently matched
// nothing and the whole rule was dead, which is why it is verified against a
// real parse rather than a hand-built AST.
func readModeMicroflow(readMode string) (string, bool) {
	trimmed := strings.TrimSpace(readMode)
	const prefix = "microflow"
	if len(trimmed) < len(prefix) || !strings.EqualFold(trimmed[:len(prefix)], prefix) {
		return "", false
	}
	name := strings.TrimSpace(trimmed[len(prefix):])
	return name, name != ""
}

// takesHTTPRequest reports whether a microflow can see the request at all. A
// System.* parameter is a qualified name, which the parser cannot tell apart
// from an enumeration, so both refs are checked (see CLAUDE.md on the
// TypeEnumeration/TypeEntity ambiguity).
func takesHTTPRequest(mf *ast.CreateMicroflowStmt) bool {
	if mf == nil {
		return false
	}
	for _, p := range mf.Parameters {
		for _, ref := range []*ast.QualifiedName{p.Type.EntityRef, p.Type.EnumRef} {
			if ref != nil && strings.EqualFold(ref.String(), httpRequestType) {
				return true
			}
		}
	}
	return false
}

// boolOrTrueAST resolves a tri-state query option: nil publishes Mendix's
// default of true.
func boolOrTrueAST(p *bool) bool { return p == nil || *p }
