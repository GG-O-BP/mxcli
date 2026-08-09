// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/visitor"
)

// readContractScript builds a service whose read microflow takes params and
// whose published entity carries entityProps and members.
func readContractScript(params, entityProps, members string) string {
	return readContractScriptBody(params, entityProps, members, "", "")
}

// readContractScriptBody additionally injects statements into the read
// microflow's body, and extra top-level declarations before the service — what
// MDL-ODATA03 now reads to decide whether an advertised option is implemented.
func readContractScriptBody(params, entityProps, members, body, extra string) string {
	return `create module T;
create non-persistent entity T.Row (K: string(20));
` + extra + `
CREATE MICROFLOW T.Read_Rows (` + params + `)
  RETURNS List of T.Row AS $Rows
BEGIN
  $Rows = CREATE LIST OF T.Row;
` + body + `
  RETURN $Rows;
END;

create odata service T.Api (
  path: 'odata/t/',
  version: '1.0.0',
  ODataVersion: OData4,
  namespace: 'T.Api',
  ServiceName: 'Api'
)
{
  publish entity T.Row as 'Rows' (
    ReadMode: microflow T.Read_Rows,
    InsertMode: not_supported, UpdateMode: not_supported, DeleteMode: not_supported` + entityProps + `
  )
  expose ( ` + members + ` )
};
`
}

func odataReadRuleIDs(t *testing.T, script string) []string {
	t.Helper()
	prog, errs := visitor.Build(script)
	if len(errs) > 0 {
		t.Fatalf("parsing the script: %v", errs)
	}
	var ids []string
	for _, v := range ValidateODataReadContract(prog) {
		ids = append(ids, v.RuleID)
	}
	return ids
}

func hasODataReadRule(ids []string, id string) bool {
	for _, got := range ids {
		if got == id {
			return true
		}
	}
	return false
}

// mxcli-formula1 §37: the resource declares a KEY, the read microflow is never
// told about it, and a client holding a row re-reads it by key — gets the
// collection default, and adopts the first row as that object's identity. No
// error: valid collection, right count, 200. Fifteen restart cycles.
func TestODataReadContract_FlagsAKeyTheMicroflowCannotAnswer(t *testing.T) {
	ids := odataReadRuleIDs(t, readContractScript(
		"$Response: System.ODataResponse", "", "K as 'k' (KEY)"))
	if !hasODataReadRule(ids, "MDL-ODATA02") {
		t.Errorf("the unanswerable KEY was not flagged, got %v", ids)
	}
}

// §20: Mendix applies no query options to a read-microflow resource, so an
// unspecified TopSupported still publishes `true` and the client believes it
// received a page when it received the whole collection.
func TestODataReadContract_FlagsCapabilitiesNothingImplements(t *testing.T) {
	ids := odataReadRuleIDs(t, readContractScript(
		"$Response: System.ODataResponse", "", "K as 'k'"))
	if !hasODataReadRule(ids, "MDL-ODATA03") {
		t.Errorf("the unimplementable paging claim was not flagged, got %v", ids)
	}
	if hasODataReadRule(ids, "MDL-ODATA02") {
		t.Errorf("no KEY was declared, so the key rule must stay quiet: %v", ids)
	}
}

// The request parameter is shared between two unrelated concerns, and it used to
// be the whole test. Adding it to answer the KEY (MDL-ODATA02) therefore
// silenced the paging rule as a side effect, while nothing about the paging
// changed — a false negative exactly on a half-fixed resource, which is the one
// place the warning is still worth having (mxcli-formula1 §42).
func TestODataReadContract_FlagsPagingTheRequestAwareMicroflowNeverReads(t *testing.T) {
	ids := odataReadRuleIDs(t, readContractScript(
		"$Request: System.HttpRequest, $Response: System.ODataResponse", "", "K as 'k' (KEY)"))
	if !hasODataReadRule(ids, "MDL-ODATA03") {
		t.Errorf("having the request but never reading $top/$skip was not flagged, got %v", ids)
	}
	// The key rule is a separate concern and the parameter genuinely answers it.
	if hasODataReadRule(ids, "MDL-ODATA02") {
		t.Errorf("the request parameter answers the KEY concern: %v", ids)
	}
}

// The rule reports the absence of the option's name, so naming it has to be
// enough to silence it — otherwise a correctly paged resource is warned about
// forever and the rule gets switched off.
func TestODataReadContract_SilentWhenTheMicroflowReadsBothOptions(t *testing.T) {
	ids := odataReadRuleIDs(t, readContractScriptBody(
		"$Request: System.HttpRequest", "", "K as 'k'",
		"  declare $Paging string = find($Request/Uri, '$top=') + find($Request/Uri, '$skip=');", ""))
	if len(ids) != 0 {
		t.Errorf("a microflow that reads both options must not be flagged, got %v", ids)
	}
}

// Half-implemented is the shape §42 is about, so the warning has to name the
// half that is missing and leave the half that works alone — telling an author
// to withdraw working $top support would be a regression dressed as advice.
func TestODataReadContract_FlagsOnlyTheUnreadOption(t *testing.T) {
	prog, errs := visitor.Build(readContractScriptBody(
		"$Request: System.HttpRequest", "", "K as 'k'",
		"  declare $Paging string = find($Request/Uri, '$top=');", ""))
	if len(errs) > 0 {
		t.Fatalf("parsing the script: %v", errs)
	}
	vs := ValidateODataReadContract(prog)
	if len(vs) != 1 {
		t.Fatalf("want one violation, got %d: %v", len(vs), vs)
	}
	if strings.Contains(vs[0].Message, "TopSupported") {
		t.Errorf("$top IS read, so TopSupported must not be flagged: %s", vs[0].Message)
	}
	if !strings.Contains(vs[0].Message, "SkipSupported") {
		t.Errorf("$skip is never read, so SkipSupported must be flagged: %s", vs[0].Message)
	}
}

