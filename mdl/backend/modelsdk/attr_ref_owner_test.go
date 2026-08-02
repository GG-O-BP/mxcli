// SPDX-License-Identifier: Apache-2.0

package modelsdkbackend

import "testing"

// TestAttrRefBelongsTo pins the distinction reconciliation depends on: only a
// reference qualified against THIS entity names one of its own attributes and can
// be validated from this domain model. An inherited reference carries an
// ancestor's name — possibly from another module or System, neither loaded here —
// and deleting it as "stale" is mendixlabs/mxcli#758.
func TestAttrRefBelongsTo(t *testing.T) {
	tests := []struct {
		ref    string
		module string
		entity string
		want   bool
	}{
		{"Sec.Item.OwnField", "Sec", "Item", true},
		{"sec.item.OwnField", "Sec", "Item", true}, // Mendix names are case-insensitive
		{"Sec.Base.SharedField", "Sec", "Item", false},
		{"System.FileDocument.Name", "Sec", "Attachment", false},
		{"Other.Base.Field", "Sec", "Item", false},
		{"NoDots", "Sec", "Item", false},
		{"", "Sec", "Item", false},
		// A same-named entity in another module is NOT this entity.
		{"Other.Item.OwnField", "Sec", "Item", false},
	}
	for _, tc := range tests {
		if got := attrRefBelongsTo(tc.ref, tc.module, tc.entity); got != tc.want {
			t.Errorf("attrRefBelongsTo(%q, %q, %q) = %v, want %v",
				tc.ref, tc.module, tc.entity, got, tc.want)
		}
	}
}
