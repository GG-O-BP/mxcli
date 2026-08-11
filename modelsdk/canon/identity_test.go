// SPDX-License-Identifier: Apache-2.0

package canon

import (
	"bytes"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func microflowUnit(t *testing.T, stableID bson.Binary, name string) []byte {
	t.Helper()
	b, err := bson.Marshal(bson.D{
		{Key: "$Type", Value: "Microflows$Microflow"},
		{Key: "$ID", Value: bin(1)},
		{Key: "Name", Value: name},
		{Key: "StableId", Value: stableID},
		{Key: "Documentation", Value: "trailing field, so a botched patch corrupts something visible"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func readStableID(t *testing.T, raw []byte) []byte {
	t.Helper()
	var d bson.M
	if err := bson.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	v, ok := d["StableId"]
	if !ok {
		return nil
	}
	b, ok := v.(bson.Binary)
	if !ok {
		t.Fatalf("StableId is %T", v)
	}
	return b.Data
}

func TestCarryIdentity_CarriesStableId(t *testing.T) {
	stored := microflowUnit(t, bin(0xAA), "Flow")
	fresh := microflowUnit(t, bin(0xBB), "Flow")

	out := CarryIdentity(fresh, stored)

	if got := readStableID(t, out); !bytes.Equal(got, bin(0xAA).Data) {
		t.Errorf("StableId = %x, want the stored %x", got, bin(0xAA).Data)
	}
	// The rest of the document must be untouched, and the input must not be
	// mutated behind the caller's back.
	if got := readStableID(t, fresh); !bytes.Equal(got, bin(0xBB).Data) {
		t.Errorf("input document was mutated: StableId = %x", got)
	}
	var d bson.M
	if err := bson.Unmarshal(out, &d); err != nil {
		t.Fatalf("patched document no longer parses: %v", err)
	}
	if d["Name"] != "Flow" {
		t.Errorf("Name = %v, want Flow", d["Name"])
	}
}

func TestCarryIdentity_LeavesOtherTypesAlone(t *testing.T) {
	mk := func(id bson.Binary) []byte {
		b, err := bson.Marshal(bson.D{
			{Key: "$Type", Value: "Pages$Page"},
			{Key: "$ID", Value: bin(1)},
			{Key: "StableId", Value: id}, // not an identity field on this type
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return b
	}
	out := CarryIdentity(mk(bin(0xBB)), mk(bin(0xAA)))
	if got := readStableID(t, out); !bytes.Equal(got, bin(0xBB).Data) {
		t.Errorf("a type with no identity fields must pass through unchanged, got %x", got)
	}
}

// A document that does not carry the key must not have one invented for it:
// property names are version-specific, and writing one the stored document never
// had is how a unit becomes unopenable in Studio Pro (CLAUDE.md, overlay writes).
func TestCarryIdentity_NeverInventsAKey(t *testing.T) {
	stored := microflowUnit(t, bin(0xAA), "Flow")
	noField, err := bson.Marshal(bson.D{
		{Key: "$Type", Value: "Microflows$Microflow"},
		{Key: "$ID", Value: bin(1)},
		{Key: "Name", Value: "Flow"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := CarryIdentity(noField, stored)
	if got := readStableID(t, out); got != nil {
		t.Errorf("StableId was invented on a document that had none: %x", got)
	}

	// ... and the reverse: a stored document without the field cannot supply one.
	out = CarryIdentity(microflowUnit(t, bin(0xBB), "Flow"), noField)
	if got := readStableID(t, out); !bytes.Equal(got, bin(0xBB).Data) {
		t.Errorf("nothing to carry, so the new value should stand; got %x", got)
	}
}
