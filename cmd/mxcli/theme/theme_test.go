// SPDX-License-Identifier: Apache-2.0

package theme

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newProject fakes the parts of a Mendix project a theme touches: the .mpr, the
// three-line theme/web/main.scss Mendix ships, and the stock custom-variables.
func newProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "App.mpr"), "")
	write(t, filepath.Join(dir, "themesource", "atlas_core", "web", "main.scss"), "// atlas\n")
	write(t, filepath.Join(dir, "theme", "web", "main.scss"),
		"@import \"custom-variables\";\n@import \"theme-dark\";\n@import \"theme-neutral\";\n")
	write(t, filepath.Join(dir, "theme", "web", "custom-variables.scss"),
		"$brand-logo: false;\n:root {\n  --brand-primary: #264ae5;\n}\n")
	return dir
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDefaultThemeIsEmbeddedAndWellFormed(t *testing.T) {
	themes, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(themes) == 0 {
		t.Fatal("no themes embedded")
	}
	def, err := Get(DefaultName)
	if err != nil {
		t.Fatalf("the default theme must be embedded: %v", err)
	}
	if def.Title == "" || def.Version == "" || def.Summary == "" {
		t.Errorf("theme.json is missing display fields: %+v", def)
	}
	if len(def.Colorway) == 0 {
		t.Error("colorway is empty; the chart theme in P2 reads it")
	}
}

