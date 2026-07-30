// SPDX-License-Identifier: Apache-2.0

package modelsdkbackend

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/mendixlabs/mxcli/mdl/bsonutil"
	"github.com/mendixlabs/mxcli/mdl/settingsoverlay"
	"github.com/mendixlabs/mxcli/model"
	mmpr "github.com/mendixlabs/mxcli/modelsdk/mpr"
)

// The fixture project's Default configuration has no constant overrides, no custom
// settings and a null Tracing, so seeding is needed to observe what a write drops.
// The seeded shapes mirror what Studio Pro stores:
//
//	CustomSettings: [3, {Settings$CustomSetting Name/Value}, …]
//	Tracing:        {Settings$Tracing …}
//	ConstantValues: [3, {Settings$ConstantValue ConstantId, SharedOrPrivateValue:
//	                     {Settings$SharedValue Value}}]
//
// The overlay treats CustomSettings and Tracing as opaque, so their exact field
// layout does not matter to the assertions — only that they survive unchanged.
const (
	seededConstantID = "MyFirstModule.SeededConstant"
	seededConstValue = "seeded-value"
)

// seedConfiguration rewrites the fixture's Settings$ProjectSettings unit, adding
// custom settings, a Tracing document and a nested-shape constant override to the
// Default configuration. It returns the configuration document as seeded.
func seedConfiguration(t *testing.T, proj string) map[string]any {
	t.Helper()

	r, err := mmpr.OpenWithOptions(proj, mmpr.OpenOptions{ReadOnly: false})
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	refs, err := r.ListUnitsByType("Settings$ProjectSettings")
	if err != nil || len(refs) != 1 {
		r.Close()
		t.Fatalf("ListUnitsByType(Settings$ProjectSettings) = %v, %v", refs, err)
	}
	unitID := refs[0].ID
	raw, err := r.GetRawUnitBytes(unitID)
	if err != nil {
		r.Close()
		t.Fatalf("GetRawUnitBytes: %v", err)
	}
	var doc bson.M
	if err := bson.Unmarshal(raw, &doc); err != nil {
		r.Close()
		t.Fatalf("unmarshal: %v", err)
	}

	var seeded map[string]any
	for _, part := range settingsoverlay.ArrayElements(doc["Settings"]) {
		if part["$Type"] != "Settings$ConfigurationSettings" {
			continue
		}
		cfgs := settingsoverlay.ArrayElements(part["Configurations"])
		if len(cfgs) == 0 {
			r.Close()
			t.Fatalf("fixture has no configurations to seed")
		}
		cfg := cfgs[0]
		cfg["CustomSettings"] = bson.A{
			int32(3),
			bson.M{
				"$ID":   bsonutil.NewIDBsonBinary(),
				"$Type": "Settings$CustomSetting",
				"Name":  "MicroflowConstraintsDisabled",
				"Value": "true",
			},
		}
		cfg["Tracing"] = bson.M{
			"$ID":   bsonutil.NewIDBsonBinary(),
			"$Type": "Settings$Tracing",
			"Level": "Feedback",
		}
		cfg["ConstantValues"] = bson.A{
			int32(3),
			bson.M{
				"$ID":        bsonutil.NewIDBsonBinary(),
				"$Type":      "Settings$ConstantValue",
				"ConstantId": seededConstantID,
				"SharedOrPrivateValue": bson.M{
					"$ID":   bsonutil.NewIDBsonBinary(),
					"$Type": "Settings$SharedValue",
					"Value": seededConstValue,
				},
			},
		}
		seeded = cfg
	}
	if seeded == nil {
		r.Close()
		t.Fatalf("fixture has no Settings$ConfigurationSettings part")
	}

	contents, err := bson.Marshal(bsonutil.OrderStorageValue(doc))
	if err != nil {
		r.Close()
		t.Fatalf("marshal seeded settings: %v", err)
	}
	w := mmpr.NewWriterWithReader(r)
	if err := w.UpdateRawUnit(unitID, contents); err != nil {
		r.Close()
		t.Fatalf("UpdateRawUnit: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}
	return seeded
}

// readConfiguration returns the raw Default configuration document from disk.
func readConfiguration(t *testing.T, proj string) map[string]any {
	t.Helper()

	r, err := mmpr.Open(proj)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()
	refs, err := r.ListUnitsByType("Settings$ProjectSettings")
	if err != nil || len(refs) != 1 {
		t.Fatalf("ListUnitsByType = %v, %v", refs, err)
	}
	raw, err := r.GetRawUnitBytes(refs[0].ID)
	if err != nil {
		t.Fatalf("GetRawUnitBytes: %v", err)
	}
	var doc bson.M
	if err := bson.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, part := range settingsoverlay.ArrayElements(doc["Settings"]) {
		if part["$Type"] != "Settings$ConfigurationSettings" {
			continue
		}
		cfgs := settingsoverlay.ArrayElements(part["Configurations"])
		if len(cfgs) == 0 {
			t.Fatalf("no configurations on disk")
		}
		return cfgs[0]
	}
	t.Fatalf("no Settings$ConfigurationSettings part on disk")
	return nil
}

// TestUpdateProjectSettings_PreservesConfigurationData is the regression test for
// mendixlabs/mxcli#801: any ALTER SETTINGS rebuilt each ServerConfiguration from
// the semantic model, which emptied CustomSettings, nulled Tracing, reset the
// unmodelled Open*Port flags, downgraded the list version markers from 3 to 2, and
// rewrote constant overrides into the flat "Value" shape the platform ignores.
func TestUpdateProjectSettings_PreservesConfigurationData(t *testing.T) {
	proj := copyFixture(t)
	before := seedConfiguration(t, proj)
	wantOpenAdminPort, ok := before["OpenAdminPort"].(bool)
	if !ok || !wantOpenAdminPort {
		t.Fatalf("fixture precondition: expected OpenAdminPort=true, got %#v", before["OpenAdminPort"])
	}

	b := New()
	if err := b.Connect(proj); err != nil {
		t.Fatalf("connect: %v", err)
	}
	ps, err := b.GetProjectSettings()
	if err != nil {
		t.Fatalf("GetProjectSettings: %v", err)
	}
	if ps.Configuration == nil || len(ps.Configuration.Configurations) == 0 {
		t.Fatalf("no configuration read back")
	}
	cfg := ps.Configuration.Configurations[0]
	if len(cfg.ConstantValues) != 1 || cfg.ConstantValues[0].Value != seededConstValue {
		t.Fatalf("seeded constant override not read: %#v", cfg.ConstantValues)
	}

	// The narrowest possible edit: change one modelled scalar, as `ALTER SETTINGS
	// CONFIGURATION 'Default' (HttpPortNumber: 8123)` would.
	cfg.HttpPortNumber = 8123
	if err := b.UpdateProjectSettings(ps); err != nil {
		t.Fatalf("UpdateProjectSettings: %v", err)
	}
	if err := b.Disconnect(); err != nil {
		t.Fatalf("disconnect: %v", err)
	}

	after := readConfiguration(t, proj)

	// The edit landed.
	if got := after["HttpPortNumber"]; got != int64(8123) && got != int32(8123) {
		t.Errorf("HttpPortNumber = %#v, want 8123", got)
	}

	// CustomSettings survived, marker included.
	customs := settingsoverlay.ArrayElements(after["CustomSettings"])
	if len(customs) != 1 || customs[0]["Name"] != "MicroflowConstraintsDisabled" {
		t.Errorf("CustomSettings dropped: %#v", after["CustomSettings"])
	}
	if m := settingsoverlay.ArrayMarker(after["CustomSettings"], -1); m != 3 {
		t.Errorf("CustomSettings marker = %d, want 3", m)
	}

	// Tracing survived as a document rather than being nulled.
	tracing, ok := settingsoverlay.AsMap(after["Tracing"])
	if !ok {
		t.Errorf("Tracing = %#v, want the seeded document", after["Tracing"])
	} else if tracing["Level"] != "Feedback" {
		t.Errorf("Tracing.Level = %#v, want Feedback", tracing["Level"])
	}

	// The unmodelled port flags were not reset to their zero values.
	if after["OpenAdminPort"] != true {
		t.Errorf("OpenAdminPort = %#v, want true (not read into the model, must pass through)", after["OpenAdminPort"])
	}

	// The constant override kept its nested storage shape and its value.
	if m := settingsoverlay.ArrayMarker(after["ConstantValues"], -1); m != 3 {
		t.Errorf("ConstantValues marker = %d, want 3", m)
	}
	cvs := settingsoverlay.ArrayElements(after["ConstantValues"])
	if len(cvs) != 1 {
		t.Fatalf("ConstantValues = %#v, want 1 override", after["ConstantValues"])
	}
	if cvs[0]["ConstantId"] != seededConstantID {
		t.Errorf("ConstantId = %#v, want %s", cvs[0]["ConstantId"], seededConstantID)
	}
	shared, ok := settingsoverlay.AsMap(cvs[0]["SharedOrPrivateValue"])
	if !ok {
		t.Fatalf("SharedOrPrivateValue dropped, override rewritten as %#v", cvs[0])
	}
	if shared["Value"] != seededConstValue {
		t.Errorf("SharedOrPrivateValue.Value = %#v, want %s", shared["Value"], seededConstValue)
	}
	if shared["$Type"] != "Settings$SharedValue" {
		t.Errorf("SharedOrPrivateValue.$Type = %#v, want Settings$SharedValue", shared["$Type"])
	}
}

// TestUpdateProjectSettings_ConstantOverrideRoundTrip covers the two write paths for
// a constant override: updating one that already exists on disk (nested shape kept)
// and adding one that does not (nested shape written, since the platform ignores a
// flat "Value").
func TestUpdateProjectSettings_ConstantOverrideRoundTrip(t *testing.T) {
	proj := copyFixture(t)
	seedConfiguration(t, proj)

	b := New()
	if err := b.Connect(proj); err != nil {
		t.Fatalf("connect: %v", err)
	}
	ps, err := b.GetProjectSettings()
	if err != nil {
		t.Fatalf("GetProjectSettings: %v", err)
	}
	cfg := ps.Configuration.Configurations[0]
	cfg.ConstantValues[0].Value = "updated-value"
	added := &model.ConstantValue{ConstantId: "MyFirstModule.AddedConstant", Value: "added-value"}
	added.TypeName = "Settings$ConstantValue"
	cfg.ConstantValues = append(cfg.ConstantValues, added)
	if err := b.UpdateProjectSettings(ps); err != nil {
		t.Fatalf("UpdateProjectSettings: %v", err)
	}
	if err := b.Disconnect(); err != nil {
		t.Fatalf("disconnect: %v", err)
	}

	after := readConfiguration(t, proj)
	cvs := settingsoverlay.ArrayElements(after["ConstantValues"])
	if len(cvs) != 2 {
		t.Fatalf("ConstantValues = %#v, want 2 overrides", after["ConstantValues"])
	}
	byID := map[string]map[string]any{}
	for _, cv := range cvs {
		id, _ := cv["ConstantId"].(string)
		byID[id] = cv
	}
	for id, want := range map[string]string{
		seededConstantID:              "updated-value",
		"MyFirstModule.AddedConstant": "added-value",
	} {
		cv, ok := byID[id]
		if !ok {
			t.Errorf("override %s missing from %#v", id, byID)
			continue
		}
		shared, ok := settingsoverlay.AsMap(cv["SharedOrPrivateValue"])
		if !ok {
			t.Errorf("override %s has no SharedOrPrivateValue: %#v", id, cv)
			continue
		}
		if shared["Value"] != want {
			t.Errorf("override %s value = %#v, want %s", id, shared["Value"], want)
		}
		if flat, has := cv["Value"]; has && flat != "" {
			t.Errorf("override %s has a non-empty flat Value %#v alongside the nested one", id, flat)
		}
	}

	// The change is visible to the reader too.
	b2 := New()
	if err := b2.Connect(proj); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	t.Cleanup(func() { _ = b2.Disconnect() })
	ps2, err := b2.GetProjectSettings()
	if err != nil {
		t.Fatalf("GetProjectSettings(2): %v", err)
	}
	got := map[string]string{}
	for _, cv := range ps2.Configuration.Configurations[0].ConstantValues {
		got[cv.ConstantId] = cv.Value
	}
	if got[seededConstantID] != "updated-value" || got["MyFirstModule.AddedConstant"] != "added-value" {
		t.Errorf("overrides read back as %#v", got)
	}
}

// TestUpdateProjectSettings_RefusesWithoutRawParts guards the ADR-0005
// guard-don't-drop contract: with no captured raw parts the write would replace
// every settings part with an empty array.
func TestUpdateProjectSettings_RefusesWithoutRawParts(t *testing.T) {
	proj := copyFixture(t)
	b := New()
	if err := b.Connect(proj); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = b.Disconnect() })

	ps, err := b.GetProjectSettings()
	if err != nil {
		t.Fatalf("GetProjectSettings: %v", err)
	}
	ps.RawParts = nil
	if err := b.UpdateProjectSettings(ps); err == nil {
		t.Fatal("UpdateProjectSettings with no RawParts succeeded; want a refusal")
	}
}
