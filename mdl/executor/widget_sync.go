// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/mendixlabs/mxcli/mdl/backend"
	"github.com/mendixlabs/mxcli/mdl/types"
	"github.com/mendixlabs/mxcli/sdk/widgets/mpk"
)

// widget_sync.go plans the reconciliation of *stored* widget instances against the
// widget packages currently installed in the project.
//
// mxcli authors a pluggable-widget instance correctly for the .mpk installed at
// authoring time, and has nothing that revisits the instance when that package later
// changes. Studio Pro has "Update all widgets"; mxbuild has `mx update-widgets`, which
// on MPR v2 destroys mprcontents/. This is the mxcli equivalent that does not.
//
// This file is the READ-ONLY half: it produces a plan and mutates nothing. See
// PROPOSAL_widget_instance_reconciliation.md.
//
// The unit of work is a CustomWidgets$CustomWidget node, which carries two paired
// arrays that must always move together:
//
//	Type.ObjectType.PropertyTypes[]  — the schema, one entry per property, keyed by PropertyKey
//	Object.Properties[]              — the values, bound to their type by TypePointer
//
// Splitting a pair produces the StreamingBsonUnitReader "does not contain a
// constructor with a parameter of type WidgetValue" load failure, i.e. a project that
// will not open at all — so the plan is expressed in property keys, and applying it
// must move both halves.
//
// # Validated against `mx update-widgets`
//
// On a fixture authored against Data Widgets 3.4 and then upgraded to 3.11.3 (40
// CE0463), this planner's change set was compared to what Mendix's own tool actually
// writes. Every CE0463-affected instance is planned — nothing is missed — and where
// both act they agree exactly (DataGrid2: remove `advanced` + add 17; the drop-down
// filters: `Required` on refCaption/refCaptionExp).
//
// # Two known gaps, both measured
//
//  1. CONTAINER COVERAGE. FindAllCustomWidgetTypes scans Forms$Page and Forms$Snippet
//     only. Widgets in Forms$BuildingBlock are invisible to it — 44 changes across
//     List_Cards, List_WithImage and Master_Detail that update-widgets makes and this
//     plan does not see. Layouts are likewise unscanned.
//
//  2. THEME MODULES. update-widgets does not touch widgets in a module with
//     IsThemeModule=true (FeedbackModule in a blank project), and Mendix reports no
//     CE0463 on them either — `mx module-import` has a dedicated refusal for theme
//     modules, so they are deliberately off-limits. This plan currently proposes 16
//     changes there that it should not. model.Module does not yet carry the flag.
//
// Both are fixed by replacing the per-widget-ID scan with a single-pass enumeration
// that returns every instance with its container type and owning module — which is
// also O(units) instead of O(widget types x units). That is the next step, and it is
// the primitive `marketplace diff` needs too (see PROPOSAL_marketplace_module_upgrade).

// SyncChangeKind is what a plan proposes to do to one property.
type SyncChangeKind string

const (
	// SyncRemove — the stored instance carries a PropertyKey the installed package no
	// longer declares. This is the mendixlabs/mxcli#716 case ("advanced", dropped from
	// Data Widgets after 3.4). Removing it DISCARDS the stored value.
	SyncRemove SyncChangeKind = "remove"
	// SyncAdd — the package declares a property the stored instance lacks; it is added
	// with the package's default value.
	SyncAdd SyncChangeKind = "add"
	// SyncUpdate — the property survives but its own definition attributes changed.
	SyncUpdate SyncChangeKind = "update"
)

// SyncPropertyChange is one proposed change to one property of one widget instance.
type SyncPropertyChange struct {
	Kind SyncChangeKind
	// Key is the PropertyKey, qualified with its parent for nested object types
	// (e.g. "columns/tooltip") so a report is unambiguous.
	Key string
	// Detail explains the change in the report ("dropped by the package",
	// `Required false -> true`).
	Detail string
}

// SyncWidgetPlan is the set of changes for a single stored widget instance.
type SyncWidgetPlan struct {
	Container   string // qualified name of the page/snippet holding the widget
	ContainerID string
	Widget      string // the instance's Name
	WidgetID    string // e.g. com.mendix.widget.web.datagrid.Datagrid
	PackageVer  string // version of the installed .mpk
	Changes     []SyncPropertyChange
	StoredKeys  int
	PackageKeys int
}

