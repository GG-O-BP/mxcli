// SPDX-License-Identifier: Apache-2.0

package settingsoverlay

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/mendixlabs/mxcli/model"
)

func TestArrayMarker(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want int32
	}{
		{"int32 marker", bson.A{int32(3), bson.M{}}, 3},
		{"int64 marker", bson.A{int64(2)}, 2},
		{"int marker", bson.A{5}, 5},
		{"empty array falls back", bson.A{}, 7},
		{"not an array falls back", "nope", 7},
		{"nil falls back", nil, 7},
		{"unmarked array falls back", bson.A{bson.M{"$Type": "x"}}, 7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ArrayMarker(tc.in, 7); got != tc.want {
				t.Errorf("ArrayMarker(%#v) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestArrayElements_SkipsMarkerAndScalars(t *testing.T) {
	in := bson.A{int32(3), bson.M{"Name": "a"}, "junk", map[string]any{"Name": "b"}}
	got := ArrayElements(in)
	if len(got) != 2 {
		t.Fatalf("ArrayElements returned %d elements, want 2: %#v", len(got), got)
	}
	if got[0]["Name"] != "a" || got[1]["Name"] != "b" {
		t.Errorf("ArrayElements = %#v", got)
	}
}

// TestServerConfiguration_PreservesUnknownKeys pins the core of #801: keys the
// semantic model does not carry must pass through the overlay untouched.
func TestServerConfiguration_PreservesUnknownKeys(t *testing.T) {
	raw := map[string]any{
		"$ID":            "existing-id-sentinel",
		"$Type":          "Settings$ServerConfiguration",
		"Name":           "Default",
		"OpenAdminPort":  true,
		"OpenHttpPort":   true,
		"CustomSettings": bson.A{int32(3), bson.M{"Name": "Foo", "Value": "1"}},
		"Tracing":        bson.M{"$Type": "Settings$Tracing", "Level": "Feedback"},
		"SomeFutureKey":  "keep me",
		"ConstantValues": bson.A{int32(3)},
	}
	cfg := &model.ServerConfiguration{Name: "Default", HttpPortNumber: 8123}

	got := ServerConfiguration(cfg, raw, nil)

	if got["OpenAdminPort"] != true || got["OpenHttpPort"] != true {
		t.Errorf("Open*Port reset: OpenAdminPort=%#v OpenHttpPort=%#v", got["OpenAdminPort"], got["OpenHttpPort"])
	}
	if len(ArrayElements(got["CustomSettings"])) != 1 {
		t.Errorf("CustomSettings dropped: %#v", got["CustomSettings"])
	}
	if _, ok := AsMap(got["Tracing"]); !ok {
		t.Errorf("Tracing nulled: %#v", got["Tracing"])
	}
	if got["SomeFutureKey"] != "keep me" {
		t.Errorf("unknown key dropped: %#v", got["SomeFutureKey"])
	}
	if got["$ID"] != "existing-id-sentinel" {
		t.Errorf("$ID rewritten: %#v", got["$ID"])
	}
	if got["HttpPortNumber"] != int64(8123) {
		t.Errorf("HttpPortNumber = %#v, want 8123", got["HttpPortNumber"])
	}
	if m := ArrayMarker(got["ConstantValues"], -1); m != 3 {
		t.Errorf("ConstantValues marker = %d, want the stored 3", m)
	}
}

func TestConstantValues_UpdatesNestedShapeInPlace(t *testing.T) {
	raw := bson.A{
		int32(3),
		bson.M{
			"$ID":        "cv-id",
			"$Type":      "Settings$ConstantValue",
			"ConstantId": "Mod.C1",
			"Value":      "stale-flat",
			"SharedOrPrivateValue": bson.M{
				"$Type": "Settings$SharedValue",
				"Value": "old",
			},
		},
	}
	got := ConstantValues([]*model.ConstantValue{{ConstantId: "Mod.C1", Value: "new"}}, raw)

	cvs := ArrayElements(got)
	if len(cvs) != 1 {
		t.Fatalf("got %d overrides, want 1: %#v", len(cvs), got)
	}
	shared, ok := AsMap(cvs[0]["SharedOrPrivateValue"])
	if !ok {
		t.Fatalf("SharedOrPrivateValue dropped: %#v", cvs[0])
	}
	if shared["Value"] != "new" {
		t.Errorf("nested Value = %#v, want new", shared["Value"])
	}
	// The stale flat sibling is cleared so the reader cannot report it instead.
	if cvs[0]["Value"] != "" {
		t.Errorf("flat Value = %#v, want cleared", cvs[0]["Value"])
	}
	if cvs[0]["$ID"] != "cv-id" {
		t.Errorf("$ID rewritten: %#v", cvs[0]["$ID"])
	}
}

func TestConstantValues_KeepsLegacyFlatShape(t *testing.T) {
	raw := bson.A{
		int32(3),
		bson.M{"$ID": "cv-id", "$Type": "Settings$ConstantValue", "ConstantId": "Mod.C1", "Value": "old"},
	}
	cvs := ArrayElements(ConstantValues([]*model.ConstantValue{{ConstantId: "Mod.C1", Value: "new"}}, raw))
	if len(cvs) != 1 {
		t.Fatalf("got %d overrides, want 1", len(cvs))
	}
	if cvs[0]["Value"] != "new" {
		t.Errorf("flat Value = %#v, want new", cvs[0]["Value"])
	}
	if _, ok := cvs[0]["SharedOrPrivateValue"]; ok {
		t.Errorf("a nested value was added to a flat-only override: %#v", cvs[0])
	}
}

// TestConstantValues_NewOverrideIsNested: a brand-new override has no stored shape,
// so it must be written in the shape Studio Pro and mxbuild actually read.
func TestConstantValues_NewOverrideIsNested(t *testing.T) {
	cvs := ArrayElements(ConstantValues([]*model.ConstantValue{{ConstantId: "Mod.New", Value: "v"}}, bson.A{int32(3)}))
	if len(cvs) != 1 {
		t.Fatalf("got %d overrides, want 1", len(cvs))
	}
	if cvs[0]["ConstantId"] != "Mod.New" {
		t.Errorf("ConstantId = %#v", cvs[0]["ConstantId"])
	}
	shared, ok := AsMap(cvs[0]["SharedOrPrivateValue"])
	if !ok {
		t.Fatalf("new override is not nested: %#v", cvs[0])
	}
	if shared["Value"] != "v" || shared["$Type"] != "Settings$SharedValue" {
		t.Errorf("SharedOrPrivateValue = %#v", shared)
	}
	if _, ok := cvs[0]["Value"]; ok {
		t.Errorf("new override also wrote a flat Value: %#v", cvs[0])
	}
}

// TestConstantValues_MatchesByConstantKey covers the "Constant" spelling the gen type
// binds, alongside the "ConstantId" Studio Pro writes.
func TestConstantValues_MatchesByConstantKey(t *testing.T) {
	raw := bson.A{
		int32(3),
		bson.M{"$Type": "Settings$ConstantValue", "Constant": "Mod.C1", "Value": "old"},
	}
	cvs := ArrayElements(ConstantValues([]*model.ConstantValue{{ConstantId: "Mod.C1", Value: "new"}}, raw))
	if len(cvs) != 1 || cvs[0]["Value"] != "new" {
		t.Errorf("override matched by Constant key not updated in place: %#v", cvs)
	}
}

func TestConfigurations_MatchesByNameAndDrops(t *testing.T) {
	part := map[string]any{
		"$Type": "Settings$ConfigurationSettings",
		"Configurations": bson.A{
			int32(3),
			bson.M{"$ID": "id-default", "Name": "Default", "SomeFutureKey": "a"},
			bson.M{"$ID": "id-prod", "Name": "Production", "SomeFutureKey": "b"},
		},
	}
	cs := &model.ConfigurationSettings{
		// Production dropped; Default matched case-insensitively.
		Configurations: []*model.ServerConfiguration{{Name: "default", HttpPortNumber: 9000}},
	}

	got := Configurations(cs, part)
	cfgs := ArrayElements(got["Configurations"])
	if len(cfgs) != 1 {
		t.Fatalf("got %d configurations, want 1 (Production dropped): %#v", len(cfgs), got["Configurations"])
	}
	if cfgs[0]["$ID"] != "id-default" {
		t.Errorf("matched the wrong raw configuration: %#v", cfgs[0])
	}
	if cfgs[0]["SomeFutureKey"] != "a" {
		t.Errorf("unknown key dropped on the matched configuration: %#v", cfgs[0])
	}
	if m := ArrayMarker(got["Configurations"], -1); m != 3 {
		t.Errorf("Configurations marker = %d, want the stored 3", m)
	}
}

// TestConfigurations_NewConfigurationUsesSiblingShape: CREATE CONFIGURATION has no
// raw document, so the shape is taken from a sibling — minus its identity and its
// per-configuration collections.
func TestConfigurations_NewConfigurationUsesSiblingShape(t *testing.T) {
	part := map[string]any{
		"Configurations": bson.A{
			int32(3),
			bson.M{
				"$ID":            "id-default",
				"Name":           "Default",
				"OpenAdminPort":  true,
				"CustomSettings": bson.A{int32(3), bson.M{"Name": "Foo"}},
				"ConstantValues": bson.A{int32(3), bson.M{"ConstantId": "Mod.C1", "Value": "x"}},
				"Tracing":        bson.M{"$Type": "Settings$Tracing"},
			},
		},
	}
	cs := &model.ConfigurationSettings{
		Configurations: []*model.ServerConfiguration{
			{Name: "Default"},
			{Name: "Acceptance", DatabaseType: "PostgreSQL"},
		},
	}

	cfgs := ArrayElements(Configurations(cs, part)["Configurations"])
	if len(cfgs) != 2 {
		t.Fatalf("got %d configurations, want 2", len(cfgs))
	}
	var added map[string]any
	for _, c := range cfgs {
		if c["Name"] == "Acceptance" {
			added = c
		}
	}
	if added == nil {
		t.Fatalf("new configuration not written: %#v", cfgs)
	}
	if added["$ID"] == nil || added["$ID"] == "id-default" {
		t.Errorf("new configuration must get a fresh $ID, got %#v", added["$ID"])
	}
	if added["OpenAdminPort"] != true {
		t.Errorf("sibling shape not inherited: OpenAdminPort=%#v", added["OpenAdminPort"])
	}
	if got := ArrayElements(added["CustomSettings"]); len(got) != 0 {
		t.Errorf("new configuration inherited the sibling's custom settings: %#v", got)
	}
	if got := ArrayElements(added["ConstantValues"]); len(got) != 0 {
		t.Errorf("new configuration inherited the sibling's constant overrides: %#v", got)
	}
	if added["DatabaseType"] != "PostgreSQL" {
		t.Errorf("DatabaseType = %#v, want PostgreSQL", added["DatabaseType"])
	}
	// The sibling itself is untouched by having been used as a template.
	for _, c := range cfgs {
		if c["Name"] != "Default" {
			continue
		}
		if len(ArrayElements(c["CustomSettings"])) != 1 {
			t.Errorf("template sibling lost its custom settings: %#v", c["CustomSettings"])
		}
	}
}

// TestServerConfiguration_NoSiblings covers a project with no configuration at all:
// the fallback field set must still carry the keys Studio Pro expects.
func TestServerConfiguration_NoSiblings(t *testing.T) {
	got := ServerConfiguration(&model.ServerConfiguration{Name: "Default", HttpPortNumber: 8080}, nil, nil)
	for _, key := range []string{"$ID", "$Type", "Name", "CustomSettings", "ConstantValues", "OpenAdminPort", "OpenHttpPort"} {
		if _, ok := got[key]; !ok {
			t.Errorf("fallback configuration is missing %q: %#v", key, got)
		}
	}
	if _, ok := got["Tracing"]; !ok {
		t.Errorf("fallback configuration is missing Tracing: %#v", got)
	}
	if m := ArrayMarker(got["CustomSettings"], -1); m != DefaultListMarker {
		t.Errorf("CustomSettings marker = %d, want %d", m, DefaultListMarker)
	}
}

func TestSafeInt64_Bounds(t *testing.T) {
	const maxSafe = int64(1) << 53
	if got := SafeInt64(8080); got != 8080 {
		t.Errorf("SafeInt64(8080) = %d", got)
	}
	if got := SafeInt64(int(maxSafe) + 10); got != maxSafe {
		t.Errorf("SafeInt64 above range = %d, want clamp to %d", got, maxSafe)
	}
	if got := SafeInt64(-int(maxSafe) - 10); got != -maxSafe {
		t.Errorf("SafeInt64 below range = %d, want clamp to %d", got, -maxSafe)
	}
}
