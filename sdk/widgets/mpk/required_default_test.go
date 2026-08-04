// SPDX-License-Identifier: Apache-2.0

package mpk

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// The Mendix pluggable-widget XML schema defaults `required` to true; only an
// explicit required="false" makes a property optional. Most properties in the
// shipped packages omit the attribute entirely — DataGrid2 3.4 omits it on 24 of
// its 40 properties.
//
// Reading missing→false made syncDefinitionAttrs (augment.go) overwrite 24
// correct `true`s with `false` on every authored Data grid 2, which Mendix
// reports as CE0463 "the definition of this widget has changed"
// (mendixlabs/mxcli#716). modelsdk/widgets/mpk has read it correctly since #600;
// this is the sdk copy catching up, so the two engines agree.
func TestRequiredDefaultsToTrueWhenAttributeAbsent(t *testing.T) {
	path := writeTestMPK(t, `<?xml version="1.0" encoding="utf-8"?>
<widget id="com.mendix.widget.test.Req" pluginWidget="true" needsEntityContext="true"
        xmlns="http://www.mendix.com/widget/1.0/">
  <name>Req</name>
  <properties>
    <propertyGroup caption="General">
      <property key="absent" type="boolean" defaultValue="false">
        <caption>Absent</caption><description/>
      </property>
      <property key="explicitTrue" type="boolean" required="true" defaultValue="false">
        <caption>Explicit true</caption><description/>
      </property>
      <property key="explicitFalse" type="boolean" required="false" defaultValue="false">
        <caption>Explicit false</caption><description/>
      </property>
      <property key="parent" type="object" isList="true">
        <caption>Parent</caption><description/>
        <properties>
          <propertyGroup caption="Child">
            <property key="childAbsent" type="boolean" defaultValue="false">
              <caption>Child absent</caption><description/>
            </property>
            <property key="childFalse" type="boolean" required="false" defaultValue="false">
              <caption>Child false</caption><description/>
            </property>
          </propertyGroup>
        </properties>
      </property>
    </propertyGroup>
  </properties>
</widget>`)

	ClearCache()
	def, err := ParseMPK(path)
	if err != nil {
		t.Fatalf("ParseMPK: %v", err)
	}

	got := map[string]bool{}
	var index func([]PropertyDef)
	index = func(ps []PropertyDef) {
		for _, p := range ps {
			got[p.Key] = p.Required
			index(p.Children)
		}
	}
	index(def.Properties)

	want := map[string]bool{
		"absent":        true,
		"explicitTrue":  true,
		"explicitFalse": false,
		"parent":        true,
		"childAbsent":   true,
		"childFalse":    false,
	}
	for key, exp := range want {
		act, ok := got[key]
		if !ok {
			t.Errorf("property %q not parsed", key)
			continue
		}
		if act != exp {
			t.Errorf("property %q: Required = %v, want %v", key, act, exp)
		}
	}
}

// writeTestMPK builds a minimal single-widget .mpk (a ZIP holding package.xml
// plus the widget XML) and returns its path.
func writeTestMPK(t *testing.T, widgetXML string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.mpk")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create mpk: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	add := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip entry %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	add("package.xml", `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.mendix.com/package/1.0/">
  <clientModule name="Req" version="1.0.0" xmlns="http://www.mendix.com/clientModule/1.0/">
    <widgetFiles><widgetFile path="Req.xml"/></widgetFiles>
  </clientModule>
</package>`)
	add("Req.xml", widgetXML)

	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return path
}
