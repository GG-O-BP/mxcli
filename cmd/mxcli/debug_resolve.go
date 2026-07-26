// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/mendixlabs/mxcli/sdk/microflows"
	"github.com/mendixlabs/mxcli/sdk/mpr"
)

// debug_resolve.go turns a microflow name + activity selector into the model GUID
// the debugger uses as object_id, and keeps a small local record of the
// breakpoints mxcli has set (the runtime exposes no "list breakpoints" call).
// Slice 2 of the microflow debugger.

// activityInfo is one microflow object, with the model GUID the debugger wants as
// its object_id (mxcli's activity GetID() already yields the Microsoft-GUID /
// bytes_le form the debugger expects — see types.BlobToUUID).
type activityInfo struct {
	Index    int    // 1-based, in object-collection order
	Type     string // struct name, e.g. ActionActivity / ExclusiveSplit / StartEvent
	Caption  string // Mendix caption, e.g. "Create 'Game'" (empty for plain events)
	ObjectID string // model GUID == debugger object_id
}

// resolveMicroflowActivities opens the project and lists a microflow's objects.
func resolveMicroflowActivities(projectPath, qualifiedName string) ([]activityInfo, error) {
	r, err := mpr.Open(projectPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	contents, err := r.GetRawMicroflowByName(qualifiedName)
	if err != nil {
		return nil, err
	}
	mf, err := mpr.ParseMicroflowBSON(contents, "", "")
	if err != nil {
		return nil, fmt.Errorf("parsing microflow %s: %w", qualifiedName, err)
	}
	return extractActivities(mf), nil
}

// extractActivities flattens a microflow's object collection into activityInfo.
func extractActivities(mf *microflows.Microflow) []activityInfo {
	var out []activityInfo
	if mf == nil || mf.ObjectCollection == nil {
		return out
	}
	for i, o := range mf.ObjectCollection.Objects {
		typeName, caption := objectTypeCaption(o)
		out = append(out, activityInfo{
			Index:    i + 1,
			Type:     typeName,
			Caption:  caption,
			ObjectID: string(o.GetID()),
		})
	}
	return out
}

// objectTypeCaption returns a microflow object's struct name and its Caption (if
// the concrete type carries one — action activities, splits, loops, annotations
// do; bare start/end events do not).
func objectTypeCaption(o microflows.MicroflowObject) (typeName, caption string) {
	v := reflect.ValueOf(o)
	for v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return fmt.Sprintf("%T", o), ""
	}
	typeName = v.Type().Name()
	if f := v.FieldByName("Caption"); f.IsValid() && f.Kind() == reflect.String {
		caption = f.String()
	}
	return typeName, caption
}

// matchActivity selects one activity by an "#<index>" (1-based) or a
// case-insensitive caption substring that must match exactly one object.
func matchActivity(acts []activityInfo, selector string) (activityInfo, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return activityInfo{}, fmt.Errorf("empty --activity; use '#<index>' or a caption substring (see 'mxcli debug activities')")
	}
	if strings.HasPrefix(selector, "#") {
		n, err := strconv.Atoi(strings.TrimPrefix(selector, "#"))
		if err != nil || n < 1 || n > len(acts) {
			return activityInfo{}, fmt.Errorf("activity index %q out of range (1..%d)", selector, len(acts))
		}
		return acts[n-1], nil
	}
	low := strings.ToLower(selector)
	var matches []activityInfo
	for _, a := range acts {
		if a.Caption != "" && strings.Contains(strings.ToLower(a.Caption), low) {
			matches = append(matches, a)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return activityInfo{}, fmt.Errorf("no activity caption matches %q — run 'mxcli debug activities' or use --activity '#<index>'", selector)
	default:
		return activityInfo{}, fmt.Errorf("%q matches %d activities — be more specific or use --activity '#<index>'", selector, len(matches))
	}
}

// localBreakpoint is one breakpoint mxcli has set, recorded so 'breaks' can show
// the name→GUID reverse map (the runtime has no read-back).
type localBreakpoint struct {
	Microflow string `json:"microflow"`
	Activity  string `json:"activity"` // caption or type#index, for display
	ObjectID  string `json:"objectId"`
	Condition string `json:"condition,omitempty"`
}

// loadBreakpoints reads the local breakpoint record (missing file = empty).
func loadBreakpoints(path string) ([]localBreakpoint, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var bps []localBreakpoint
	if err := json.Unmarshal(b, &bps); err != nil {
		return nil, err
	}
	return bps, nil
}

// saveBreakpoints writes the local breakpoint record.
func saveBreakpoints(path string, bps []localBreakpoint) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(bps, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