func TestApply_WritesTheThreeLayersAndTheFonts(t *testing.T) {
	dir := newProject(t)

	res, err := Apply(dir, DefaultName, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed() {
		t.Fatal("apply reported no changes")
	}

	// Layer 1: the palette lands in the file every module imports. It declares
	// the theme's own tokens; the Atlas variables are mapped from them one file
	// down, which is what lets a variant restate ~30 values instead of ~60.
	vars := read(t, filepath.Join(dir, "theme", "web", "custom-variables.scss"))
	if !strings.Contains(vars, "--mxt-brand: #0f6e6b") {
		t.Error("brand token not written")
	}
	atlasMap := read(t, filepath.Join(dir, "theme", "web", "_mxcli-atlas-map.scss"))
	if !strings.Contains(atlasMap, "--brand-primary: var(--mxt-brand)") {
		t.Error("Atlas wiring not written")
	}
	if !strings.Contains(vars, "$brand-logo: false;") {
		t.Error("Mendix's own content was dropped from custom-variables.scss")
	}

	// Layer 2: the partial, plus the one-line import from the file that
	// compiles last. Without the import the partial is dead weight.
	if _, err := os.Stat(filepath.Join(dir, "theme", "web", "_mxcli-signal.scss")); err != nil {
		t.Errorf("Layer 2 partial not written: %v", err)
	}
	main := read(t, filepath.Join(dir, "theme", "web", "main.scss"))
	if !strings.Contains(main, `@import "mxcli-signal"`) {
		t.Error("partial is not imported from theme/web/main.scss")
	}
	if !strings.Contains(main, `@import "theme-dark"`) {
		t.Error("Mendix's own imports were dropped from main.scss")
	}

	// Fonts are vendored, so the app renders correctly with no network.
	fonts, err := filepath.Glob(filepath.Join(dir, "theme", "web", "mxcli-fonts", "*.woff2"))
	if err != nil || len(fonts) == 0 {
		t.Errorf("no fonts vendored: %v", err)
	}
}

// Every url() the theme emits must resolve to a file the theme also ships.
// A typo here compiles clean and only shows up as a silent fallback to
// system-ui in the browser.
func TestApply_EveryFontURLResolvesToAVendoredFile(t *testing.T) {
	dir := newProject(t)
	if _, err := Apply(dir, DefaultName, Options{}); err != nil {
		t.Fatal(err)
	}

	partial := read(t, filepath.Join(dir, "theme", "web", "_mxcli-signal.scss"))
	weights := map[string][]string{
		"sans": {"400", "500", "600", "700"},
		"mono": {"400", "500", "600"},
	}
	for family, ws := range weights {
		for _, w := range ws {
			name := "ibm-plex-" + family + "-latin-" + w + "-normal.woff2"
			if _, err := os.Stat(filepath.Join(dir, "theme", "web", "mxcli-fonts", name)); err != nil {
				t.Errorf("%s referenced by the @font-face loop but not vendored", name)
			}
		}
	}
	if !strings.Contains(partial, `url("./mxcli-fonts/`) {
		t.Error("font URLs must be relative to theme.compiled.css at the web root")
	}
}

func TestApply_IsIdempotent(t *testing.T) {
	dir := newProject(t)
	if _, err := Apply(dir, DefaultName, Options{}); err != nil {
		t.Fatal(err)
	}
	before := read(t, filepath.Join(dir, "theme", "web", "custom-variables.scss"))

	res, err := Apply(dir, DefaultName, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed() {
		t.Errorf("second apply reported changes: %+v", res.Files)
	}
	if after := read(t, filepath.Join(dir, "theme", "web", "custom-variables.scss")); after != before {
		t.Error("second apply rewrote the file")
	}
}

func TestApply_RefusesWhenTheUserHasEditedTheBlock(t *testing.T) {
	dir := newProject(t)
	if _, err := Apply(dir, DefaultName, Options{}); err != nil {
		t.Fatal(err)
	}

	varsPath := filepath.Join(dir, "theme", "web", "custom-variables.scss")
	edited := strings.Replace(read(t, varsPath), "#0f6e6b", "#ff6b35", 1)
	write(t, varsPath, edited)

	_, err := Apply(dir, DefaultName, Options{})
	var modified *ErrBlockModified
	if !errors.As(err, &modified) {
		t.Fatalf("err = %v, want ErrBlockModified", err)
	}
	if modified.Path == "" {
		t.Error("the error must name the file so the user can find it")
	}
	if !strings.Contains(read(t, varsPath), "#ff6b35") {
		t.Fatal("the user's re-brand was overwritten")
	}
}

func TestApply_DryRunWritesNothing(t *testing.T) {
	dir := newProject(t)
	before := read(t, filepath.Join(dir, "theme", "web", "main.scss"))

	res, err := Apply(dir, DefaultName, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed() {
		t.Error("dry run should still report the changes it would make")
	}
	if read(t, filepath.Join(dir, "theme", "web", "main.scss")) != before {
		t.Error("dry run modified main.scss")
	}
	if _, err := os.Stat(filepath.Join(dir, "theme", "web", "_mxcli-signal.scss")); !os.IsNotExist(err) {
		t.Error("dry run created the partial")
	}
}

func TestRemove_LeavesTheProjectAsItWas(t *testing.T) {
	dir := newProject(t)
	mainBefore := read(t, filepath.Join(dir, "theme", "web", "main.scss"))
	varsBefore := read(t, filepath.Join(dir, "theme", "web", "custom-variables.scss"))

	if _, err := Apply(dir, DefaultName, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(dir, DefaultName, Options{}); err != nil {
		t.Fatal(err)
	}

	if got := read(t, filepath.Join(dir, "theme", "web", "main.scss")); got != mainBefore {
		t.Errorf("main.scss not restored:\nwant %q\ngot  %q", mainBefore, got)
	}
	if got := read(t, filepath.Join(dir, "theme", "web", "custom-variables.scss")); got != varsBefore {
		t.Errorf("custom-variables.scss not restored:\nwant %q\ngot  %q", varsBefore, got)
	}
	if _, err := os.Stat(filepath.Join(dir, "theme", "web", "_mxcli-signal.scss")); !os.IsNotExist(err) {
		t.Error("the partial should be deleted, not left empty")
	}
	if fonts, _ := filepath.Glob(filepath.Join(dir, "theme", "web", "mxcli-fonts", "*.woff2")); len(fonts) > 0 {
		t.Error("vendored fonts should be removed with the theme")
	}
	if _, err := os.Stat(filepath.Join(dir, "theme", "web", "mxcli-fonts")); !os.IsNotExist(err) {
		t.Error("the font directory should be pruned, not left empty")
	}
}

// A directory the theme owns must not be pruned when the user has put something
// of their own in it.
func TestRemove_KeepsADirectoryHoldingUserFiles(t *testing.T) {
	dir := newProject(t)
	if _, err := Apply(dir, DefaultName, Options{}); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(dir, "theme", "web", "mxcli-fonts", "my-brand-font.woff2")
	write(t, mine, "not really a font")

	if _, err := Remove(dir, DefaultName, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mine); err != nil {
		t.Errorf("user's own file in a theme directory was deleted: %v", err)
	}
}

func TestApply_RefusesADirectoryThatIsNotAMendixProject(t *testing.T) {
	if _, err := Apply(t.TempDir(), DefaultName, Options{}); err == nil {
		t.Fatal("expected a refusal for a non-project directory")
	}
}

func TestGet_UnknownThemeNamesTheDiscoveryCommand(t *testing.T) {
	_, err := Get("nope")
	if err == nil || !strings.Contains(err.Error(), "mxcli theme list") {
		t.Errorf("err = %v; should point at `mxcli theme list`", err)
	}
}

// The Layer-1 file is imported once per module, so a CSS rule there is emitted
// once per module too. Declarations only.
func TestLayer1BlockContainsNoRules(t *testing.T) {
	body, err := assetsFS.ReadFile("assets/signal/files/theme/web/custom-variables.scss")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, "{") && !strings.HasPrefix(trimmed, ":root") {
			t.Errorf("custom-variables.scss must hold declarations only, found selector: %q", trimmed)
		}
	}
}

// ---------------------------------------------------------------------------
// Variants and the multi-theme registry
// ---------------------------------------------------------------------------

func TestAllThemesAreWellFormed(t *testing.T) {
	themes, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(themes) < 3 {
		t.Fatalf("expected signal, ledger and console; got %d", len(themes))
	}
	for _, th := range themes {
		if th.DefaultVariant != VariantLight && th.DefaultVariant != VariantDark {
			t.Errorf("%s: defaultVariant %q is neither light nor dark", th.Name, th.DefaultVariant)
		}
		if th.AltVariant() == th.DefaultVariant {
			t.Errorf("%s: alt variant equals the default", th.Name)
		}
		if len(th.Colorway) == 0 || th.Summary == "" || th.Title == "" {
			t.Errorf("%s: incomplete theme.json: %+v", th.Name, th)
		}
	}
}

// The Atlas wiring is what makes a palette swap cheap, so every theme has to
// run through the same one. Shipped per theme (a theme package is meant to be
// self-contained), which is exactly why it can drift.
func TestAtlasMapIsIdenticalInEveryTheme(t *testing.T) {
	themes, err := List()
	if err != nil {
		t.Fatal(err)
	}
	var reference []byte
	var referenceName string
	for _, th := range themes {
		body, err := assetsFS.ReadFile("assets/" + th.Name + "/files/theme/web/_mxcli-atlas-map.scss")
		if err != nil {
			t.Fatalf("%s ships no Atlas map: %v", th.Name, err)
		}
		if reference == nil {
			reference, referenceName = body, th.Name
			continue
		}
		if string(body) != string(reference) {
			t.Errorf("%s's Atlas map has drifted from %s's", th.Name, referenceName)
		}
	}
}

// A palette that pins Atlas leaves to literal colours cannot survive a variant
// flip: the ink stays near-black on a near-black ground. Every theme must go
// through --mxt-* instead, which is what the Atlas map exists for.
func TestPalettesDeclareOnlyThemeTokens(t *testing.T) {
	themes, err := List()
	if err != nil {
		t.Fatal(err)
	}
	for _, th := range themes {
		body, err := assetsFS.ReadFile("assets/" + th.Name + "/files/theme/web/custom-variables.scss")
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(body), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "--") {
				continue
			}
			if !strings.HasPrefix(trimmed, "--mxt-") {
				t.Errorf("%s custom-variables.scss:%d declares an Atlas variable directly: %q",
					th.Name, i+1, trimmed)
			}
		}
	}
}

