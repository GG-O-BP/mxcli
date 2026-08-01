// SPDX-License-Identifier: Apache-2.0

package settingsoverlay

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/mendixlabs/mxcli/model"
)

// privateOverride is the shape Studio Pro stores for a constant override whose
// value is private: a Settings$PrivateValue marker with no properties, because the
// value lives on the developer's workstation rather than in the shared model.
func privateOverride() bson.A {
	return bson.A{
		int32(3),
		bson.M{
			"$ID":        "cv-id",
			"$Type":      "Settings$ConstantValue",
			"ConstantId": "Mod.ApiToken",
			"SharedOrPrivateValue": bson.M{
				"$ID":   "spv-id",
				"$Type": PrivateValueType,
			},
		},
	}
}

// TestConstantValues_PreservesPrivateValue: the overlay assumed every stored
// SharedOrPrivateValue was a SharedValue and wrote cv.Value into it. For a private
// override cv.Value is always "" (the value is not in the model), so the write both
// fabricated a "Value" property on a type that defines none — the #759
// "Sequence contains no matching element" shape — and reported the override as
// empty. Configurations must respect the constant's private/shared choice, so the
// node is preserved exactly as stored.
func TestConstantValues_PreservesPrivateValue(t *testing.T) {
	cvs := ArrayElements(ConstantValues(
		[]*model.ConstantValue{{ConstantId: "Mod.ApiToken", IsPrivate: true}},
		privateOverride(),
	))
	if len(cvs) != 1 {
		t.Fatalf("got %d overrides, want 1", len(cvs))
	}
	spv, ok := AsMap(cvs[0]["SharedOrPrivateValue"])
	if !ok {
		t.Fatalf("SharedOrPrivateValue dropped: %#v", cvs[0])
	}
	if spv["$Type"] != PrivateValueType {
		t.Errorf("$Type = %#v, want %q — the private marker was replaced", spv["$Type"], PrivateValueType)
	}
	if v, has := spv["Value"]; has {
		t.Errorf("wrote Value = %#v onto a %s, which defines no properties", v, PrivateValueType)
	}
	if len(spv) != 2 {
		t.Errorf("private marker gained properties: %#v", spv)
	}
}

// TestConstantValues_PreservesPrivateValue_UnrelatedWrite is the realistic path:
// nothing about the constant is being edited, the settings document is merely
// rewritten because some other property changed. Every ALTER SETTINGS and CREATE
// CONFIGURATION goes through this, and configurations are shared in version
// control — so one developer's unrelated edit corrupted every developer's private
// overrides.
func TestConstantValues_PreservesPrivateValue_UnrelatedWrite(t *testing.T) {
	part := map[string]any{
		"Configurations": bson.A{
			int32(3),
			bson.M{
				"$ID":            "id-default",
				"Name":           "Default",
				"ConstantValues": privateOverride(),
			},
		},
	}
	cs := &model.ConfigurationSettings{
		Configurations: []*model.ServerConfiguration{{
			Name:           "Default",
			HttpPortNumber: 8099, // the only thing actually being changed
			ConstantValues: []*model.ConstantValue{{ConstantId: "Mod.ApiToken", IsPrivate: true}},
		}},
	}

	cfgs := ArrayElements(Configurations(cs, part)["Configurations"])
	spv, ok := AsMap(ArrayElements(cfgs[0]["ConstantValues"])[0]["SharedOrPrivateValue"])
	if !ok {
		t.Fatalf("SharedOrPrivateValue dropped: %#v", cfgs[0])
	}
	if _, has := spv["Value"]; has {
		t.Errorf("an unrelated port change corrupted the private override: %#v", spv)
	}
}

// TestConstantValues_SharedStillUpdates guards the fix from over-reaching: a
// shared override must still be written in place.
func TestConstantValues_SharedStillUpdates(t *testing.T) {
	raw := bson.A{
		int32(3),
		bson.M{
			"$ID":        "cv-id",
			"$Type":      "Settings$ConstantValue",
			"ConstantId": "Mod.C1",
			"SharedOrPrivateValue": bson.M{
				"$Type": "Settings$SharedValue",
				"Value": "old",
			},
		},
	}
	cvs := ArrayElements(ConstantValues([]*model.ConstantValue{{ConstantId: "Mod.C1", Value: "new"}}, raw))
	spv, _ := AsMap(cvs[0]["SharedOrPrivateValue"])
	if spv["Value"] != "new" {
		t.Errorf("shared override not updated: %#v", spv)
	}
}
