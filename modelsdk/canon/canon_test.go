// SPDX-License-Identifier: Apache-2.0

package canon

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// bin builds a 16-byte GUID binary whose bytes are all n, so two documents can
// be given deliberately different IDs without any UUID plumbing.
func bin(n byte) bson.Binary {
	d := make([]byte, 16)
	for i := range d {
		d[i] = n
	}
	return bson.Binary{Subtype: 0x00, Data: d}
}

func marshal(t *testing.T, d bson.D) []byte {
	t.Helper()
	b, err := bson.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// doc models the shape that makes canonical comparison necessary at all: a
// parent element, two children, and a pointer from one child to the other held
// as a plain binary property (not a containment edge). ids picks the three
// element IDs, so the same structure can be built twice with different IDs.
func doc(t *testing.T, root, a, b byte, name string) []byte {
	t.Helper()
	return marshal(t, bson.D{
		{Key: "$Type", Value: "Microflows$Microflow"},
		{Key: "$ID", Value: bin(root)},
		{Key: "Name", Value: name},
		{Key: "Objects", Value: bson.A{
			int32(3),
			bson.D{{Key: "$Type", Value: "Microflows$StartEvent"}, {Key: "$ID", Value: bin(a)}},
			bson.D{
				{Key: "$Type", Value: "Microflows$EndEvent"},
				{Key: "$ID", Value: bin(b)},
				// A reference, structurally indistinguishable from any other binary.
				{Key: "OriginPointer", Value: bin(a)},
			},
		}},
	})
}

// TestEqual_DifferentIDsSameShape is the whole premise: a rebuild mints new IDs
// for every sub-element, and that must not read as a change.
func TestEqual_DifferentIDsSameShape(t *testing.T) {
	x := doc(t, 1, 2, 3, "Flow")
	y := doc(t, 1, 9, 8, "Flow") // same shape, different sub-element IDs

	if string(x) == string(y) {
		t.Fatal("fixture is wrong: the two documents are byte-identical, so this proves nothing")
	}
	eq, err := Equal(x, y)
	if err != nil {
		t.Fatalf("Equal: %v", err)
	}
	if !eq {
		t.Error("documents differing only in element ID choice must be canonically equal")
	}
}

// TestEqual_RewiredPointer guards the failure mode that makes the ID
// normalisation subtle: if references were merely erased rather than
// renumbered, pointing at a different element would look like no change.
func TestEqual_RewiredPointer(t *testing.T) {
	x := doc(t, 1, 2, 3, "Flow")
	// Same IDs, but the end event now points at itself instead of the start.
	y := marshal(t, bson.D{
		{Key: "$Type", Value: "Microflows$Microflow"},
		{Key: "$ID", Value: bin(1)},
		{Key: "Name", Value: "Flow"},
		{Key: "Objects", Value: bson.A{
			int32(3),
			bson.D{{Key: "$Type", Value: "Microflows$StartEvent"}, {Key: "$ID", Value: bin(2)}},
			bson.D{
				{Key: "$Type", Value: "Microflows$EndEvent"},
				{Key: "$ID", Value: bin(3)},
				{Key: "OriginPointer", Value: bin(3)},
			},
		}},
	})
	eq, err := Equal(x, y)
	if err != nil {
		t.Fatalf("Equal: %v", err)
	}
	if eq {
		t.Error("a pointer aimed at a different element is a real difference")
	}
}

// TestEqual_ContentDifference is the safety direction: real edits must never be
// elided. A false "equal" silently discards the user's intent.
func TestEqual_ContentDifference(t *testing.T) {
	x := doc(t, 1, 2, 3, "Flow")
	y := doc(t, 1, 2, 3, "FlowRenamed")
	eq, err := Equal(x, y)
	if err != nil {
		t.Fatalf("Equal: %v", err)
	}
	if eq {
		t.Error("a renamed document must not compare equal")
	}
}

// TestEqual_StableIdIsContent pins the decision that the write path depends on:
// canonicalisation does NOT mask StableId. Preservation is what makes an
// unchanged microflow compare equal; if canon masked the field instead, a write
// would be skipped while the stored and intended values silently disagreed.
func TestEqual_StableIdIsContent(t *testing.T) {
	base := bson.D{
		{Key: "$Type", Value: "Microflows$Microflow"},
		{Key: "$ID", Value: bin(1)},
		{Key: "Name", Value: "Flow"},
	}
	x := marshal(t, append(append(bson.D{}, base...), bson.E{Key: "StableId", Value: bin(7)}))
	y := marshal(t, append(append(bson.D{}, base...), bson.E{Key: "StableId", Value: bin(8)}))

	eq, err := Equal(x, y)
	if err != nil {
		t.Fatalf("Equal: %v", err)
	}
	if eq {
		t.Error("StableId reaches storage and is therefore content: differing values must not compare equal")
	}

	// ... and the masking variant, which exists only for measurement, agrees the
	// difference is confined to that one field.
	dx, err := DigestMasking(x, map[string]bool{"StableId": true})
	if err != nil {
		t.Fatalf("DigestMasking: %v", err)
	}
	dy, err := DigestMasking(y, map[string]bool{"StableId": true})
	if err != nil {
		t.Fatalf("DigestMasking: %v", err)
	}
	if dx != dy {
		t.Error("masking StableId should make these identical; something else differs")
	}
}

// TestEqual_FieldOrder: BSON preserves insertion order, and a rebuild has no
// obligation to reproduce the stored order. Order alone is not a difference.
func TestEqual_FieldOrder(t *testing.T) {
	x := marshal(t, bson.D{
		{Key: "$Type", Value: "Microflows$Microflow"},
		{Key: "$ID", Value: bin(1)},
		{Key: "Name", Value: "Flow"},
		{Key: "Documentation", Value: "doc"},
	})
	y := marshal(t, bson.D{
		{Key: "Documentation", Value: "doc"},
		{Key: "$ID", Value: bin(1)},
		{Key: "$Type", Value: "Microflows$Microflow"},
		{Key: "Name", Value: "Flow"},
	})
	eq, err := Equal(x, y)
	if err != nil {
		t.Fatalf("Equal: %v", err)
	}
	if !eq {
		t.Error("field order alone must not read as a difference")
	}
}

// TestEqual_UnrelatedGUIDNotNormalised: a 16-byte binary that is not one of this
// document's element IDs (a GUID, a cross-unit id) is a value, and a change in
// it is a real change. Normalising every binary would erase exactly this.
func TestEqual_UnrelatedGUIDNotNormalised(t *testing.T) {
	mk := func(guid byte) []byte {
		return marshal(t, bson.D{
			{Key: "$Type", Value: "DomainModels$Entity"},
			{Key: "$ID", Value: bin(1)},
			{Key: "GUID", Value: bin(guid)},
		})
	}
	eq, err := Equal(mk(4), mk(5))
	if err != nil {
		t.Fatalf("Equal: %v", err)
	}
	if eq {
		t.Error("a binary that is not an element ID of this document is content")
	}
}

// TestEqual_MalformedIsAnError: the write path is biased toward writing, which
// only works if canonicalisation reports failure instead of guessing.
func TestEqual_MalformedIsAnError(t *testing.T) {
	good := doc(t, 1, 2, 3, "Flow")
	if _, err := Equal(good, []byte{0x01, 0x02}); err == nil {
		t.Error("malformed BSON must report an error, not a verdict")
	}
}