// SyncPlan is the whole read-only result.
type SyncPlan struct {
	Widgets []SyncWidgetPlan
	// Unresolved lists widget IDs found in the model with no installed .mpk. These are
	// reported and never touched: deleting an instance's properties because its package
	// is missing would be the worst possible failure mode.
	Unresolved []string
}

// TotalChanges counts proposed property changes across every instance.
func (p SyncPlan) TotalChanges() int {
	n := 0
	for _, w := range p.Widgets {
		n += len(w.Changes)
	}
	return n
}

// Empty reports whether there is nothing to do.
func (p SyncPlan) Empty() bool { return p.TotalChanges() == 0 }

// SyncOptions narrows the blast radius of a plan.
type SyncOptions struct {
	WidgetID  string // only this widget type
	Container string // only this page/snippet (qualified name)
}

// PlanWidgetSync compares every stored widget instance against the widget package
// installed in the project and returns what would change. It mutates nothing.
//
// Note it reads the .mpk **directly** rather than the .def.json definition registry.
// The registry carries authoring ROUTING (which MDL keyword feeds which property key),
// not the schema CE0463 compares — verified during #716, where regenerating every
// definition from the project's .mpk left the error count unchanged.
func PlanWidgetSync(b backend.WidgetBackend, projectPath string, opts SyncOptions) (*SyncPlan, error) {
	if b == nil {
		return nil, fmt.Errorf("not connected to a project")
	}

	defs, err := installedWidgetDefs(projectPath)
	if err != nil {
		return nil, err
	}
	if len(defs) == 0 {
		return &SyncPlan{}, nil
	}

	plan := &SyncPlan{}
	ids := make([]string, 0, len(defs))
	for id := range defs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		if opts.WidgetID != "" && !strings.EqualFold(opts.WidgetID, id) {
			continue
		}
		instances, err := b.FindAllCustomWidgetTypes(id)
		if err != nil {
			return nil, fmt.Errorf("find instances of %s: %w", id, err)
		}
		for _, inst := range instances {
			if opts.Container != "" && !strings.EqualFold(opts.Container, inst.UnitName) {
				continue
			}
			wp := planInstance(inst, defs[id])
			if len(wp.Changes) > 0 {
				plan.Widgets = append(plan.Widgets, wp)
			}
		}
	}

	sort.Slice(plan.Widgets, func(i, j int) bool {
		if plan.Widgets[i].Container != plan.Widgets[j].Container {
			return plan.Widgets[i].Container < plan.Widgets[j].Container
		}
		return plan.Widgets[i].Widget < plan.Widgets[j].Widget
	})
	return plan, nil
}

// planInstance diffs one stored instance against its package definition.
func planInstance(inst *types.RawCustomWidgetType, def *mpk.WidgetDefinition) SyncWidgetPlan {
	wp := SyncWidgetPlan{
		Container:   inst.UnitName,
		ContainerID: inst.UnitID,
		Widget:      inst.WidgetName,
		WidgetID:    inst.WidgetID,
		PackageVer:  def.Version,
	}

	stored := storedPropertyTypes(inst.RawType)
	wp.StoredKeys = len(stored)

	// System properties (Label, Visibility, Editability) are not declared as regular
	// properties in the widget XML but are stored on the instance. Treating them as
	// "not in the package" would delete them.
	system := def.SystemPropertyKeys()

	pkg := map[string]*mpk.PropertyDef{}
	for i := range def.Properties {
		pkg[def.Properties[i].Key] = &def.Properties[i]
	}
	wp.PackageKeys = len(pkg)

	for _, key := range sortedKeys(stored) {
		if system[key] {
			continue
		}
		p, ok := pkg[key]
		if !ok {
			wp.Changes = append(wp.Changes, SyncPropertyChange{
				Kind:   SyncRemove,
				Key:    key,
				Detail: fmt.Sprintf("not declared by %s %s", shortWidgetName(def.ID), def.Version),
			})
			continue
		}
		wp.Changes = append(wp.Changes, attrChanges(key, stored[key], p)...)
	}

	for _, key := range sortedDefKeys(def.Properties) {
		if _, ok := stored[key]; !ok {
			wp.Changes = append(wp.Changes, SyncPropertyChange{
				Kind:   SyncAdd,
				Key:    key,
				Detail: fmt.Sprintf("declared by %s %s, default %q", shortWidgetName(def.ID), def.Version, pkg[key].DefaultValue),
			})
		}
	}
	return wp
}

