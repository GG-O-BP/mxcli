// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"fmt"
	"sort"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/mendixlabs/mxcli/mdl/backend"
	"github.com/mendixlabs/mxcli/mdl/types"
	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/modelsdk/widgets"
	"github.com/mendixlabs/mxcli/modelsdk/widgets/mpk"
)

// widget_sync_apply.go writes the reconciliation the planner describes.
//
// Scope of this step: REMOVE and UPDATE only.
//
//   - remove — a PropertyKey the installed package no longer declares. This is the
//     mendixlabs/mxcli#716 case (`advanced`, dropped from Data Widgets after 3.4) and
//     the operation that actually clears CE0463.
//   - update — Required / OnChangeProperty on a surviving property, in place only.
//
// ADD is deliberately not applied yet. `mx update-widgets` does not add missing
// properties unconditionally — it adds 12 to each stale Gallery but none to four
// FeedbackModule Image instances that are short by four, and Mendix reports no CE0463
// on those. Adding is also the only operation that invents nodes, so it is the one
// worth being sure about. See PlanWidgetSync's comment.
//
// # The pairing invariant
//
// A CustomWidgets$WidgetProperty in Object.Properties is bound to its
// CustomWidgets$WidgetPropertyType in Type.ObjectType.PropertyTypes by TypePointer.
// Removing one half without the other yields a project Mendix cannot LOAD (the
// StreamingBsonUnitReader "does not contain a constructor with a parameter of type
// WidgetValue" failure) — which `mx check` reports as "0 errors" because it never got
// far enough to check anything. Both halves move together here, keyed on the
// PropertyType's $ID.

// SyncResult reports what was written.
type SyncResult struct {
	UnitsChanged    int
	WidgetsChanged  int
	PropertiesFixed int
	Skipped         []string // changes the plan proposed that this step does not apply
}

// ApplyWidgetSync reconciles stored widget instances and writes the affected units.
func ApplyWidgetSync(b backend.RawUnitBackend, projectPath string, opts SyncOptions) (*SyncResult, *SyncPlan, error) {
	plan, err := PlanWidgetSync(b, projectPath, opts)
	if err != nil {
		return nil, nil, err
	}

	// Group by unit so each document is read, mutated and written exactly once.
	byUnit := map[string][]SyncWidgetPlan{}
	for _, w := range plan.Widgets {
		byUnit[w.ContainerID] = append(byUnit[w.ContainerID], w)
	}
	unitIDs := make([]string, 0, len(byUnit))
	for id := range byUnit {
		unitIDs = append(unitIDs, id)
	}
	sort.Strings(unitIDs)

	defs, err := installedWidgetDefs(projectPath)
	if err != nil {
		return nil, plan, err
	}

	res := &SyncResult{}
	for _, unitID := range unitIDs {
		raw, err := b.GetRawUnitBytes(model.ID(unitID))
		if err != nil {
			return nil, plan, fmt.Errorf("read unit %s: %w", unitID, err)
		}
		var doc bson.D
		if err := bson.Unmarshal(raw, &doc); err != nil {
			return nil, plan, fmt.Errorf("parse unit %s: %w", unitID, err)
		}

		wanted := map[string][]SyncPropertyChange{}
		widgetDef := map[string]*mpk.WidgetDefinition{}
		for _, w := range byUnit[unitID] {
			wanted[w.Widget] = append(wanted[w.Widget], w.Changes...)
			widgetDef[w.Widget] = defs[w.WidgetID]
		}

		changed := 0
		widgets := 0
		out := mapWidgets(doc, func(name string, widget bson.D) (bson.D, bool) {
			changes, ok := wanted[name]
			if !ok {
				return widget, false
			}
			def := widgetDef[name]
			if def == nil {
				return widget, false
			}
			updated, n, skipped := applyToWidget(widget, changes, def, opts.AddMissing)
			if n == 0 {
				return widget, false
			}
			res.Skipped = append(res.Skipped, skipped...)
			changed += n
			widgets++
			return updated, true
		})

		if changed == 0 {
			continue
		}
		encoded, err := bson.Marshal(out)
		if err != nil {
			return nil, plan, fmt.Errorf("encode unit %s: %w", unitID, err)
		}
		if err := b.UpdateRawUnit(unitID, encoded); err != nil {
			return nil, plan, fmt.Errorf("write unit %s: %w", unitID, err)
		}
		res.UnitsChanged++
		res.WidgetsChanged += widgets
		res.PropertiesFixed += changed
	}
	return res, plan, nil
}

