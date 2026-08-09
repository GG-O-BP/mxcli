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
	return `create module T;
create non-persistent entity T.Row (K: string(20));

CREATE MICROFLOW T.Read_Rows (` + params + `)
  RETURNS List of T.Row AS $Rows
BEGIN
  $Rows = CREATE LIST OF T.Row;
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

// A microflow that takes the request gets the benefit of the doubt. Proving
// WHICH options it parses would need real analysis, and a rule that guesses is
// a rule people switch off.
func TestODataReadContract_SilentWhenTheMicroflowSeesTheRequest(t *testing.T) {
	ids := odataReadRuleIDs(t, readContractScript(
		"$Request: System.HttpRequest, $Response: System.ODataResponse", "", "K as 'k' (KEY)"))
	if len(ids) != 0 {
		t.Errorf("a request-aware microflow must not be flagged, got %v", ids)
	}
}

// An honest contract — no KEY, capabilities declared off — is the other way to
// be correct, and is the only one available to a microflow that cannot parse a
// URI. It must be silent, or the rule punishes the fix it recommends.
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
