// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// binID builds the 16-byte binary form Mendix stores a $ID / GUID in.
func binID(b byte) primitive.Binary {
	data := make([]byte, 16)
	for i := range data {
		data[i] = b
	}
	return primitive.Binary{Subtype: 0, Data: data}
}

// TestElementIDReadsBinary is the regression that matters most: $ID is a
// 16-byte binary, not a string. Reading it as a string returns nothing and no
// error, which silently produces a snapshot with zero nested elements — the
// failure looks like "this module has no entities" rather than like a bug.
func TestElementIDReadsBinary(t *testing.T) {
	id, ok := elementID(map[string]any{"$ID": binID(0xab)})
	if !ok {
		t.Fatal("elementID returned !ok for a 16-byte binary $ID")
	}
	if want := "abababab-abab-abab-abab-ababababab"; !strings.HasPrefix(id, want[:8]) {
		t.Errorf("elementID = %q, want a UUID rendering of the binary", id)
	}

	if _, ok := elementID(map[string]any{"$ID": primitive.Binary{Data: []byte{1, 2, 3}}}); ok {
		t.Error("elementID accepted a binary that is not 16 bytes")
	}
	if _, ok := elementID(map[string]any{}); ok {
		t.Error("elementID accepted a document with no $ID")
	}
}

// TestNestedElementsSkipsListMarker covers the second trap: Mendix list arrays
// carry an int32 marker at index 0 before the documents. A walk that assumes
// every array member is a document either panics or mis-indexes every path.
func TestNestedElementsSkipsListMarker(t *testing.T) {
	domainModel := map[string]any{
		"$ID":   binID(0x01),
		"$Type": "DomainModels$DomainModel",
		"Entities": primitive.A{
			int32(3), // the list marker
			map[string]any{
				"$ID":   binID(0x02),
				"$Type": "DomainModels$EntityImpl",
				"Name":  "Account",
				"GUID":  binID(0x03),
				"Attributes": primitive.A{
					int32(1),
					map[string]any{
						"$ID":   binID(0x04),
						"$Type": "DomainModels$Attribute",
						"Name":  "Email",
					},
				},
			},
		},
	}

	got := nestedElements(domainModel, "Administration/DomainModel", false)
	if len(got) != 2 {
		t.Fatalf("got %d elements, want 2 (the entity and its attribute): %+v", len(got), got)
	}

	if got[0].path != "Administration/DomainModel/Entities/Account" {
		t.Errorf("entity path = %q", got[0].path)
	}
	if got[0].guid == "" {
		t.Error("entity GUID not captured; it is a separate identity from $ID and must be tracked")
	}
	if got[0].guid == got[0].id {
		t.Error("GUID and $ID collapsed to the same value")
	}
	if want := "Administration/DomainModel/Entities/Account/Attributes/Email"; got[1].path != want {
		t.Errorf("attribute path = %q, want %q", got[1].path, want)
	}
}

// TestNestedElementsHandlesDriverTypes guards the type-assertion trap: the mongo
// driver hands back named types (bson.M, bson.A) that do not satisfy a
// map[string]any / []any assertion.
func TestNestedElementsHandlesDriverTypes(t *testing.T) {
	doc := bson.M{
		"Widgets": bson.A{
			int32(1),
			bson.M{"$ID": binID(0x05), "$Type": "Forms$DivContainer", "Name": "container1"},
		},
	}

	got := nestedElements(doc, "Mod/Page", false)
	if len(got) != 1 {
		t.Fatalf("got %d elements from bson.M/bson.A input, want 1", len(got))
	}
	if got[0].path != "Mod/Page/Widgets/container1" {
		t.Errorf("path = %q", got[0].path)
	}
}

