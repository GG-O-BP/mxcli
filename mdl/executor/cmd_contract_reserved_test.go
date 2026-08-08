// SPDX-License-Identifier: Apache-2.0

package executor

import "testing"

// mxcli-formula1 findings #28: an attribute named `name` came out of
// CREATE EXTERNAL ENTITIES prefixed with the remote type — `Stg_Drivername`,
// `Circuitname` — so a page written against the published $metadata failed with
// "The selected attribute 'F1Live.Drivers.name' no longer exists", and the same
// field carried a different name in every module because the remote type names
// differ.
//
// `name` was simply not reserved. Adjudicated on 11.12.1 by importing a contract
// with a property for every name on the list, prefixing disabled: Mendix
// answered CE7247 for seven of the eight and said nothing about `name`.
func TestAttrNameForOData(t *testing.T) {
	// Every one of these is a CE7247 "The name 'x' is a reserved word."
	for _, reserved := range []string{"id", "owner", "changedBy", "changedDate", "createdDate", "type", "context"} {
		if got := attrNameForOData(reserved, "Driver"); got != "Driver"+reserved {
			t.Errorf("attrNameForOData(%q) = %q, want it disambiguated — Mendix rejects the bare name with CE7247", reserved, got)
		}
	}

	// `name` is an ordinary attribute name and must survive untouched. So must
	// anything else the contract happens to call a property.
	for _, ok := range []string{"name", "driverRef", "surname", "nationality", "label"} {
		if got := attrNameForOData(ok, "Driver"); got != ok {
			t.Errorf("attrNameForOData(%q) = %q, want it unchanged — Mendix accepts it", ok, got)
		}
	}
}

// The check is case-insensitive: a contract using Id or TYPE hits the same
// reserved word.
func TestAttrNameForOData_CaseInsensitive(t *testing.T) {
	for _, v := range []string{"Id", "ID", "TYPE", "Owner"} {
		if got := attrNameForOData(v, "Thing"); got == v {
			t.Errorf("attrNameForOData(%q) left it unchanged; reserved words are case-insensitive", v)
		}
	}
	// …but a name that merely contains one is fine.
	for _, v := range []string{"identifier", "typeCode", "ownerName"} {
		if got := attrNameForOData(v, "Thing"); got != v {
			t.Errorf("attrNameForOData(%q) = %q, want unchanged — it is not the reserved word itself", v, got)
		}
	}
}
