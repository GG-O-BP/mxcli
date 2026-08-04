// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/mendixlabs/mxcli/mdl/backend"
	"github.com/mendixlabs/mxcli/mdl/types"
)

// widget_scan.go enumerates every stored pluggable-widget instance in the model in a
// single pass, with the context needed to decide whether it may be modified.
//
// It replaces the per-widget-ID scan (WidgetBackend.FindAllCustomWidgetTypes), which
// had two measured defects:
//
//   - it searches Forms$Page and Forms$Snippet only, so widgets inside a
//     Forms$BuildingBlock are invisible — 44 changes `mx update-widgets` makes in a
//     blank project's Atlas building blocks that a page/snippet scan cannot see;
//   - it re-reads every unit once per widget type, i.e. O(widget types x units).
//     42 installed widget definitions over 370 units is ~15k unit reads for what is
//     one pass over the model.
//
// It also runs on both engines: it is built on ListRawUnitsByType, which the MPR and
// modelsdk backends both implement, whereas FindAllCustomWidgetTypes exists only in
// the legacy backend.

// widgetContainerTypes are the unit types that can hold a widget tree. Building
// blocks are included deliberately: mxcli supports authoring them, and Mendix's own
// update-widgets reconciles the widgets inside them.
var widgetContainerTypes = []string{
	"Forms$Page",
	"Forms$Snippet",
	"Forms$BuildingBlock",
	"Forms$PageTemplate",
	"Forms$Layout",
}

// scanCustomWidgetInstances walks every widget-bearing unit and returns one entry per
// stored CustomWidgets$CustomWidget node.
func scanCustomWidgetInstances(b backend.RawUnitBackend) ([]*types.CustomWidgetInstance, error) {
	modules, err := scanModules(b)
	if err != nil {
		return nil, err
	}

	var out []*types.CustomWidgetInstance
	for _, unitType := range widgetContainerTypes {
		units, err := b.ListRawUnitsByType(unitType)
		if err != nil {
			// A unit type absent from this project is not an error.
			continue
		}
		for _, u := range units {
			var doc bson.D
			if err := bson.Unmarshal(u.Contents, &doc); err != nil {
				continue
			}
			unitName := bsonString(doc, "Name")
			mod := modules.owner(string(u.ContainerID))
			collectWidgets(doc, func(w *types.CustomWidgetInstance) {
				w.UnitID = string(u.ID)
				w.UnitName = unitName
				w.UnitType = unitType
				w.ModuleName = mod.name
				w.ModuleIsTheme = mod.isTheme
				out = append(out, w)
			})
		}
	}
	return out, nil
}

// moduleInfo is what the scan needs to know about the module owning a unit.
type moduleInfo struct {
	name    string
	isTheme bool
}

// moduleIndex resolves a unit's owning module by walking container IDs upward.
type moduleIndex struct {
	byID     map[string]moduleInfo
	parentOf map[string]string
}

// owner walks up from a container ID until it reaches a module. Units nest inside
// folders, so the immediate ContainerID is often not the module itself.
func (m *moduleIndex) owner(containerID string) moduleInfo {
	seen := map[string]bool{}
	for id := containerID; id != "" && !seen[id]; id = m.parentOf[id] {
		seen[id] = true
		if info, ok := m.byID[id]; ok {
			return info
		}
	}
	return moduleInfo{}
}

// scanModules builds the module index, recording IsThemeModule.
//
// IsThemeModule is recorded but deliberately NOT used to exclude anything. It looked
// like the rule that explains why update-widgets leaves four FeedbackModule images
// alone, and it is wrong: Atlas_Core, Atlas_Web_Content and DataWidgets are theme
// modules too, and update-widgets reconciles 18 Atlas_Web_Content containers. The flag
// is kept because it is cheap and callers may want to report it.
func scanModules(b backend.RawUnitBackend) (*moduleIndex, error) {
	idx := &moduleIndex{byID: map[string]moduleInfo{}, parentOf: map[string]string{}}

	units, err := b.ListRawUnitsByType("Projects$ModuleImpl")
	if err != nil {
		return nil, fmt.Errorf("list modules: %w", err)
	}
	for _, u := range units {
		var doc bson.D
		if err := bson.Unmarshal(u.Contents, &doc); err != nil {
			continue
		}
		idx.byID[string(u.ID)] = moduleInfo{
			name:    bsonString(doc, "Name"),
			isTheme: bsonBool(doc, "IsThemeModule"),
		}
	}

	// Folders sit between a document and its module, so the walk needs the full
	// parent chain, not just the module units.
	for _, t := range []string{"Projects$Folder", "Projects$ModuleImpl"} {
		folders, err := b.ListRawUnitsByType(t)
		if err != nil {
			continue
		}
		for _, f := range folders {
			idx.parentOf[string(f.ID)] = string(f.ContainerID)
		}
	}
	return idx, nil
}

