// SPDX-License-Identifier: Apache-2.0

package linter_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/catalog"
	"github.com/mendixlabs/mxcli/mdl/linter"
)

// javaActionFixture builds a catalog holding one module, two Java actions (one
// documented, one not) and three parameters (one documented, two not).
//
// Rows are inserted directly rather than round-tripped through a reader because
// the subject here is the query + Starlark surface, not the MPR parser.
func javaActionFixture(t *testing.T) *catalog.Catalog {
	t.Helper()

	cat, err := catalog.NewFromFile(filepath.Join(t.TempDir(), "cat.db"))
	if err != nil {
		t.Fatalf("NewFromFile: %v", err)
	}
	t.Cleanup(func() { cat.Close() })
	db := cat.CatalogDB()

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("exec %s: %v", q, err)
		}
	}

	exec(`INSERT INTO modules_data (Id, Name, ProjectId, SnapshotId) VALUES (?,?,?,?)`,
		"mod-1", "Formula1Backend", "default", "s1")

	insertAction := func(id, name, doc string) {
		exec(`INSERT INTO java_actions_data
			(Id, Name, QualifiedName, ModuleName, Folder, Documentation, ExportLevel,
			 ReturnType, ParameterCount, ProjectId, SnapshotId)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			id, name, "Formula1Backend."+name, "Formula1Backend", "", doc, "Hidden",
			"String", 0, "default", "s1")
	}
	insertParam := func(id, actionID, actionName, name, desc string, ordinal int) {
		exec(`INSERT INTO java_action_parameters_data
			(Id, JavaActionId, JavaActionName, QualifiedName, ModuleName, Name,
			 Description, ParameterType, IsRequired, Ordinal, ProjectId, SnapshotId)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, actionID, actionName, "Formula1Backend."+actionName+"."+name,
			"Formula1Backend", name, desc, "String", 1, ordinal, "default", "s1")
	}

	insertAction("ja-1", "ParseTop", "")                         // undocumented
	insertAction("ja-2", "FormatLapTime", "Renders a lap time.") // documented
	insertParam("p-1", "ja-1", "ParseTop", "pUri", "", 0)        // undocumented
	insertParam("p-2", "ja-1", "ParseTop", "pDefault", "", 1)    // undocumented
	insertParam("p-3", "ja-2", "FormatLapTime", "pMillis", "Elapsed milliseconds.", 0)

	return cat
}

// The catalog stored only a parameter COUNT, so nothing downstream could see a
// parameter's description — the field a caller reads in Studio Pro's action
// dialog. This asserts the rows land and arrive attached to their action, in
// declaration order.
func TestJavaActions_CarryTheirParameters(t *testing.T) {
	ctx := linter.NewLintContext(javaActionFixture(t), &minimalReader{})

	byName := map[string]linter.JavaAction{}
	for ja := range ctx.JavaActions() {
		byName[ja.Name] = ja
	}
	if len(byName) != 2 {
		t.Fatalf("got %d Java actions, want 2: %v", len(byName), byName)
	}

	parse := byName["ParseTop"]
	if parse.Documentation != "" {
		t.Errorf("ParseTop documentation = %q, want empty", parse.Documentation)
	}
	if len(parse.Parameters) != 2 {
		t.Fatalf("ParseTop has %d parameters, want 2", len(parse.Parameters))
	}
	// Ordinal, not alphabetical: a signature read out of order is misleading.
	if parse.Parameters[0].Name != "pUri" || parse.Parameters[1].Name != "pDefault" {
		t.Errorf("parameters out of declaration order: %v", parse.Parameters)
	}
	if parse.Parameters[0].ParameterType != "String" || !parse.Parameters[0].IsRequired {
		t.Errorf("parameter metadata lost: %+v", parse.Parameters[0])
	}

	fmtLap := byName["FormatLapTime"]
	if fmtLap.Documentation == "" {
		t.Error("FormatLapTime documentation was dropped")
	}
	if len(fmtLap.Parameters) != 1 || fmtLap.Parameters[0].Description == "" {
		t.Errorf("FormatLapTime parameter description lost: %+v", fmtLap.Parameters)
	}
}