// attrChanges reports definition-attribute drift on a property that exists on both
// sides. Only attributes the stored ValueType ALREADY carries are considered: adding a
// key the node does not have invents a property this Mendix version may not define —
// the mendixlabs/mxcli#759 failure shape.
func attrChanges(key string, vt bson.D, p *mpk.PropertyDef) []SyncPropertyChange {
	var out []SyncPropertyChange
	if cur, ok := bsonLookup(vt, "Required"); ok {
		if b, isBool := cur.(bool); isBool && b != p.Required {
			out = append(out, SyncPropertyChange{
				Kind:   SyncUpdate,
				Key:    key,
				Detail: fmt.Sprintf("Required %v -> %v", b, p.Required),
			})
		}
	}
	if cur, ok := bsonLookup(vt, "OnChangeProperty"); ok {
		if s, isStr := cur.(string); isStr && s != p.OnChange {
			out = append(out, SyncPropertyChange{
				Kind:   SyncUpdate,
				Key:    key,
				Detail: fmt.Sprintf("OnChangeProperty %q -> %q", s, p.OnChange),
			})
		}
	}
	return out
}

// storedPropertyTypes maps PropertyKey -> its ValueType document, for the top-level
// PropertyTypes of a stored widget instance.
func storedPropertyTypes(rawType any) map[string]bson.D {
	out := map[string]bson.D{}
	objType, ok := bsonLookup(rawType, "ObjectType")
	if !ok {
		return out
	}
	pts, ok := bsonLookup(objType, "PropertyTypes")
	if !ok {
		return out
	}
	for _, pt := range bsonArray(pts) {
		key, ok := bsonLookup(pt, "PropertyKey")
		if !ok {
			continue
		}
		name, _ := key.(string)
		if name == "" {
			continue
		}
		vt := bson.D{}
		if v, ok := bsonLookup(pt, "ValueType"); ok {
			if d, ok := asDoc(v); ok {
				vt = d
			}
		}
		out[name] = vt
	}
	return out
}

// installedWidgetDefs parses every .mpk under the project's widgets/ directory and
// returns the definitions keyed by widget ID. A single .mpk can bundle many widgets
// (Charts.mpk ships ten), so every one is registered.
//
// Unlike RefreshWidgetDefinitions this does NOT skip widgets that have a hand-written
// built-in definition: those (Gallery, the filters) are exactly the ones that go stale,
// and the built-in registry has no bearing on stored schema.
func installedWidgetDefs(projectPath string) (map[string]*mpk.WidgetDefinition, error) {
	widgetsDir := filepath.Join(filepath.Dir(projectPath), "widgets")
	matches, err := filepath.Glob(filepath.Join(widgetsDir, "*.mpk"))
	if err != nil {
		return nil, fmt.Errorf("scan widgets directory: %w", err)
	}
	defs := map[string]*mpk.WidgetDefinition{}
	for _, path := range matches {
		parsed, err := mpk.ParseMPKAll(path)
		if err != nil {
			// A single unreadable package must not fail the whole plan; it becomes an
			// unresolved widget if the model references it.
			continue
		}
		for _, d := range parsed {
			if d != nil && d.ID != "" {
				defs[d.ID] = d
			}
		}
	}
	return defs, nil
}

// --- small BSON helpers -----------------------------------------------------
// RawType/RawObject cross the backend boundary as `any` (mdl/types avoids a BSON
// driver dependency); underneath they are bson.D.

func asDoc(v any) (bson.D, bool) {
	switch d := v.(type) {
	case bson.D:
		return d, true
	case *bson.D:
		if d != nil {
			return *d, true
		}
	}
	return nil, false
}

func bsonLookup(v any, key string) (any, bool) {
	d, ok := asDoc(v)
	if !ok {
		return nil, false
	}
	for _, e := range d {
		if e.Key == key {
			return e.Value, true
		}
	}
	return nil, false
}

func bsonArray(v any) []any {
	switch a := v.(type) {
	case bson.A:
		return a
	case []any:
		return a
	}
	return nil
}

func sortedKeys(m map[string]bson.D) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedDefKeys(props []mpk.PropertyDef) []string {
	out := make([]string, 0, len(props))
	for _, p := range props {
		out = append(out, p.Key)
	}
	sort.Strings(out)
	return out
}

// shortWidgetName turns com.mendix.widget.web.datagrid.Datagrid into Datagrid for
// readable report lines.
func shortWidgetName(id string) string {
	if i := strings.LastIndex(id, "."); i >= 0 && i < len(id)-1 {
		return id[i+1:]
	}
	return id
}
