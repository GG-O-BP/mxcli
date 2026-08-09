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
// MDL-ODATA02 (the KEY promise) fires on one provable condition — the microflow
// does not take a System.HttpRequest parameter — because without the request it
// cannot see a key at all.
//
// MDL-ODATA03 (paging) needs more than that, because the two concerns share one
// parameter. Adding $Request to answer the KEY also silenced the paging rule
// while nothing about the paging changed, which is a false negative exactly on a
// half-fixed resource (mxcli-formula1 §42). So it asks whether the option is
// *used*, not whether it could be: an OData query option is named `$top` /
// `$skip` on the wire, so a microflow that implements one must spell it
// somewhere. Silence is only reported when the whole reachable body is
// readable — a call into a Java action, a JavaScript action, or a microflow this
// script does not define makes the rule say nothing, since a rule that guesses
// is a rule people switch off.
//
// mxcli-formula1 §37 (the KEY promise), §20 (capabilities Mendix does not apply
// itself) and §42 (the shared-parameter false negative).
package executor

import (
	"fmt"
	"reflect"
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
			if !found {
				continue
			}
			where := fmt.Sprintf("publish entity %s as %q in %s",
				e.Entity.String(), e.ExposedName, svc.Name.String())
			if !takesHTTPRequest(decl) {
				out = append(out, keyPromiseViolations(where, e, mf)...)
			}
			out = append(out, capabilityViolations(where, e, mf, decl, flows)...)
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
// microflow does not implement (MDL-ODATA03).
//
// An option is unimplemented when either the microflow cannot see the request at
// all, or it can and never spells the option's name. The two cases share a rule
// because they are the same defect to a client — a 200 carrying an unpaged
// collection — but they read differently to an author, so they are worded apart.
func capabilityViolations(where string, e *ast.PublishedEntityDef, mf string,
	decl *ast.CreateMicroflowStmt, flows map[string]*ast.CreateMicroflowStmt) []linter.Violation {

	// nil means "not specified", which publishes Mendix's default of true — so
	// silence is a claim, and that is exactly what makes this worth flagging.
	sees := takesHTTPRequest(decl)
	ev := pagingEvidence(decl, flows)
	if sees && ev.opaque {
		// The microflow hands the request off to something this script cannot
		// read. Say nothing rather than guess.
		return nil
	}

	var claimed []string
	if boolOrTrueAST(e.TopSupported) && !(sees && ev.top) {
		claimed = append(claimed, "TopSupported")
	}
	if boolOrTrueAST(e.SkipSupported) && !(sees && ev.skip) {
		claimed = append(claimed, "SkipSupported")
	}
	if len(claimed) == 0 {
		return nil
	}

	// The two readings differ in what the author has to look at: one is a missing
	// parameter, the other a parameter that is there for a different purpose.
	reason := fmt.Sprintf("%s never sees the request (no %s parameter)", mf, httpRequestType)
	remedy := fmt.Sprintf("Either parse them from `$Request/Uri` — %s needs a `$Request: %s` parameter first — or declare",
		mf, httpRequestType)
	if sees {
		reason = fmt.Sprintf("nothing in %s reads %s from `$Request/Uri`",
			mf, strings.Join(quotedOptions(claimed), " or "))
		remedy = "Either parse them from `$Request/Uri`, or declare"
	}

	return []linter.Violation{{
		RuleID:   "MDL-ODATA03",
		Severity: linter.SeverityWarning,
		Message: fmt.Sprintf("%s: advertises %s, but %s, so no paging is applied",
			where, strings.Join(claimed, " and "), reason),
		Suggestion: fmt.Sprintf(
			"Mendix applies no query options to a read-microflow resource — it returns exactly what the microflow returns — so these annotations describe %s, not the platform. "+
				"A client asking for $top=5 gets the whole collection with a 200 and believes it received a page. "+
				"%s `%s`. "+
				"Declaring No is the read path's substitute for the 400 it cannot send: the read capability has no System.HttpResponse parameter (%s).",
			mf, remedy, declineClause(claimed), odataDocsCustomResponse),
	}}
}

// quotedOptions renders TopSupported/SkipSupported as the query options a client
// actually sends, which is what the author has to grep the microflow for.
func quotedOptions(claimed []string) []string {
	out := make([]string, 0, len(claimed))
	for _, c := range claimed {
		out = append(out, "$"+strings.ToLower(strings.TrimSuffix(c, "Supported")))
	}
	return out
}

// declineClause renders the properties to set to No — only the ones flagged, so
// a half-implemented resource is not told to withdraw the half that works.
func declineClause(claimed []string) string {
	parts := make([]string, 0, len(claimed))
	for _, c := range claimed {
		parts = append(parts, c+": No")
	}
	return strings.Join(parts, ", ")
}

// paging records what a read microflow was shown to do with the request.
//
// opaque is the escape hatch, and it outranks the other two: it means the
// reachable body leaves what this script can read, so absence of evidence is not
// evidence of absence.
type paging struct {
	top    bool
	skip   bool
	opaque bool
}

// pagingEvidence walks a microflow body — and, transitively, every microflow in
// the same script that it calls — looking for the literal query option names.
//
// A microflow cannot implement `$top` without naming it: the option is spelled
// that way on the wire, so extracting it means a `find`/`substring` against the
// literal. That makes the presence of the text weak evidence of handling and its
// absence strong evidence of the opposite — which is the direction this rule
// needs, since it only ever reports the absence.
func pagingEvidence(mf *ast.CreateMicroflowStmt, flows map[string]*ast.CreateMicroflowStmt) paging {
	var ev paging
	seen := map[string]bool{}
	var visit func(*ast.CreateMicroflowStmt)
	visit = func(m *ast.CreateMicroflowStmt) {
		if m == nil {
			return
		}
		key := strings.ToLower(m.Name.String())
		if seen[key] {
			return
		}
		seen[key] = true

		var callees []string
		walkForPaging(reflect.ValueOf(m.Body), &ev, &callees)
		for _, callee := range callees {
			next, found := flows[strings.ToLower(callee)]
			if !found {
				// Defined elsewhere in the project (or not at all). Unreadable.
				ev.opaque = true
				continue
			}
			visit(next)
		}
	}
	visit(mf)
	return ev
}

// walkForPaging reflects over microflow statements collecting the option names
// any string mentions, the calls that leave this script's view, and the calls
// worth following.
//
// Reflection rather than a type switch per statement: MDL gains activity types
// regularly, and a hand-written walker that misses one turns a false negative
// into a false positive — the rule would report "nothing reads $top" about a body
// it simply did not look at. Reflection covers new statement types, and new
// nesting (loops, if-branches, error handlers), on the day they are added.
func walkForPaging(v reflect.Value, ev *paging, callees *[]string) {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		// Unwrap to the concrete value; the Ptr case below classifies it. Doing
		// the type switch here too would count every statement twice.
		walkForPaging(v.Elem(), ev, callees)
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		switch s := v.Interface().(type) {
		// Opaque code: a Java or JavaScript action could parse the URI in a
		// language mxcli does not read, so its presence ends the analysis. A
		// nanoflow is not indexed with the microflows, so it can never be
		// followed — treat every one the same way.
		case *ast.CallJavaActionStmt, *ast.CallJavaScriptActionStmt,
			*ast.CallWebServiceStmt, *ast.CallNanoflowStmt:
			ev.opaque = true
		case *ast.CallMicroflowStmt:
			*callees = append(*callees, s.MicroflowName.String())
		}
		walkForPaging(v.Elem(), ev, callees)
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkForPaging(v.Index(i), ev, callees)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			walkForPaging(v.MapIndex(k), ev, callees)
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			// Prose about the microflow is not behaviour of the microflow. An
			// @annotation reading "$top is not applied here" must not be read as
			// applying it.
			switch t.Field(i).Name {
			case "Annotations", "Documentation", "Comment":
				continue
			}
			if !t.Field(i).IsExported() {
				continue
			}
			walkForPaging(v.Field(i), ev, callees)
		}
	case reflect.String:
		s := strings.ToLower(v.String())
		if strings.Contains(s, "$top") {
			ev.top = true
		}
		if strings.Contains(s, "$skip") {
			ev.skip = true
		}
	}
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
