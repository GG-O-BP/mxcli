// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/mendixlabs/mxcli/mdl/bsonutil"
)

// widget_convert.go moves a stored widget subtree between the two representations the
// codebase already has for the same thing:
//
//	bson.D          — how a widget is stored in a unit (ordered, IDs as 16-byte binary)
//	map[string]any  — what widgets.AugmentTemplate operates on (IDs as hex strings)
//
// The point is to run the SIX reconciliation passes AugmentTemplate already performs
// (enum values, property metadata, ValueType scalars, the AllowUpload envelope,
// PropertyType order, definition attributes) against a stored instance, instead of
// maintaining a second, weaker set of hand-rolled mutations. Hand-rolling produced a
// sync that left 47 Captions, 32 Categories and every ValueType/Translations wrong,
// because those are reconciled by passes it never called.
//
// # Key order
//
// map[string]any is unordered and Mendix cares about BSON key order (a documented
// CE0463 cause). Stored documents are ordered alphabetically — a PropertyType reads
// $ID, $Type, Caption, Category, Description, IsDefault, PropertyKey, ValueType — so
// converting back with sorted keys reproduces it. That assumption is not taken on
// faith: TestWidgetRoundTripIsByteStable converts every widget in a real project and
// asserts the re-encoded bytes are identical.

// widgetToMap converts stored BSON into the map form, rendering binary IDs as hex.
func widgetToMap(v any) any {
	switch t := v.(type) {
	case bson.D:
		out := make(map[string]any, len(t))
		for _, e := range t {
			out[e.Key] = widgetToMap(e.Value)
		}
		return out
	case bson.A:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = widgetToMap(item)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = widgetToMap(item)
		}
		return out
	case primitive.Binary:
		return bsonutil.BsonBinaryToID(t)
	case []byte:
		return bsonutil.BsonBinaryToID(primitive.Binary{Subtype: 0x00, Data: t})
	case int32:
		// The template pipeline models Mendix's array markers and small ints as
		// float64; keep one numeric representation so comparisons behave.
		return float64(t)
	case int64:
		return float64(t)
	}
	return v
}

// mapToWidgetDoc converts back, restoring binary IDs and alphabetical key order.
func mapToWidgetDoc(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(bson.D, 0, len(keys))
		for _, k := range keys {
			out = append(out, bson.E{Key: k, Value: mapValueToWidgetBSON(k, t[k])})
		}
		return out
	case []any:
		out := make(bson.A, len(t))
		for i, item := range t {
			out[i] = mapToWidgetDoc(item)
		}
		return out
	}
	return v
}

func mapValueToWidgetBSON(key string, v any) any {
	if s, ok := v.(string); ok && isWidgetIDField(key) {
		if b, err := bsonutil.IDToBsonBinaryErr(s); err == nil {
			return b
		}
	}
	return mapToWidgetDoc(v)
}

// isWidgetIDField names the fields Mendix stores as binary GUIDs. $ID plus the
// *Pointer references (TypePointer binds a WidgetProperty to its WidgetPropertyType).
func isWidgetIDField(key string) bool {
	return key == "$ID" || strings.HasSuffix(key, "Pointer")
}
