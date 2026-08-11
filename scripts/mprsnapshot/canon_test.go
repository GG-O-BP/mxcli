// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

// elem builds a document node with a $ID and arbitrary extra fields.
func elem(id string, kv ...any) map[string]any {
	m := map[string]any{"$ID": id}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

// The whole premise: two documents that differ only in which UUIDs were minted
// are canonically equal, so a write between them changed nothing.
func TestCanonical_IgnoresIDChoice(t *testing.T) {
	a := elem("aaa", "Name", "Greet", "Flows", []any{
		elem("f1", "OriginPointer", "aaa", "DestinationPointer", "f1"),
	})
	b := elem("zzz", "Name", "Greet", "Flows", []any{
		elem("q9", "OriginPointer", "zzz", "DestinationPointer", "q9"),
	})

	ca, _ := canonicalDigests(a)
	cb, _ := canonicalDigests(b)
	if ca != cb {
		t.Errorf("same document with different IDs digested differently: %s vs %s", ca, cb)
	}
}

// ...but a document whose pointer aims somewhere else is genuinely different,
// and must not be canonicalised into equality. This is the assertion that keeps
// the digest from being a rubber stamp.
func TestCanonical_DetectsARetargetedPointer(t *testing.T) {
	a := elem("root", "Kids", []any{
		elem("k1"), elem("k2", "Ref", "k1"),
	})
	b := elem("root", "Kids", []any{
		elem("k1"), elem("k2", "Ref", "root"), // points at the root instead
	})

	ca, _ := canonicalDigests(a)
	cb, _ := canonicalDigests(b)
	if ca == cb {
		t.Error("a pointer aimed at a different element digested as identical")
	}
}

func TestCanonical_DetectsAValueChange(t *testing.T) {
	a := elem("x", "Name", "Greet")
	b := elem("y", "Name", "Farewell")
	ca, _ := canonicalDigests(a)
	cb, _ := canonicalDigests(b)
	if ca == cb {
		t.Error("a changed Name digested as identical")
	}
}

// A UUID that is not an element ID of this document is not a reference — a GUID,
// say — so a change in it is a real change and must survive canonicalisation.
func TestCanonical_ForeignUUIDIsNotTreatedAsAReference(t *testing.T) {
	a := elem("x", "GUID", "11111111-1111-1111-1111-111111111111")
	b := elem("x", "GUID", "22222222-2222-2222-2222-222222222222")
	ca, _ := canonicalDigests(a)
	cb, _ := canonicalDigests(b)
	if ca == cb {
		t.Error("a changed GUID was canonicalised away; only this document's own $IDs may be")
	}
}

// The masked digest is what separates "mxcli regenerates this by policy" from
// "the document really changed". Without it, a microflow always differs from
// itself and the measurement cannot say why.
func TestCanonical_MaskedDigestIsolatesVolatileFields(t *testing.T) {
	a := elem("x", "Name", "Greet", "StableId", "guid-one")
	b := elem("x", "Name", "Greet", "StableId", "guid-two")

	ca, ma := canonicalDigests(a)
	cb, mb := canonicalDigests(b)
	if ca == cb {
		t.Error("the unmasked digest should still see StableId")
	}
	if ma != mb {
		t.Error("the masked digest should hide StableId, leaving these identical")
	}

	// Masking must not hide a real difference that happens to sit next to one.
	c := elem("x", "Name", "Farewell", "StableId", "guid-two")
	_, mc := canonicalDigests(c)
	if mb == mc {
		t.Error("masking hid a Name change")
	}
}

// Field order is a serialisation detail, not a semantic difference.
func TestCanonical_IgnoresFieldOrder(t *testing.T) {
	a := map[string]any{"$ID": "x", "A": "1", "B": "2"}
	b := map[string]any{"B": "2", "$ID": "x", "A": "1"}
	ca, _ := canonicalDigests(a)
	cb, _ := canonicalDigests(b)
	if ca != cb {
		t.Error("field order changed the digest")
	}
}