// collectWidgets walks a unit document and calls visit for every CustomWidget node,
// including nested ones (a widget inside a container inside a data view).
func collectWidgets(node any, visit func(*types.CustomWidgetInstance)) {
	switch v := node.(type) {
	case bson.D:
		if bsonString(v, "$Type") == "CustomWidgets$CustomWidget" {
			w := &types.CustomWidgetInstance{WidgetName: bsonString(v, "Name")}
			for _, e := range v {
				switch e.Key {
				case "Type":
					if t, ok := e.Value.(bson.D); ok {
						w.RawType = t
						w.WidgetID = bsonString(t, "WidgetId")
					}
				case "Object":
					if o, ok := e.Value.(bson.D); ok {
						w.RawObject = o
					}
				}
			}
			if w.WidgetID != "" {
				visit(w)
			}
			// Fall through: a pluggable widget can hold other widgets in a
			// `widgets`-typed property (DataGrid2 columns with custom content).
		}
		for _, e := range v {
			collectWidgets(e.Value, visit)
		}
	case bson.A:
		for _, item := range v {
			collectWidgets(item, visit)
		}
	case []any:
		for _, item := range v {
			collectWidgets(item, visit)
		}
	}
}

func bsonString(d bson.D, key string) string {
	for _, e := range d {
		if e.Key == key {
			s, _ := e.Value.(string)
			return s
		}
	}
	return ""
}

func bsonBool(d bson.D, key string) bool {
	for _, e := range d {
		if e.Key == key {
			b, _ := e.Value.(bool)
			return b
		}
	}
	return false
}

// --- bson.D editing helpers -------------------------------------------------
// bson.D is an ordered slice, and Mendix cares about key order, so these edit in
// place (replacing a value at its existing position) rather than rebuilding.

func docField(d bson.D, key string) (bson.D, bool) {
	for _, e := range d {
		if e.Key == key {
			v, ok := e.Value.(bson.D)
			return v, ok
		}
	}
	return nil, false
}

func arrField(d bson.D, key string) (bson.A, bool) {
	for _, e := range d {
		if e.Key == key {
			switch a := e.Value.(type) {
			case bson.A:
				return a, true
			case []any:
				return bson.A(a), true
			}
		}
	}
	return nil, false
}

// setField replaces key's value, preserving its position. Appends only if absent —
// callers here always pass a key that exists.
func setField(d bson.D, key string, value any) bson.D {
	for i := range d {
		if d[i].Key == key {
			d[i].Value = value
			return d
		}
	}
	return append(d, bson.E{Key: key, Value: value})
}

// idOf returns a node's $ID as a comparable hex string.
func idOf(d bson.D) (string, bool) { return idField(d, "$ID") }

func idField(d bson.D, key string) (string, bool) {
	for _, e := range d {
		if e.Key != key {
			continue
		}
		switch v := e.Value.(type) {
		case []byte:
			return fmt.Sprintf("%x", v), true
		case primitive.Binary:
			return fmt.Sprintf("%x", v.Data), true
		case string:
			return v, true
		}
	}
	return "", false
}

// mapWidgets walks a unit document and gives visit a chance to rewrite every
// CustomWidget node, returning the rebuilt document.
func mapWidgets(node any, visit func(name string, widget bson.D) (bson.D, bool)) any {
	switch v := node.(type) {
	case bson.D:
		if bsonString(v, "$Type") == "CustomWidgets$CustomWidget" {
			if replaced, ok := visit(bsonString(v, "Name"), v); ok {
				v = replaced
			}
		}
		out := make(bson.D, len(v))
		for i, e := range v {
			out[i] = bson.E{Key: e.Key, Value: mapWidgets(e.Value, visit)}
		}
		return out
	case bson.A:
		out := make(bson.A, len(v))
		for i, item := range v {
			out[i] = mapWidgets(item, visit)
		}
		return out
	case []any:
		out := make(bson.A, len(v))
		for i, item := range v {
			out[i] = mapWidgets(item, visit)
		}
		return out
	}
	return node
}

// hasKey reports whether a document already carries a key — the guard that keeps an
// update from inventing a property (mendixlabs/mxcli#759).
func hasKey(d bson.D, key string) bool {
	for _, e := range d {
		if e.Key == key {
			return true
		}
	}
	return false
}