func TestApplyVariant_AutoShipsBothPalettes(t *testing.T) {
	dir := newProject(t)
	if _, err := Apply(dir, DefaultName, Options{Variant: VariantAuto}); err != nil {
		t.Fatal(err)
	}
	main := read(t, filepath.Join(dir, "theme", "web", "main.scss"))
	if !strings.Contains(main, "$mxcli-theme-variant: auto;") {
		t.Errorf("variant not written into main.scss:\n%s", main)
	}
	partial := read(t, filepath.Join(dir, "theme", "web", "_mxcli-signal.scss"))
	if !strings.Contains(partial, "prefers-color-scheme") {
		t.Error("auto must follow the OS")
	}
	// The explicit-class path has to outrank Mendix's own _theme-dark.scss,
	// which also declares :root.theme-dark.
	if !strings.Contains(partial, ":root.theme-dark") {
		t.Error("auto must honour an explicit theme-dark class")
	}
}

func TestApplyVariant_PinnedIsWrittenThrough(t *testing.T) {
	for _, v := range []Variant{VariantLight, VariantDark} {
		dir := newProject(t)
		if _, err := Apply(dir, DefaultName, Options{Variant: v}); err != nil {
			t.Fatal(err)
		}
		main := read(t, filepath.Join(dir, "theme", "web", "main.scss"))
		if !strings.Contains(main, "$mxcli-theme-variant: "+string(v)+";") {
			t.Errorf("variant %q not written into main.scss:\n%s", v, main)
		}
		if strings.Contains(main, "{{VARIANT}}") {
			t.Errorf("variant placeholder left unexpanded for %q", v)
		}
	}
}

