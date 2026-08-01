// SPDX-License-Identifier: Apache-2.0

// mendixlabs/mxcli#812: every Forms$FormSettings mxcli wrote carried an empty
// Microflows$TextTemplate as its TitleOverride instead of null. An empty template is
// not the absence of an override — it IS an override, to the empty string — so every
// popup opened by an mxcli-authored button or Show Page action rendered with a blank
// caption and only the close button. A scan of one project found 10 broken popups,
// all mxcli-authored, against 58 correct ones from Studio Pro.
//
// The same defect hid a second one: an override the author DID ask for
// (`show page M.P with title = 'X'`) was thrown away, because the empty template was
// written regardless of OverridePageTitle.
//
// These assert the encoded BSON rather than the registry, since the registration is
// only a means to the output — and a duplicate RegisterTypeDefaults call for the same
// $Type silently wins by init order, which is what kept #812 alive after the first
// attempt at fixing it.
package modelsdkbackend

import (
	"testing"

	bsonv1 "go.mongodb.org/mongo-driver/bson"

	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/modelsdk/codec"
	"github.com/mendixlabs/mxcli/modelsdk/element"
	"github.com/mendixlabs/mxcli/sdk/microflows"
)

// encodeDoc encodes an element and returns it as a decoded bson.D for inspection.
func encodeDoc(t *testing.T, elem element.Element) bsonv1.D {
	t.Helper()
	raw, err := (&codec.Encoder{}).Encode(elem)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var d bsonv1.D
	if err := bsonv1.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return d
}

// lookup returns the value of key and whether the key was present at all.
func lookupKey(d bsonv1.D, key string) (any, bool) {
	for _, e := range d {
		if e.Key == key {
			return e.Value, true
		}
	}
	return nil, false
}

// TestShowPageFormSettings_NoOverrideIsNull is the reported bug. The key must be
// present and null — present, because that is what Studio Pro writes and what this
// repo's own debug-bson.md documents as the correct Forms$FormSettings shape; null,
// because an empty template blanks the caption.
func TestShowPageFormSettings_NoOverrideIsNull(t *testing.T) {
	d := encodeDoc(t, showPageFormSettingsToGen(&microflows.ShowPageAction{PageName: "Sales.OrderPopup", FormSettingsID: "fs-1"}))

	v, ok := lookupKey(d, "TitleOverride")
	if !ok {
		t.Fatal("TitleOverride key missing entirely; Studio Pro writes it as an explicit null (#812)")
	}
	if v != nil {
		t.Errorf("TitleOverride = %#v, want nil — an empty template overrides the page "+
			"title with the empty string, blanking the popup caption (#812)", v)
	}
	// The consolidation must not have dropped the other default.
	if mappings, ok := lookupKey(d, "ParameterMappings"); !ok {
		t.Error("ParameterMappings marker lost")
	} else if arr, _ := mappings.(bsonv1.A); len(arr) == 0 || arr[0] != int32(2) {
		t.Errorf("ParameterMappings = %#v, want marker 2", mappings)
	}
}

// TestShowPageFormSettings_OverrideIsPreserved is the second, unreported half. Before
// the fix the authored title reached the model and was then discarded by the writer:
// `grep -rn OverridePageTitle` matched only the struct field and its assignment.
func TestShowPageFormSettings_OverrideIsPreserved(t *testing.T) {
	title := &model.Text{
		BaseElement:  model.BaseElement{ID: "txt-1", TypeName: "Texts$Text"},
		Translations: map[string]string{"en_US": "Explicit Override"},
	}
	d := encodeDoc(t, showPageFormSettingsToGen(&microflows.ShowPageAction{PageName: "Sales.OrderPopup", FormSettingsID: "fs-1", OverridePageTitle: title}))

	v, ok := lookupKey(d, "TitleOverride")
	if !ok || v == nil {
		t.Fatalf("an explicitly authored title override was dropped (#812): %#v", v)
	}
	sub, isDoc := v.(bsonv1.D)
	if !isDoc {
		t.Fatalf("TitleOverride = %#v, want a Microflows$TextTemplate document", v)
	}
	if ty, _ := lookupKey(sub, "$Type"); ty != "Microflows$TextTemplate" {
		t.Errorf("TitleOverride $Type = %v, want Microflows$TextTemplate", ty)
	}
	// The text itself must actually be in there — an empty template would still be a
	// TextTemplate, which is exactly how the drop went unnoticed.
	if !containsString(sub, "Explicit Override") {
		t.Errorf("the override text is absent from the emitted template: %#v", sub)
	}
}

// TestFormSettingsToGen_TitleOverrideIsNull covers the widget-action side — a button
// opening a page, which is the case the reporter actually hit. There is no MDL syntax
// for overriding the title there, so the value is always null.
func TestFormSettingsToGen_TitleOverrideIsNull(t *testing.T) {
	d := encodeDoc(t, formSettingsToGen("Sales.OrderPopup"))

	v, ok := lookupKey(d, "TitleOverride")
	if !ok {
		t.Fatal("TitleOverride key missing entirely (#812)")
	}
	if v != nil {
		t.Errorf("TitleOverride = %#v, want nil — every popup opened by an mxcli-authored "+
			"button showed a blank caption (#812)", v)
	}
}

func containsString(d bsonv1.D, want string) bool {
	for _, e := range d {
		switch v := e.Value.(type) {
		case string:
			if v == want {
				return true
			}
		case bsonv1.D:
			if containsString(v, want) {
				return true
			}
		case bsonv1.A:
			for _, item := range v {
				if sub, ok := item.(bsonv1.D); ok && containsString(sub, want) {
					return true
				}
				if s, ok := item.(string); ok && s == want {
					return true
				}
			}
		}
	}
	return false
}
