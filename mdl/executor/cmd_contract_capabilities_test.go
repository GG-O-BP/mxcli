// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"testing"

	"github.com/mendixlabs/mxcli/mdl/types"
	"github.com/mendixlabs/mxcli/sdk/domainmodel"
)

// generatedEntity runs the external-entity generator over one entity set.
func generatedEntity(es *types.EdmEntitySet) *domainmodel.Entity {
	ent := &domainmodel.Entity{}
	applyExternalEntityFields(ent, &types.EdmEntityType{Name: "Season"}, true,
		"M.Client", es, nil, nil)
	return ent
}

// mxcli-formula1 §42: the generator stamped SkipSupported/TopSupported true
// whatever the contract said. So the honest remedy MDL-ODATA03 recommends —
// declare `TopSupported: No` on the publishing side — could not be used: the
// contract correctly carried Bool="false", the consuming app still said true,
// and the consumer failed to build:
//
//	CE6630 "'Seasons' is marked supports $top=False in the OData service,
//	        but True in the app."
//
// Countable next to them was already derived from the contract, and the comment
// above the two lines even explained the CE6630 mechanism — the hardcode
// contradicted its own stated rationale.
func TestExternalEntity_SkipTopFollowTheContract(t *testing.T) {
	ent := generatedEntity(&types.EdmEntitySet{
		Name:          "Seasons",
		TopSupported:  boolPtr(false),
		SkipSupported: boolPtr(false),
	})
	if ent.TopSupported {
		t.Error("TopSupported stayed true against a contract that says false — CE6630")
	}
	if ent.SkipSupported {
		t.Error("SkipSupported stayed true against a contract that says false — CE6630")
	}
}

// OData's own default is supported, so a service that annotates nothing must
// still generate an app that says true. Defaulting to false would invert CE6630
// for every unannotated service — including every one that worked before.
func TestExternalEntity_UnannotatedSkipTopStayTrue(t *testing.T) {
	ent := generatedEntity(&types.EdmEntitySet{Name: "Seasons"})
	if !ent.TopSupported || !ent.SkipSupported {
		t.Errorf("unannotated set generated Top=%v Skip=%v, want both true",
			ent.TopSupported, ent.SkipSupported)
	}
}

// An explicit true is honoured as an explicit true, not merely as the default.
func TestExternalEntity_ExplicitlySupportedStaysTrue(t *testing.T) {
	ent := generatedEntity(&types.EdmEntitySet{
		Name: "Seasons", TopSupported: boolPtr(true), SkipSupported: boolPtr(true),
	})
	if !ent.TopSupported || !ent.SkipSupported {
		t.Errorf("Top=%v Skip=%v, want both true", ent.TopSupported, ent.SkipSupported)
	}
}

// The two can differ — a service may page with $top but not $skip.
func TestExternalEntity_SkipAndTopAreIndependent(t *testing.T) {
	ent := generatedEntity(&types.EdmEntitySet{
		Name: "Seasons", TopSupported: boolPtr(true), SkipSupported: boolPtr(false),
	})
	if !ent.TopSupported {
		t.Error("TopSupported should be true")
	}
	if ent.SkipSupported {
		t.Error("SkipSupported should be false")
	}
}
