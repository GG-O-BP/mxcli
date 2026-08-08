// SPDX-License-Identifier: Apache-2.0

package modelsdkbackend

import (
	"testing"

	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/microflows"
)

// TestMicroflowRoundTrip_InheritanceSplit covers a corruption found while
// testing enum-split `else` across versions: `split type` produced a project
// mxbuild could not LOAD at all —
//
//	KeyNotFoundException: The given key '<guid>' was not present in the
//	dictionary  at StreamingBsonUnitReader.ResolvePostponedProperties()
//
// on both 11.6.6 and 11.13.0, while `mxcli check` passed. Two gaps, both the
// #791 shape (an object dropped at serialization while the flows pointing at
// it were written):
//
//  1. microflowObjectToGen had no *microflows.InheritanceSplit case, so the
//     split hit `default: return nil` and vanished. Three sequence flows
//     referenced its $ID — that is the dangling pointer the loader trips on.
//  2. caseValueToGen had no InheritanceCase case, so every branch flow got a
//     bare NoCase and the entity each branch selects on was lost.
func TestMicroflowRoundTrip_InheritanceSplit(t *testing.T) {
	split := &microflows.InheritanceSplit{VariableName: "A", Caption: "split"}
	split.ID = model.ID("split-1")

	mf := &microflows.Microflow{
		Name: "TypeSplit",
		ObjectCollection: &microflows.MicroflowObjectCollection{
			Objects: []microflows.MicroflowObject{split},
		},
	}
	mf.ID = model.ID("mf-1")

	got := roundTripMicroflow(t, mf)

	var found *microflows.InheritanceSplit
	if got.ObjectCollection != nil {
		for _, obj := range got.ObjectCollection.Objects {
			if s, ok := obj.(*microflows.InheritanceSplit); ok {
				found = s
			}
		}
	}
	if found == nil {
		t.Fatal("InheritanceSplit did not survive the round trip — the object is dropped at " +
			"serialization while flows still point at its $ID, which is the KeyNotFoundException")
	}
	if found.VariableName != "A" {
		t.Errorf("VariableName = %q, want A", found.VariableName)
	}
}

// The branch's case value must round-trip as an InheritanceCase naming the
// entity, not degrade to a NoCase.
func TestCaseValueToGen_InheritanceCase(t *testing.T) {
	el := caseValueToGen(&microflows.InheritanceCase{EntityQualifiedName: "SP.Dog"})
	if el == nil {
		t.Fatal("caseValueToGen returned nil for an InheritanceCase")
	}
	if got := el.TypeName(); got != "Microflows$InheritanceCase" {
		t.Fatalf("$Type = %q, want Microflows$InheritanceCase (a NoCase loses the branch entity)", got)
	}
}

// The visitor sometimes yields value receivers; those must dispatch the same
// way, exactly as the existing normalisation does for EnumerationCase.
func TestCaseValueToGen_InheritanceCaseValueReceiver(t *testing.T) {
	el := caseValueToGen(microflows.InheritanceCase{EntityQualifiedName: "SP.Dog"})
	if el == nil || el.TypeName() != "Microflows$InheritanceCase" {
		t.Fatalf("value-receiver InheritanceCase degraded to %v", el)
	}
}