// A Java action parses the URI in a language mxcli cannot read. Absence of the
// literal proves nothing there, so the rule says nothing — the same stance it
// already takes toward a microflow defined outside the script.
func TestODataReadContract_SilentWhenPagingCouldBeInAJavaAction(t *testing.T) {
	ids := odataReadRuleIDs(t, readContractScriptBody(
		"$Request: System.HttpRequest", "", "K as 'k'",
		"  $Ignored = call java action T.ApplyPaging (Request = $Request);", ""))
	if len(ids) != 0 {
		t.Errorf("an opaque Java action must suppress the rule, got %v", ids)
	}
}

// Real code factors paging out into a helper, so the analysis follows calls into
// microflows the script defines rather than stopping at the resource's own body.
func TestODataReadContract_FollowsCallsIntoHelpersItCanRead(t *testing.T) {
	helper := `
CREATE MICROFLOW T.Apply_Paging ($Request: System.HttpRequest)
  RETURNS String AS $Out
BEGIN
  declare $Out string = find($Request/Uri, '$top=') + find($Request/Uri, '$skip=');
  RETURN $Out;
END;
`
	ids := odataReadRuleIDs(t, readContractScriptBody(
		"$Request: System.HttpRequest", "", "K as 'k'",
		"  $P = call microflow T.Apply_Paging (Request = $Request);", helper))
	if len(ids) != 0 {
		t.Errorf("paging done in a readable helper must not be flagged, got %v", ids)
	}
}

// The converse: a helper that exists somewhere else in the project cannot be
// read, so its call is as opaque as a Java action's.
func TestODataReadContract_SilentWhenAHelperIsOutsideTheScript(t *testing.T) {
	ids := odataReadRuleIDs(t, readContractScriptBody(
		"$Request: System.HttpRequest", "", "K as 'k'",
		"  $P = call microflow Other.Apply_Paging (Request = $Request);", ""))
	if len(ids) != 0 {
		t.Errorf("an unreadable helper must suppress the rule, got %v", ids)
	}
}

// Prose about the microflow is not behaviour of the microflow. An annotation
// documenting the limitation names `$top` while implementing nothing, and
// reading it as evidence would silence the rule on precisely the resource that
// admits the defect in writing.
func TestODataReadContract_DoesNotReadAnnotationsAsImplementation(t *testing.T) {
	ids := odataReadRuleIDs(t, readContractScriptBody(
		"$Request: System.HttpRequest", "", "K as 'k'",
		"  @annotation '$top and $skip are not applied here'\n  $Rows = CREATE LIST OF T.Row;", ""))
	if !hasODataReadRule(ids, "MDL-ODATA03") {
		t.Errorf("an annotation is documentation, not paging, got %v", ids)
	}
}

// Declining the query options is the one thing a URI-blind microflow CAN do
// honestly, and the rule must stay silent for it — otherwise it punishes the fix
// it recommends.
//
// The fixture omits the KEY only to isolate MDL-ODATA03 from MDL-ODATA02. Do not
// read that as a supported shape: Mendix requires a published entity to have a
// key (CE6585 "Published entity must have a key defined"), so a real resource
// always declares one and must therefore answer the lookup. Query options are
// declinable; the key is not.
func TestODataReadContract_SilentWhenTheContractIsHonest(t *testing.T) {
	ids := odataReadRuleIDs(t, readContractScript(
		"", ",\n    Countable: false, TopSupported: false, SkipSupported: false", "K as 'k'"))
	if len(ids) != 0 {
		t.Errorf("an honestly declared resource must not be flagged, got %v", ids)
	}
}

// ReadFromDatabase is Mendix's own implementation; the microflow rules do not
// apply to it at all.
func TestODataReadContract_IgnoresNonMicroflowReadModes(t *testing.T) {
	script := strings.Replace(
		readContractScript("", "", "K as 'k' (KEY)"),
		"ReadMode: microflow T.Read_Rows", "ReadMode: source", 1)
	if ids := odataReadRuleIDs(t, script); len(ids) != 0 {
		t.Errorf("a database-backed resource must not be flagged, got %v", ids)
	}
}

// A microflow this script does not define cannot be inspected — it may well
// take the request. Silence, not a guess.
func TestODataReadContract_SilentWhenTheMicroflowIsNotInThisScript(t *testing.T) {
	script := strings.Replace(
		readContractScript("$Response: System.ODataResponse", "", "K as 'k' (KEY)"),
		"ReadMode: microflow T.Read_Rows", "ReadMode: microflow Other.Read_Elsewhere", 1)
	if ids := odataReadRuleIDs(t, script); len(ids) != 0 {
		t.Errorf("an unknown microflow must not be flagged, got %v", ids)
	}
}

// The visitor stores ReadMode as `MICROFLOW Module.Name`, upper-cased. A
// case-sensitive prefix match made the whole rule dead while every unit test
// built on a hand-made AST still passed, so the casing is pinned here.
func TestODataReadContract_MatchesTheVisitorsReadModeCasing(t *testing.T) {
	name, named := readModeMicroflow("MICROFLOW T.Read_Rows")
	if !named || name != "T.Read_Rows" {
		t.Errorf("readModeMicroflow(%q) = (%q, %v); the visitor writes it upper-cased",
			"MICROFLOW T.Read_Rows", name, named)
	}
}
