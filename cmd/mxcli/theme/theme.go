// SPDX-License-Identifier: Apache-2.0

// Package theme applies mxcli's built-in default styling to a Mendix project.
//
// A theme is a set of files dropped into the project's theme/ folder — no model
// (.mpr) changes at all — so it compiles through Mendix's normal SCSS chain, hot
// applies under `mxcli run --local --watch`, and is removed by deleting a block.
//
// Where the files go was settled against a real Mendix 11.13 project rather than
// assumed, and two placements matter:
//
//   - theme/web/custom-variables.scss is imported by *every* module's theme
//     source (once per module), so it holds declarations only — never rules.
//     Atlas maps these CSS custom properties onto its components and onto the
//     brand-aware pluggable widgets, which is why a token retune re-brands the
//     whole app for free.
//
//   - theme/web/main.scss compiles LAST — after Atlas Core and after every
//     module theme source — so the partial it imports can override any Atlas
//     rule without !important. This is also why a theme must not write to
//     themesource/<name>/: a theme source folder is only compiled when <name>
//     matches a real module in the model, so an invented folder is silently
//     dropped from the build.
package theme

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultName is the theme applied when the user does not choose one.
const DefaultName = "signal"

// NoneName opts out of default styling, leaving Atlas untouched.
const NoneName = "none"

// Variant selects which light/dark behaviour a theme is written with.
type Variant string

const (
	// VariantAuto follows the OS preference and honours a theme-light /
	// theme-dark class on the root element. The default.
	VariantAuto Variant = "auto"
	// VariantLight bakes the light palette with no switching.
	VariantLight Variant = "light"
	// VariantDark bakes the dark palette with no switching.
	VariantDark Variant = "dark"
)

// ParseVariant validates a user-supplied variant name.
func ParseVariant(s string) (Variant, error) {
	switch Variant(s) {
	case VariantAuto, VariantLight, VariantDark:
		return Variant(s), nil
	}
	return "", fmt.Errorf("unknown variant %q (want auto, light or dark)", s)
}

// FileSpec documents one file a theme writes, for `mxcli theme show`.
type FileSpec struct {
	Path    string `json:"path"`
	Mode    string `json:"mode"` // "block" or "verbatim"
	Purpose string `json:"purpose"`
}

// Theme is a named styling package embedded in the mxcli binary.
type Theme struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Version     string   `json:"version"`
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Colorway    []string `json:"colorway"`
	// DefaultVariant is the palette the theme is written around; the other one
	// is what `auto` switches to. Console is dark-first, Signal and Ledger are
	// light-first.
	DefaultVariant Variant    `json:"defaultVariant"`
	Files          []FileSpec `json:"files"`
}

// AltVariant is the variant `auto` switches to — the opposite of the default.
func (t *Theme) AltVariant() Variant {
	if t.DefaultVariant == VariantDark {
		return VariantLight
	}
	return VariantDark
}

// FileResult is what applying one file did.
type FileResult struct {
	Path   string
	Action Action
}

// Result is the outcome of an Apply or Remove.
type Result struct {
	Theme string
	Files []FileResult
}

// Changed reports whether anything on disk actually moved.
func (r *Result) Changed() bool {
	for _, f := range r.Files {
		if f.Action != ActionUnchanged && f.Action != ActionSkipped {
			return true
		}
	}
	return false
}

// Options tunes an Apply or Remove.
type Options struct {
	// Force overwrites blocks that carry local edits.
	Force bool
	// DryRun reports what would change without writing.
	DryRun bool
	// Variant selects light/dark behaviour. Empty means VariantAuto.
	Variant Variant
	// KeepOthers leaves other themes' blocks in place. Off by default: two
	// themes both mapping the Atlas leaves would fight in the cascade, and
	// which one won would depend on import order rather than on intent.
	KeepOthers bool
}

// List returns the embedded themes, ordered by name.
func List() ([]Theme, error) {
	entries, err := fs.ReadDir(assetsFS, assetsRoot)
	if err != nil {
		return nil, fmt.Errorf("reading embedded themes: %w", err)
	}
	var themes []Theme
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t, err := Get(e.Name())
		if err != nil {
			return nil, err
		}
		themes = append(themes, *t)
	}
	sort.Slice(themes, func(i, j int) bool { return themes[i].Name < themes[j].Name })
	return themes, nil
}

// Get returns one embedded theme by name.
func Get(name string) (*Theme, error) {
	raw, err := assetsFS.ReadFile(path.Join(assetsRoot, name, "theme.json"))
	if err != nil {
		return nil, fmt.Errorf("unknown theme %q (run `mxcli theme list`)", name)
	}
	var t Theme
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("theme %q: malformed theme.json: %w", name, err)
	}
	if t.Name != name {
		return nil, fmt.Errorf("theme %q: theme.json declares name %q", name, t.Name)
	}
	return &t, nil
}