func TestParseVariant_RejectsNonsense(t *testing.T) {
	if _, err := ParseVariant("sepia"); err == nil {
		t.Fatal("expected an error for an unknown variant")
	}
	for _, ok := range []string{"auto", "light", "dark"} {
		if _, err := ParseVariant(ok); err != nil {
			t.Errorf("ParseVariant(%q) = %v", ok, err)
		}
	}
}

// Two themes at once would both map the Atlas leaves, and which palette won
// would come down to SCSS import order rather than to what was asked for.
func TestApply_RemovesThePreviousTheme(t *testing.T) {
	dir := newProject(t)
	if _, err := Apply(dir, "ledger", Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "theme", "web", "_mxcli-ledger.scss")); err != nil {
		t.Fatalf("ledger not applied: %v", err)
	}

	if _, err := Apply(dir, "signal", Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "theme", "web", "_mxcli-ledger.scss")); !os.IsNotExist(err) {
		t.Error("ledger's partial survived a switch to signal")
	}
	main := read(t, filepath.Join(dir, "theme", "web", "main.scss"))
	if strings.Contains(main, "mxcli-ledger") {
		t.Errorf("ledger is still imported after switching to signal:\n%s", main)
	}
	if !strings.Contains(main, "mxcli-signal") {
		t.Error("signal is not imported")
	}
	// Ledger's fonts must go too, or the binary's payload accumulates in the
	// project every time someone tries a theme.
	if got, _ := filepath.Glob(filepath.Join(dir, "theme", "web", "mxcli-fonts", "source-*.woff2")); len(got) > 0 {
		t.Errorf("ledger's fonts survived the switch: %v", got)
	}
}

func TestSwitcherMDL_TargetsTheRequestedModule(t *testing.T) {
	mdl := SwitcherMDL("Ops")
	if strings.Contains(mdl, "{{MODULE}}") {
		t.Error("module placeholder left unexpanded")
	}
	for _, want := range []string{
		"create or modify javascript action Ops.ToggleAppTheme",
		"create or modify javascript action Ops.SetAppTheme",
		"create or modify javascript action Ops.ApplyStoredTheme",
		"create or replace nanoflow Ops.ACT_ToggleTheme",
		SwitcherStorageKey,
	} {
		if !strings.Contains(mdl, want) {
			t.Errorf("switcher MDL is missing %q", want)
		}
	}
	// The class has to land on the root element: popups and modals render at
	// <body>, outside any page container, and must follow the theme too.
	if !strings.Contains(mdl, "document.documentElement") {
		t.Error("the theme class must be set on the root element")
	}
}
