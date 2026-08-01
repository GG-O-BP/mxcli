// SPDX-License-Identifier: Apache-2.0

// mendixlabs/mxcli#762: `dataview dv (… , FormOrientation: Vertical)` had no effect
// on the default (modelsdk) engine. Studio Pro's "Form orientation" radio is stored
// as LabelWidth in BSON — Vertical is 0, Horizontal is Mendix's default of 3 — and
// only the legacy writer performed that translation. The modelsdk writer emitted
// LabelWidth solely when an explicit `LabelWidth:` was given, so FormOrientation was
// read into the model and then dropped.
//
// The resolution now lives on the model, so both writers share one definition of the
// mapping instead of one of them owning it.
package pages

import "testing"

func TestResolvedLabelWidth(t *testing.T) {
	lw := func(n int) *int { return &n }

	tests := []struct {
		name        string
		orientation FormOrientation
		labelWidth  *int
		want        int
	}{
		{"unset is Mendix's default", "", nil, 3},
		{"horizontal is the default", FormOrientationHorizontal, nil, 3},
		{"vertical puts the label above", FormOrientationVertical, nil, 0},
		// An explicit LabelWidth is the more specific statement and wins, which is
		// what the documented `LabelWidth: 0` ⇔ `FormOrientation: Vertical` note means.
		{"explicit LabelWidth wins over orientation", FormOrientationVertical, lw(4), 4},
		{"explicit LabelWidth wins over horizontal", FormOrientationHorizontal, lw(0), 0},
		{"explicit LabelWidth alone", "", lw(6), 6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dv := &DataView{FormOrientation: tc.orientation, LabelWidth: tc.labelWidth}
			if got := dv.ResolvedLabelWidth(); got != tc.want {
				t.Errorf("ResolvedLabelWidth() = %d, want %d", got, tc.want)
			}
		})
	}
}
