// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"testing"

	"github.com/mendixlabs/mxcli/sdk/domainmodel"
)

// An enumeration published as Edm.String must also carry EnumerationAsString.
// The pair is one setting: with the flag false, Mendix wants the enumeration
// published in the service as its own EDM enum type and rejects the string —
// CE5016 plus CE4583 "Enumeration 'Edm.Colour' is not published in this
// service". mxcli wrote Edm.String with the flag hardcoded false, which is the
// one combination that cannot build.
func TestEnumerationPublishesAsString(t *testing.T) {
	if !enumPublishedAsString(&domainmodel.EnumerationAttributeType{}) {
		t.Error("an enumeration attribute must set EnumerationAsString")
	}
	// A plain String also publishes as Edm.String but is not an enumeration —
	// the flag is what tells the two apart, so it must not be set here.
	if enumPublishedAsString(&domainmodel.StringAttributeType{}) {
		t.Error("a String attribute must not set EnumerationAsString")
	}
	if enumPublishedAsString(nil) {
		t.Error("a nil type must not set EnumerationAsString")
	}
}