// applyToWidget rewrites one CustomWidget node. Returns the node, how many changes
// were applied, and the descriptions of any it declined to apply.
func applyToWidget(widget bson.D, changes []SyncPropertyChange, def *mpk.WidgetDefinition, addMissing bool) (bson.D, int, []string) {
	remove := map[string]bool{}
	update := map[string][]SyncPropertyChange{}
	var add []string
	var skipped []string
	for _, c := range changes {
		switch c.Kind {
		case SyncRemove:
			remove[c.Key] = true
		case SyncUpdate:
			update[c.Key] = append(update[c.Key], c)
		case SyncAdd:
			if addMissing {
				add = append(add, c.Key)
			} else {
				skipped = append(skipped, fmt.Sprintf("add %s", c.Key))
			}
		}
	}
	if len(remove) == 0 && len(update) == 0 && len(add) == 0 {
		return widget, 0, skipped
	}

	typeDoc, ok := docField(widget, "Type")
	if !ok {
		return widget, 0, skipped
	}
	objType, ok := docField(typeDoc, "ObjectType")
	if !ok {
		return widget, 0, skipped
	}
	propTypes, ok := arrField(objType, "PropertyTypes")
	if !ok {
		return widget, 0, skipped
	}

	// Pass 1 — decide which PropertyTypes go, remembering their $IDs so the paired
	// WidgetProperty can be removed with them.
	doomed := map[string]bool{}
	newPropTypes := bson.A{}
	applied := 0
	for _, item := range propTypes {
		pt, ok := item.(bson.D)
		if !ok {
			newPropTypes = append(newPropTypes, item) // array marker, preserved
			continue
		}
		key := bsonString(pt, "PropertyKey")
		if remove[key] {
			if id, ok := idOf(pt); ok {
				doomed[id] = true
			}
			applied++
			continue
		}
		// Definition attributes live on the PropertyType's ValueType, not on the
		// PropertyType — writing them one level up is a silent no-op guarded by the
		// "key must already exist" check (mendixlabs/mxcli#716). Update in place
		// only: adding a key the node does not carry invents a property this Mendix
		// version may not define (the #759 failure shape).
		if attrs, ok := update[key]; ok {
			if vt, ok := docField(pt, "ValueType"); ok {
				for _, a := range attrs {
					if a.Attr == "" || !hasKey(vt, a.Attr) {
						continue
					}
					vt = setField(vt, a.Attr, a.Value)
					applied++
				}
				pt = setField(pt, "ValueType", vt)
			}
		}
		newPropTypes = append(newPropTypes, pt)
	}

	// Pass 2 — build the pairs for properties the package declares and this instance
	// lacks. Construction is delegated to the authoring path (widgets.NewPropertyPair)
	// so there is one implementation of what a property pair looks like, not two.
	var newProps bson.A
	for _, key := range add {
		p := def.FindProperty(key)
		if p == nil {
			skipped = append(skipped, fmt.Sprintf("add %s (not found in package)", key))
			continue
		}
		ptMap, propMap, ok := widgets.NewPropertyPair(*p, types.GenerateID)
		if !ok {
			// An XML type with no BSON mapping: skip rather than invent a shape.
			skipped = append(skipped, fmt.Sprintf("add %s (unmapped type %q)", key, p.Type))
			continue
		}
		newPropTypes = append(newPropTypes, mapToBSON(ptMap))
		newProps = append(newProps, mapToBSON(propMap))
		applied++
	}

	// Pass 3 — drop the paired WidgetProperty for each removed PropertyType.
	objDoc, hasObj := docField(widget, "Object")
	if hasObj {
		if props, ok := arrField(objDoc, "Properties"); ok {
			kept := bson.A{}
			for _, item := range props {
				p, ok := item.(bson.D)
				if !ok {
					kept = append(kept, item)
					continue
				}
				if tp, ok := idField(p, "TypePointer"); ok && doomed[tp] {
					continue
				}
				kept = append(kept, p)
			}
			kept = append(kept, newProps...)
			objDoc = setField(objDoc, "Properties", kept)
			widget = setField(widget, "Object", objDoc)
		}
	}

	// Mendix checks the WidgetType's PropertyType ORDER, so appended properties must be
	// moved into the package's declaration order — appending at the end is itself a
	// CE0463 cause. (The WidgetObject's Properties order is tolerated.)
	objType = setField(objType, "PropertyTypes", orderPropertyTypes(newPropTypes, def))
	typeDoc = setField(typeDoc, "ObjectType", objType)
	widget = setField(widget, "Type", typeDoc)
	return widget, applied, skipped
}