// Apply writes a theme's files into projectDir.
//
// projectDir is the folder holding the .mpr — the theme/ tree sits beside it.
func Apply(projectDir, name string, opts Options) (*Result, error) {
	t, err := Get(name)
	if err != nil {
		return nil, err
	}
	if err := requireMendixProject(projectDir); err != nil {
		return nil, err
	}
	if opts.Variant == "" {
		opts.Variant = VariantAuto
	}

	// Only one theme at a time. Both would map the same Atlas leaves, so which
	// palette won would come down to SCSS import order rather than to what the
	// user asked for. Removing first also cleans up the previous theme's fonts.
	if !opts.KeepOthers {
		if err := removeRivalThemes(projectDir, t, opts); err != nil {
			return nil, err
		}
	}

	res := &Result{Theme: name}
	root := path.Join(assetsRoot, name, "files")
	// Edited blocks are collected rather than thrown on the first hit: a user who
	// has hand-tuned two files should see both named once, not discover the second
	// only after dealing with the first.
	var skipped []error

	walkErr := fs.WalkDir(assetsFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, root+"/")
		target := filepath.Join(projectDir, filepath.FromSlash(rel))

		body, err := assetsFS.ReadFile(p)
		if err != nil {
			return err
		}

		var fr FileResult
		if isBlockFile(rel) {
			fr, err = applyBlockFile(target, rel, t, expand(string(body), t, opts), opts)
		} else {
			fr, err = applyVerbatimFile(target, rel, body, opts)
		}
		if err != nil {
			var modified *ErrBlockModified
			if !errors.As(err, &modified) {
				return err
			}
			skipped = append(skipped, err)
		}
		res.Files = append(res.Files, fr)
		return nil
	})
	if walkErr != nil {
		return res, walkErr
	}

	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].Path < res.Files[j].Path })
	return res, errors.Join(skipped...)
}

// Remove cuts a theme's blocks back out and deletes the files it owns outright.
func Remove(projectDir, name string, opts Options) (*Result, error) {
	return remove(projectDir, name, opts, nil)
}

// remove is Remove with a set of project-relative paths that must survive,
// because another theme is about to write them.
func remove(projectDir, name string, opts Options, protect map[string]bool) (*Result, error) {
	t, err := Get(name)
	if err != nil {
		return nil, err
	}

	res := &Result{Theme: name}
	root := path.Join(assetsRoot, name, "files")
	var skipped []error

	walkErr := fs.WalkDir(assetsFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, root+"/")
		target := filepath.Join(projectDir, filepath.FromSlash(rel))

		existing, readErr := os.ReadFile(target)
		if os.IsNotExist(readErr) {
			res.Files = append(res.Files, FileResult{Path: rel, Action: ActionUnchanged})
			return nil
		}
		if readErr != nil {
			return readErr
		}

		if !isBlockFile(rel) {
			// A verbatim file another theme is about to write (every theme ships
			// mxcli-fonts/OFL.txt) stays put; deleting it would make the incoming
			// apply report a change on a project that ends up identical.
			if protect[rel] {
				res.Files = append(res.Files, FileResult{Path: rel, Action: ActionUnchanged})
				return nil
			}
			if !opts.DryRun {
				if err := os.Remove(target); err != nil {
					return err
				}
			}
			res.Files = append(res.Files, FileResult{Path: rel, Action: ActionRemoved})
			return nil
		}

		out, action, err := removeBlock(string(existing), t.Name, opts.Force)
		if err != nil {
			var modified *ErrBlockModified
			if !errors.As(err, &modified) {
				return err
			}
			modified.Path = rel
			skipped = append(skipped, modified)
			res.Files = append(res.Files, FileResult{Path: rel, Action: ActionSkipped})
			return nil
		}
		// A file that is entirely ours is deleted rather than left empty — unless
		// the incoming theme is about to write it, in which case leave the empty
		// shell for its apply to fill.
		if out == "" && protect[rel] {
			res.Files = append(res.Files, FileResult{Path: rel, Action: action})
			return nil
		}
		if out == "" {
			if !opts.DryRun {
				if err := os.Remove(target); err != nil {
					return err
				}
			}
			res.Files = append(res.Files, FileResult{Path: rel, Action: ActionRemoved})
			return nil
		}
		if action != ActionUnchanged && !opts.DryRun {
			if err := os.WriteFile(target, []byte(out), 0o644); err != nil {
				return err
			}
		}
		res.Files = append(res.Files, FileResult{Path: rel, Action: action})
		return nil
	})
	if walkErr != nil {
		return res, walkErr
	}
	pruneEmptyThemeDirs(projectDir, root)

	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].Path < res.Files[j].Path })
	return res, errors.Join(skipped...)
}

