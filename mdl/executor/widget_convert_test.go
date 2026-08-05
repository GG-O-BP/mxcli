// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"bytes"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// The whole point of converting a stored widget to map form is to run
// widgets.AugmentTemplate's reconciliation passes over it and convert back. That is
// only safe if a conversion with NO reconciliation in between is byte-identical:
// otherwise every synced widget picks up spurious differences, and on a structure
// where key order is a documented CE0463 cause those differences are not cosmetic.
//
// map[string]any is unordered, so the round trip re-derives key order by sorting.
// This test is what justifies that assumption.
func TestWidgetRoundTripIsByteStable(t *testing.T) {
	id := func(b byte) primitive.Binary {
		return primitive.Binary{Subtype: 0x00, Data: bytes.Repeat([]byte{b}, 16)}
	}

	// A widget shaped like the real thing: ordered alphabetically, IDs as binary,
	// a paired PropertyType/WidgetProperty bound by TypePointer, array markers, and
	// a nested ObjectType.
	widget := bson.D{
		{Key: "$ID", Value: id(0x01)},
		{Key: "$Type", Value: "CustomWidgets$CustomWidget"},
		{Key: "Name", Value: "dgTest"},
		{Key: "Object", Value: bson.D{
			{Key: "$ID", Value: id(0x02)},
			{Key: "$Type", Value: "CustomWidgets$WidgetObject"},
			{Key: "Properties", Value: bson.A{
				float64(2),
				bson.D{
					{Key: "$ID", Value: id(0x03)},
					{Key: "$Type", Value: "CustomWidgets$WidgetProperty"},
					{Key: "TypePointer", Value: id(0x05)},
					{Key: "Value", Value: bson.D{
						{Key: "$ID", Value: id(0x04)},
						{Key: "$Type", Value: "CustomWidgets$WidgetValue"},
						{Key: "PrimitiveValue", Value: "true"},
					}},
				},
			}},
		}},
		{Key: "Type", Value: bson.D{
			{Key: "$ID", Value: id(0x06)},
			{Key: "$Type", Value: "CustomWidgets$CustomWidgetType"},
			{Key: "ObjectType", Value: bson.D{
				{Key: "$ID", Value: id(0x07)},
				{Key: "$Type", Value: "CustomWidgets$WidgetObjectType"},
				{Key: "PropertyTypes", Value: bson.A{
					float64(2),
					bson.D{
						{Key: "$ID", Value: id(0x05)},
						{Key: "$Type", Value: "CustomWidgets$WidgetPropertyType"},
						{Key: "Caption", Value: "Advanced"},
						{Key: "Category", Value: "Behavior::Selection"},
						{Key: "Description", Value: ""},
						{Key: "IsDefault", Value: false},
						{Key: "PropertyKey", Value: "advanced"},
						{Key: "ValueType", Value: bson.D{
							{Key: "$ID", Value: id(0x08)},
							{Key: "$Type", Value: "CustomWidgets$WidgetValueType"},
							{Key: "AllowUpload", Value: false},
							{Key: "EnumerationValues", Value: bson.A{float64(2)}},
							{Key: "Required", Value: true},
							{Key: "Translations", Value: bson.A{float64(2)}},
							{Key: "Type", Value: "Boolean"},
						}},
					},
				}},
			}},
			{Key: "WidgetId", Value: "com.mendix.widget.web.datagrid.Datagrid"},
		}},
	}

	original, err := bson.Marshal(widget)
	if err != nil {
		t.Fatalf("marshal original: %v", err)
	}

	asMap := widgetToMap(widget)
	if _, ok := asMap.(map[string]any); !ok {
		t.Fatalf("widgetToMap returned %T, want map[string]any", asMap)
	}

	back, ok := mapToWidgetDoc(asMap).(bson.D)
	if !ok {
		t.Fatal("mapToWidgetDoc did not return a bson.D")
	}
	encoded, err := bson.Marshal(back)
	if err != nil {
		t.Fatalf("marshal round-tripped: %v", err)
	}

	if !bytes.Equal(original, encoded) {
		t.Errorf("round trip changed the document\n original %d bytes\n  encoded %d bytes", len(original), len(encoded))
		var a, b bson.D
		_ = bson.Unmarshal(original, &a)
		_ = bson.Unmarshal(encoded, &b)
		t.Errorf("original: %v", a)
		t.Errorf("encoded : %v", b)
	}
}

// A TypePointer must survive as binary and still equal the PropertyType's $ID —
// breaking that pairing yields a project Mendix cannot load at all.
func TestWidgetRoundTripPreservesTypePointerBinding(t *testing.T) {
	ptID := primitive.Binary{Subtype: 0x00, Data: bytes.Repeat([]byte{0xAB}, 16)}

	doc := bson.D{
		{Key: "PropertyTypes", Value: bson.A{bson.D{
			{Key: "$ID", Value: ptID},
			{Key: "PropertyKey", Value: "advanced"},
		}}},
		{Key: "Properties", Value: bson.A{bson.D{
			{Key: "TypePointer", Value: ptID},
		}}},
	}

	back, ok := mapToWidgetDoc(widgetToMap(doc)).(bson.D)
	if !ok {
		t.Fatal("round trip did not return a bson.D")
	}

	pts, _ := arrField(back, "PropertyTypes")
	props, _ := arrField(back, "Properties")
	if len(pts) != 1 || len(props) != 1 {
		t.Fatalf("arrays lost: %d PropertyTypes, %d Properties", len(pts), len(props))
	}
	gotID, ok := idOf(pts[0].(bson.D))
	if !ok {
		t.Fatal("$ID did not survive as an ID")
	}
	gotPtr, ok := idField(props[0].(bson.D), "TypePointer")
	if !ok {
		t.Fatal("TypePointer did not survive as an ID")
	}
	if gotID != gotPtr {
		t.Errorf("pairing broken: $ID %s != TypePointer %s", gotID, gotPtr)
	}
}
