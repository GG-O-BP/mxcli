// SPDX-License-Identifier: Apache-2.0

package modelsdkbackend

import (
	"strings"

	"github.com/mendixlabs/mxcli/model"
)

// The gen ConsumedODataService / PublishedODataService2 accessors don't surface
// (or bind to mismatched storage keys for) the HttpConfiguration sub-document and
// the EntityTypes/EntitySets member tree. A read that drops them makes any
// CREATE OR MODIFY / ALTER round-trip lossy — dropping the ServiceUrl (CE5111) or
// stripping the published entity tree (NullReferenceException on load). These
// helpers read the affected sub-documents straight from the normalised raw unit,
// mirroring the legacy parser's keys and the modelsdk writer's vocabulary so the
// read→modify→write round-trip is faithful.

// httpConfigFromRaw parses a Microflows$HttpConfiguration sub-document. Keys and
// field mapping mirror sdk/mpr.parseODataHttpConfiguration (the real Studio Pro
// storage names, which the modelsdk writer also emits).
func odataHttpConfigFromRaw(raw map[string]any) *model.HttpConfiguration {
	cfg := &model.HttpConfiguration{}
	cfg.ID = model.ID(jsExtractBsonID(raw["$ID"]))
	cfg.TypeName = jsExtractString(raw["$Type"])
	cfg.UseAuthentication = jsExtractBool(raw["UseHttpAuthentication"])
	cfg.Username = jsExtractString(raw["HttpAuthenticationUserName"])
	cfg.Password = jsExtractString(raw["HttpAuthenticationPassword"])
	cfg.HttpMethod = jsExtractString(raw["HttpMethod"])
	cfg.OverrideLocation = jsExtractBool(raw["OverrideLocation"])
	cfg.CustomLocation = jsExtractString(raw["CustomLocation"])
	cfg.ClientCertificate = jsExtractString(raw["ClientCertificate"])
	for _, h := range jsAsSlice(raw["HttpHeaderEntries"]) {
		hm := jsToMap(h)
		if hm == nil {
			continue
		}
		cfg.HeaderEntries = append(cfg.HeaderEntries, &model.HttpHeaderEntry{
			BaseElement: model.BaseElement{ID: model.ID(jsExtractBsonID(hm["$ID"])), TypeName: jsExtractString(hm["$Type"])},
			Key:         jsExtractString(hm["Key"]),
			Value:       jsExtractString(hm["Value"]),
		})
	}
	return cfg
}

// publishedEntityTreeFromRaw parses the EntityTypes + EntitySets of a published
// OData service from its raw unit, resolving each entity set's EntityTypePointer
// back to the entity-type name (mirrors sdk/mpr.parsePublishedODataService).
func publishedEntityTreeFromRaw(raw map[string]any) ([]*model.PublishedEntityType, []*model.PublishedEntitySet) {
	var ets []*model.PublishedEntityType
	byID := map[string]*model.PublishedEntityType{}
	for _, e := range jsAsSlice(raw["EntityTypes"]) {
		em := jsToMap(e)
		if em == nil {
			continue
		}
		et := publishedEntityTypeFromRaw(em)
		ets = append(ets, et)
		byID[string(et.ID)] = et
	}

	var sets []*model.PublishedEntitySet
	for _, e := range jsAsSlice(raw["EntitySets"]) {
		sm := jsToMap(e)
		if sm == nil {
			continue
		}
		sets = append(sets, publishedEntitySetFromRaw(sm, byID))
	}
	return ets, sets
}

// publishedMicroflowsFromRaw parses the Microflows part list — the service's
// OData actions. Without it DESCRIBE would emit a service missing its actions,
// and replaying that output would silently drop them.
func publishedMicroflowsFromRaw(raw map[string]any) []*model.PublishedMicroflow {
	var out []*model.PublishedMicroflow
	for _, e := range jsAsSlice(raw["Microflows"]) {
		m := jsToMap(e)
		if m == nil {
			continue
		}
		pm := &model.PublishedMicroflow{
			Microflow:   jsExtractString(m["Microflow"]),
			ExposedName: jsExtractString(m["ExposedName"]),
			Summary:     jsExtractString(m["Summary"]),
			Description: jsExtractString(m["Description"]),
		}
		pm.TypeName = "ODataPublish$PublishedMicroflow"
		pm.ReturnTypeKind, pm.ReturnTypeRef = odataDataTypeFromRaw(m["ReturnType"])
		for _, p := range jsAsSlice(m["Parameters"]) {
			pmap := jsToMap(p)
			if pmap == nil {
				continue
			}
			mp := &model.PublishedMicroflowParameter{
				MicroflowParameter: jsExtractString(pmap["MicroflowParameter"]),
				ExposedName:        jsExtractString(pmap["ExposedName"]),
				CanBeEmpty:         jsExtractBool(pmap["CanBeEmpty"]),
				Summary:            jsExtractString(pmap["Summary"]),
				Description:        jsExtractString(pmap["Description"]),
			}
			mp.TypeName = "ODataPublish$PublishedMicroflowParameter"
			mp.DataTypeKind, mp.DataTypeRef = odataDataTypeFromRaw(pmap["DataType"])
			pm.Parameters = append(pm.Parameters, mp)
		}
		out = append(out, pm)
	}
	return out
}