// pruneEmptyThemeDirs removes directories the theme introduced once its files
// are gone, so `theme remove` leaves no empty theme/web/mxcli-fonts/ behind.
// os.Remove is the guard: it refuses a directory that still holds anything the
// project put there.
func pruneEmptyThemeDirs(projectDir, root string) {
	var dirs []string
	_ = fs.WalkDir(assetsFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || p == root {
			return err
		}
		dirs = append(dirs, strings.TrimPrefix(p, root+"/"))
		return nil
	})
	// Deepest first, so a nested tree empties from the bottom up.
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, rel := range dirs {
		_ = os.Remove(filepath.Join(projectDir, filepath.FromSlash(rel)))
	}
}

func applyBlockFile(target, rel string, t *Theme, body string, opts Options) (FileResult, error) {
	existing, err := os.ReadFile(target)
	if err != nil && !os.IsNotExist(err) {
		return FileResult{}, err
	}

	out, action, err := applyBlock(string(existing), t.Name, t.Version, body, opts.Force)
	if err != nil {
		var modified *ErrBlockModified
		if errors.As(err, &modified) {
			modified.Path = rel
			return FileResult{Path: rel, Action: ActionSkipped}, modified
		}
		return FileResult{}, err
	}
	if action == ActionUnchanged || opts.DryRun {
		return FileResult{Path: rel, Action: action}, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return FileResult{}, err
	}
	if err := os.WriteFile(target, []byte(out), 0o644); err != nil {
		return FileResult{}, err
	}
	return FileResult{Path: rel, Action: action}, nil
}

func applyVerbatimFile(target, rel string, body []byte, opts Options) (FileResult, error) {
	existing, err := os.ReadFile(target)
	switch {
	case err == nil && string(existing) == string(body):
		return FileResult{Path: rel, Action: ActionUnchanged}, nil
	case err != nil && !os.IsNotExist(err):
		return FileResult{}, err
	}

	action := ActionCreated
	if err == nil {
		action = ActionUpdated
	}
	if opts.DryRun {
		return FileResult{Path: rel, Action: action}, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return FileResult{}, err
	}
	if err := os.WriteFile(target, body, 0o644); err != nil {
		return FileResult{}, err
	}
	return FileResult{Path: rel, Action: action}, nil
}

// expand fills the placeholders a theme asset may carry. Kept deliberately
// tiny — the assets are real SCSS that must stay readable and editable in
// place, so the only things templated are the two values a user chooses at
// apply time.
func expand(body string, t *Theme, opts Options) string {
	r := strings.NewReplacer(
		"{{VARIANT}}", string(opts.Variant),
		"{{THEME}}", t.Name,
	)
	return r.Replace(body)
}

// removeRivalThemes strips every other embedded theme from the project. A
// hand-edited rival block is left alone and reported, same as anywhere else.
//
// Files the incoming theme also ships are protected. Themes share paths —
// every one of them writes theme/web/mxcli-fonts/OFL.txt — so without this the
// rival pass deletes a file that is about to be written again, which shows up
// as an apply that is never idempotent.
func removeRivalThemes(projectDir string, incoming *Theme, opts Options) error {
	all, err := List()
	if err != nil {
		return err
	}
	protect, err := assetPaths(incoming.Name)
	if err != nil {
		return err
	}
	for _, other := range all {
		if other.Name == incoming.Name {
			continue
		}
		if _, err := remove(projectDir, other.Name, opts, protect); err != nil {
			return fmt.Errorf("removing previous theme %q: %w", other.Name, err)
		}
	}
	return nil
}

// assetPaths is the set of project-relative paths a theme writes.
func assetPaths(name string) (map[string]bool, error) {
	root := path.Join(assetsRoot, name, "files")
	out := map[string]bool{}
	err := fs.WalkDir(assetsFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		out[strings.TrimPrefix(p, root+"/")] = true
		return nil
	})
	return out, err
}

// isBlockFile reports whether a file gets the marker treatment. Anything else
// (fonts, licences) is copied byte for byte.
func isBlockFile(rel string) bool {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".scss", ".css", ".js":
		return true
	}
	return false
}

// requireMendixProject refuses to scatter theme files into a directory that is
// not a Mendix project. Applying to the wrong folder is silent otherwise: the
// files land, nothing compiles them, and the app just looks unstyled.
func requireMendixProject(dir string) error {
	if entries, err := filepath.Glob(filepath.Join(dir, "*.mpr")); err == nil && len(entries) > 0 {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, "themesource", "atlas_core")); err == nil {
		return nil
	}
	return fmt.Errorf("%s does not look like a Mendix project (no .mpr and no themesource/atlas_core)", dir)
}
