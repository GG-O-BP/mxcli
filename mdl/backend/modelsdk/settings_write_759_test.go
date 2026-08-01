// SPDX-License-Identifier: Apache-2.0

package modelsdkbackend

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/mendixlabs/mxcli/mdl/bsonutil"
	"github.com/mendixlabs/mxcli/mdl/settingsoverlay"
	mmpr "github.com/mendixlabs/mxcli/modelsdk/mpr"
)

// writeSettingsBackUnchanged performs the smallest possible settings write: read
// the document and hand it straight back. Every ALTER SETTINGS / CREATE
// CONFIGURATION goes through this same read-modify-write, so whatever this loses
// they all lose.
func writeSettingsBackUnchanged(t *testing.T, proj string) {
	t.Helper()
	b := New()
	if err := b.Connect(proj); err != nil {
		t.Fatalf("connect: %v", err)
	}
	ps, err := b.GetProjectSettings()
	if err != nil {
		t.Fatalf("GetProjectSettings: %v", err)
	}
	if err := b.UpdateProjectSettings(ps); err != nil {
		t.Fatalf("UpdateProjectSettings: %v", err)
	}
}

// TestUpdateProjectSettings_PreservesPorts is the regression test for
// mendixlabs/mxcli#759: the gen Configuration binds the two ports under their SDK
// names (RuntimePortNumber / AdminPortNumber) while Studio Pro stores
// HttpPortNumber / ServerPortNumber, so the read returned 0 for both and the
// overlay wrote that 0 back — every settings write silently reset the ports of
// every existing configuration.
func TestUpdateProjectSettings_PreservesPorts(t *testing.T) {
	proj := copyFixture(t)

	before := readConfiguration(t, proj)
	wantHTTP, wantServer := before["HttpPortNumber"], before["ServerPortNumber"]
	if toInt(wantHTTP) == 0 || toInt(wantServer) == 0 {
		t.Fatalf("fixture precondition: expected non-zero ports, got http=%v server=%v",
			wantHTTP, wantServer)
	}

	writeSettingsBackUnchanged(t, proj)

	after := readConfiguration(t, proj)
	if got := toInt(after["HttpPortNumber"]); got != toInt(wantHTTP) {
		t.Errorf("HttpPortNumber = %d, want %d", got, toInt(wantHTTP))
	}
	if got := toInt(after["ServerPortNumber"]); got != toInt(wantServer) {
		t.Errorf("ServerPortNumber = %d, want %d", got, toInt(wantServer))
	}
}

// TestUpdateProjectSettings_JavaVersionKeyFollowsDocument covers the second half of
// #759. Mendix renamed the runtime Java version property between 11.6
// ("JavaVersion" = "Java21") and 11.12 ("JavaMajorVersion" = "21"). mxcli wrote the
// 11.6 name unconditionally, so on an 11.12 project a settings write left
// JavaMajorVersion stale and added a JavaVersion property that version's metamodel
// does not define — which is what Studio Pro fails to resolve on the next open.
func TestUpdateProjectSettings_JavaVersionKeyFollowsDocument(t *testing.T) {
	tests := []struct {
		name      string
		storedKey string
		value     string
	}{
		{name: "mendix_11_6", storedKey: "JavaVersion", value: "Java21"},
		{name: "mendix_11_12", storedKey: "JavaMajorVersion", value: "21"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			proj := copyFixture(t)
			seedJavaVersionKey(t, proj, tc.storedKey, tc.value)

			writeSettingsBackUnchanged(t, proj)

			ms := readModelSettings(t, proj)
			if got := ms[tc.storedKey]; got != tc.value {
				t.Errorf("%s = %v, want %q", tc.storedKey, got, tc.value)
			}
			for _, other := range []string{"JavaVersion", "JavaMajorVersion"} {
				if other == tc.storedKey {
					continue
				}
				if v, ok := ms[other]; ok {
					t.Errorf("write invented %s = %v; this Mendix version stores %s",
						other, v, tc.storedKey)
				}
			}
		})
	}
}

// seedJavaVersionKey rewrites the fixture's Settings$ModelSettings part so it
// carries exactly one Java-version key, standing in for the Mendix version that
// spells it that way.
func seedJavaVersionKey(t *testing.T, proj, key, value string) {
	t.Helper()
	mutateSettingsPart(t, proj, "Settings$ModelSettings", func(part map[string]any) {
		delete(part, "JavaVersion")
		delete(part, "JavaMajorVersion")
		part[key] = value
	})
}

// mutateSettingsPart applies fn to the named part of the Settings$ProjectSettings
// unit and writes the document back.
func mutateSettingsPart(t *testing.T, proj, typeName string, fn func(map[string]any)) {
	t.Helper()
	r, err := mmpr.OpenWithOptions(proj, mmpr.OpenOptions{ReadOnly: false})
	if err != nil {
		t.Fatalf("open fixture: %v", err)
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
	found := false
	for _, part := range settingsoverlay.ArrayElements(doc["Settings"]) {
		if part["$Type"] == typeName {
			fn(part)
			found = true
		}
	}
	if !found {
		t.Fatalf("fixture has no %s part", typeName)
	}
	contents, err := bson.Marshal(bsonutil.OrderStorageValue(doc))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := mmpr.NewWriterWithReader(r).UpdateRawUnit(refs[0].ID, contents); err != nil {
		t.Fatalf("UpdateRawUnit: %v", err)
	}
}

// readModelSettings returns the raw Settings$ModelSettings part from disk.
func readModelSettings(t *testing.T, proj string) map[string]any {
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
		if part["$Type"] == "Settings$ModelSettings" {
			return part
		}
	}
	t.Fatal("no Settings$ModelSettings part on disk")
	return nil
}

func toInt(v any) int {
	switch n := v.(type) {
	case int32:
		return int(n)
	case int64:
		return int(n)
	case int:
		return n
	}
	return 0
}
