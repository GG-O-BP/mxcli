// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/types"
)

// menuMDL renders items through the DESCRIBE emitter.
func menuMDL(items []*types.NavMenuItem) string {
	var b bytes.Buffer
	printMenuMDL(&b, items, 0)
	return b.String()
}

// DESCRIBE emits re-executable MDL, so an icon it read must come back out — the
// alternative is output that silently rewrites the menu when replayed.
func TestPrintMenuMDL_RoundTripsAnIconCollectionIcon(t *testing.T) {
	got := menuMDL([]*types.NavMenuItem{{
		Caption:  "Dashboard",
		Page:     "M.Dash",
		Icon:     "Atlas_Core.Atlas.align-center",
		IconType: "Forms$IconCollectionIcon",
	}})
	want := "menu item 'Dashboard' page M.Dash icon 'Atlas_Core.Atlas.align-center';\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A sub-menu carries its icon before the parenthesised body, matching the
// grammar's second alternative.
func TestPrintMenuMDL_RoundTripsASubMenuIcon(t *testing.T) {
	got := menuMDL([]*types.NavMenuItem{{
		Caption:  "Reports",
		Icon:     "Atlas_Core.Atlas.folder",
		IconType: "Forms$IconCollectionIcon",
		Items:    []*types.NavMenuItem{{Caption: "Monthly", Page: "M.Monthly"}},
	}})
	if !strings.HasPrefix(got, "menu 'Reports' icon 'Atlas_Core.Atlas.folder' (\n") {
		t.Errorf("sub-menu header lost its icon: %q", got)
	}
	if !strings.Contains(got, "menu item 'Monthly' page M.Monthly;") {
		t.Errorf("sub-items went missing: %q", got)
	}
}

// The other two variants are real and appear in Studio Pro-authored projects,
// but CREATE NAVIGATION cannot write them. Emitting `icon '…'` for an ImageIcon
// would convert it to an IconCollectionIcon on replay — a silent variant swap.
// The loss has to be visible instead.
func TestPrintMenuMDL_FlagsAnIconItCannotReproduce(t *testing.T) {
	for _, tc := range []struct{ name, iconType, icon, wantIn string }{
		{"image icon", "Forms$ImageIcon", "System.Images.Close", "System.Images.Close"},
		{"glyph icon", "Forms$GlyphIcon", "", "a numeric glyph code"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := menuMDL([]*types.NavMenuItem{{
				Caption: "Close", Page: "M.Close", Icon: tc.icon, IconType: tc.iconType,
			}})
			if strings.Contains(got, "icon '") {
				t.Errorf("emitted an ICON clause for %s, which replay would convert: %q",
					tc.iconType, got)
			}
			if !strings.Contains(got, "-- icon") || !strings.Contains(got, tc.wantIn) {
				t.Errorf("the unreproducible icon was dropped silently: %q", got)
			}
		})
	}
}

// No icon means no clause and no note — the common case must stay clean.
func TestPrintMenuMDL_SilentWhenThereIsNoIcon(t *testing.T) {
	got := menuMDL([]*types.NavMenuItem{{Caption: "Dashboard", Page: "M.Dash"}})
	if got != "menu item 'Dashboard' page M.Dash;\n" {
		t.Errorf("got %q", got)
	}
}