// odataDataTypeFromRaw reads a DataTypes$* element into the kind + ref pair the
// semantic model carries. Mirrors sdk/mpr.parseODataDataType.
func odataDataTypeFromRaw(v any) (kind, ref string) {
	m := jsToMap(v)
	if m == nil {
		return "", ""
	}
	t := jsExtractString(m["$Type"])
	t = strings.TrimSuffix(strings.TrimPrefix(t, "DataTypes$"), "Type")
	switch t {
	case "":
		return "", ""
	case "Object", "List":
		return t, jsExtractString(m["Entity"])
	case "Enumeration":
		return t, jsExtractString(m["Enumeration"])
	}
	return t, ""
}

func publishedEntityTypeFromRaw(raw map[string]any) *model.PublishedEntityType {
	et := &model.PublishedEntityType{
		BaseElement: model.BaseElement{ID: model.ID(jsExtractBsonID(raw["$ID"])), TypeName: jsExtractString(raw["$Type"])},
		Entity:      jsExtractString(raw["Entity"]),
		ExposedName: jsExtractString(raw["ExposedName"]),
		Summary:     jsExtractString(raw["Summary"]),
		Description: jsExtractString(raw["Description"]),
	}
	for _, m := range jsAsSlice(raw["ChildMembers"]) {
		mm := jsToMap(m)
		if mm == nil {
			continue
		}
		et.Members = append(et.Members, publishedMemberFromRaw(mm))
	}
	return et
}

func publishedMemberFromRaw(raw map[string]any) *model.PublishedMember {
	m := &model.PublishedMember{
		BaseElement: model.BaseElement{ID: model.ID(jsExtractBsonID(raw["$ID"])), TypeName: jsExtractString(raw["$Type"])},
		ExposedName: jsExtractString(raw["ExposedName"]),
		Filterable:  jsExtractBool(raw["Filterable"]),
		Sortable:    jsExtractBool(raw["Sortable"]),
		IsPartOfKey: jsExtractBool(raw["IsPartOfKey"]),
	}
	switch m.TypeName {
	case "ODataPublish$PublishedAttribute":
		m.Kind = "attribute"
		m.Name = jsExtractString(raw["Attribute"])
		m.EdmType = jsExtractString(raw["EdmType"])
	case "ODataPublish$PublishedAssociationEnd":
		m.Kind = "association"
		m.Name = jsExtractString(raw["Association"])
		m.AssociationTargetEntity = jsExtractString(raw["Entity"])
		m.ExposedAssociationName = jsExtractString(raw["ExposedAssociationName"])
		m.IsMany = jsExtractBool(raw["IsMany"])
	case "ODataPublish$PublishedId":
		m.Kind = "id"
		m.Name = jsExtractString(raw["Attribute"])
	default:
		m.Kind = "unknown"
	}
	return m
}

func publishedEntitySetFromRaw(raw map[string]any, byID map[string]*model.PublishedEntityType) *model.PublishedEntitySet {
	es := &model.PublishedEntitySet{
		BaseElement: model.BaseElement{ID: model.ID(jsExtractBsonID(raw["$ID"])), TypeName: jsExtractString(raw["$Type"])},
		ExposedName: jsExtractString(raw["ExposedName"]),
		UsePaging:   jsExtractBool(raw["UsePaging"]),
		PageSize:    jsExtractInt(raw["PageSize"]),
		ReadMode:    parseODataModeRaw(raw["ReadMode"]),
		InsertMode:  parseODataModeRaw(raw["InsertMode"]),
		UpdateMode:  parseODataModeRaw(raw["UpdateMode"]),
		DeleteMode:  parseODataModeRaw(raw["DeleteMode"]),
	}
	// Query options round-trip so DESCRIBE can print a turned-off one. Absent
	// (or true, the default) is left nil so DESCRIBE stays quiet about it.
	if qo := jsToMap(raw["QueryOptions"]); qo != nil {
		es.Countable = falseOnly(qo["Countable"])
		es.SkipSupported = falseOnly(qo["SkipSupported"])
		es.TopSupported = falseOnly(qo["TopSupported"])
	}
	if et, ok := byID[jsExtractBsonID(raw["EntityTypePointer"])]; ok {
		es.EntityTypeName = et.Entity
	}
	return es
}

// parseODataModeRaw maps a Read/Change source sub-document to the string the
// modelsdk writer expects (odataReadModeToGen/odataChangeModeToGen), keeping the
// ALTER round-trip faithful. Mirrors sdk/mpr.parseChangeMode.
func parseODataModeRaw(v any) string {
	mm := jsToMap(v)
	if mm == nil {
		return ""
	}
	switch jsExtractString(mm["$Type"]) {
	case "ODataPublish$ReadSource":
		return "ReadFromDatabase"
	case "ODataPublish$ChangeSource":
		return "ChangeFromDatabase"
	case "ODataPublish$ChangeNotSupported":
		return "NotSupported"
	case "ODataPublish$CallMicroflowToRead", "ODataPublish$CallMicroflowToChange":
		if mf := jsExtractString(mm["Microflow"]); mf != "" {
			return "CallMicroflow:" + mf
		}
		return "CallMicroflow"
	default:
		return jsExtractString(mm["$Type"])
	}
}

// jsExtractInt reads an int from a normalised raw value (int32/int64/float64).
func jsExtractInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// falseOnly returns a pointer to false when v is present and false, else nil.
// A stored true is the default, and printing every default back would make
// DESCRIBE output noisier than what the author wrote.
func falseOnly(v any) *bool {
	if v == nil || jsExtractBool(v) {
		return nil
	}
	f := false
	return &f
}
