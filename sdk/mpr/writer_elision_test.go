// SPDX-License-Identifier: Apache-2.0

package mpr

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mendixlabs/mxcli/model"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// The legacy engine shares the modelsdk engine's no-op elision (ADR-0008
// decision 1) rather than reimplementing it, because which engine ran is an
// --engine flag and must not be visible in a user's diff.
//
// These tests also pin the assumption the wiring rests on — that the unit id the
// writer is handed is the same form GetRawUnitBytes expects. That is invisible by
// inspection, and getting it wrong would fail silently as "unreadable, so write
// it": no elision, no error, no test failure.
//
// Two fixtures, deliberately:
//
//	v1-project      Mendix 9.24, MPR v1 — unit contents live in SQLite, a
//	                different branch of updateUnit from everything else. It has
//	                no microflows, so it carries the elision half only.
//	expr-checker    Mendix 11.6, MPR v2 — has microflows with a StableId, so it
//	                carries the identity half.
//
// Neither test may skip. A skipped test reports success and proves nothing,
// which is how #808 stayed green while broken.

func copyProject(t *testing.T, srcDir, mprName string) string {
	t.Helper()
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(srcDir)); err != nil {
		t.Fatalf("copy %s: %v", srcDir, err)
	}
	return filepath.Join(dst, mprName)
}

// aUnit returns the lowest-id unit of the given type, so the choice is stable
// across runs. An empty typeName accepts any unit.
func aUnit(t *testing.T, r *Reader, typeName string) (model.ID, []byte) {
	t.Helper()
	units, err := r.ListUnits()
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}
	var bestID model.ID
	var bestRaw []byte
	for _, u := range units {
		if typeName != "" && u.Type != typeName {
			continue
		}
		raw, err := r.GetRawUnitBytes(u.ID)
		if err != nil || len(raw) == 0 {
			continue
		}
		if bestID == "" || u.ID < bestID {
			bestID, bestRaw = u.ID, append([]byte(nil), raw...)
		}
	}
	if bestID == "" {
		t.Fatalf("fixture has no readable unit of type %q — this test cannot prove anything", typeName)
	}
	return bestID, bestRaw
}

// reordered returns the same document with its top-level fields in reverse
// order: byte-different, canonically identical. It stands in for a rebuild
// without having to renumber a graph of element IDs by hand.
func reordered(t *testing.T, raw []byte) []byte {
	t.Helper()
	var d bson.D
	if err := bson.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(d) < 2 {
		t.Fatalf("document has %d top-level fields; reordering cannot make it byte-different", len(d))
	}
	rev := make(bson.D, 0, len(d))
	for i := len(d) - 1; i >= 0; i-- {
		rev = append(rev, d[i])
	}
	out, err := bson.Marshal(rev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Equal(out, raw) {
		t.Fatal("reordering produced identical bytes; this fixture cannot exercise canonical elision")
	}
	return out
}

func stableIDBytes(raw []byte) []byte {
	var d bson.M
	if err := bson.Unmarshal(raw, &d); err != nil {
		return nil
	}
	b, ok := d["StableId"].(primitive.Binary)
	if !ok {
		return nil
	}
	return b.Data
}

// TestLegacyUpdateUnit_ElidesCanonicallyEqualWrite covers the MPR v1 branch:
// contents in SQLite rather than .mxunit files.
func TestLegacyUpdateUnit_ElidesCanonicallyEqualWrite(t *testing.T) {
	w, err := NewWriter(copyProject(t, "testdata/v1-project", "App.mpr"))
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	id, before := aUnit(t, w.reader, "")
	if err := w.UpdateRawUnit(string(id), reordered(t, before)); err != nil {
		t.Fatalf("UpdateRawUnit: %v", err)
	}
	after, err := w.reader.GetRawUnitBytes(id)
	if err != nil {
		t.Fatalf("GetRawUnitBytes: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("a canonically-equal write was not elided: %d bytes -> %d bytes", len(before), len(after))
	}
}

// The control: with elision off the same write must land, otherwise the test
// above is passing for a reason that has nothing to do with elision.
func TestLegacyUpdateUnit_ControlWritesWhenElisionOff(t *testing.T) {
	t.Setenv("MXCLI_ALWAYS_WRITE", "1")
	w, err := NewWriter(copyProject(t, "testdata/v1-project", "App.mpr"))
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	id, before := aUnit(t, w.reader, "")
	if err := w.UpdateRawUnit(string(id), reordered(t, before)); err != nil {
		t.Fatalf("UpdateRawUnit: %v", err)
	}
	after, err := w.reader.GetRawUnitBytes(id)
	if err != nil {
		t.Fatalf("GetRawUnitBytes: %v", err)
	}
	if bytes.Equal(before, after) {
		t.Fatal("with elision disabled the write did not land — the elision test proves nothing")
	}
}

// TestLegacyUpdateUnit_StableIdOnlyDifferenceIsElided is the identity half, and
// the exact shape of re-running an unchanged microflow: the incoming document
// differs from storage only in a freshly minted StableId. The stored identity is
// carried in first, what remains compares equal, and nothing is written.
func TestLegacyUpdateUnit_StableIdOnlyDifferenceIsElided(t *testing.T) {
	w, err := NewWriter(copyProject(t, "../../testdata/expr-checker", "minimal.mpr"))
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	id, before := aUnit(t, w.reader, "Microflows$Microflow")
	original := stableIDBytes(before)
	if len(original) != 16 {
		t.Fatalf("fixture microflow %s has no 16-byte StableId; this test cannot prove preservation", id)
	}

	var d bson.D
	if err := bson.Unmarshal(before, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	fresh := bytes.Repeat([]byte{0x5A}, 16)
	replaced := false
	for i := range d {
		if d[i].Key == "StableId" {
			d[i].Value = primitive.Binary{Subtype: 0x00, Data: fresh}
			replaced = true
		}
	}
	if !replaced {
		t.Fatal("StableId not found as a top-level field")
	}
	mutated, err := bson.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if err := w.UpdateRawUnit(string(id), mutated); err != nil {
		t.Fatalf("UpdateRawUnit: %v", err)
	}
	after, err := w.reader.GetRawUnitBytes(id)
	if err != nil {
		t.Fatalf("GetRawUnitBytes: %v", err)
	}
	if got := stableIDBytes(after); !bytes.Equal(got, original) {
		t.Errorf("StableId = %x, want the stored %x", got, original)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("a write differing only in StableId should have been elided; stored bytes changed")
	}
}