// TestUnnamedElementsAreOptional: index-keyed paths are unstable under
// reordering, so unnamed elements stay out of the default snapshot.
func TestUnnamedElementsAreOptional(t *testing.T) {
	doc := map[string]any{
		"Rows": primitive.A{
			int32(1),
			map[string]any{"$ID": binID(0x06), "$Type": "Forms$LayoutGridRow"},
		},
	}

	if got := nestedElements(doc, "p", false); len(got) != 0 {
		t.Errorf("unnamed element included by default: %+v", got)
	}
	if got := nestedElements(doc, "p", true); len(got) != 1 {
		t.Errorf("--all did not include the unnamed element: %+v", got)
	}
}

// TestHashIgnoresIDsButNotContent is what makes the diff readable: the hash must
// separate "this element was edited" from "this element was renumbered", so it
// has to be blind to $ID and sensitive to everything else.
func TestHashIgnoresIDsButNotContent(t *testing.T) {
	withID := func(id byte, name string) map[string]any {
		return map[string]any{
			"$ID":  binID(id),
			"Name": name,
			"Kids": primitive.A{int32(1), map[string]any{"$ID": binID(id + 1), "Name": name + "-kid"}},
		}
	}

	if a, b := hashWithoutIDs(withID(0x10, "x")), hashWithoutIDs(withID(0x20, "x")); a != b {
		t.Errorf("hash changed when only $IDs changed: %s vs %s", a, b)
	}
	if a, b := hashWithoutIDs(withID(0x10, "x")), hashWithoutIDs(withID(0x10, "y")); a == b {
		t.Error("hash did not change when content changed")
	}
}

// TestHashIsDeterministic: map iteration order must never reach the hash, or two
// snapshots of an unchanged project would differ and the experiment would report
// churn that is entirely the tool's own.
func TestHashIsDeterministic(t *testing.T) {
	doc := map[string]any{
		"Alpha": "a", "Beta": "b", "Gamma": "c", "Delta": "d",
		"Eps": map[string]any{"One": 1, "Two": 2, "Three": 3},
	}

	first := hashWithoutIDs(doc)
	for range 200 {
		if got := hashWithoutIDs(doc); got != first {
			t.Fatalf("hash not stable across runs: %s vs %s", first, got)
		}
	}
}

// TestReferencesReportPointersNotOwnID: the R lines exist to show where an
// element points, so the element's own $ID must not appear among them.
func TestReferencesReportPointersNotOwnID(t *testing.T) {
	doc := map[string]any{
		"$ID":            binID(0x30),
		"ParentPointer":  binID(0x31),
		"Name":           "Assoc",
		"NotAPointer":    "Administration.Account",
		"ShortBinary":    primitive.Binary{Data: []byte{9}},
		"ChildContainer": map[string]any{"ChildPointer": binID(0x32)},
	}

	got := references(doc)
	if len(got) != 2 {
		t.Fatalf("got %d references, want 2 (ParentPointer, ChildPointer): %v", len(got), got)
	}
	for _, r := range got {
		if strings.Contains(r, "$ID") {
			t.Errorf("own $ID reported as a reference: %s", r)
		}
	}
	if !strings.HasPrefix(got[0], "/ChildContainer/ChildPointer=") {
		t.Errorf("nested pointer path wrong: %s", got[0])
	}
}

// TestResolvePathOmitsProjectRoot: the root unit is unnamed, so including it
// would prefix every path with a bare UUID and break --module.
func TestResolvePathOmitsProjectRoot(t *testing.T) {
	root := &unit{id: "root", typ: projectRootType}
	module := &unit{id: "mod", containerID: "root", typ: "Projects$ModuleImpl", name: "Administration"}
	page := &unit{id: "pg", containerID: "mod", typ: "Forms$Page", name: "Account_Overview"}
	units := map[string]*unit{"root": root, "mod": module, "pg": page}

	if got := resolvePath(page, units, 0); got != "Administration/Account_Overview" {
		t.Errorf("resolvePath = %q, want %q", got, "Administration/Account_Overview")
	}
	if !inModule(resolvePath(page, units, 0), "Administration") {
		t.Error("resolved path did not match its own module filter")
	}
}