// Marketplace and system modules carry code the user did not write, and every
// other document iterator here excludes them. Reporting "the Community Commons
// Java actions are undocumented" would bury the findings that are actionable.
func TestJavaActions_ExcludeMarketplaceModules(t *testing.T) {
	cat := javaActionFixture(t)
	db := cat.CatalogDB()
	if _, err := db.Exec(
		`INSERT INTO modules_data (Id, Name, Source, ProjectId, SnapshotId) VALUES (?,?,?,?,?)`,
		"mod-2", "CommunityCommons", "Marketplace", "default", "s1"); err != nil {
		t.Fatalf("insert module: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO java_actions_data
		(Id, Name, QualifiedName, ModuleName, Folder, Documentation, ExportLevel,
		 ReturnType, ParameterCount, ProjectId, SnapshotId)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		"ja-9", "StringUtil", "CommunityCommons.StringUtil", "CommunityCommons", "",
		"", "Hidden", "String", 0, "default", "s1"); err != nil {
		t.Fatalf("insert action: %v", err)
	}

	ctx := linter.NewLintContext(cat, &minimalReader{})
	for ja := range ctx.JavaActions() {
		if ja.ModuleName == "CommunityCommons" {
			t.Errorf("a Marketplace module's Java action was reported: %s", ja.QualifiedName)
		}
	}
}

// The rule under test is the one that ships, loaded from disk — not a copy
// inlined here. A test against an inlined copy proves the Starlark builtin works
// and says nothing about whether QUAL002 uses it.
func runMissingDocRule(t *testing.T, cat *catalog.Catalog, options map[string]any) []linter.Violation {
	t.Helper()
	rule, err := linter.LoadStarlarkRule("../../.claude/lint-rules/missing_documentation.star")
	if err != nil {
		t.Fatalf("loading the shipped QUAL002: %v", err)
	}
	if options != nil {
		rule.Configure(options)
	}
	return rule.Check(linter.NewLintContext(cat, &minimalReader{}))
}

// mxcli-formula1: entities and microflows were covered; Java actions and their
// parameters were not reachable from Starlark at all.
func TestQUAL002_FlagsUndocumentedJavaActionsAndParameters(t *testing.T) {
	vs := runMissingDocRule(t, javaActionFixture(t), nil)

	var messages []string
	for _, v := range vs {
		messages = append(messages, v.Message)
	}
	joined := strings.Join(messages, "\n")

	for _, want := range []string{
		"Java action 'ParseTop' has no documentation.",
		"Java action parameter 'ParseTop.pUri' has no description.",
		"Java action parameter 'ParseTop.pDefault' has no description.",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("QUAL002 did not report %q; got:\n%s", want, joined)
		}
	}
	// The documented action and its documented parameter must stay quiet,
	// otherwise the rule is noise rather than a signal.
	if strings.Contains(joined, "FormatLapTime") {
		t.Errorf("a documented action was flagged:\n%s", joined)
	}
}

// Each target is switchable, so a project that has decided Java actions are
// self-describing can silence just that half without losing the rest.
func TestQUAL002_TargetsAreIndividuallySwitchable(t *testing.T) {
	cat := javaActionFixture(t)

	noParams := runMissingDocRule(t, cat, map[string]any{"check_java_action_params": false})
	for _, v := range noParams {
		if strings.Contains(v.Message, "parameter") {
			t.Errorf("check_java_action_params=false still reported: %s", v.Message)
		}
	}
	if len(noParams) == 0 {
		t.Error("switching off parameters must not switch off the actions too")
	}

	noActions := runMissingDocRule(t, cat, map[string]any{"check_java_actions": false})
	for _, v := range noActions {
		if strings.Contains(v.Message, "Java action '") {
			t.Errorf("check_java_actions=false still reported: %s", v.Message)
		}
	}
}

// A guard against the fixture silently rotting: if the schema loses the
// parameters table, every assertion above would pass by reporting nothing.
func TestJavaActionParametersTableExists(t *testing.T) {
	cat := javaActionFixture(t)
	var n int
	if err := cat.CatalogDB().QueryRow(
		`SELECT COUNT(*) FROM java_action_parameters`).Scan(&n); err != nil && err != sql.ErrNoRows {
		t.Fatalf("java_action_parameters is not queryable: %v", err)
	}
	if n != 3 {
		t.Errorf("java_action_parameters holds %d rows, want 3", n)
	}
}
